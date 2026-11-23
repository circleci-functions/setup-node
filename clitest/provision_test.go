package clitest_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// provisionEnv is the PATH and npm a pinned package manager is installed
// with. The npm stub records its arguments to npmArgs and, when asked to
// install a package under a --prefix, puts a stub of that package manager in
// the prefix's bin directory the way a real install would. The stub answers
// --version with the version it was installed at.
type provisionEnv struct {
	path    string
	bin     string
	npmArgs string
}

func newProvisionEnv(t *testing.T) provisionEnv {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the npm and package manager stubs are shell scripts")
	bin := t.TempDir()
	npmArgs := filepath.Join(t.TempDir(), "npm-args")
	store := t.TempDir()
	// $4 is the --prefix; the package spec is the last argument.
	npm := "#!/bin/sh\n" +
		"echo \"$@\" >> '" + npmArgs + "'\n" +
		"[ \"$1 $2 $3\" = 'install --global --prefix' ] || exit 0\n" +
		"for spec; do :; done\n" +
		"name=${spec%@*}; version=${spec##*@}\n" +
		"mkdir -p \"$4/bin\"\n" +
		"printf '#!/bin/sh\\ncase \"$1\" in\\nstore) echo %s ;;\\n--version) echo %s ;;\\nesac\\nexit 0\\n' '" + store + "' \"$version\" > \"$4/bin/$name\"\n" +
		"chmod 700 \"$4/bin/$name\"\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "npm"), []byte(npm), 0700))
	return provisionEnv{
		path:    bin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		bin:     bin,
		npmArgs: npmArgs,
	}
}

// withPreinstalled puts a pm reporting version in a directory ahead of the
// rest of e's PATH, as an image that ships one would, and returns that
// directory.
func (e *provisionEnv) withPreinstalled(t *testing.T, pm, version string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\nstore) echo '" + t.TempDir() + "' ;;\n--version) echo " + version + " ;;\nesac\nexit 0\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(dir, pm), []byte(script), 0700))
	e.path = dir + string(os.PathListSeparator) + e.path
	return dir
}

// npmRuns returns the argument lists npm was run with, one per run.
func (e provisionEnv) npmRuns(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.npmArgs)
	if os.IsNotExist(err) {
		return nil
	}
	assert.NilError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// writeManifest replaces appDir's package.json with manifest.
func writeManifest(t *testing.T, appDir, manifest string) {
	t.Helper()
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "package.json"), []byte(manifest), 0600))
}

// TestCLI_InstallPackages_InstallsPinnedPackageManager verifies that a package
// manager missing from PATH is installed with npm, at the version package.json
// pins, into a directory of setup-node's own, and linked onto PATH before the
// cache asks it anything.
func TestCLI_InstallPackages_InstallsPinnedPackageManager(t *testing.T) {
	s, run := sandbox(t)
	env := newProvisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0+sha512.abc123", "dependencies": {"is-odd": "^3.0.1"}}`)

	out, err := run(t, map[string]string{"PATH": env.path}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)

	root := filepath.Join(s.Home, ".cache", "setup-node", "package-managers", "pnpm-9.1.0")
	assert.DeepEqual(t, env.npmRuns(t), []string{"install --global --prefix " + root + " --no-audit --no-fund pnpm@9.1.0"})
	linked, err := filepath.EvalSymlinks(filepath.Join(env.bin, "pnpm"))
	assert.NilError(t, err, "expected pnpm linked into the directory on PATH; output: %s", out)
	want, err := filepath.EvalSymlinks(filepath.Join(root, "bin", "pnpm"))
	assert.NilError(t, err)
	assert.Equal(t, linked, want)
	assert.Assert(t, strings.Contains(out, "pnpm cache directory"),
		"expected the installed pnpm to report its cache directory; got: %s", out)
}

