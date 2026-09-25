//go:build acceptance

package setupnode_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// acceptanceVersion returns the Node.js version spec to install:
// SETUP_NODE_ACCEPTANCE_VERSION is set in CI from the pipeline's node-version
// parameter, lts/* locally.
func acceptanceVersion() string {
	if v := os.Getenv("SETUP_NODE_ACCEPTANCE_VERSION"); v != "" {
		return v
	}
	return "lts/*"
}

var exactVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// TestAcceptance_InstallsOntoTheRealPath is the one test that lets setup-node
// choose where node is linked, rather than telling it with --bin-dir. That
// choice depends on the executor, and a sandboxed suite never reaches it.
//
// It changes the machine it runs on, up to taking over a Node.js already
// there, so it runs only where that is disposable: a CircleCI job, or a
// container the caller vouches for with $SETUP_NODE_DISPOSABLE (see
// `task smoke-local`).
func TestAcceptance_InstallsOntoTheRealPath(t *testing.T) {
	if os.Getenv("SETUP_NODE_DISPOSABLE") == "" {
		sdktest.SkipUnlessInJob(t)
	}

	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	spec := acceptanceVersion()

	// The job's own environment, which is what a step gets: $HOME and PATH
	// decide which directory is picked. $BASH_ENV is redirected because CI
	// writes to the job's own.
	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		opts := []runner.Opt{
			runner.WithUnsafeOSEnvVars(),
			runner.WithUnsafeAdditionalEnvVars(map[string]string{"BASH_ENV": bashEnv}),
		}
		if dir != "" {
			opts = append(opts, runner.WithCmdDir(dir))
		}
		r := runner.New(t, binaryPath, opts...)
		_, err := r.Run(args...)
		out := r.StdoutString() + r.StderrString()
		assert.NilError(t, err, "got: %s", out)
		return out
	}

	installArgs := []string{
		"--node-version", spec,
		"--install-dir", s.InstallDir,
		// Isolates this run's cache entry, so the cold install below cannot
		// restore one an earlier pipeline saved.
		"--cache-nonce", s.CacheKeyNonce,
	}
	out := run(t, "", installArgs...)
	assert.Assert(t, !strings.Contains(out, "from cache"),
		"a per-run nonce must miss the cache, so this install downloads for real; got: %s", out)

	version := reported(t, out, `to Node\.js (v[0-9.]+)`)
	if exactVersion.MatchString(spec) {
		assert.Equal(t, version, "v"+strings.TrimPrefix(spec, "v"))
	}
	// Worth reading even when this passes: which directory an executor offered
	// is the thing that goes wrong.
	linked := reported(t, out, `linked into (.+)$`)
	t.Logf("setup-node installed Node.js %s and linked it into %s", version, linked)

	// The install dir goes away with the sandbox, so the links must too, or a
	// dangling node follows this test into the rest of the job.
	removeLinks(t, linked)

	t.Run("ResolvesByNameInALaterStep", func(t *testing.T) {
		got, err := sdktest.LaterStep(t, "node", "--version")
		assert.NilError(t, err, "a later step could not resolve node: %s", got)
		assert.Equal(t, strings.TrimSpace(got), version)
		for _, tool := range []string{"npm", "npx"} {
			got, err := sdktest.LaterStep(t, tool, "--version")
			assert.NilError(t, err, "a later step could not resolve %s: %s", tool, got)
		}
	})

	// The shell $BASH_ENV never reached, and where npm.ps1 beats npm.cmd.
	if runtime.GOOS == "windows" {
		t.Run("ResolvesInPowerShellToo", func(t *testing.T) {
			got, err := sdktest.LaterStep(t, "powershell.exe", "-Command", "node --version; npm --version")
			assert.NilError(t, err, "PowerShell could not resolve node and npm: %s", got)
			assert.Assert(t, strings.Contains(got, version), "PowerShell resolved a different node: %s", got)
		})
	}

	t.Run("GlobalInstallsResolveInALaterStep", func(t *testing.T) {
		got, err := sdktest.LaterStep(t, "npm", "install", "--global", "semver@7.6.3")
		assert.NilError(t, err, "npm install --global: %s", got)
		got, err = sdktest.LaterStep(t, "semver", "1.2.3")
		assert.NilError(t, err, "a later step could not resolve what npm install --global installed: %s", got)
		assert.Equal(t, strings.TrimSpace(got), "1.2.3")
	})

	t.Run("InstallsPackagesWithTheInstalledNpm", func(t *testing.T) {
		skip.If(t, !sdk.InCircleCIJob(), "the cache round trip needs a CircleCI job's cache")
		app := t.TempDir()
		assert.NilError(t, os.WriteFile(filepath.Join(app, "package.json"),
			[]byte(`{"dependencies": {"is-odd": "^3.0.1"}}`), 0600))
		npm(t, app, "install", "--package-lock-only")

		out := run(t, app, "install-packages", "--package-manager", "npm")
		t.Logf("first install: %s", out)
		assertInstalled(t, app)

		// Simulate a fresh executor: npm's cache is the executor's stable
		// ~/.npm, so the second install has to restore it from the live cache.
		assert.NilError(t, os.RemoveAll(strings.TrimSpace(npm(t, app, "config", "get", "cache"))))
		assert.NilError(t, os.RemoveAll(filepath.Join(app, "node_modules")))

		out = run(t, app, "install-packages", "--package-manager", "npm")
		assert.Assert(t, strings.Contains(out, "from cache"), "expected the npm cache to be restored; got: %s", out)
		assertInstalled(t, app)
	})

	t.Run("RestoresFromTheCacheOnAFreshExecutor", func(t *testing.T) {
		skip.If(t, !sdk.InCircleCIJob(), "the cache round trip needs a CircleCI job's cache")
		assert.NilError(t, os.RemoveAll(s.InstallDir))
		out := run(t, "", installArgs...)
		assert.Assert(t, strings.Contains(out, "restored Node.js "+version+" from cache"),
			"expected the install saved above to be restored; got: %s", out)
		got, err := sdktest.LaterStep(t, "node", "--version")
		assert.NilError(t, err, got)
		assert.Equal(t, strings.TrimSpace(got), version)
	})

	t.Run("NothingIsExportedToAShell", func(t *testing.T) {
		content, err := os.ReadFile(bashEnv) //nolint:gosec // a path this test chose
		if os.IsNotExist(err) {
			return
		}
		assert.NilError(t, err)
		assert.Assert(t, !strings.Contains(string(content), "export PATH="),
			"node goes on PATH by being linked there, so nothing should be exported: %s", content)
	})

	t.Run("NotIntoADirectoryOwnedByAnotherTool", func(t *testing.T) {
		for _, part := range strings.Split(filepath.ToSlash(linked), "/") {
			assert.Assert(t, part != "shims" && part != ".nvm",
				"a version manager rewrites its own directories, taking the link with it: %s", linked)
		}
	})
}

// npm runs the npm on PATH in dir and returns its stdout.
func npm(t *testing.T, dir string, args ...string) string {
	t.Helper()
	bin, err := exec.LookPath("npm")
	assert.NilError(t, err)
	var stderr strings.Builder
	cmd := exec.CommandContext(t.Context(), bin, args...)
	cmd.Dir = dir
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	assert.NilError(t, err, "npm %s: %s%s", strings.Join(args, " "), out, stderr.String())
	return string(out)
}

// removeLinks takes node, its tools and the package the test installed
// globally back off PATH at the end of the test.
func removeLinks(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range []string{"node", "npm", "npx", "corepack", "semver"} {
			for _, ext := range []string{"", ".exe", ".bat", ".cmd", ".ps1"} {
				_ = os.Remove(filepath.Join(dir, name+ext))
			}
		}
	})
}

// reported returns the first submatch of pattern in setup-node's output.
func reported(t *testing.T, out, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	for line := range strings.SplitSeq(out, "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	t.Fatalf("setup-node did not report %q:\n%s", pattern, out)
	return ""
}
