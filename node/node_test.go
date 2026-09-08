package node_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

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

	t.Run("falls back to ~/.npm when env var unset", func(t *testing.T) {
		t.Setenv("NPM_CONFIG_CACHE", "")

		dir, err := node.NPM.CacheDir(ctx, t.TempDir())
		assert.NilError(t, err)
		assert.Assert(t, strings.HasSuffix(dir, ".npm"), "expected path ending in .npm, got %s", dir)
	})
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