// TestCLI_InstallPackages_ReplacesOtherVersionOnPath verifies that a package
// manager on PATH at a version other than the one package.json pins is
// shadowed by the pinned one, so the package manager can't download the
// pinned version itself.
func TestCLI_InstallPackages_ReplacesOtherVersionOnPath(t *testing.T) {
	_, run := sandbox(t)
	env := newProvisionEnv(t)
	image := env.withPreinstalled(t, "pnpm", "8.0.0")
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)

	out, err := run(t, map[string]string{"PATH": env.path}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(env.npmRuns(t)), 1, "expected npm to install the pinned pnpm; output: %s", out)
	assert.Assert(t, strings.Contains(out, "pnpm on PATH is 8.0.0, but package.json pins 9.1.0"), "output: %s", out)
	got, err := exec.Command(filepath.Join(image, "pnpm"), "--version").Output()
	assert.NilError(t, err)
	assert.Equal(t, strings.TrimSpace(string(got)), "9.1.0", "expected PATH to resolve the pinned pnpm")
}

// TestCLI_InstallPackages_PinnedVersionAlreadyOnPath verifies that a package
// manager PATH already has at the pinned version is used as it is.
func TestCLI_InstallPackages_PinnedVersionAlreadyOnPath(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	env := newProvisionEnv(t)
	env.withPreinstalled(t, "pnpm", "9.1.0")
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)

	out, err := run(t, map[string]string{"PATH": env.path, "CIRCLECI_PLUGIN_URL": pluginURL}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(env.npmRuns(t)), 0, "expected npm not to be run; output: %s", out)
	for _, r := range rec.restores {
		assert.Assert(t, !strings.HasPrefix(r.Keys[0], "setup-node-package-manager-"), "expected no package manager restore; got %v", r.Keys)
	}
}

// TestCLI_InstallPackages_PinnedPackageManagerCache verifies that the pinned
// package manager is restored before npm is asked to install it, and saved
// after, under a key of the platform, package manager, version and install
// directory.
func TestCLI_InstallPackages_PinnedPackageManagerCache(t *testing.T) {
	s, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	env := newProvisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)

	out, err := run(t, map[string]string{"PATH": env.path, "CIRCLECI_PLUGIN_URL": pluginURL},
		"install-packages", "--app-dir", appDir, "--cache-nonce", "run-42")
	assert.NilError(t, err, "output: %s", out)

	root := filepath.Join(s.Home, ".cache", "setup-node", "package-managers", "pnpm-9.1.0")
	rootHash := sha256.Sum256([]byte(root))
	wantKey := fmt.Sprintf("setup-node-package-manager-v1-%s-%s-pnpm-9.1.0-%x-run-42", runtime.GOOS, runtime.GOARCH, rootHash[:4])
	assert.Assert(t, len(rec.restores) >= 1)
	assert.DeepEqual(t, rec.restores[0].Keys, []string{wantKey})
	assert.Equal(t, rec.restores[0].Name, "Restoring pnpm 9.1.0")
	assert.Assert(t, len(rec.saves) >= 1)
	assert.Equal(t, rec.saves[0].Key, wantKey)
	assert.DeepEqual(t, rec.saves[0].Paths, []string{root})
	assert.Equal(t, rec.saves[0].Name, "Saving pnpm 9.1.0")
}

// TestCLI_InstallPackages_RestoresPinnedPackageManager verifies that a later
// job restores the pinned package manager from the cache rather than
// installing it again.
func TestCLI_InstallPackages_RestoresPinnedPackageManager(t *testing.T) {
	s, run := sandbox(t)
	env := newProvisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)
	args := []string{"install-packages", "--app-dir", appDir}

	out, err := run(t, map[string]string{"PATH": env.path}, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(env.npmRuns(t)), 1)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-node")))
	assert.NilError(t, os.Remove(filepath.Join(env.bin, "pnpm")))
	out, err = run(t, map[string]string{"PATH": env.path}, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(env.npmRuns(t)), 1, "expected the second run to restore pnpm rather than install it; output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored pnpm 9.1.0 from cache"), "output: %s", out)
}

