package node

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PackageManager is a supported Node.js package manager.
type PackageManager string

const (
	NPM  PackageManager = "npm"
	Yarn PackageManager = "yarn"
	PNPM PackageManager = "pnpm"
	Bun  PackageManager = "bun"
)

// lockfileOrder defines which lockfile wins when multiple are present.
// Earlier entries take priority.
var lockfileOrder = []struct {
	file string
	pm   PackageManager
}{
	{"bun.lock", Bun},
	{"bun.lockb", Bun},
	{"pnpm-lock.yaml", PNPM},
	{"yarn.lock", Yarn},
	{"package-lock.json", NPM},
	{"npm-shrinkwrap.json", NPM},
}

// PackageManagerFromDir scans dir for a known lockfile and returns the matching package manager.
// Returns an error if no lockfile is found.
func PackageManagerFromDir(dir string) (PackageManager, error) {
	for _, entry := range lockfileOrder {
		if _, err := os.Stat(filepath.Join(dir, entry.file)); err == nil {
			return entry.pm, nil
		}
	}
	return "", fmt.Errorf("no lockfile found in %s", dir)
}

// Lockfile returns the canonical lockfile name for pm.
func (pm PackageManager) Lockfile() string {
	switch pm {
	case NPM:
		return "package-lock.json"
	case Yarn:
		return "yarn.lock"
	case PNPM:
		return "pnpm-lock.yaml"
	case Bun:
		return "bun.lock"
	default:
		return ""
	}
}

// installCommand returns the default frozen/CI-safe install command args for pm.
// The returned slice includes the binary name as the first element.
func (pm PackageManager) installCommand() []string {
	switch pm {
	case NPM:
		return []string{"npm", "ci", "--no-audit", "--no-fund"}
	case Yarn:
		return []string{"yarn", "install", "--frozen-lockfile"}
	case PNPM:
		return []string{"pnpm", "install", "--frozen-lockfile"}
	case Bun:
		return []string{"bun", "install", "--frozen-lockfile"}
	default:
		return nil
	}
}

// CacheEnvKey returns the name of the env var to set the PackageManager's
// cache directory.
func (pm PackageManager) CacheDirKey() string {
	switch pm {
	case NPM:
		return "NPM_CONFIG_CACHE"
	case Yarn:
		return "YARN_CACHE_FOLDER"
	case PNPM:
		return "PNPM_STORE_PATH"
	case Bun:
		return "BUN_INSTALL_CACHE_DIR"
	default:
		return ""
	}
}

// Binary returns the name of the pm executable.
func (pm PackageManager) Binary() string {
	return string(pm)
}

// CacheDir returns the filesystem paths that should be cached for pm.
// For pnpm and yarn it may invoke the binary to resolve the store/cache path.
// CacheDir returns the directory pm caches downloads in, asked of pm itself
// where pm will answer. appDir is the project directory: bun only reports its
// cache from inside one, and a project's own config can move yarn's or pnpm's.
func (pm PackageManager) CacheDir(ctx context.Context, appDir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	switch pm {
	case NPM:
		return npmCacheDir(ctx, appDir, home), nil

	case Yarn:
		// Ask yarn where its cache lives.
		out, err := inDir(exec.CommandContext(ctx, "yarn", "cache", "dir"), appDir).Output()
		if err != nil {
			// Fall back to the conventional default.
			if runtime.GOOS == "windows" {
				if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
					return filepath.Join(localAppData, "Yarn", "Cache"), nil
				} else {
					return "", fmt.Errorf("yarn: could not determine cache dir")
				}
			}
			return filepath.Join(home, ".cache", "yarn"), nil
		}
		return strings.TrimSpace(string(out)), nil

	case PNPM:
		out, err := inDir(exec.CommandContext(ctx, "pnpm", "store", "path"), appDir).Output()
		if err != nil {
			return "", fmt.Errorf("pnpm store path: %w", err)
		}
		return strings.TrimSpace(string(out)), nil

	case Bun:
		// Ask bun: it resolves $BUN_INSTALL_CACHE_DIR, then
		// $XDG_CACHE_HOME/.bun/install/cache, then ~/.bun/install/cache, and
		// only the last of those is what a hardcoded path gets right.
		out, err := inDir(exec.CommandContext(ctx, "bun", "pm", "cache"), appDir).Output()
		if err == nil {
			if dir := strings.TrimSpace(string(out)); dir != "" {
				return dir, nil
			}
		}
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			return filepath.Join(xdg, ".bun", "install", "cache"), nil
		}
		return filepath.Join(home, ".bun", "install", "cache"), nil

	default:
		return "", fmt.Errorf("unknown package manager: %s", pm)
	}
}

// Version returns the version of pm that runs in appDir, where a project's
// packageManager field can pin a different one through corepack. A cache
// written by one version may be laid out differently from what another reads.
func (pm PackageManager) Version(ctx context.Context, appDir string) (string, error) {
	out, err := inDir(exec.CommandContext(ctx, pm.Binary(), "--version"), appDir).Output() //nolint:gosec // pm is one of a fixed set of package managers
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", pm, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s --version printed nothing", pm)
	}
	return fields[len(fields)-1], nil
}

// npmCacheDir asks npm where its cache is, falling back to where npm puts it
// by default: %LOCALAPPDATA%\npm-cache on Windows, ~/.npm elsewhere.
func npmCacheDir(ctx context.Context, appDir, home string) string {
	out, err := inDir(exec.CommandContext(ctx, "npm", "config", "get", "cache"), appDir).Output()
	if err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return dir
		}
	}
	if v := os.Getenv("NPM_CONFIG_CACHE"); v != "" {
		return v
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); runtime.GOOS == "windows" && localAppData != "" {
		return filepath.Join(localAppData, "npm-cache")
	}
	return filepath.Join(home, ".npm")
}

// inDir runs cmd from dir, so a package manager answers for the project rather
// than for wherever the function happens to be running.
func inDir(cmd *exec.Cmd, dir string) *exec.Cmd {
	cmd.Dir = dir
	return cmd
}

// InstallPackages executes the command to install packages in appDir.
// If overrideCommand is non-empty it is shell-split and used instead of
// pm.InstallCommand(), stdout and stderr are forwarded to the process's own
// stdout/stderr so output is visible to the caller.
func (pm PackageManager) InstallPackages(ctx context.Context, appDir, overrideCommand string) error {
	// Check that the PackageManager's binary exists in PATH
	if _, err := exec.LookPath(pm.Binary()); err != nil {
		return fmt.Errorf("%s binary not found on PATH", pm)
	}

	args, err := pm.buildArgs(overrideCommand)
	if err != nil {
		return err
	}

	//nolint:gosec // args[0] is either pm.Binary() or a caller-supplied binary name
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = appDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s install: %w", pm, err)
	}
	return nil
}

// buildArgs returns the command args to run, either from overrideCommand or
// from pm.InstallCommand().
func (pm PackageManager) buildArgs(overrideCommand string) ([]string, error) {
	if overrideCommand != "" {
		args := strings.Fields(overrideCommand)
		if len(args) == 0 {
			return nil, fmt.Errorf("override command is empty after parsing")
		}
		return args, nil
	}
	args := pm.installCommand()
	if len(args) == 0 {
		return nil, fmt.Errorf("no install command defined for package manager %q", pm)
	}
	return args, nil
}
