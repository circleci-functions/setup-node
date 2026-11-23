package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/circleci-functions/toolkit/pathlink"
)

// EnsureBinary makes sure pm's executable is on PATH before anything runs it,
// so a missing package manager fails with an error that says how to get it,
// rather than with whatever the first command to run it reports. It is for a
// package manager package.json doesn't pin; a pinned one is installed with
// Install instead.
func (pm PackageManager) EnsureBinary() error {
	if _, err := exec.LookPath(pm.Binary()); err == nil {
		return nil
	}
	if pm == NPM {
		return fmt.Errorf("npm binary not found on PATH; npm comes with Node.js, which setup-node installs when run without a command")
	}
	return fmt.Errorf("%[1]s binary not found on PATH; install it in an earlier step with `npm install --global %[1]s@<version>`, "+
		"or pin it in package.json with \"packageManager\": \"%[1]s@<version>\" for setup-node to install it", pm)
}

// PinnedVersion returns the version of pm that appDir's package.json pins with
// its packageManager field, or "" if it pins none, pins another package
// manager, or pm is npm, which comes with Node.js. corepack's "+sha..."
// integrity suffix is dropped, since npm has no way to check it.
func PinnedVersion(appDir string, pm PackageManager) string {
	if pm == NPM {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(appDir, "package.json")) //nolint:gosec // package.json in the directory the caller named
	if err != nil {
		return ""
	}
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	name, version, ok := strings.Cut(manifest.PackageManager, "@")
	if !ok || name != string(pm) {
		return ""
	}
	version, _, _ = strings.Cut(version, "+")
	return version
}

// PathVersion returns the version of the pm PATH resolves, or "" if there is
// none or it can't say. It asks from the temp directory rather than the
// project, where pnpm would first download whatever version packageManager
// pins and report that instead.
func (pm PackageManager) PathVersion(ctx context.Context) string {
	if _, err := exec.LookPath(pm.Binary()); err != nil {
		return ""
	}
	v, err := pm.Version(ctx, os.TempDir())
	if err != nil {
		return ""
	}
	return v
}

// Installed reports whether root holds an install of pm made by Install.
func (pm PackageManager) Installed(root string) bool {
	_, err := os.Stat(pm.installedBinary(root))
	return err == nil
}

// Install installs version of pm under root with `npm install --global
// --prefix`, so it lands in a directory of its own that can be cached and
// linked onto PATH. npm runs from appDir, so the project's .npmrc picks the
// registry. corepack is not used: Node.js 25 and later don't ship it, and the
// corepack in older releases can fail to verify downloads since npm rotated
// its signing keys.
func (pm PackageManager) Install(ctx context.Context, appDir, root, version string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm binary not found on PATH, so %s@%s can't be installed; npm comes with Node.js, which setup-node installs when run without a command", pm, version)
	}
	// A failed install can leave a partial tree behind; start from nothing.
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear %s: %w", root, err)
	}
	pkg := pm.npmPackage(version)
	fmt.Fprintf(os.Stderr, "setup-node: installing %s, the version package.json's packageManager pins\n", pkg)
	cmd := exec.CommandContext(ctx, "npm", "install", "--global", "--prefix", root, "--no-audit", "--no-fund", pkg)
	cmd.Dir = appDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install --global %s: %w", pkg, err)
	}
	if !pm.Installed(root) {
		return fmt.Errorf("installed %s with npm, but it left no %s in %s", pkg, pm, root)
	}
	return nil
}

// Link makes the install of version of pm at root the one PATH resolves, and
// returns the directory it linked into. binDir, when set, links into exactly
// that directory instead of discovering one on PATH.
func (pm PackageManager) Link(root, version, binDir string) (string, error) {
	if runtime.GOOS == "windows" {
		return pm.linkWindows(root, version, binDir)
	}
	from := filepath.Join(root, "bin")
	res, err := pathlink.Ensure(pathlink.Options{
		Names:     pm.binNames(from),
		FromDir:   from,
		Preferred: pathlink.UserBinDirs(),
		Dir:       binDir,
		Verify:    verifyVersion(filepath.Join(from, pm.Binary()), version),
	})
	if err != nil {
		return "", err
	}
	return res.Dir, nil
}

