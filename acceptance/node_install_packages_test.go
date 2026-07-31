package nodeinstallpackages_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"
)

func TestCLI_UnknownFlag(t *testing.T) {
	_, err := run(t, nil, "install", "--no-such-flag")
	assert.Assert(t, err != nil, "expected non-zero exit for unknown flag")
}

func TestCLI_Install(t *testing.T) {
	tests := []struct {
		name string
		// pm, if set, is looked up on PATH; the test case is skipped if it's not found.
		pm string
		// text fixture path
		fixture string
		// extraLockfile writes an extra, empty lockfile into appDir - used to make
		// auto-detection ambiguous so an explicit --package-manager can be shown to win.
		extraLockfile string
		// appDirOverride, if set, is passed as --app-dir verbatim instead of a fixture dir
		// (e.g. to point at a path that doesn't exist).
		appDirOverride string
		// omitAppDir skips passing --app-dir at all.
		omitAppDir bool
		// args are extra flags appended after "install" (and --app-dir, unless omitted).
		args []string
		// pathOverride points PATH at an empty dir, so no package manager binary is found.
		pathOverride bool
		// expectedErr is a substring expected in output; empty means the command must succeed.
		expectedErr string
		// wantsCache is whether BASH_ENV should have been written, checked on success only.
		wantsCache bool
		// wantOutput is a substring expected in stdout regardless of outcome.
		wantOutput string
		// checkInstalled verifies node_modules/is-odd exists after a successful install.
		checkInstalled bool
	}{
		{
			name:           "install with npm",
			pm:             "npm",
			fixture:        "testdata/npm",
			wantsCache:     true,
			checkInstalled: true,
		},
		{
			name:           "install with yarn",
			pm:             "yarn",
			fixture:        "testdata/yarn",
			wantsCache:     true,
			checkInstalled: true,
		},
		{
			name:           "install with pnpm",
			pm:             "pnpm",
			fixture:        "testdata/pnpm",
			wantsCache:     true,
			checkInstalled: true,
		},
		{
			name:           "install with bun",
			pm:             "bun",
			fixture:        "testdata/bun",
			wantsCache:     true,
			checkInstalled: true,
		},
		{
			name:       "auto-detects package manager from lockfile",
			pm:         "npm",
			fixture:    "testdata/npm",
			wantsCache: true,
			wantOutput: "using package manager npm",
		},
		{
			name:        "invalid --package-manager",
			omitAppDir:  true,
			args:        []string{"--package-manager", "not-a-real-pm"},
			expectedErr: "package-manager",
		},
		{
			name:    "explicit --package-manager overrides auto-detection",
			pm:      "npm",
			fixture: "testdata/npm",
			// A yarn.lock is also present so auto-detection would pick yarn; the
			// explicit --package-manager flag should override that and use npm.
			extraLockfile: "yarn.lock",
			args:          []string{"--package-manager", "npm"},
			wantsCache:    true,
			wantOutput:    "using package manager npm",
		},
		{
			name:    "--no-with-cache skips BASH_ENV",
			pm:      "npm",
			fixture: "testdata/npm",
			args:    []string{"--package-manager", "npm", "--no-with-cache"},
		},
		{
			name:           "--app-dir does not exist",
			appDirOverride: "/nonexistent-dir-xyz/app",
			args:           []string{"--package-manager", "npm"},
			expectedErr:    "app-dir",
		},
		{
			name:         "package manager binary not found on PATH",
			fixture:      "testdata/npm",
			args:         []string{"--package-manager", "npm"},
			pathOverride: true,
			expectedErr:  "not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pm != "" {
				_, err := exec.LookPath(tc.pm)
				skip.If(t, err != nil, tc.pm+" not found on PATH")
			}

			var appDir string
			switch {
			case tc.appDirOverride != "":
				appDir = tc.appDirOverride
			case tc.omitAppDir:
				// leave appDir empty; no --app-dir flag is passed below.
			default:
				appDir = t.TempDir()
				if tc.fixture != "" {
					copyFixture(t, tc.fixture, appDir)
				}
			}
			if tc.extraLockfile != "" {
				assert.NilError(t, os.WriteFile(filepath.Join(appDir, tc.extraLockfile), []byte(""), 0600))
			}

			bashEnvFile := filepath.Join(t.TempDir(), "bash_env")
			env := map[string]string{"BASH_ENV": bashEnvFile}
			if tc.pathOverride {
				env["PATH"] = t.TempDir()
			}

			args := []string{"install"}
			if appDir != "" {
				args = append(args, "--app-dir", appDir)
			}
			args = append(args, tc.args...)

			out, err := run(t, env, args...)

			if tc.expectedErr == "" {
				assert.NilError(t, err, "output: %s", out)
			} else {
				assert.Assert(t, err != nil, "expected non-zero exit; output: %s", out)
				assert.Assert(t, strings.Contains(out, tc.expectedErr),
					"expected output to contain %q; got: %s", tc.expectedErr, out)
			}

			if tc.wantOutput != "" {
				assert.Assert(t, strings.Contains(out, tc.wantOutput),
					"expected output to contain %q; got: %s", tc.wantOutput, out)
			}

			if tc.checkInstalled {
				_, statErr := os.Stat(filepath.Join(appDir, "node_modules", "is-odd"))
				assert.NilError(t, statErr, "expected node_modules/is-odd to exist after install")
			}

			if tc.expectedErr == "" {
				_, statErr := os.Stat(bashEnvFile)
				if tc.wantsCache {
					assert.NilError(t, statErr, "expected BASH_ENV to be written")
				} else {
					assert.Assert(t, os.IsNotExist(statErr), "BASH_ENV should not be written")
				}
			}
		})
	}
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	assert.NilError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		assert.NilError(t, err)
		// #nosec G703: Test fixture paths are controlled within the test suite
		assert.NilError(t, os.WriteFile(filepath.Join(dst, e.Name()), data, 0600))
	}
}
