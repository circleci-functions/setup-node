//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// yarnBerryVersion returns the Yarn release for TestAcceptance_YarnBerry to
// install: SETUP_NODE_ACCEPTANCE_YARN_BERRY_VERSION is set in CI from the
// pipeline's yarn-berry-version parameter, the newest Yarn 4 locally.
func yarnBerryVersion() string {
	if v := os.Getenv("SETUP_NODE_ACCEPTANCE_YARN_BERRY_VERSION"); v != "" {
		return v
	}
	return "4"
}

// TestAcceptance_YarnBerry checks that setup-node caches the folder Yarn 4
// fills. Yarn 2 and later have no `yarn cache dir`, which is what Yarn 1
// answers to.
//
// The rest of the suite runs the Yarn 1 on PATH, so this installs Yarn 4 from
// npm's @yarnpkg/cli-dist into a directory of its own and puts that first on
// PATH for this test alone.
func TestAcceptance_YarnBerry(t *testing.T) {
	requirePM(t, "npm")
	prefix := t.TempDir()
	npm(t, prefix, "install", "--prefix", prefix, "@yarnpkg/cli-dist@"+yarnBerryVersion())
	bin := filepath.Join(prefix, "node_modules", ".bin")
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")

	s, run := sandbox(t)
	appDir := copyFixture(t, "yarn-berry")

	out, err := run(t, map[string]string{"PATH": path}, "install-packages", "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)
	_, err = os.Stat(filepath.Join(appDir, "node_modules", "is-odd"))
	assert.NilError(t, err, "expected node_modules/is-odd after install")

	cmd := exec.CommandContext(t.Context(), filepath.Join(bin, "yarn"), "config", "get", "cacheFolder")
	cmd.Dir = appDir
	cmd.Env = append(s.CmdEnv(os.Environ()), "PATH="+path)
	reported, err := cmd.Output()
	assert.NilError(t, err, "yarn could not report its cache folder")

	saved := savedCacheDir(t, out)
	assert.Equal(t, realPath(strings.TrimSpace(string(reported))), saved,
		"setup-node must cache the folder yarn uses")
	assert.Assert(t, entryCount(saved) > 0, "yarn's cache folder is empty, so nothing was cached: %s", saved)
}
