package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

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
	// with no lockfile at all, or when the package manager can't report its
	// version.
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

		lockfile, err := os.ReadFile(filepath.Join(c.AppDir, pm.Lockfile())) //nolint:gosec // lockfile name is one of a fixed set of known filenames
		if err != nil {
			fmt.Fprintf(os.Stderr, "setup-node: no %s found, skipping cache restore\n", pm.Lockfile())
		} else if pmVersion, err := pm.Version(ctx, appDir); err != nil {
			fmt.Fprintf(os.Stderr, "setup-node: warning: %v; skipping cache restore\n", err)
		} else {
			var fallbackKey string
			cacheKey, fallbackKey = packagesCacheKeys(pm, pmVersion, cacheDir, lockfile)
			before := entryNames(cacheDir)
			_, err := client.RestoreCache(ctx, []string{cacheKey, fallbackKey}, sdk.WithStepName(fmt.Sprintf("Restoring %s cache", pm)))
			sdk.LogCacheErr("restore", err)
			// RestoreCache reports "success" even on a miss, so the only way to
			// tell a hit apart is to check whether it actually added to cacheDir.
			if err == nil && addedEntries(cacheDir, before) {
				fmt.Fprintf(os.Stderr, "setup-node: restored %s from cache\n", cacheDir)
			}
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

// packagesCacheKeys returns the key to save pm's cache under, and a prefix of
// it to fall back to when nothing is saved under that key yet. The prefix
// matches the newest cache saved for any lockfile, so a changed lockfile
// downloads only what changed. Everything that can make a cache unreadable to
// this job goes before the lockfile hash, so the fallback never crosses it: the
// package manager and its version, which set the cache's layout, and the cache
// directory, since a cache restored anywhere else is never read.
func packagesCacheKeys(pm node.PackageManager, pmVersion, cacheDir string, lockfile []byte) (key, fallback string) {
	dirHash := sha256.Sum256([]byte(cacheDir))
	fallback = fmt.Sprintf("setup-node-packages-v3-%s-%s-%s-%x-", sdk.OSArch(), pm, pmVersion, dirHash[:4])
	return fmt.Sprintf("%s%x", fallback, sha256.Sum256(lockfile)), fallback
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

// entryNames returns the names of dir's entries, empty if it cannot be read.
func entryNames(dir string) map[string]bool {
	names := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

// addedEntries reports whether dir now holds an entry that before did not.
func addedEntries(dir string, before map[string]bool) bool {
	for name := range entryNames(dir) {
		if !before[name] {
			return true
		}
	}
	return false
}
