# setup-node

Installs your Node.js dependencies with the right package manager and caches
what it downloads, so later steps start from a warm cache.

```yaml
steps:
  - run:
      name: Install dependencies
      command: circleci run setup-node@<version> -- --app-dir ./frontend
  - run: npm test
```

setup-node detects the package manager from your lockfile, runs a frozen
install, and caches what the package manager downloads — so later steps that
run the package manager pick up the same cache automatically.

## Choosing a package manager

By default setup-node detects the package manager from the lockfile in
`--app-dir`:

| Lockfile | Package manager |
|----------|-----------------|
| `bun.lockb` | bun |
| `pnpm-lock.yaml` | pnpm |
| `yarn.lock` | yarn |
| `package-lock.json` or `npm-shrinkwrap.json` | npm |

When more than one is present they are matched in that order. Set
`--package-manager` to pick one explicitly instead:

```yaml
- run: circleci run setup-node@<version> -- --package-manager pnpm
```

Each package manager runs a frozen install that fails if the lockfile is out of
sync — `npm ci`, or `<pm> install --frozen-lockfile` for yarn, pnpm and bun.
Replace it entirely with `--override-command`:

```yaml
- run: circleci run setup-node@<version> -- --override-command "npm install --legacy-peer-deps"
```

## Caching

Caching is on by default. setup-node restores the package manager's cache
directory before installing and saves it back afterwards, keyed on the OS,
architecture, package manager and a checksum of the lockfile — so a changed
lockfile refreshes the cache on its own.

Nothing is exported to `$BASH_ENV`: each package manager resolves the same
cache directory on its own, so later steps read and write it whatever shell
they run in.

Turn caching off with `--cache=false`. Cache problems never fail the job, and
if the lockfile can't be read (e.g. a `bun.lockb`-only repo, or
`--override-command` with nothing checked out) caching is skipped with a warning
while the install still runs.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--app-dir` | `.` | Directory containing `package.json` and the lockfile |
| `--package-manager` | (auto) | `npm`, `yarn`, `pnpm` or `bun`. Auto-detected from the lockfile if omitted. |
| `--override-command` | (empty) | Replace the default frozen install command |
| `--cache` | true | Set `--cache=false` to disable caching |

## Platform support

Linux, macOS and Windows, on amd64 and arm64.

On Windows the cache-path export is written for a bash shell, so later steps
need Git Bash or MSYS2 to pick it up.
