package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestCLI_UnknownFlag verifies an unrecognised flag is a non-zero exit rather
// than being silently ignored.
func TestCLI_UnknownFlag(t *testing.T) {
	_, run := sandbox(t)
	_, err := run(t, nil, "--no-such-flag")
	assert.Assert(t, err != nil, "expected non-zero exit for unknown flag")
}

// TestCLI_InstallPackages_InvalidPackageManager verifies that a --package-manager
// outside the known set is rejected at flag parsing, before anything runs.
func TestCLI_InstallPackages_InvalidPackageManager(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "install-packages", "--package-manager", "not-a-real-pm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "package-manager"),
		"expected the error to mention package-manager; got: %s", out)
}

// TestCLI_InstallPackages_AppDirDoesNotExist verifies that a missing --app-dir fails
// with an error naming the flag rather than proceeding.
func TestCLI_InstallPackages_AppDirDoesNotExist(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "install-packages", "--app-dir", "/nonexistent-dir-xyz/app", "--package-manager", "npm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "app-dir"),
		"expected the error to mention app-dir; got: %s", out)
}

// TestCLI_InstallPackages_PackageManagerNotOnPATH verifies that an explicitly named
// package manager missing from PATH is a clear error. PATH is pointed at an
// empty dir so no package manager binary is found.
func TestCLI_InstallPackages_PackageManagerNotOnPATH(t *testing.T) {
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")

	out, err := run(t, map[string]string{"PATH": t.TempDir()},
		"install-packages", "--app-dir", appDir, "--package-manager", "npm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "not found"),
		"expected the error to mention the binary was not found; got: %s", out)
}

// TestCLI_InstallPackages_DefaultNpmCommand verifies that npm's default install
// is a clean install that skips npm's audit and funding checks, which a CI
// install has no use for.
func TestCLI_InstallPackages_DefaultNpmCommand(t *testing.T) {
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")

	argsFile := filepath.Join(t.TempDir(), "args")
	stub := t.TempDir()
	script := "#!/bin/sh\necho \"$@\" > '" + argsFile + "'\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(stub, "npm"), []byte(script), 0700))

	out, err := run(t, map[string]string{"PATH": stub + string(os.PathListSeparator) + os.Getenv("PATH")},
		"install-packages", "--app-dir", appDir, "--cache=false")
	assert.NilError(t, err, "output: %s", out)

	got, err := os.ReadFile(argsFile) //nolint:gosec // a path this test chose
	assert.NilError(t, err, "npm was not run; output: %s", out)
	assert.Equal(t, strings.TrimSpace(string(got)), "ci --no-audit --no-fund")
}
