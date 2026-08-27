// Package clitest_test exercises the setup-node CLI contract end to end: flag
// parsing, error paths, and how the command drives the cache SDK. It is
// hermetic — no real package manager and no network — because every case here
// either fails before an install runs or replaces the install with a trivial
// --override-command. Real package-manager installs and real-infra cache
// round-trips are covered by acceptance/.
package clitest_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

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

// sandbox returns a sandbox plus a run function bound to its env. It is the only
// way to run the binary in this package, so no test can install into the
// developer's real home directory or reach a cache it did not set up.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()

	s := sdktest.NewSandbox(t)
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(sandboxEnv(s, extraEnv)))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}

// sandboxEnv returns the sandbox's environment with extra applied on top.
func sandboxEnv(s *sdktest.Sandbox, extra map[string]string) map[string]string {
	env := s.Env()
	maps.Copy(env, extra)
	return env
}

// fixtureDir resolves a checked-in package-manager fixture (a package.json plus
// its lockfile) to an absolute path. The binary runs with its own temp dir as
// cwd, so a relative path would resolve against the wrong directory.
func fixtureDir(t *testing.T, pm string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "internal", "testing", "testdata", pm))
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}
	return abs
}

// stubPMPath returns a PATH value with a no-op npm prepended to the inherited
// PATH. It satisfies the CLI's "is the package manager on PATH" check without a
// real package manager, so cache-contract tests stay hermetic: the stub exits 0
// without installing anything, and the registry is never reached. The real PATH
// is kept on the end so `go` and coreutils resolve.
func stubPMPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0700))
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// copyFixture copies the fixture for pm into a fresh temp app dir and returns
// it. pm is a parameter, rather than hardcoded, to keep the helper identical to
// acceptance/'s so the two packages read the same way.
func copyFixture(t *testing.T, pm string) string { //nolint:unparam // parallel with acceptance/'s copyFixture
	t.Helper()
	dst := t.TempDir()
	src := fixtureDir(t, pm)
	entries, err := os.ReadDir(src)
	assert.NilError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name())) //nolint:gosec // fixture path is controlled by the test suite
		assert.NilError(t, err)
		//nolint:gosec // fixture path is controlled by the test suite; fixtures are not secrets
		assert.NilError(t, os.WriteFile(filepath.Join(dst, e.Name()), data, 0600))
	}
	return dst
}
