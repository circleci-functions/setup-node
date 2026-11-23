//go:build acceptance

package setupnode_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

// pinnedPNPM is the pnpm the test pins in package.json. It ships as a native
// binary, as pnpm does from version 11, so the in-memory stand-in for the cache,
// which copies a symlink's target rather than the link, restores a working
// install.
const pinnedPNPM = "12.18.0"

// TestAcceptance_PinnedPackageManager checks that install-packages puts the
// pnpm package.json pins on PATH ahead of the one already there, and that a
// later job restores it from the cache rather than installing it again.
//
// The second run points npm at a registry that refuses connections, so it can
// only pass by restoring. The cache key carries a nonce unique to the run: the
// live cache keeps the first save under a key, and an earlier run's save
// points at that run's temp $HOME.
func TestAcceptance_PinnedPackageManager(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "npm installs pnpm as .cmd scripts on Windows, which this test doesn't run")
	requirePM(t, "npm")
	requirePM(t, "pnpm")
	out, err := exec.Command("pnpm", "--version").Output()
	assert.NilError(t, err)
	skip.If(t, strings.TrimSpace(string(out)) == pinnedPNPM, "pnpm on PATH is already "+pinnedPNPM)

	s, run := sandbox(t)
	appDir := copyFixture(t, "pnpm")
	writePackageManager(t, appDir, "pnpm@"+pinnedPNPM)
	binDir := t.TempDir()
	env := map[string]string{"PATH": binDir + string(os.PathListSeparator) + os.Getenv("PATH")}
	args := []string{
		"install-packages", "--app-dir", appDir, "--bin-dir", binDir,
		"--cache-nonce", fmt.Sprintf("acceptance-%d", time.Now().UnixNano()),
		"--override-command", "pnpm --version",
	}

	out1, err := run(t, env, args...)
	assert.NilError(t, err, "output: %s", out1)
	assert.Assert(t, strings.Contains(out1, "installing pnpm@"+pinnedPNPM), "output: %s", out1)
	assert.Equal(t, pnpmVersion(t, binDir), pinnedPNPM)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-node")))
	entries, err := os.ReadDir(binDir)
	assert.NilError(t, err)
	for _, e := range entries {
		assert.NilError(t, os.Remove(filepath.Join(binDir, e.Name())))
	}
	env["npm_config_registry"] = "http://127.0.0.1:9/"

	out2, err := run(t, env, args...)
	assert.NilError(t, err, "output: %s", out2)
	assert.Assert(t, strings.Contains(out2, "restored pnpm "+pinnedPNPM+" from cache"), "output: %s", out2)
	assert.Assert(t, !strings.Contains(out2, "installing pnpm@"), "expected no install on the second run; output: %s", out2)
	assert.Equal(t, pnpmVersion(t, binDir), pinnedPNPM)
}

// writePackageManager sets the packageManager field of appDir's package.json.
func writePackageManager(t *testing.T, appDir, spec string) {
	t.Helper()
	manifest := filepath.Join(appDir, "package.json")
	data, err := os.ReadFile(manifest) //nolint:gosec // fixture path is controlled by the test suite
	assert.NilError(t, err)
	body := strings.TrimSpace(string(data))
	assert.Assert(t, strings.HasPrefix(body, "{"), "unexpected package.json: %s", body)
	body = `{"packageManager": "` + spec + `", ` + strings.TrimPrefix(body, "{")
	//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
	assert.NilError(t, os.WriteFile(manifest, []byte(body), 0600))
}

// pnpmVersion returns what the pnpm linked into binDir reports, asked from
// outside any project so packageManager can't change the answer.
func pnpmVersion(t *testing.T, binDir string) string {
	t.Helper()
	cmd := exec.Command(filepath.Join(binDir, "pnpm"), "--version")
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	assert.NilError(t, err)
	return strings.TrimSpace(string(out))
}
