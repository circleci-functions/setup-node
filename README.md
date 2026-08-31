# setup-node

Detects the Node.js package manager in use and runs the appropriate CI-safe frozen install command. Optionally restores/saves the package manager's cache directory to the CircleCI job cache and exports it to `$BASH_ENV` so subsequent steps can locate it too.

Invoked via:

```
circleci run setup-node@<version> install [flags]
```

## Capabilities

- **Auto-detects** the package manager from the lockfile present in `--app-dir`
- **Runs** the frozen/CI-safe install command for the detected or specified package manager
- **Restores and saves** the package manager's cache directory via the CircleCI job cache, keyed on the lockfile contents (default on)
- **Exports** cache directory environment variables to `$BASH_ENV` (default on)
- Supports **npm**, **yarn**, **pnpm**, and **bun**

## Flags

| Flag | Short | Type | Default | Description |
|---|---|---|---|---|
| `--app-dir` | `-C` | string | `.` | Directory containing `package.json` and the lockfile |
| `--package-manager` | | enum | _(auto)_ | Package manager to use: `npm`, `yarn`, `pnpm`, or `bun`. Auto-detected from lockfile if omitted. |
| `--override-command` | | string | _(none)_ | Override the default CI install command (e.g. `npm install --legacy-peer-deps`) |
| `--with-cache` / `--no-with-cache` | | bool | `true` | Restore/save the package manager cache via the CircleCI job cache and export its path to `$BASH_ENV` |

## Package manager detection

When `--package-manager` is not set, the lockfile present in `--app-dir` determines the package manager. Priority order when multiple lockfiles are present:

1. `bun.lockb` → bun
2. `pnpm-lock.yaml` → pnpm
3. `yarn.lock` → yarn
4. `package-lock.json` → npm
5. `npm-shrinkwrap.json` → npm

If no lockfile is found and `--package-manager` is not set, the command exits with an error.

## Install commands

Each package manager runs a frozen install that fails if the lockfile is out of sync:

| Package manager | Command |
|---|---|
| npm | `npm ci` |
| yarn | `yarn install --frozen-lockfile` |
| pnpm | `pnpm install --frozen-lockfile` |
| bun | `bun install --frozen-lockfile` |

`--override-command` replaces the entire command (binary + arguments).

## Job cache

When `--with-cache` is set (the default), the package manager's cache directory (see below) is
restored from the CircleCI job cache before installing and saved back afterwards, via the same
mechanism `circleci run` steps use for privileged operations. The cache key is derived from the
OS, architecture, package manager, and a checksum of the lockfile, so a changed lockfile busts
the cache automatically.

If the lockfile can't be read (e.g. a `bun.lockb`-only repo, or `--override-command` used with no
lockfile present), the restore/save steps are skipped with a warning, but the install still runs.
Cache errors — including running outside a CircleCI job, where they're expected — are logged but
never fail the command.

## Cache environment variables

When `--with-cache` is set (the default), the following variable is appended to `$BASH_ENV`:

| Package manager | Variable |
|---|---|
| npm | `NPM_CONFIG_CACHE` |
| yarn | `YARN_CACHE_FOLDER` |
| pnpm | `PNPM_STORE_PATH` |
| bun | `BUN_INSTALL_CACHE_DIR` |

The value is read from the package manager's own resolution logic:
- **npm**: uses `$NPM_CONFIG_CACHE` if set, otherwise `~/.npm`
- **yarn**: runs `yarn cache dir`, falls back to `~/.cache/yarn`
- **pnpm**: runs `pnpm store path` (error if pnpm is not installed)
- **bun**: always `~/.bun/install/cache`

If `$BASH_ENV` is not set, exports are written to stdout only.

## Preconditions

- The package manager binary (`npm`, `yarn`, `pnpm`, or `bun`) must be on `PATH`
- `--app-dir` must exist and be a directory

## Examples

```bash
# Auto-detect from lockfile in current directory
circleci run setup-node@v1.0.0 install

# Specify app directory (monorepo)
circleci run setup-node@v1.0.0 install --app-dir ./packages/web

# Explicit package manager, skip cache export
circleci run setup-node@v1.0.0 install --pkg-manager pnpm --no-with-cache

# Custom install command
circleci run setup-node@v1.0.0 install --override-command "npm install --legacy-peer-deps"
```

## Platform support

Linux and macOS (amd64 and arm64). Windows support is deferred.

## Tests

Three layers, each in the cheapest place that can hold it.

**Unit** — `node/` covers package-manager detection and command building
directly.

**CLI** — `clitest/` compiles the real binary and runs it as a subprocess to
cover the flag surface and its error paths: unknown flags, invalid
`--package-manager`, a missing `--app-dir`, a package manager absent from PATH,
and the cache round-trip contract (which keys are restored and saved). It is
hermetic — a no-op package-manager stub stands in for the real one and the
registry is never reached — so it needs no network and runs under `task test` on
every commit. Each test runs in a `toolkit/sdk/sdktest` sandbox, so `$HOME`,
`$BASH_ENV` and the cache are all temp locations.

**Acceptance** — `acceptance/`, behind a `//go:build acceptance` tag so it stays
out of `task test`. Same sandboxing, but against real infrastructure: a real
frozen install for each package manager, and a real CircleCI task-agent cache
when running inside a job (`TOOLKIT_CACHE_MODE=live`; it falls back to the
in-memory cache elsewhere, so the suite still runs on a laptop). Run with `task
acceptance`; it needs network access and the package managers on PATH, but will
not touch your real home directory or a shared cache key.

In CI, `acceptance` installs the package managers with the `node` orb and runs
the full matrix against the live cache. A separate `smoke-test` job runs the
cross-compiled artifact `publish` is about to ship through one install, checking
the cache-directory export it writes is sourced from `$BASH_ENV` into a later
step by the real job runner.
