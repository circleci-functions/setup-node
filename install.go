package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-node/nodedist"
	"github.com/circleci-functions/setup-node/nodepath"
	"github.com/circleci-functions/setup-node/nodeversion"
)

// installCmd installs an official Node.js build for the running platform and
// makes node, npm and npx resolvable by name for the rest of the job.
type installCmd struct {
	NodeVersion     string `help:"Node.js version to install: an exact or partial version (22, 22.11.0), a semver range (>=20 <23), lts/*, lts/<codename>, lts/-n, or latest. Defaults to .nvmrc, then .node-version, then lts/*."`
	NodeVersionFile string `help:"File to read the Node.js version from: .nvmrc, .node-version, .tool-versions, mise.toml or package.json." type:"path"`
	Mirror          string `help:"Base URL of a server laid out like https://nodejs.org/dist to download Node.js from." default:"https://nodejs.org/dist"`
	MirrorToken     string `help:"Authorization header value to send to --mirror. The token can also be set via the NODE_MIRROR_TOKEN env var." env:"NODE_MIRROR_TOKEN"`
	Cache           bool   `help:"Restore the Node.js install from the job cache, and save it after a fresh download. Set to false to disable caching." default:"true"`
	InstallDir      string `help:"Directory to install Node.js versions under." default:"~/.cache/setup-node" hidden:""`
	BinDir          string `help:"Link node, npm and npx into this directory instead of discovering one on PATH." type:"path" hidden:""`
	CacheNonce      string `help:"Partition the Node.js install cache, so a test run cannot restore an earlier run's install." hidden:""`
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	spec, err := c.versionSpec()
	if err != nil {
		return err
	}

	platform, err := nodedist.Host()
	if err != nil {
		return err
	}
	installDir, err := expandHome(c.InstallDir)
	if err != nil {
		return err
	}
	dist := nodedist.New(ctx, c.Mirror, c.MirrorToken)

	version, exact := nodeversion.Exact(spec)
	if !exact {
		if version, err = resolve(ctx, dist, spec, platform); err != nil {
			return err
		}
	}
	root := filepath.Join(installDir, version+"-"+platform.String())
	if !c.restore(ctx, client, version, root) {
		if exact {
			if _, err := resolve(ctx, dist, spec, platform); err != nil {
				return err
			}
		}
		if err := c.download(ctx, client, dist, version, platform, root); err != nil {
			return err
		}
	}

	dir, err := nodepath.Link(nodepath.Options{Root: root, Version: version, BinDir: c.BinDir})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "setup-node: Node.js %s installed at %s, linked into %s\n", version, root, dir)

	if prefix, err := nodepath.SetGlobalPrefix(root, dir); err != nil {
		fmt.Fprintf(os.Stderr, "setup-node: warning: %v; executables from npm install -g will not be on PATH\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "setup-node: set npm's default global prefix to %s (a prefix set in ~/.npmrc or $npm_config_prefix takes precedence)\n", prefix)
	}
	return nil
}

// resolve returns the version of the newest release in dist's index that
// matches spec and ships a build for platform.
func resolve(ctx context.Context, dist *nodedist.Dist, spec string, platform nodedist.Platform) (string, error) {
	releases, err := dist.Index(ctx)
	if err != nil {
		return "", err
	}
	release, err := nodeversion.Resolve(spec, releases, platform.FileID())
	if err != nil {
		return "", fmt.Errorf("resolve Node.js version: %w", err)
	}
	fmt.Fprintf(os.Stderr, "setup-node: resolved %q to Node.js %s (%s)\n", spec, release.Version, platform)
	return release.Version, nil
}

// versionSpec picks the version spec from --node-version, --node-version-file,
// or the default version files in the working directory, in that order.
func (c *installCmd) versionSpec() (string, error) {
	switch {
	case c.NodeVersion != "":
		if c.NodeVersionFile != "" {
			fmt.Fprintf(os.Stderr, "setup-node: both --node-version and --node-version-file are set; using --node-version\n")
		}
		return c.NodeVersion, nil
	case c.NodeVersionFile != "":
		spec, err := nodeversion.FromFile(c.NodeVersionFile)
		if err != nil {
			return "", fmt.Errorf("node-version-file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "setup-node: read Node.js version %q from %s\n", spec, c.NodeVersionFile)
		return spec, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	spec, source, err := nodeversion.Default(wd)
	if err != nil {
		return "", err
	}
	if source == "" {
		fmt.Fprintf(os.Stderr, "setup-node: no --node-version, .nvmrc or .node-version; using %s\n", spec)
	} else {
		fmt.Fprintf(os.Stderr, "setup-node: read Node.js version %q from %s\n", spec, source)
	}
	return spec, nil
}

// restore reports whether root holds the install of version, from an earlier
// run or, failing that, the job cache.
func (c *installCmd) restore(ctx context.Context, client *sdk.Client, version, root string) bool {
	if installed(root) {
		return true
	}
	if !c.Cache {
		return false
	}
	_, err := client.RestoreCache(ctx, []string{toolchainCacheKey(version, c.CacheNonce)}, sdk.WithStepName("Restoring Node.js "+version))
	sdk.LogCacheErr("restore", err)
	if !installed(root) {
		return false
	}
	fmt.Fprintf(os.Stderr, "setup-node: restored Node.js %s from cache\n", version)
	return true
}

// download installs version at root from dist, and saves it to the job cache.
func (c *installCmd) download(ctx context.Context, client *sdk.Client, dist *nodedist.Dist, version string, platform nodedist.Platform, root string) error {
	if err := dist.Install(ctx, version, platform, root); err != nil {
		return err
	}
	if c.Cache {
		sdk.LogCacheErr("save", client.SaveCache(ctx, toolchainCacheKey(version, c.CacheNonce), []string{root}, sdk.WithStepName("Saving Node.js "+version)))
	}
	return nil
}

func toolchainCacheKey(version, nonce string) string {
	key := fmt.Sprintf("setup-node-toolchain-v1-%s-%s", sdk.OSArch(), version)
	if nonce != "" {
		key += "-" + nonce
	}
	return key
}

// expandHome resolves a leading ~ against $HOME. kong's path type resolves it
// against the passwd entry instead, which ignores a redirected $HOME.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func installed(root string) bool {
	node := filepath.Join(nodepath.BinDir(root), "node")
	if _, err := os.Stat(node); err == nil {
		return true
	}
	_, err := os.Stat(node + ".exe")
	return err == nil
}
