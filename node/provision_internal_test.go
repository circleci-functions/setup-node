package node

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestPinnedVersion(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		pm       PackageManager
		want     string
	}{
		{name: "pinned", manifest: `{"packageManager": "pnpm@9.1.0"}`, pm: PNPM, want: "9.1.0"},
		{name: "integrity suffix dropped", manifest: `{"packageManager": "yarn@4.5.0+sha512.abc123"}`, pm: Yarn, want: "4.5.0"},
		{name: "another package manager", manifest: `{"packageManager": "yarn@4.5.0"}`, pm: PNPM, want: ""},
		{name: "no packageManager", manifest: `{"name": "app"}`, pm: PNPM, want: ""},
		{name: "no version", manifest: `{"packageManager": "pnpm"}`, pm: PNPM, want: ""},
		{name: "invalid JSON", manifest: `{`, pm: PNPM, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(tc.manifest), 0600))
			assert.Equal(t, pinnedVersion(dir, tc.pm), tc.want)
		})
	}

	t.Run("no package.json", func(t *testing.T) {
		assert.Equal(t, pinnedVersion(t.TempDir(), PNPM), "")
	})
}

func TestNPMPackage(t *testing.T) {
	tests := []struct {
		pm      PackageManager
		version string
		want    string
	}{
		{pm: PNPM, version: "9.1.0", want: "pnpm@9.1.0"},
		{pm: Bun, version: "1.1.38", want: "bun@1.1.38"},
		{pm: Yarn, version: "1.22.22", want: "yarn@1.22.22"},
		{pm: Yarn, version: "4.5.0", want: "@yarnpkg/cli-dist@4.5.0"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.pm.npmPackage(tc.version), tc.want)
		})
	}
}
