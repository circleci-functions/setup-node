//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// laterStepPath returns a PATH holding only the sandbox's ~/.local/bin and the
// system directories, so setup-node has to link into the sandbox rather than
// take over a Node.js already installed on the machine, and applies it and the
// sandbox's $HOME to this process for sdktest.LaterStep. It returns the PATH
// entry setup-node should link into, too.
func laterStepPath(t *testing.T, s *sdktest.Sandbox) (map[string]string, string) {
	t.Helper()
	binDir := filepath.Join(s.Home, ".local", "bin")
	assert.NilError(t, os.MkdirAll(binDir, 0750))
	path := strings.Join([]string{binDir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	for _, dir := range []string{"/usr/bin", "/bin"} {
		_, err := os.Stat(filepath.Join(dir, "node"))
		skip.If(t, err == nil, "%s/node would shadow the install", dir)
	}
	t.Setenv("PATH", path)
	t.Setenv("HOME", s.Home)
	return map[string]string{"PATH": path}, binDir
}

func TestAcceptance_InstallNode(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		files map[string]string
		want  string
	}{
		{name: "exact", args: []string{"--node-version", "22.12.0"}, want: "v22.12.0"},
		{name: "lts codename", args: []string{"--node-version", "lts/iron"}, want: "v20."},
		{name: "nvmrc", files: map[string]string{".nvmrc": "lts/jod\n"}, want: "v22."},
		{name: "package.json", args: []string{"--node-version-file", "package.json"}, files: map[string]string{"package.json": `{"engines":{"node":"^24"}}`}, want: "v24."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := sandbox(t)
			env, binDir := laterStepPath(t, s)
			dir := t.TempDir()
			for name, content := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
			}

			r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, env)), runner.WithCmdDir(dir))
			_, err := r.Run(tc.args...)
			out := r.StdoutString() + r.StderrString()
			assert.NilError(t, err, "output: %s", out)
			assert.Assert(t, strings.Contains(out, "linked into "+binDir), "output: %s", out)

			got, err := sdktest.LaterStep(t, "node", "--version")
			assert.NilError(t, err, got)
			assert.Assert(t, strings.HasPrefix(strings.TrimSpace(got), tc.want), "node --version: %s", got)

			for _, tool := range []string{"npm", "npx"} {
				got, err := sdktest.LaterStep(t, tool, "--version")
				assert.NilError(t, err, "%s: %s", tool, got)
			}
		})
	}
}

// TestAcceptance_InstallNode_CacheRoundTrip restores the install from the cache
// on a simulated fresh executor. The cache nonce keeps the cold install from
// restoring an entry an earlier pipeline saved under the same write-once key.
func TestAcceptance_InstallNode_CacheRoundTrip(t *testing.T) {
	s, run := sandbox(t)
	env, _ := laterStepPath(t, s)
	args := []string{"--node-version", "22.12.0", "--cache-nonce", s.CacheKeyNonce}

	out, err := run(t, env, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"), "output: %s", out)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-node")))
	out, err = run(t, env, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored Node.js v22.12.0 from cache"), "output: %s", out)

	got, err := sdktest.LaterStep(t, "node", "--version")
	assert.NilError(t, err, got)
	assert.Equal(t, strings.TrimSpace(got), "v22.12.0")
}

func TestAcceptance_InstallNode_GlobalInstallOnPath(t *testing.T) {
	s, run := sandbox(t)
	env, _ := laterStepPath(t, s)

	out, err := run(t, env, "--node-version", "22.12.0")
	assert.NilError(t, err, "output: %s", out)

	npm := exec.Command("npm", "install", "--global", "semver@7.6.3")
	npm.Env = s.CmdEnv(os.Environ())
	installOut, err := npm.CombinedOutput()
	assert.NilError(t, err, "npm install --global: %s", installOut)

	got, err := sdktest.LaterStep(t, "semver", "1.2.3")
	assert.NilError(t, err, got)
	assert.Equal(t, strings.TrimSpace(got), "1.2.3")
}
