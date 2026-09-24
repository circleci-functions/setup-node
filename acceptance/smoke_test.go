//go:build acceptance

package setupnode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestAcceptance_Smoke runs the sequence the CircleCI smoke-test job used to run
// as bash steps, against a real npm install: install packages, then simulate a
// fresh executor and confirm the second install comes back from the cache.
//
// The cache is in-memory here (see sandboxInMemory): the round-trip is exact
// only against a cache with no prior history of setup-node's lockfile-derived,
// write-once key. The live round-trip end to end is the CI smoke job's job.
func TestAcceptance_Smoke(t *testing.T) {
	requirePM(t, "npm")
	s, run := sandboxInMemory(t)
	appDir := copyFixture(t, "npm")

	installArgs := []string{"install-packages", "--app-dir", appDir, "--package-manager", "npm"}

	if !t.Run("Install", func(t *testing.T) {
		out, err := run(t, nil, installArgs...)
		assert.NilError(t, err, "got: %s", out)
		assert.Assert(t, !strings.Contains(out, "from cache"),
			"the first install has nothing to restore; got: %s", out)
		assertInstalled(t, appDir)
	}) {
		t.Fatal("install failed; aborting remaining steps")
	}

	// The cache directory npm resolves for itself, which is the one setup-node
	// saved.
	cacheDir := resolveCacheDir(t, "npm", appDir, s.CmdEnv(os.Environ()))

	t.Run("NothingIsExportedToAShell", func(t *testing.T) {
		_, err := os.Stat(s.BashEnv)
		assert.Assert(t, os.IsNotExist(err),
			"each package manager finds its own cache, so $BASH_ENV must be untouched; got %v", err)
	})

	// Simulate a fresh executor: the package cache and installed modules are
	// gone, so the next install has to come back from the cache saved above.
	assert.NilError(t, os.RemoveAll(cacheDir))
	assert.NilError(t, os.RemoveAll(filepath.Join(appDir, "node_modules")))

	t.Run("Install_CacheHit", func(t *testing.T) {
		out, err := run(t, nil, installArgs...)
		assert.NilError(t, err, "got: %s", out)
		assert.Assert(t, strings.Contains(out, "from cache"),
			"expected the second install to restore the package cache; got: %s", out)
		assertInstalled(t, appDir)
	})
}

func assertInstalled(t *testing.T, appDir string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(appDir, "node_modules", "is-odd"))
	assert.NilError(t, err, "expected node_modules/is-odd after install")
}
