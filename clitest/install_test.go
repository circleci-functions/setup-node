package clitest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"

	"github.com/circleci-functions/setup-node/internal/testing/fakedist"
	"github.com/circleci-functions/setup-node/nodedist"
)

var releases = []fakedist.Release{
	{Version: "22.1.0", LTS: "Jod"},
	{Version: "22.2.0", LTS: "Jod"},
	{Version: "24.0.0", LTS: "Krypton"},
	{Version: "25.0.0"},
}

// installEnv starts a fake dist server and returns it with a bin dir to link
// into. The fake builds' node is a shell script, so these tests need a Unix.
func installEnv(t *testing.T) (*fakedist.Server, string) {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake Node.js builds are shell scripts")
	return fakedist.New(t, releases...), t.TempDir()
}

// runIn runs the binary in s's environment with dir as its working directory.
func runIn(t *testing.T, s *sdktest.Sandbox, dir string, extraEnv map[string]string, args ...string) (string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, extraEnv)), runner.WithCmdDir(dir))
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

func nodeVersion(t *testing.T, binDir string) string {
	t.Helper()
	out, err := exec.Command(filepath.Join(binDir, "node"), "--version").Output()
	assert.NilError(t, err)
	return strings.TrimSpace(string(out))
}

func TestCLI_Install_ResolvesVersion(t *testing.T) {
	appDir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "package.json"), []byte(`{"engines":{"node":">=22 <24"}}`), 0600))

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "major", args: []string{"--node-version", "22"}, want: "v22.2.0"},
		{name: "exact", args: []string{"--node-version", "22.1.0"}, want: "v22.1.0"},
		{name: "lts", args: []string{"--node-version", "lts/*"}, want: "v24.0.0"},
		{name: "latest", args: []string{"--node-version", "latest"}, want: "v25.0.0"},
		{name: "version file", args: []string{"--node-version-file", filepath.Join(appDir, "package.json")}, want: "v22.2.0"},
		{name: "explicit install command", args: []string{"install", "--node-version", "24"}, want: "v24.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			_, run := sandbox(t)

			out, err := run(t, nil, append(tc.args, "--mirror", srv.URL, "--bin-dir", binDir)...)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, nodeVersion(t, binDir), tc.want)
		})
	}
}

func TestCLI_Install_VersionFlagWinsOverFile(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	nvmrc := filepath.Join(t.TempDir(), ".nvmrc")
	assert.NilError(t, os.WriteFile(nvmrc, []byte("24\n"), 0600))

	out, err := run(t, nil, "--node-version", "22", "--node-version-file", nvmrc, "--mirror", srv.URL, "--bin-dir", binDir)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, nodeVersion(t, binDir), "v22.2.0")
	assert.Assert(t, strings.Contains(out, "using --node-version"), "expected a warning; got: %s", out)
}

func TestCLI_Install_DefaultVersion(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "nvmrc", files: map[string]string{".nvmrc": "22.1.0\n"}, want: "v22.1.0"},
		{name: "node-version", files: map[string]string{".node-version": "22\n"}, want: "v22.2.0"},
		{name: "nvmrc first", files: map[string]string{".nvmrc": "25", ".node-version": "22"}, want: "v25.0.0"},
		{name: "newest lts", want: "v24.0.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			s := sdktest.NewSandbox(t)
			dir := t.TempDir()
			for name, content := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
			}

			out, err := runIn(t, s, dir, nil, "--mirror", srv.URL, "--bin-dir", binDir)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, nodeVersion(t, binDir), tc.want)
		})
	}
}

func TestCLI_Install_LinksTools(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)

	out, err := run(t, nil, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir)
	assert.NilError(t, err, "output: %s", out)
	for _, tool := range []string{"node", "npm", "npx", "corepack"} {
		_, err := os.Stat(filepath.Join(binDir, tool))
		assert.NilError(t, err, "%s not linked", tool)
	}
}

func TestCLI_Install_Cache(t *testing.T) {
	platform, err := nodedist.Host()
	assert.NilError(t, err)
	wantKey := "setup-node-toolchain-v1-" + runtime.GOOS + "-" + runtime.GOARCH + "-v22.2.0"

	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir)
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, len(rec.restores), 1)
	assert.DeepEqual(t, rec.restores[0].Keys, []string{wantKey})
	assert.Equal(t, rec.restores[0].Name, "Restoring Node.js v22.2.0")
	assert.Equal(t, len(rec.saves), 1)
	assert.Equal(t, rec.saves[0].Key, wantKey)
	assert.DeepEqual(t, rec.saves[0].Paths, []string{filepath.Join(s.Home, ".cache", "setup-node", "v22.2.0-"+platform.String())})
	assert.Equal(t, rec.saves[0].Name, "Saving Node.js v22.2.0")
}

