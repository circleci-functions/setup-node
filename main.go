// Command setup-node detects the Node.js package manager in use
// and runs the appropriate frozen/CI-safe install command. When --cache
// is set (the default), it restores the package manager's cache directory
// from the CircleCI job cache (keyed on the lockfile contents) before
// installing, and saves it back afterwards.
//
// Later steps need nothing exported: each package manager resolves the same
// cache directory on its own.
//
// Supported platforms: linux, darwin, and windows.
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-node/node"
)

type cli struct {
	AppDir          string `help:"Directory containing package.json." default:"."`
	PackageManager  string `help:"Package manager to use: npm, yarn, pnpm, or bun. Auto-detected from lockfile if omitted." enum:"npm,yarn,pnpm,bun," default:""`
	OverrideCommand string `help:"Override the default CI install command (e.g. 'npm install --legacy-peer-deps')."`
	Cache           bool   `help:"Restore the package manager cache before installing and save it at the end. Set to false to disable caching." default:"true"`
}

func (c *cli) Run(ctx context.Context, client *sdk.Client) error {
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
func (c *cli) resolvePackageManager() (node.PackageManager, error) {
	if c.PackageManager != "" {
		return node.PackageManager(c.PackageManager), nil
	}
	pm, err := node.PackageManagerFromDir(c.AppDir)
	if err != nil {
		return "", fmt.Errorf("no lockfile found in %s; use --package-manager to specify one explicitly", c.AppDir)
	}
	return pm, nil
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-node"),
		kong.Description("Install Node.js packages, caching what the package manager downloads.\n\nExample:\n  circleci run setup-node@<version> -- --app-dir ./frontend"),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
