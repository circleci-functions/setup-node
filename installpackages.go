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
	AppDir          string `help:"Directory containing package.json and the lockfile." default:"."`
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
	if err := pm.EnsureBinary(ctx, c.AppDir); err != nil {
		return err
	}

	var cacheKey, cacheDir string
	if c.Cache {
		cacheKey, cacheDir = c.restoreCache(ctx, client, pm)
	}

	if err := pm.InstallPackages(ctx, c.AppDir, c.OverrideCommand); err != nil {
		return err
	}

	if cacheKey != "" {
		sdk.LogCacheErr("save", client.SaveCache(ctx, cacheKey, []string{cacheDir}, sdk.WithStepName(fmt.Sprintf("Saving %s cache", pm))))
	}

	return nil
}

// restoreCache restores pm's download cache, and returns the key to save it
// back under and the directory to save. The key is empty, so nothing is
// restored or saved, when pm can't report its cache directory or its version,
// or the lockfile can't be read (e.g. a bun.lockb-only repo, or an
// --override-command run with no lockfile at all). Each of those is a warning
// rather than an error, so the install still runs.
func (c *installPackagesCmd) restoreCache(ctx context.Context, client *sdk.Client, pm node.PackageManager) (key, dir string) {
	dir, err := pm.CacheDir(ctx, c.AppDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-node: warning: %v; skipping cache restore\n", err)
		return "", ""
	}
	// Worth reading even on a pass: a later step reaches this by asking the
	// package manager the same question, so a wrong answer here is a cache
	// that never gets used.
	fmt.Fprintf(os.Stderr, "setup-node: %s cache directory %s\n", pm, dir)

	lockfile, err := os.ReadFile(filepath.Join(c.AppDir, pm.Lockfile())) //nolint:gosec // lockfile name is one of a fixed set of known filenames
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-node: no %s found, skipping cache restore\n", pm.Lockfile())
		return "", dir
	}
	pmVersion, err := pm.Version(ctx, c.AppDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup-node: warning: %v; skipping cache restore\n", err)
		return "", dir
	}

	keys := packagesCacheKeys(pm, pmVersion, dir, lockfile)
	before := entryNames(dir)
	_, err = client.RestoreCache(ctx, keys, sdk.WithStepName(fmt.Sprintf("Restoring %s cache", pm)))
	sdk.LogCacheErr("restore", err)
	// RestoreCache reports "success" even on a miss, so the only way to
	// tell a hit apart is to check whether it actually added to dir.
	if err == nil && addedEntries(dir, before) {
		fmt.Fprintf(os.Stderr, "setup-node: restored %s from cache\n", dir)
	}
	return keys[0], dir
}

// packagesCacheKeys returns the keys to restore pm's cache from, best first;
// the first is also the key to save it under. After the exact key, a prefix of
// it matches the newest cache saved for any lockfile, so a changed lockfile
// downloads only what changed.
//
// Everything that can make a cache unreadable to this job goes before the
// lockfile hash, so the fallback never crosses it: the cache directory, since
// a cache restored anywhere else is never read, and the package manager and
// its version, which set the cache's layout. A cache directory named after its
// layout version, as pnpm's store (store/v10) and Yarn 1's cache (v6) are,
// changes name when the layout does, so for those a last prefix leaves the
// version out too, and a package manager upgrade restores the cache the
// previous version saved rather than starting from nothing.
func packagesCacheKeys(pm node.PackageManager, pmVersion, cacheDir string, lockfile []byte) []string {
	dirHash := sha256.Sum256([]byte(cacheDir))
	anyVersion := fmt.Sprintf("setup-node-packages-v4-%s-%s-%x-", sdk.OSArch(), pm, dirHash[:4])
	sameVersion := anyVersion + pmVersion + "-"
	keys := []string{fmt.Sprintf("%s%x", sameVersion, sha256.Sum256(lockfile)), sameVersion}
	if layoutVersioned(cacheDir) {
		keys = append(keys, anyVersion)
	}
	return keys
}

// layoutVersioned reports whether dir is named after a cache layout version,
// like v10.
func layoutVersioned(dir string) bool {
	base := filepath.Base(dir)
	if len(base) < 2 || base[0] != 'v' {
		return false
	}
	for _, r := range base[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
