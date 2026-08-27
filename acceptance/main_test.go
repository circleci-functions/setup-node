//go:build acceptance

// Package setupnode_test exercises setup-node against real infrastructure: a
// real package-manager install that reaches the registry, and a real CircleCI
// task-agent cache when running inside a job. Tagged out of `task test` because
// it needs network and the package managers on PATH. The CLI contract itself is
// covered hermetically by clitest/.
package setupnode_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	p := compiler.NewParallel(1)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &binaryPath,
		Name:   "setup-node",
		Target: "..",
		Source: ".",
	})

	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// runFunc runs the compiled binary with extraEnv applied on top of the
// sandbox's environment, so entries in extraEnv take precedence.
type runFunc func(t *testing.T, extraEnv map[string]string, args ...string) (string, error)

// sandbox returns a sandbox plus a run function bound to its env.
//
// The cache is CircleCI's own when the caller asks for it with
// TOOLKIT_CACHE_MODE=live — which the CI job does, so the round-trips there are
// exact — and in-memory otherwise, so the same suite runs on a laptop.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, extraEnv)))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}

// sandboxEnv returns the sandbox's environment with extra applied on top. The
// package managers resolve their cache directories under $HOME, which the
// sandbox points at a temp dir, so an install never touches the real one.
func sandboxEnv(s *sdktest.Sandbox, extra map[string]string) map[string]string {
	env := s.Env()
	maps.Copy(env, extra)
	return env
}

// requirePM skips the test if pm is not on PATH. CI installs all four; a laptop
// may have only some, so the suite still runs against whatever is present.
func requirePM(t *testing.T, pm string) {
	t.Helper()
	_, err := exec.LookPath(pm)
	skip.If(t, err != nil, pm+" not found on PATH")
}

// fixtureDir resolves a checked-in package-manager fixture to an absolute path.
func fixtureDir(t *testing.T, pm string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "internal", "testing", "testdata", pm))
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	return abs
}

// copyFixture copies the fixture for pm into a fresh temp app dir and returns it.
func copyFixture(t *testing.T, pm string) string {
	t.Helper()
	dst := t.TempDir()
	copyFixtureInto(t, pm, dst)
	return dst
}

// copyFixtureInto copies the fixture for pm into dst.
func copyFixtureInto(t *testing.T, pm, dst string) {
	t.Helper()
	src := fixtureDir(t, pm)
	entries, err := os.ReadDir(src)
	assert.NilError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name())) //nolint:gosec // fixture path is controlled by the test suite
		assert.NilError(t, err)
		//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
		assert.NilError(t, os.WriteFile(filepath.Join(dst, e.Name()), data, 0600))
	}
}
