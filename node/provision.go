package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsureBinary makes sure pm's executable is on PATH before anything runs it,
// so a missing package manager fails with an error that says how to get it,
// rather than with whatever the first command to run it reports.
//
// A missing pnpm, yarn or bun that appDir's package.json pins with its
// packageManager field is installed with `npm install --global`, which puts
// it on PATH wherever setup-node pointed npm's global prefix. corepack is not
// used: Node.js 25 and later don't ship it, and the corepack in older releases
// can fail to verify downloads since npm rotated its signing keys.
func (pm PackageManager) EnsureBinary(ctx context.Context, appDir string) error {
	if _, err := exec.LookPath(pm.Binary()); err == nil {
		return nil
	}
	if pm == NPM {
		return fmt.Errorf("npm binary not found on PATH; npm comes with Node.js, which setup-node installs when run without a command")
	}
	version := pinnedVersion(appDir, pm)
	if version == "" {
		return fmt.Errorf("%[1]s binary not found on PATH; install it in an earlier step with `npm install --global %[1]s@<version>`, "+
			"or pin it in package.json with \"packageManager\": \"%[1]s@<version>\" for setup-node to install it", pm)
	}

	pkg := pm.npmPackage(version)
	fmt.Fprintf(os.Stderr, "setup-node: %s is not on PATH; installing %s, the version package.json's packageManager pins\n", pm, pkg)
	cmd := exec.CommandContext(ctx, "npm", "install", "--global", pkg)
	cmd.Dir = appDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install --global %s: %w", pkg, err)
	}
	if _, err := exec.LookPath(pm.Binary()); err != nil {
		return fmt.Errorf("installed %s with npm, but %s is still not on PATH; put npm's global bin directory (under `npm prefix --global`) on PATH", pkg, pm)
	}
	return nil
}

// pinnedVersion returns the version of pm that appDir's package.json pins with
// its packageManager field, or "" if it pins none, or pins another package
// manager. corepack's "+sha..." integrity suffix is dropped, since npm has no
// way to check it.
func pinnedVersion(appDir string, pm PackageManager) string {
	data, err := os.ReadFile(filepath.Join(appDir, "package.json")) //nolint:gosec // package.json in the directory the caller named
	if err != nil {
		return ""
	}
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	name, version, ok := strings.Cut(manifest.PackageManager, "@")
	if !ok || name != string(pm) {
		return ""
	}
	version, _, _ = strings.Cut(version, "+")
	return version
}

// npmPackage returns the npm package spec that installs version of pm. The
// yarn package on npm stops at Yarn 1; later releases are published as
// @yarnpkg/cli-dist.
func (pm PackageManager) npmPackage(version string) string {
	if major, _, _ := strings.Cut(version, "."); pm == Yarn && major != "1" {
		return "@yarnpkg/cli-dist@" + version
	}
	return string(pm) + "@" + version
}
