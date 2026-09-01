package clitest_test

import (
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

// TestCLI_Install_InvalidPackageManager verifies that a --package-manager
// outside the known set is rejected at flag parsing, before anything runs.
func TestCLI_Install_InvalidPackageManager(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "--package-manager", "not-a-real-pm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "package-manager"),
		"expected the error to mention package-manager; got: %s", out)
}

// TestCLI_Install_AppDirDoesNotExist verifies that a missing --app-dir fails
// with an error naming the flag rather than proceeding.
func TestCLI_Install_AppDirDoesNotExist(t *testing.T) {
	_, run := sandbox(t)
	out, err := run(t, nil, "--app-dir", "/nonexistent-dir-xyz/app", "--package-manager", "npm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "app-dir"),
		"expected the error to mention app-dir; got: %s", out)
}

// TestCLI_Install_PackageManagerNotOnPATH verifies that an explicitly named
// package manager missing from PATH is a clear error. PATH is pointed at an
// empty dir so no package manager binary is found.
func TestCLI_Install_PackageManagerNotOnPATH(t *testing.T) {
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")

	out, err := run(t, map[string]string{"PATH": t.TempDir()},
		"--app-dir", appDir, "--package-manager", "npm")
	assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
	assert.Assert(t, strings.Contains(out, "not found"),
		"expected the error to mention the binary was not found; got: %s", out)
}
