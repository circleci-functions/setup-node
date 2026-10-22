package clitest_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

type cacheSave struct {
	Key   string
	Paths []string
	Name  string
}

type cacheRestore struct {
	Keys []string
	Name string
}

// cacheRecorder is a fake CircleCI plugin server that records save/restore
// calls made against it, for asserting how setup-node drives the cache SDK.
// It stands in for the sandbox's own cache when a test needs to inspect the
// exact keys and paths, which the round-trip cache does not expose.
type cacheRecorder struct {
	mu       sync.Mutex
	restores []cacheRestore
	saves    []cacheSave
}

func newCacheRecorder(t *testing.T) (*cacheRecorder, string) {
	t.Helper()
	rec := &cacheRecorder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/step/cache/restore", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys []string `json:"keys"`
			Name string   `json:"name"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.restores = append(rec.restores, cacheRestore{Keys: req.Keys, Name: req.Name})
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})
	mux.HandleFunc("/step/cache/save", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key   string   `json:"key"`
			Paths []string `json:"paths"`
			Name  string   `json:"name"`
		}
		assert.NilError(t, json.NewDecoder(r.Body).Decode(&req))
		rec.mu.Lock()
		rec.saves = append(rec.saves, cacheSave{Key: req.Key, Paths: req.Paths, Name: req.Name})
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success"})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// These tests put a no-op npm stub on PATH (stubPMPath): the cache
// restore/save happens either side of the install regardless of what the
// install does, so a stub that installs nothing keeps them hermetic while still
// exercising the full cache-driving path against a recorder.

// TestCLI_Cache_SaveAndRestoreCalled verifies that an install restores the
// package manager cache before installing and saves it back under the same key
// afterwards, keyed on the package manager and its version and pointed at its
// cache directory. The restore falls back to the key minus its lockfile hash.
func TestCLI_Cache_SaveAndRestoreCalled(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	appDir := copyFixture(t, "npm")
	cacheDir := t.TempDir()

	env := map[string]string{
		"CIRCLECI_PLUGIN_URL": pluginURL,
		"NPM_CONFIG_CACHE":    cacheDir,
		"PATH":                stubPMPath(t),
	}

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 1, "expected exactly one restore call")
	assert.Equal(t, len(rec.restores[0].Keys), 2, "expected an exact restore key and a fallback")
	key, fallback := rec.restores[0].Keys[0], rec.restores[0].Keys[1]
	wantFallback := regexp.MustCompile(`^setup-node-packages-v3-` + runtime.GOOS + `-` + runtime.GOARCH + `-npm-10\.9\.0-[0-9a-f]{8}-$`)
	assert.Assert(t, wantFallback.MatchString(fallback), "unexpected fallback key: %s", fallback)
	assert.Assert(t, regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(strings.TrimPrefix(key, fallback)),
		"expected the exact key to be the fallback plus the lockfile hash: %s", key)
	assert.Equal(t, rec.restores[0].Name, "Restoring npm cache", "expected the restore step to be named")

	assert.Equal(t, len(rec.saves), 1, "expected exactly one save call")
	assert.Equal(t, rec.saves[0].Key, key, "save must use the same key as restore")
	assert.DeepEqual(t, rec.saves[0].Paths, []string{cacheDir})
	assert.Equal(t, rec.saves[0].Name, "Saving npm cache", "expected the save step to be named")
}

// TestCLI_Cache_NoCallsWithCacheDisabled verifies that --cache=false skips both
// the restore and the save call, and does not write $BASH_ENV.
func TestCLI_Cache_NoCallsWithCacheDisabled(t *testing.T) {
	s, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	appDir := copyFixture(t, "npm")

	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL, "PATH": stubPMPath(t)}

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "npm", "--cache=false")
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")

	_, statErr := os.Stat(s.BashEnv)
	assert.Assert(t, os.IsNotExist(statErr), "$BASH_ENV should not be written with --cache=false")
}

// TestCLI_Cache_KeyChangesWithLockfileContent verifies that the cache key is
// derived from the lockfile contents, so a changed lockfile busts the cache,
// while the fallback key stays the same so the restore still warm-starts.
func TestCLI_Cache_KeyChangesWithLockfileContent(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	cacheDir := t.TempDir()

	runOnce := func(lockfileSuffix string) []string {
		appDir := copyFixture(t, "npm")
		lockfile := filepath.Join(appDir, "package-lock.json")
		data, readErr := os.ReadFile(lockfile) //nolint:gosec // fixture path is controlled by the test suite
		assert.NilError(t, readErr)
		//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
		assert.NilError(t, os.WriteFile(lockfile, append(data, []byte(lockfileSuffix)...), 0600))

		env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL, "NPM_CONFIG_CACHE": cacheDir, "PATH": stubPMPath(t)}
		out, runErr := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "npm")
		assert.NilError(t, runErr, "output: %s", out)
		return rec.restores[len(rec.restores)-1].Keys
	}

	keys1 := runOnce("")
	keys2 := runOnce("\n")
	assert.Assert(t, keys1[0] != keys2[0], "expected cache key to change when the lockfile content changes")
	assert.Equal(t, keys1[1], keys2[1], "expected the fallback key to survive a lockfile change")
}

// TestCLI_Cache_KeysChangeWithCacheInputs verifies that everything that makes a
// saved cache unusable to a job lands in the fallback key as well as the exact
// one, so a fallback restore never brings back a cache the job can't read: a
// different package manager version may lay its cache out differently, and a
// cache restored into a directory the package manager doesn't read is unused.
func TestCLI_Cache_KeysChangeWithCacheInputs(t *testing.T) {
	_, run := sandbox(t)
	cacheDir := t.TempDir()

	keys := func(t *testing.T, env map[string]string) []string {
		t.Helper()
		rec, pluginURL := newCacheRecorder(t)
		appDir := copyFixture(t, "npm")
		merged := map[string]string{
			"CIRCLECI_PLUGIN_URL": pluginURL,
			"NPM_CONFIG_CACHE":    cacheDir,
			"PATH":                stubPMPath(t),
		}
		maps.Copy(merged, env)
		out, err := run(t, merged, "install-packages", "--app-dir", appDir, "--package-manager", "npm")
		assert.NilError(t, err, "output: %s", out)
		assert.Equal(t, len(rec.restores), 1, "output: %s", out)
		return rec.restores[0].Keys
	}

	base := keys(t, nil)
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "package manager version", env: map[string]string{"PATH": stubPMPathVersion(t, "11.0.0")}},
		{name: "cache directory", env: map[string]string{"NPM_CONFIG_CACHE": t.TempDir()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := keys(t, tc.env)
			assert.Assert(t, got[0] != base[0], "expected the cache key to change: %s", got[0])
			assert.Assert(t, got[1] != base[1], "expected the fallback key to change: %s", got[1])
		})
	}
}

// TestCLI_Cache_SkippedWhenVersionUnknown verifies that a package manager that
// can't report its version skips the cache rather than saving one under a key
// that would collide across versions, and still installs.
func TestCLI_Cache_SkippedWhenVersionUnknown(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	appDir := copyFixture(t, "npm")
	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL, "NPM_CONFIG_CACHE": t.TempDir(), "PATH": stubPMPathVersion(t, "")}

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "skipping cache restore"),
		"expected output to mention skipping cache restore; got: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")
}

// TestCLI_Cache_SkippedWhenLockfileMissing verifies that a missing lockfile
// (e.g. an --override-command run with nothing checked out) skips the
// restore/save calls without failing the install.
func TestCLI_Cache_SkippedWhenLockfileMissing(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	appDir := t.TempDir() // no package.json / lockfile

	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL, "NPM_CONFIG_CACHE": t.TempDir(), "PATH": stubPMPath(t)}

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "skipping cache restore"),
		"expected output to mention skipping cache restore; got: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")
}

// TestCLI_Cache_SkippedWhenCacheDirUnknown verifies that a package manager that
// can't report its cache directory skips the cache with a warning that carries
// the package manager's own reason, and still installs.
func TestCLI_Cache_SkippedWhenCacheDirUnknown(t *testing.T) {
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	appDir := copyFixture(t, "pnpm")
	pnpm := `#!/bin/sh
case "$1" in
store) echo "ERR_PNPM_STORE_BROKEN the store is unreadable" >&2; exit 1 ;;
--version) echo 10.0.0 ;;
esac
exit 0
`
	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL, "PATH": stubPATH(t, map[string]string{"pnpm": pnpm})}

	out, err := run(t, env, "install-packages", "--app-dir", appDir, "--package-manager", "pnpm")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "skipping cache restore"),
		"expected output to mention skipping cache restore; got: %s", out)
	assert.Assert(t, strings.Contains(out, "ERR_PNPM_STORE_BROKEN the store is unreadable"),
		"expected the warning to carry pnpm's stderr; got: %s", out)

	assert.Equal(t, len(rec.restores), 0, "expected no restore calls")
	assert.Equal(t, len(rec.saves), 0, "expected no save calls")
}
