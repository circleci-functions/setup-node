package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// provisionEnv returns a PATH holding no pnpm, and an npm stub that records
// its arguments to the returned file and, when asked to install pnpm, puts a
// pnpm stub on that PATH the way a global install would.
func provisionEnv(t *testing.T) (path, npmArgs string) {
	t.Helper()
	staged := t.TempDir()
	pnpm := "#!/bin/sh\ncase \"$1\" in\nstore) echo '" + t.TempDir() + "' ;;\n--version) echo 9.1.0 ;;\nesac\nexit 0\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(staged, "pnpm"), []byte(pnpm), 0700))

	bin := t.TempDir()
	npmArgs = filepath.Join(t.TempDir(), "npm-args")
	npm := "#!/bin/sh\necho \"$@\" >> '" + npmArgs + "'\n" +
		"case \"$3\" in pnpm@*) cp '" + filepath.Join(staged, "pnpm") + "' '" + bin + "' ;; esac\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "npm"), []byte(npm), 0700))
	return bin + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin", npmArgs
}

// writeManifest replaces appDir's package.json with manifest.
func writeManifest(t *testing.T, appDir, manifest string) {
	t.Helper()
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "package.json"), []byte(manifest), 0600))
}

// TestCLI_InstallPackages_InstallsPinnedPackageManager verifies that a package
// manager missing from PATH is installed with npm at the version package.json
// pins, before the cache asks it anything, and then runs the install.
func TestCLI_InstallPackages_InstallsPinnedPackageManager(t *testing.T) {
	_, run := sandbox(t)
	path, npmArgs := provisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "pnpm@9.1.0+sha512.abc123", "dependencies": {"is-odd": "^3.0.1"}}`)

	out, err := run(t, map[string]string{"PATH": path}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)

	got, err := os.ReadFile(npmArgs) //nolint:gosec // a path this test chose
	assert.NilError(t, err, "npm was not run; output: %s", out)
	assert.Equal(t, strings.TrimSpace(string(got)), "install --global pnpm@9.1.0")
	assert.Assert(t, strings.Contains(out, "pnpm cache directory"),
		"expected the installed pnpm to report its cache directory; got: %s", out)
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
			path, npmArgs := provisionEnv(t)
			appDir := copyFixture(t, "pnpm")
			writeManifest(t, appDir, tc.manifest)

			out, err := run(t, map[string]string{"PATH": path}, "install-packages", "--app-dir", appDir)
			assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
			assert.Assert(t, strings.Contains(out, "pnpm binary not found on PATH"), "got: %s", out)
			assert.Assert(t, strings.Contains(out, "npm install --global pnpm@<version>"), "got: %s", out)
			assert.Assert(t, strings.Contains(out, `"packageManager": "pnpm@<version>"`), "got: %s", out)

			_, statErr := os.Stat(npmArgs)
			assert.Assert(t, os.IsNotExist(statErr), "expected npm not to be run; output: %s", out)
		})
	}
}

// TestCLI_InstallPackages_PinnedPackageManagerStillMissing verifies that an
// install that leaves the package manager off PATH, as one into a global
// prefix PATH doesn't hold would, fails saying so.
func TestCLI_InstallPackages_PinnedPackageManagerStillMissing(t *testing.T) {
	_, run := sandbox(t)
	path, npmArgs := provisionEnv(t)
	appDir := copyFixture(t, "pnpm")
	writeManifest(t, appDir, `{"packageManager": "bun@1.1.38"}`)

	out, err := run(t, map[string]string{"PATH": path}, "install-packages", "--app-dir", appDir, "--package-manager", "bun")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "installed bun@1.1.38 with npm, but bun is still not on PATH"), "got: %s", out)

	got, err := os.ReadFile(npmArgs) //nolint:gosec // a path this test chose
	assert.NilError(t, err, "npm was not run; output: %s", out)
	assert.Equal(t, strings.TrimSpace(string(got)), "install --global bun@1.1.38")
}
