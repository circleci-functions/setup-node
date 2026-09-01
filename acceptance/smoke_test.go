//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/bashenv"
)

// TestAcceptance_Smoke runs the sequence the CircleCI smoke-test job used to run
// as bash steps, against a real npm install: install packages, confirm the
// cache-dir export reaches a later step through $BASH_ENV, then simulate a fresh
// executor and confirm the second install comes back from the cache.
//
// The cache is in-memory here (see sandboxInMemory): the round-trip is exact
// only against a cache with no prior history of setup-node's lockfile-derived,
// write-once key. The live round-trip end to end is the CI smoke job's job.
func TestAcceptance_Smoke(t *testing.T) {
	requirePM(t, "npm")
	s, run := sandboxInMemory(t)
	appDir := copyFixture(t, "npm")

	installArgs := []string{"--app-dir", appDir, "--package-manager", "npm"}

	if !t.Run("Install", func(t *testing.T) {
		out, err := run(t, nil, installArgs...)
		assert.NilError(t, err, "got: %s", out)
		assert.Assert(t, !strings.Contains(out, "from cache"),
			"the first install has nothing to restore; got: %s", out)
		assertInstalled(t, appDir)
	}) {
		t.Fatal("install failed; aborting remaining steps")
	}

	// The cache directory setup-node exported for later steps to reuse.
	cacheDir := exportedCacheDir(t, s.BashEnv, "NPM_CONFIG_CACHE")

	t.Run("BashEnv_SourcesIntoALaterStep", func(t *testing.T) {
		assertBashEnvExports(t, s.BashEnv, "NPM_CONFIG_CACHE", cacheDir)
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

// exportedCacheDir reads the cache directory setup-node exported to $BASH_ENV
// under key, failing if it was not exported.
func exportedCacheDir(t *testing.T, bashEnvFile, key string) string {
	t.Helper()
	vars, err := bashenv.Vars(bashEnvFile, nil)
	assert.NilError(t, err, "read $BASH_ENV")
	dir, ok := vars[key]
	assert.Assert(t, ok, "expected %s exported to $BASH_ENV; got: %v", key, vars)
	return dir
}

// assertBashEnvExports sources $BASH_ENV in a real shell and reads key back,
// which is what a later job step does. bashenv.Vars parses the file; this
// checks a shell agrees, so a quoting mistake the parser tolerates still fails.
func assertBashEnvExports(t *testing.T, bashEnvFile, key, want string) {
	t.Helper()
	script := `set -e; . "$BASH_ENV"; printf %s "${` + key + `}"`
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script) //nolint:gosec
	cmd.Env = append(os.Environ(), "BASH_ENV="+bashEnvFile)

	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "sourcing $BASH_ENV failed: %s", out)
	assert.Equal(t, strings.TrimSpace(string(out)), want,
		"expected a later step to see %s=%q after sourcing $BASH_ENV", key, want)
}
