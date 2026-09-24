//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-node/node"
)

// TestAcceptance_BunCacheDirIsWhatBunUses checks setup-node caches the
// directory bun actually fills.
//
// bun resolves $BUN_INSTALL_CACHE_DIR, then $XDG_CACHE_HOME/.bun/install/cache,
// then ~/.bun/install/cache. A path built from $HOME alone matches only the
// last, so with $XDG_CACHE_HOME set — which a test sandbox does, and an image
// may — setup-node saved a directory bun never wrote to.
func TestAcceptance_BunCacheDirIsWhatBunUses(t *testing.T) {
	requirePM(t, "bun")
	s, run := sandbox(t)
	appDir := copyFixture(t, "bun")

	out, err := run(t, nil, "install-packages", "--app-dir", appDir, "--package-manager", "bun")
	assert.NilError(t, err, "output: %s", out)

	// The binary ran in the sandbox, so put this process there too: the answer
	// depends on $HOME and $XDG_CACHE_HOME, and comparing across two
	// environments would compare two different questions.
	s.ApplyToProcess(t)

	computed, err := node.Bun.CacheDir(t.Context(), appDir)
	assert.NilError(t, err)

	cmd := exec.CommandContext(t.Context(), "bun", "pm", "cache")
	cmd.Dir = appDir
	reported, err := cmd.Output()
	assert.NilError(t, err, "bun could not report its cache directory")

	assert.Equal(t, realPath(computed), realPath(strings.TrimSpace(string(reported))),
		"setup-node must cache the directory bun uses")

	entries, err := os.ReadDir(computed)
	assert.NilError(t, err, "bun's cache directory should exist after an install")
	assert.Assert(t, len(entries) > 0, "bun's cache directory is empty, so nothing was cached")
}
