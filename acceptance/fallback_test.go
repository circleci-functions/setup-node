//go:build acceptance

package setupnode_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

// TestAcceptance_LockfileChangeRestoresFallback checks that a changed lockfile
// still restores the newest cache, so the package manager reuses what it
// already downloaded rather than fetching everything again.
//
// The second install runs offline, so it only passes if the restore brought
// the packages back. The key holds a hash of the cache directory, which is
// under the sandbox's temp $HOME, so on the live cache the fallback can only
// match what this run saved.
//
// npm stands in for every package manager: the fallback is the same for all
// of them, and pnpm's store holds a symlink the in-memory cache cannot copy.
func TestAcceptance_LockfileChangeRestoresFallback(t *testing.T) {
	requirePM(t, "npm")
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")
	args := []string{"install-packages", "--app-dir", appDir, "--package-manager", "npm"}

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)

	assert.NilError(t, os.RemoveAll(savedCacheDir(t, out)))
	assert.NilError(t, os.RemoveAll(filepath.Join(appDir, "node_modules")))
	lockfile := filepath.Join(appDir, "package-lock.json")
	data, err := os.ReadFile(lockfile) //nolint:gosec // fixture path is controlled by the test suite
	assert.NilError(t, err)
	//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
	assert.NilError(t, os.WriteFile(lockfile, append(data, '\n'), 0600))

	out, err = run(t, nil, append(args, "--override-command", "npm ci --offline")...)
	assert.NilError(t, err, "expected the offline install to find the packages the fallback restored; output: %s", out)
	_, err = os.Stat(filepath.Join(appDir, "node_modules", "is-odd"))
	assert.NilError(t, err, "expected node_modules/is-odd after install")
}
