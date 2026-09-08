//go:build acceptance

package setupnode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestAcceptance_Install runs a real frozen install for each package manager
// against the registry and checks the packages landed. Whether a later step
// reaches the cache is TestAcceptance_CacheDirReuse's job.
func TestAcceptance_Install(t *testing.T) {
	for _, pm := range []string{"npm", "yarn", "pnpm", "bun"} {
		t.Run(pm, func(t *testing.T) {
			requirePM(t, pm)
			_, run := sandbox(t)
			appDir := copyFixture(t, pm)

			out, err := run(t, nil, "--app-dir", appDir, "--package-manager", pm)
			assert.NilError(t, err, "output: %s", out)

			_, statErr := os.Stat(filepath.Join(appDir, "node_modules", "is-odd"))
			assert.NilError(t, statErr, "expected node_modules/is-odd after install")
		})
	}
}

// TestAcceptance_Install_AutoDetectsFromLockfile verifies the package manager
// is picked from the lockfile when --package-manager is omitted.
func TestAcceptance_Install_AutoDetectsFromLockfile(t *testing.T) {
	requirePM(t, "npm")
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")

	out, err := run(t, nil, "--app-dir", appDir)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "using package manager npm"),
		"expected npm to be auto-detected; got: %s", out)
}

// TestAcceptance_Install_ExplicitOverridesAutoDetection verifies an explicit
// --package-manager wins over what the lockfiles would auto-detect. A yarn.lock
// is added alongside the npm fixture so auto-detection would pick yarn.
func TestAcceptance_Install_ExplicitOverridesAutoDetection(t *testing.T) {
	requirePM(t, "npm")
	_, run := sandbox(t)
	appDir := copyFixture(t, "npm")
	// #nosec G306: fixture file, not a secret
	assert.NilError(t, os.WriteFile(filepath.Join(appDir, "yarn.lock"), []byte(""), 0600))

	out, err := run(t, nil, "--app-dir", appDir, "--package-manager", "npm")
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "using package manager npm"),
		"expected the explicit --package-manager npm to win; got: %s", out)
}
