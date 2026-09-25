package nodepath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// fakeInstall writes a Unix Node.js install whose node reports v22.1.0.
func fakeInstall(t *testing.T, tools ...string) string {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake install's node is a shell script")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	assert.NilError(t, os.MkdirAll(bin, 0750))
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho v22.1.0\n"), 0700)) //nolint:gosec // must be executable
	for _, tool := range tools {
		assert.NilError(t, os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\n"), 0700)) //nolint:gosec // must be executable
	}
	return root
}

func TestLink_DiscoversDirOnPath(t *testing.T) {
	tests := []struct {
		name  string
		tools []string
	}{
		{name: "with corepack", tools: []string{"npm", "npx", "corepack"}},
		{name: "without corepack", tools: []string{"npm", "npx"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fakeInstall(t, tc.tools...)
			onPath := t.TempDir()

			dir, err := Link(Options{Root: root, Version: "v22.1.0", Path: onPath})
			assert.NilError(t, err)
			assert.Equal(t, dir, onPath)
			for _, name := range append([]string{"node"}, tc.tools...) {
				target, err := os.Readlink(filepath.Join(onPath, name))
				assert.NilError(t, err)
				assert.Equal(t, target, filepath.Join(root, "bin", name))
			}
			if len(tc.tools) == 2 {
				_, err := os.Lstat(filepath.Join(onPath, "corepack"))
				assert.Assert(t, os.IsNotExist(err))
			}
		})
	}
}

func TestLink_BinDir(t *testing.T) {
	root := fakeInstall(t, "npm", "npx")
	binDir := t.TempDir()

	dir, err := Link(Options{Root: root, Version: "v22.1.0", BinDir: binDir})
	assert.NilError(t, err)
	assert.Equal(t, dir, binDir)
	_, err = os.Stat(filepath.Join(binDir, "npm"))
	assert.NilError(t, err)
}

func TestVerify(t *testing.T) {
	root := fakeInstall(t)
	other := fakeInstall(t)
	broken := fakeInstall(t)
	assert.NilError(t, os.WriteFile(filepath.Join(broken, "bin", "node"), []byte("#!/bin/sh\necho 'GLIBC_2.28 not found' >&2\nexit 1\n"), 0700)) //nolint:gosec // must be executable

	link := filepath.Join(t.TempDir(), "node")
	assert.NilError(t, os.Symlink(filepath.Join(root, "bin", "node"), link))

	tests := []struct {
		name    string
		from    string
		version string
		bin     string
		wantErr string
	}{
		{name: "our node through a link", from: filepath.Join(root, "bin"), version: "v22.1.0", bin: link},
		{name: "wrong version", from: filepath.Join(root, "bin"), version: "v24.0.0", bin: link, wantErr: "reports v22.1.0 rather than v24.0.0"},
		{name: "same version elsewhere", from: filepath.Join(other, "bin"), version: "v22.1.0", bin: link, wantErr: "is not " + filepath.Join(other, "bin", "node")},
		{name: "fails to run", from: filepath.Join(broken, "bin"), version: "v22.1.0", bin: filepath.Join(broken, "bin", "node"), wantErr: "GLIBC_2.28 not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verify(tc.from, tc.version)(tc.bin)
			if tc.wantErr == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestWriteShims(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	node, script := filepath.Join(root, "node.exe"), filepath.Join(root, "node_modules", "npm", "bin", "npm-cli.js")

	assert.NilError(t, writeShims(dir, root, "npm"))

	tests := []struct {
		file string
		want string
	}{
		{file: "npm.cmd", want: "@echo off\r\n\"" + node + "\" \"" + script + "\" %*\r\n"},
		{file: "npm.ps1", want: "& \"" + node + "\" \"" + script + "\" @args\r\nexit $LASTEXITCODE\r\n"},
		{file: "npm", want: "#!/bin/sh\nexec \"" + filepath.ToSlash(node) + "\" \"" + filepath.ToSlash(script) + "\" \"$@\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got, err := os.ReadFile(filepath.Join(dir, tc.file))
			assert.NilError(t, err)
			assert.Equal(t, string(got), tc.want)
		})
	}
}

func TestSetGlobalPrefix(t *testing.T) {
	root := fakeInstall(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(root, "lib", "node_modules", "npm"), 0750))
	home := t.TempDir()
	linked := filepath.Join(home, ".local", "bin")
	assert.NilError(t, os.MkdirAll(linked, 0750))

	prefix, err := SetGlobalPrefix(root, linked)
	assert.NilError(t, err)
	assert.Equal(t, prefix, filepath.Join(home, ".local"))
	npmrc, err := os.ReadFile(filepath.Join(root, "lib", "node_modules", "npm", "npmrc"))
	assert.NilError(t, err)
	assert.Equal(t, string(npmrc), "prefix="+filepath.Join(home, ".local")+"\n")
}

func TestSetGlobalPrefix_NotWritable(t *testing.T) {
	skip.If(t, os.Geteuid() == 0, "root can write anywhere")
	root := fakeInstall(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(root, "lib", "node_modules", "npm"), 0750))
	parent := t.TempDir()
	linked := filepath.Join(parent, "bin")
	assert.NilError(t, os.MkdirAll(linked, 0750))
	assert.NilError(t, os.Chmod(parent, 0500))       //nolint:gosec // read-only on purpose
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) }) //nolint:gosec // restore so TempDir cleanup works

	_, err := SetGlobalPrefix(root, linked)
	assert.ErrorContains(t, err, "is not writable without elevation")
	_, statErr := os.Stat(filepath.Join(root, "lib", "node_modules", "npm", "npmrc"))
	assert.Assert(t, os.IsNotExist(statErr))
}

func TestSetGlobalPrefix_LinkedDirNotNamedBin(t *testing.T) {
	root := fakeInstall(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(root, "lib", "node_modules", "npm"), 0750))

	_, err := SetGlobalPrefix(root, t.TempDir())
	assert.ErrorContains(t, err, "npm puts global executables in <prefix>/bin")
}

func TestGlobalPrefix(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		linkedDir string
		want      string
		wantErr   string
	}{
		{name: "unix bin", goos: "linux", linkedDir: "/home/u/.local/bin", want: "/home/u/.local"},
		{name: "unix not bin", goos: "darwin", linkedDir: "/opt/tools", wantErr: "npm puts global executables in <prefix>/bin"},
		{name: "windows", goos: "windows", linkedDir: "/c/Program Files/nodejs", want: "/c/Program Files/nodejs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := globalPrefix(tc.goos, tc.linkedDir)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}