func TestCLI_Install_CacheNonce(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir, "--cache-nonce", "run-42")
	assert.NilError(t, err, "output: %s", out)

	wantKey := "setup-node-toolchain-v1-" + runtime.GOOS + "-" + runtime.GOARCH + "-v22.2.0-run-42"
	assert.DeepEqual(t, rec.restores[0].Keys, []string{wantKey})
	assert.Equal(t, rec.saves[0].Key, wantKey)
}

func TestCLI_Install_CacheDisabled(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)

	out, err := run(t, map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir, "--cache=false")
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(rec.restores), 0)
	assert.Equal(t, len(rec.saves), 0)
	assert.Equal(t, srv.Downloads(), 1)
}

func TestCLI_Install_RestoresFromCache(t *testing.T) {
	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	args := []string{"--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir}

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, srv.Downloads(), 1)

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-node")))
	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, srv.Downloads(), 1, "expected the second run to restore rather than download")
	assert.Assert(t, strings.Contains(out, "restored Node.js v22.2.0 from cache"), "output: %s", out)
	assert.Equal(t, nodeVersion(t, binDir), "v22.2.0")
}

func TestCLI_Install_RerunReusesInstall(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)
	rec, pluginURL := newCacheRecorder(t)
	env := map[string]string{"CIRCLECI_PLUGIN_URL": pluginURL}
	args := []string{"--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir}

	for range 2 {
		out, err := run(t, env, args...)
		assert.NilError(t, err, "output: %s", out)
	}
	assert.Equal(t, srv.Downloads(), 1)
	assert.Equal(t, len(rec.restores), 1, "a rerun should not touch the cache")
}

func TestCLI_Install_ExactVersionNeedsNoIndexWhenAvailable(t *testing.T) {
	srv, binDir := installEnv(t)
	s, run := sandbox(t)
	args := []string{"--node-version", "22.2.0", "--mirror", srv.URL, "--bin-dir", binDir}

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, indexFetches(srv) == 1, "expected the first run to check the release against the index")
	served := len(srv.Requests())

	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(srv.Requests()), served, "expected a rerun to use the install without the server")

	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".cache", "setup-node")))
	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored Node.js v22.2.0 from cache"), "output: %s", out)
	assert.Equal(t, len(srv.Requests()), served, "expected a cache restore without the server")
	assert.Equal(t, nodeVersion(t, binDir), "v22.2.0")
}

func indexFetches(srv *fakedist.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.URL.Path == "/index.json" {
			n++
		}
	}
	return n
}

func TestCLI_Install_MirrorTokenFromEnv(t *testing.T) {
	srv, binDir := installEnv(t)
	_, run := sandbox(t)

	out, err := run(t, map[string]string{"NODE_MIRROR_TOKEN": "Bearer s3cret"}, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir)
	assert.NilError(t, err, "output: %s", out)
	for _, r := range srv.Requests() {
		assert.Equal(t, r.Header.Get("Authorization"), "Bearer s3cret", r.URL.Path)
	}
}

func TestCLI_Install_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		corrupt bool
		wantErr string
	}{
		{name: "no matching version", args: []string{"--node-version", "19"}, wantErr: `no release matches "19"`},
		{name: "invalid version", args: []string{"--node-version", "banana"}, wantErr: `invalid version spec "banana"`},
		{name: "missing version file", args: []string{"--node-version-file", "/nonexistent/.nvmrc"}, wantErr: "node-version-file"},
		{name: "checksum mismatch", args: []string{"--node-version", "22"}, corrupt: true, wantErr: "checksum mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, binDir := installEnv(t)
			if tc.corrupt {
				srv.Corrupt()
			}
			_, run := sandbox(t)

			out, err := run(t, nil, append(tc.args, "--mirror", srv.URL, "--bin-dir", binDir)...)
			assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
			assert.Assert(t, strings.Contains(out, tc.wantErr), "expected %q in output: %s", tc.wantErr, out)
		})
	}
}

func TestCLI_Install_SetsGlobalPrefix(t *testing.T) {
	srv, _ := installEnv(t)
	s, run := sandbox(t)
	binDir := filepath.Join(s.Home, ".local", "bin")
	platform, err := nodedist.Host()
	assert.NilError(t, err)

	out, err := run(t, nil, "--node-version", "22", "--mirror", srv.URL, "--bin-dir", binDir)
	assert.NilError(t, err, "output: %s", out)

	npmrc, err := os.ReadFile(filepath.Join(s.Home, ".cache", "setup-node", "v22.2.0-"+platform.String(), "lib", "node_modules", "npm", "npmrc"))
	assert.NilError(t, err)
	assert.Equal(t, string(npmrc), "prefix="+filepath.Join(s.Home, ".local")+"\n")
}
