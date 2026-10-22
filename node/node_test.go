package node_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/setup-node/node"
)

// --- FromDir ---

func TestFromDir(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantPM  node.PackageManager
		wantErr string
	}{
		{name: "package-lock.json", files: []string{"package-lock.json"}, wantPM: node.NPM},
		{name: "npm-shrinkwrap.json", files: []string{"npm-shrinkwrap.json"}, wantPM: node.NPM},
		{name: "yarn.lock", files: []string{"yarn.lock"}, wantPM: node.Yarn},
		{name: "pnpm-lock.yaml", files: []string{"pnpm-lock.yaml"}, wantPM: node.PNPM},
		{name: "bun.lockb", files: []string{"bun.lockb"}, wantPM: node.Bun},
		{name: "bun.lock", files: []string{"bun.lock"}, wantPM: node.Bun},
		{name: "no lockfile", files: nil, wantErr: "no lockfile found"},
		{name: "bun wins over npm", files: []string{"bun.lockb", "package-lock.json"}, wantPM: node.Bun},
		{name: "pnpm wins over yarn", files: []string{"pnpm-lock.yaml", "yarn.lock"}, wantPM: node.PNPM},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				assert.NilError(t, os.WriteFile(filepath.Join(dir, f), []byte(""), 0600))
			}

			pm, err := node.PackageManagerFromDir(dir)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, pm, tc.wantPM)
		})
	}
}

// --- Lockfile ---

func TestLockfile(t *testing.T) {
	tests := []struct {
		pm   node.PackageManager
		want string
	}{
		{node.NPM, "package-lock.json"},
		{node.Yarn, "yarn.lock"},
		{node.PNPM, "pnpm-lock.yaml"},
		{node.Bun, "bun.lock"},
	}

	for _, tc := range tests {
		t.Run(string(tc.pm), func(t *testing.T) {
			assert.Equal(t, tc.pm.Lockfile(), tc.want)
		})
	}
}

// --- CacheDirs ---

func TestCacheDirs_NPM(t *testing.T) {
	ctx := context.Background()

	t.Run("uses NPM_CONFIG_CACHE env var", func(t *testing.T) {
		t.Setenv("NPM_CONFIG_CACHE", "/test/custom-npm-cache")

		dir, err := node.NPM.CacheDir(ctx, t.TempDir())
		assert.NilError(t, err)
		assert.Equal(t, dir, "/test/custom-npm-cache")
	})

	t.Run("asks npm", func(t *testing.T) {
		skip.If(t, runtime.GOOS == "windows", "the npm stub is a shell script")
		stub := t.TempDir()
		//nolint:gosec // test stub with fixed content; 0700 so it is executable
		assert.NilError(t, os.WriteFile(filepath.Join(stub, "npm"), []byte("#!/bin/sh\necho /from/npm\n"), 0700))
		t.Setenv("PATH", stub+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("NPM_CONFIG_CACHE", "/test/custom-npm-cache")

		dir, err := node.NPM.CacheDir(ctx, t.TempDir())
		assert.NilError(t, err)
		assert.Equal(t, dir, "/from/npm")
	})

	t.Run("falls back to ~/.npm when env var unset", func(t *testing.T) {
		t.Setenv("NPM_CONFIG_CACHE", "")

		dir, err := node.NPM.CacheDir(ctx, t.TempDir())
		assert.NilError(t, err)
		assert.Assert(t, strings.HasSuffix(dir, ".npm"), "expected path ending in .npm, got %s", dir)
	})
}

// Yarn 2 and later have no `yarn cache dir`, so their cache comes from
// `yarn config get cacheFolder`. Yarn 1 answers that with "undefined", which
// is not a directory.
func TestCacheDirs_Yarn(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the yarn stub is a shell script")

	tests := []struct {
		name   string
		script string
		want   func(home string) string
	}{
		{
			name:   "yarn 1",
			script: "#!/bin/sh\nif [ \"$1\" = cache ]; then echo /from/yarn-cache-dir; exit 0; fi\necho undefined\n",
			want:   func(string) string { return "/from/yarn-cache-dir" },
		},
		{
			name:   "yarn 2 and later",
			script: "#!/bin/sh\nif [ \"$1\" = cache ]; then echo 'Unknown Syntax Error: Command not found'; exit 1; fi\necho /from/yarn-config\n",
			want:   func(string) string { return "/from/yarn-config" },
		},
		{
			name:   "neither answers",
			script: "#!/bin/sh\nif [ \"$1\" = cache ]; then exit 1; fi\necho undefined\n",
			want:   func(home string) string { return filepath.Join(home, ".cache", "yarn") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := t.TempDir()
			//nolint:gosec // test stub with fixed content; 0700 so it is executable
			assert.NilError(t, os.WriteFile(filepath.Join(stub, "yarn"), []byte(tc.script), 0700))
			t.Setenv("PATH", stub)
			home := t.TempDir()
			t.Setenv("HOME", home)

			dir, err := node.Yarn.CacheDir(context.Background(), t.TempDir())
			assert.NilError(t, err)
			assert.Equal(t, dir, tc.want(home))
		})
	}
}

// bun resolves its cache from $BUN_INSTALL_CACHE_DIR, then $XDG_CACHE_HOME,
// then $HOME — so a path built from $HOME alone is wrong whenever
// XDG_CACHE_HOME is set, which is what a test sandbox does.
func TestCacheDirs_Bun_HonoursXDGCacheHome(t *testing.T) {
	if _, err := exec.LookPath("bun"); err == nil {
		t.Skip("bun is installed, so it answers for itself; this covers the fallback")
	}

	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)

	dir, err := node.Bun.CacheDir(context.Background(), t.TempDir())
	assert.NilError(t, err)
	assert.Equal(t, dir, filepath.Join(xdg, ".bun", "install", "cache"))
}

// pnpm says why it can't report its store on stderr, which the error has to
// carry: on its own, the exit status tells the reader nothing.
func TestCacheDirs_PNPM_ErrorCarriesStderr(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the pnpm stub is a shell script")
	stub := t.TempDir()
	script := "#!/bin/sh\necho ERR_PNPM_STORE_BROKEN the store is unreadable >&2\nexit 1\n"
	//nolint:gosec // test stub with fixed content; 0700 so it is executable
	assert.NilError(t, os.WriteFile(filepath.Join(stub, "pnpm"), []byte(script), 0700))
	t.Setenv("PATH", stub)

	_, err := node.PNPM.CacheDir(context.Background(), t.TempDir())
	assert.ErrorContains(t, err, "pnpm store path: exit status 1: ERR_PNPM_STORE_BROKEN the store is unreadable")
}
