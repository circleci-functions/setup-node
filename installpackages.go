package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-node/node"
)

// installPackagesCmd installs a project's dependencies with its package
// manager, restoring and saving the package manager's download cache around
// the install so later steps start warm.
type installPackagesCmd struct {
	AppDir          string `help:"Directory containing package.json." default:"."`
	PackageManager  string `help:"Package manager to use: npm, yarn, pnpm, or bun. Auto-detected from lockfile if omitted." enum:"npm,yarn,pnpm,bun," default:""`
	OverrideCommand string `help:"Override the default CI install command (e.g. 'npm install --legacy-peer-deps')."`
	Cache           bool   `help:"Restore the package manager cache before installing and save it at the end. Set to false to disable caching." default:"true"`
}

func (c *installPackagesCmd) Run(ctx context.Context, client *sdk.Client) error {
	appDir, err := filepath.Abs(c.AppDir)
	if err != nil {
		return fmt.Errorf("resolve app-dir: %w", err)
	}
	info, err := os.Stat(appDir)
	if err != nil {
		return fmt.Errorf("app-dir %q: %w", appDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("app-dir %q is not a directory", appDir)
	}
	c.AppDir = appDir

	pm, err := c.resolvePackageManager()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-node: using package manager %s\n", pm)

	// cacheKey stays empty (skipping restore/save below) when the lockfile
	// can't be read, e.g. a bun.lockb-only repo or an --override-command run
	// with no lockfile at all.
	var cacheKey, cacheDir string
	if c.Cache {
		cacheDir, err = pm.CacheDir(ctx, appDir)
		if err != nil {
			return fmt.Errorf("resolve cache dirs: %w", err)
		}
		// Worth reading even on a pass: a later step reaches this by asking the
		// package manager the same question, so a wrong answer here is a cache
		// that never gets used.
		fmt.Fprintf(os.Stderr, "setup-node: %s cache directory %s\n", pm, cacheDir)

		if lockfile, err := os.ReadFile(filepath.Join(c.AppDir, pm.Lockfile())); err == nil { //nolint:gosec // lockfile name is one of a fixed set of known filenames
			cacheKey = fmt.Sprintf("setup-node-packages-v1-%s-%s-%s-%x", runtime.GOOS, runtime.GOARCH, pm, sha256.Sum256(lockfile))
			_, err := client.RestoreCache(ctx, []string{cacheKey}, sdk.WithStepName(fmt.Sprintf("Restoring %s cache", pm)))
			sdk.LogCacheErr("restore", err)
			// RestoreCache reports "success" even on a miss, so the only way to
			// tell a hit apart is to check whether it actually populated cacheDir.
			if entries, derr := os.ReadDir(cacheDir); err == nil && derr == nil && len(entries) > 0 {
				fmt.Fprintf(os.Stderr, "setup-node: restored %s from cache\n", cacheDir)
			}
		} else {
			fmt.Fprintf(os.Stderr, "setup-node: no %s found, skipping cache restore\n", pm.Lockfile())
		}
	}

	if err := pm.InstallPackages(ctx, c.AppDir, c.OverrideCommand); err != nil {
		return err
	}

	if c.Cache && cacheKey != "" {
		sdk.LogCacheErr("save", client.SaveCache(ctx, cacheKey, []string{cacheDir}, sdk.WithStepName(fmt.Sprintf("Saving %s cache", pm))))
	}

	return nil
}

// resolvePackageManager returns the package manager from the flag or by
// detecting the lockfile in AppDir.
func (c *installPackagesCmd) resolvePackageManager() (node.PackageManager, error) {
	if c.PackageManager != "" {
		return node.PackageManager(c.PackageManager), nil
	}
	pm, err := node.PackageManagerFromDir(c.AppDir)
	if err != nil {
		return "", fmt.Errorf("no lockfile found in %s; use --package-manager to specify one explicitly", c.AppDir)
	}
	return pm, nil
}
