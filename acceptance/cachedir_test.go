//go:build acceptance

package setupnode_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/bashenv"

	"github.com/circleci-functions/setup-node/node"
)

// cacheQuery is how each package manager reports the directory it will use.
var cacheQuery = map[string][]string{
	"npm":  {"npm", "config", "get", "cache"},
	"yarn": {"yarn", "cache", "dir"},
	"pnpm": {"pnpm", "store", "path"},
	"bun":  {"bun", "pm", "cache"},
}

// TestAcceptance_CacheDirReuse records, per package manager, whether a later
// step reaches the cache setup-node saved.
//
// setup-node exports the directory to $BASH_ENV so later steps use the cache it
// restored. Whether that export is needed, ignored, or actively wrong differs
// per package manager, and the answer decides what replaces it: nothing, the
// package manager's own config file, or the export as it stands. It reports
// rather than asserts until that is settled.
func TestAcceptance_CacheDirReuse(t *testing.T) {
	for _, pm := range []string{"npm", "yarn", "pnpm", "bun"} {
		t.Run(pm, func(t *testing.T) {
			requirePM(t, pm)
			s, run := sandbox(t)
			appDir := copyFixture(t, pm)

			out, err := run(t, nil, "--app-dir", appDir, "--package-manager", pm)
			// Observation only: TestAcceptance_Install is what fails when a
			// package manager cannot install.
			if err != nil {
				t.Skipf("%s could not be observed here: %v\n%s", pm, err, out)
			}

			key := node.PackageManager(pm).CacheDirKey()
			vars, err := bashenv.Vars(s.BashEnv, nil)
			assert.NilError(t, err, "read $BASH_ENV")
			exported := vars[key]

			base := s.CmdEnv(os.Environ())
			exported = realPath(exported)
			withExport := resolveCacheDir(t, pm, appDir, append(base, key+"="+exported))
			without := resolveCacheDir(t, pm, appDir, base)

			t.Logf("%s: exports %s=%s (%s)", pm, key, exported, describeDir(exported))
			t.Logf("%s: a later step with that export resolves %s", pm, withExport)
			t.Logf("%s: a later step without it resolves        %s (%s)", pm, without, describeDir(without))

			switch {
			case withExport == exported && without == exported:
				t.Logf("%s: the export changes nothing — both reach the saved cache", pm)
			case withExport == exported:
				t.Logf("%s: the export is what reaches the saved cache", pm)
			case without == exported:
				t.Logf("%s: the export moves a later step AWAY from the saved cache", pm)
			default:
				t.Logf("%s: neither reaches the saved cache", pm)
			}
		})
	}
}

// resolveCacheDir asks pm where its cache is, from appDir and under env.
func resolveCacheDir(t *testing.T, pm, appDir string, env []string) string {
	t.Helper()

	args := cacheQuery[pm]
	cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // names come from cacheQuery
	cmd.Dir = appDir
	cmd.Env = env

	// Stdout only: yarn writes warnings about the fixture's package.json to
	// stderr, and the last line is the answer.
	out, err := cmd.Output()
	if err != nil {
		return "(" + args[0] + " " + strings.Join(args[1:], " ") + " failed: " + err.Error() + ")"
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		return "(no output)"
	}
	return realPath(lines[len(lines)-1])
}

// describeDir says whether a directory exists and holds anything, which is
// what separates a cache that is reused from one that is merely agreed upon.
func describeDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "does not exist"
	}
	if len(entries) == 0 {
		return "empty"
	}
	return fmt.Sprintf("%d entries", len(entries))
}

// realPath resolves symlinks, so a macOS /var answer compares equal to the
// /private/var it points at.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
