//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// cacheQuery is how each package manager reports the directory it will use.
var cacheQuery = map[string][]string{
	"npm":  {"npm", "config", "get", "cache"},
	"yarn": {"yarn", "cache", "dir"},
	"pnpm": {"pnpm", "store", "path"},
	"bun":  {"bun", "pm", "cache"},
}

// TestAcceptance_CacheDirReuse checks that a later step reaches the cache
// setup-node saved, without anything being exported to reach it.
//
// Each package manager resolves its own cache directory, so a later step finds
// what was restored by asking the same question setup-node asked. The
// directory has to hold something as well: agreeing on a path that nothing
// filled is not reuse.
func TestAcceptance_CacheDirReuse(t *testing.T) {
	for _, pm := range []string{"npm", "yarn", "pnpm", "bun"} {
		t.Run(pm, func(t *testing.T) {
			requirePM(t, pm)
			s, run := sandbox(t)
			appDir := copyFixture(t, pm)

			out, err := run(t, nil, "--app-dir", appDir, "--package-manager", pm)
			assert.NilError(t, err, "output: %s", out)

			saved := savedCacheDir(t, out)
			resolved := resolveCacheDir(t, pm, appDir, s.CmdEnv(os.Environ()))

			assert.Equal(t, resolved, saved,
				"a later step must resolve the cache directory setup-node saved")
			assert.Assert(t, entryCount(saved) > 0,
				"the saved cache is empty, so nothing was reused: %s", saved)
		})
	}
}

// savedCacheDir reads the directory setup-node reported caching.
func savedCacheDir(t *testing.T, out string) string {
	t.Helper()
	const marker = "cache directory "
	for line := range strings.SplitSeq(out, "\n") {
		if _, dir, found := strings.Cut(line, marker); found {
			return realPath(strings.TrimSpace(dir))
		}
	}
	t.Fatalf("setup-node did not report which cache directory it used:\n%s", out)
	return ""
}

// resolveCacheDir asks pm where its cache is, from appDir and under env.
func resolveCacheDir(t *testing.T, pm, appDir string, env []string) string {
	t.Helper()

	args := cacheQuery[pm]
	cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // names come from cacheQuery
	cmd.Dir = appDir
	cmd.Env = env

	// Stdout only: yarn writes warnings about the fixture's package.json to
	// stderr, and the last field is the answer.
	out, err := cmd.Output()
	assert.NilError(t, err, "%s could not report its cache directory", pm)

	fields := strings.Fields(strings.TrimSpace(string(out)))
	assert.Assert(t, len(fields) > 0, "%s reported no cache directory", pm)
	return realPath(fields[len(fields)-1])
}

func entryCount(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}