// binNames returns the executables npm linked into from, with pm's own first,
// so pnpx, yarnpkg or bunx come from the same install as pm.
func (pm PackageManager) binNames(from string) []string {
	names := []string{pm.Binary()}
	entries, _ := os.ReadDir(from)
	for _, e := range entries {
		if e.Name() != pm.Binary() {
			names = append(names, e.Name())
		}
	}
	return names
}

// linkWindows writes shims for pm into the directory pathlink would link into.
// pathlink links .exe files, and npm installs a package manager on Windows as
// .cmd, .ps1 and extensionless scripts that find their package relative to
// themselves, so each shim calls npm's by absolute path.
func (pm PackageManager) linkWindows(root, version, binDir string) (string, error) {
	plan, err := pathlink.Check(pathlink.Options{Names: []string{pm.Binary()}, Dir: binDir})
	if err != nil {
		return "", err
	}
	if plan.Elevate {
		return "", fmt.Errorf("putting %s on PATH needs %s, which this user can't write to", pm, plan.Dir)
	}
	target := filepath.Join(root, pm.Binary())
	shims := map[string]string{
		pm.Binary() + ".cmd": fmt.Sprintf("@echo off\r\n\"%s.cmd\" %%*\r\n", target),
		pm.Binary() + ".ps1": fmt.Sprintf("& \"%s.cmd\" @args\r\nexit $LASTEXITCODE\r\n", target),
		pm.Binary():          fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"$@\"\n", filepath.ToSlash(target)),
	}
	if err := os.MkdirAll(plan.Dir, 0755); err != nil { //nolint:gosec // a directory on PATH is readable by design
		return "", err
	}
	for name, content := range shims {
		if err := os.WriteFile(filepath.Join(plan.Dir, name), []byte(content), 0755); err != nil { //nolint:gosec // must be executable
			return "", err
		}
	}
	if binDir == "" {
		bin, err := exec.LookPath(pm.Binary())
		if err != nil {
			return "", fmt.Errorf("linked %s into %s, but PATH doesn't resolve it: %w", pm, plan.Dir, err)
		}
		if err := verifyVersion("", version)(bin); err != nil {
			return "", fmt.Errorf("linked %s into %s, but %s %w", pm, plan.Dir, bin, err)
		}
	}
	return plan.Dir, nil
}

// installedBinary returns where Install leaves pm's executable under root:
// <root>/bin on Unix, and a .cmd script in root itself on Windows.
func (pm PackageManager) installedBinary(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, pm.Binary()+".cmd")
	}
	return filepath.Join(root, "bin", pm.Binary())
}

// verifyVersion accepts only a binary reporting version and, when want is
// set, that is the same file as want, so a preinstalled package manager that
// happens to answer the same does not pass for the one installed. It asks from
// the temp directory, as PathVersion does, so the project can't answer
// instead: pnpm switches to the version packageManager pins, and Yarn 2 and
// later hand over to the release .yarnrc.yml names.
func verifyVersion(want, version string) func(string) error {
	return func(bin string) error {
		cmd := pathlink.Command(bin, "--version")
		cmd.Dir = os.TempDir()
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				return fmt.Errorf("failed to run: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
			}
			return fmt.Errorf("failed to run: %w", err)
		}
		fields := strings.Fields(string(out))
		if len(fields) == 0 || fields[len(fields)-1] != version {
			return fmt.Errorf("reports %q rather than %s", strings.TrimSpace(string(out)), version)
		}
		if want != "" && !sameFile(bin, want) {
			return fmt.Errorf("is not %s", want)
		}
		return nil
	}
}

func sameFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// npmPackage returns the npm package spec that installs version of pm. The
// yarn package on npm stops at Yarn 1; later releases are published as
// @yarnpkg/cli-dist.
func (pm PackageManager) npmPackage(version string) string {
	if major, _, _ := strings.Cut(version, "."); pm == Yarn && major != "1" {
		return "@yarnpkg/cli-dist@" + version
	}
	return string(pm) + "@" + version
}
