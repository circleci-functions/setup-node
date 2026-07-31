package nodeinstallpackages_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
)

type cacheSave struct {
	Key   string
	Paths []string
}

// cacheRecorder is a fake CircleCI plugin server that records save/restore
// calls made against it, for asserting how setup-node drives the cache SDK.
type cacheRecorder struct {
	mu       sync.Mutex
	restores [][]string
	saves    []cacheSave
}

func newCacheRecorder(t *testing.T) (*cacheRecorder, string) {
	t.Helper()
	rec := &cacheRecorder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/step/cache/restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys []string `json:"keys"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.restores = append(rec.restores, req.Keys)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})
	mux.HandleFunc("/step/cache/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key   string   `json:"key"`
			Paths []string `json:"paths"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.saves = append(rec.saves, cacheSave{Key: req.Key, Paths: req.Paths})
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// TestCLI_Cache_SaveAndRestoreCalled verifies that a successful install
// restores the package manager cache before installing and saves it back
// under the same key afterwards.
func TestCLI_Cache_SaveAndRestoreCalled(t *testing.T) {
	_, err := exec.LookPath("npm")
	skip.If(t, err != nil, "npm not found on PATH")

	rec, pluginURL := newCacheRecorder(t)

	appDir := t.TempDir()
	copyFixture(t, "testdata/npm", appDir)
	cacheDir := t.TempDir()

	env := map[string]string{
		"CIRCLECI_PLUGIN_URL": pluginURL,
		"NPM_CONFIG_CACHE":    cacheDir,
		"BASH_ENV":            filepath.Join(t.TempDir(), "bash_env"),
	}

	out, err := run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 1, "expected exactly one restore call")
	assert.Equal(t, len(rec.restores[0]), 1, "expected exactly one restore key")
	key := rec.restores[0][0]
	assert.Assert(t, strings.Contains(key, "setup-node-packages-v1-"), "unexpected cache key: %s", key)
	assert.Assert(t, strings.Contains(key, "npm"), "expected cache key to mention the package manager: %s", key)

	assert.Equal(t, len(rec.saves), 1, "expected exactly one save call")
	assert.Equal(t, rec.saves[0].Key, key, "save must use the same key as restore")
	assert.DeepEqual(t, rec.saves[0].Paths, []string{cacheDir})
}

// TestCLI_Cache_NoCallsWithNoWithCache verifies that --no-with-cache skips
// both the restore and the save call entirely.
func TestCLI_Cache_NoCallsWithNoWithCache(t *testing.T) {
	_, err := exec.LookPath("npm")
	skip.If(t, err != nil, "npm not found on PATH")

	rec, pluginURL := newCacheRecorder(t)

	appDir := t.TempDir()
	copyFixture(t, "testdata/npm", appDir)

	env := map[string]string{
		"CIRCLECI_PLUGIN_URL": pluginURL,
		"NPM_CONFIG_CACHE":    t.TempDir(),
	}

	out, err := run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm", "--no-with-cache")
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")
}

// TestCLI_Cache_KeyChangesWithLockfileContent verifies that the cache key is
// derived from the lockfile contents, so a changed lockfile busts the cache.
func TestCLI_Cache_KeyChangesWithLockfileContent(t *testing.T) {
	_, err := exec.LookPath("npm")
	skip.If(t, err != nil, "npm not found on PATH")

	rec, pluginURL := newCacheRecorder(t)

	runOnce := func(lockfileSuffix string) string {
		appDir := t.TempDir()
		copyFixture(t, "testdata/npm", appDir)
		lockfile := filepath.Join(appDir, "package-lock.json")
		data, readErr := os.ReadFile(lockfile) //nolint:gosec // test fixture path
		assert.NilError(t, readErr)
		// #nosec G703: Test fixture paths are controlled within the test suite
		assert.NilError(t, os.WriteFile(lockfile, append(data, []byte(lockfileSuffix)...), 0600))

		env := map[string]string{
			"CIRCLECI_PLUGIN_URL": pluginURL,
			"NPM_CONFIG_CACHE":    t.TempDir(),
			"BASH_ENV":            filepath.Join(t.TempDir(), "bash_env"),
		}
		out, runErr := run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm")
		assert.NilError(t, runErr, "output: %s", out)
		return rec.restores[len(rec.restores)-1][0]
	}

	key1 := runOnce("")
	key2 := runOnce("\n")
	assert.Assert(t, key1 != key2, "expected cache key to change when the lockfile content changes")
}

// TestCLI_Cache_SkippedWhenLockfileMissing verifies that a missing lockfile
// (e.g. an --override-command run with nothing to check out) skips the
// restore/save calls without failing the install.
func TestCLI_Cache_SkippedWhenLockfileMissing(t *testing.T) {
	_, err := exec.LookPath("npm")
	skip.If(t, err != nil, "npm not found on PATH")

	rec, pluginURL := newCacheRecorder(t)

	appDir := t.TempDir() // no package.json / lockfile

	env := map[string]string{
		"CIRCLECI_PLUGIN_URL": pluginURL,
		"NPM_CONFIG_CACHE":    t.TempDir(),
		"BASH_ENV":            filepath.Join(t.TempDir(), "bash_env"),
	}

	out, err := run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm", "--override-command", "true")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "skipping cache restore"), "expected output to mention skipping cache restore; got: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")
}

// TestCLI_Cache_RestoreLogsOnHit verifies the full cache round-trip against a
// real (fake) plugin server: the first run saves the npm cache; after
// clearing it, the second run restores it and logs that it did, mirroring
// what the setup-node-smoke-test CI job greps for.
func TestCLI_Cache_RestoreLogsOnHit(t *testing.T) {
	_, err := exec.LookPath("npm")
	skip.If(t, err != nil, "npm not found on PATH")

	srv := sdktest.NewServer(t)
	srv.SetupEnv(t)

	appDir := t.TempDir()
	copyFixture(t, "testdata/npm", appDir)
	cacheDir := t.TempDir()

	env := map[string]string{
		"NPM_CONFIG_CACHE": cacheDir,
		"BASH_ENV":         filepath.Join(t.TempDir(), "bash_env"),
	}

	out, err := run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "run 1 output: %s", out)
	assert.Assert(t, !strings.Contains(out, "restored"), "first run should not report a cache hit; got: %s", out)

	assert.NilError(t, os.RemoveAll(cacheDir))
	assert.NilError(t, os.RemoveAll(filepath.Join(appDir, "node_modules")))

	out, err = run(t, env, "install", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "run 2 output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored"), "expected second run to report a cache hit; got: %s", out)
}