// TestCLI_InstallPackages_CacheDisabledStillInstallsPinned verifies that
// --cache=false installs the pinned package manager without touching the
// cache.
func TestCLI_InstallPackages_CacheDisabledStillInstallsPinned(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	env := newProvisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)

	out, err := run(t, map[string]string{"PATH": env.path, "CIRCLECI_PLUGIN_URL": pluginURL},
		"install-packages", "--app-dir", appDir, "--cache=false")
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(env.npmRuns(t)), 1)
	assert.Equal(t, len(rec.restores), 0)
	assert.Equal(t, len(rec.saves), 0)
}

// TestCLI_InstallPackages_MissingPackageManagerNotPinned verifies that a
// missing package manager package.json doesn't pin is an error that says how
// to provide it, and that nothing is installed in its place: a pin for a
// different package manager is not a pin for this one.
func TestCLI_InstallPackages_MissingPackageManagerNotPinned(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "no packageManager", manifest: `{"dependencies": {"is-odd": "^3.0.1"}}`},
		{name: "another package manager pinned", manifest: `{"packageManager": "yarn@4.5.0"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, run := sandbox(t)
			env := newProvisionEnv(t)
			appDir := copyFixture(t, "pnpm")
			writeManifest(t, appDir, tc.manifest)

			out, err := run(t, map[string]string{"PATH": env.path}, "install-packages", "--app-dir", appDir)
			assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
			assert.Assert(t, strings.Contains(out, "pnpm binary not found on PATH"), "got: %s", out)
			assert.Assert(t, strings.Contains(out, "npm install --global pnpm@<version>"), "got: %s", out)
			assert.Assert(t, strings.Contains(out, `"packageManager": "pnpm@<version>"`), "got: %s", out)
			assert.Equal(t, len(env.npmRuns(t)), 0, "expected npm not to be run; output: %s", out)
		})
	}
}

// TestCLI_InstallPackages_PinnedInstallLeavesNoBinary verifies that an npm
// install that leaves no executable behind fails saying so, when there is no
// package manager on PATH to fall back to.
func TestCLI_InstallPackages_PinnedInstallLeavesNoBinary(t *testing.T) {
	s, run := sandbox(t)
	env := newProvisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "bun@1.1.38"}`)
	// An npm that records its arguments and installs nothing.
	assert.NilError(t, os.WriteFile(filepath.Join(env.bin, "npm"), []byte("#!/bin/sh\necho \"$@\" >> '"+env.npmArgs+"'\n"), 0700)) //nolint:gosec // test stub

	out, err := run(t, map[string]string{"PATH": env.path}, "install-packages", "--app-dir", appDir, "--package-manager", "bun")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	root := filepath.Join(s.Home, ".cache", "setup-node", "package-managers", "bun-1.1.38")
	assert.Assert(t, strings.Contains(out, "installed bun@1.1.38 with npm, but it left no bun in "+root), "got: %s", out)
}

// TestCLI_InstallPackages_PinnedInstallFailsWithOtherVersionOnPath verifies
// that failing to install the pinned package manager is only a warning when
// PATH has the package manager at another version, which the install then
// runs with.
func TestCLI_InstallPackages_PinnedInstallFailsWithOtherVersionOnPath(t *testing.T) {
	_, run := sandbox(t)
	env := newProvisionEnv(t)
	env.withPreinstalled(t, "pnpm", "8.0.0")
	assert.NilError(t, os.WriteFile(filepath.Join(env.bin, "npm"), []byte("#!/bin/sh\nexit 1\n"), 0700)) //nolint:gosec // test stub
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0"}`)

	out, err := run(t, map[string]string{"PATH": env.path}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "setup-node: warning: npm install --global pnpm@9.1.0"), "output: %s", out)
	assert.Assert(t, strings.Contains(out, "running the pnpm 8.0.0 already on PATH"), "output: %s", out)
}
