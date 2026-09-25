// Package nodepath makes an unpacked Node.js install usable by name for the
// rest of a CircleCI job.
package nodepath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
)

// tools are linked alongside node when the install ships them. corepack is
// absent from Node.js 25 onwards.
var tools = []string{"npm", "npx", "corepack"}

// windowsScripts are the entry points, relative to a Windows install's root,
// that the .cmd, .ps1 and extensionless scripts shipped for tools run.
var windowsScripts = map[string]string{
	"npm":      filepath.Join("node_modules", "npm", "bin", "npm-cli.js"),
	"npx":      filepath.Join("node_modules", "npm", "bin", "npx-cli.js"),
	"corepack": filepath.Join("node_modules", "corepack", "dist", "corepack.js"),
}

// Options configures Link.
type Options struct {
	// Root is the root of the Node.js install.
	Root string
	// Version is what node --version must report, e.g. "v22.11.0".
	Version string
	// BinDir links into exactly this directory, skipping PATH discovery.
	BinDir string
	// Path is the search path to use. Empty means this process's PATH.
	Path string
}

// BinDir returns the directory of an install at root holding node and its
// tools: bin on Unix, the root itself on Windows.
func BinDir(root string) string {
	if runtime.GOOS == "windows" {
		return root
	}
	return filepath.Join(root, "bin")
}

// Link makes node, npm, npx and, when present, corepack resolvable by name
// from a directory already on PATH, and returns that directory.
func Link(opts Options) (string, error) {
	from := BinDir(opts.Root)
	names := []string{"node"}
	var shims []string
	for _, tool := range tools {
		switch {
		case runtime.GOOS == "windows" && exists(filepath.Join(from, windowsScripts[tool])):
			shims = append(shims, tool)
		case runtime.GOOS != "windows" && exists(filepath.Join(from, tool)):
			names = append(names, tool)
		}
	}

	res, err := pathlink.Ensure(pathlink.Options{
		Names:     names,
		FromDir:   from,
		Preferred: pathlink.UserBinDirs(),
		Dir:       opts.BinDir,
		Path:      opts.Path,
		Verify:    verify(from, opts.Version),
	})
	if err != nil {
		return "", err
	}

	for _, tool := range shims {
		if err := writeShims(res.Dir, from, tool); err != nil {
			return "", err
		}
	}
	return res.Dir, nil
}

// verify accepts only the node from this install reporting version, so a
// preinstalled node that happens to answer the same does not pass for it. On
// Windows the links are shims that call node.exe by absolute path, so there
// only the version can identify them.
func verify(from, version string) func(string) error {
	want := filepath.Join(from, "node")
	return func(bin string) error {
		out, err := pathlink.Command(bin, "--version").Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				return fmt.Errorf("failed to run: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
			}
			return fmt.Errorf("failed to run: %w", err)
		}
		if got := strings.TrimSpace(string(out)); got != version {
			return fmt.Errorf("reports %s rather than %s", got, version)
		}
		if runtime.GOOS != "windows" && !sameFile(bin, want) {
			return fmt.Errorf("is not %s", want)
		}
		return nil
	}
}

// writeShims makes a tool from a Windows install resolvable from dir, in cmd,
// PowerShell and Git Bash. pathlink links executables only, and npm, npx and
// corepack ship as scripts.
//
// The shims run node.exe on the tool's entry point rather than the scripts
// the install ships, which hand over to whatever npm is installed under the
// global prefix. Where dir belongs to a preinstalled Node.js, that is the
// preinstalled npm.
func writeShims(dir, root, tool string) error {
	node, script := filepath.Join(root, "node.exe"), filepath.Join(root, windowsScripts[tool])
	shims := map[string]string{
		tool + ".cmd": fmt.Sprintf("@echo off\r\n\"%s\" \"%s\" %%*\r\n", node, script),
		tool + ".ps1": fmt.Sprintf("& \"%s\" \"%s\" @args\r\nexit $LASTEXITCODE\r\n", node, script),
		tool:          fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"%s\" \"$@\"\n", filepath.ToSlash(node), filepath.ToSlash(script)),
	}
	for name, content := range shims {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0755); err != nil { //nolint:gosec // must be executable
			return err
		}
	}
	return nil
}

// SetGlobalPrefix points npm's global prefix for the install at root at a
// directory on PATH, so the executables of packages installed with npm
// install -g resolve by name like node itself. By default npm puts them beside
// node inside the install, which is not on PATH. It returns the prefix it set.
//
// The prefix goes in the install's builtin npmrc, the lowest-priority npm
// config, so a prefix set in ~/.npmrc or $npm_config_prefix still wins.
func SetGlobalPrefix(root, linkedDir string) (string, error) {
	prefix, err := globalPrefix(runtime.GOOS, linkedDir)
	if err != nil {
		return "", err
	}

	binDir, libDir := filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib")
	npmrc := filepath.Join(root, "lib", "node_modules", "npm", "npmrc")
	if runtime.GOOS == "windows" {
		binDir, libDir = prefix, filepath.Join(prefix, "node_modules")
		npmrc = filepath.Join(root, "node_modules", "npm", "npmrc")
	}
	if !pathlink.Writable(binDir) || !pathlink.Writable(libDir) {
		return prefix, fmt.Errorf("npm global prefix %s is not writable without elevation", prefix)
	}
	if err := os.WriteFile(npmrc, []byte("prefix="+filepath.ToSlash(prefix)+"\n"), 0644); err != nil { //nolint:gosec // npm config is not secret
		return prefix, fmt.Errorf("write %s: %w", npmrc, err)
	}
	return prefix, nil
}

// globalPrefix returns the prefix under which npm's global executables land
// in linkedDir: its parent on Unix, where npm puts them in <prefix>/bin, and
// linkedDir itself on Windows, where npm puts them in <prefix>.
func globalPrefix(goos, linkedDir string) (string, error) {
	if goos == "windows" {
		return linkedDir, nil
	}
	if filepath.Base(linkedDir) != "bin" {
		return "", fmt.Errorf("npm puts global executables in <prefix>/bin, which %s cannot be", linkedDir)
	}
	return filepath.Dir(linkedDir), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sameFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
