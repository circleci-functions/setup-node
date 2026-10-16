# setup-node

Installs Node.js and puts `node`, `npm` and `npx` on `PATH` for the rest of the
job. An optional `install-packages` command then installs your dependencies
with the right package manager, caching what it downloads.

```yaml
steps:
  - checkout
  - run:
      name: Install Node.js
      command: circleci run github.com/circleci-functions/setup-node@<version> -- --node-version 22
  - run:
      name: Install dependencies
      command: circleci run github.com/circleci-functions/setup-node@<version> -- install-packages --app-dir ./frontend
  - run: npm test
```

## Installing Node.js

With no command, setup-node downloads the official Node.js build for the
machine it runs on from nodejs.org, checks it against the release's
`SHASUMS256.txt`, and links `node`, `npm`, `npx` and, on releases that ship it,
`corepack` into a directory already on `PATH`. Later steps run them by name,
in any shell, without sourcing anything.

### Choosing a version

`--node-version` takes any of:

| Spec | Installs |
|------|----------|
| `22.11.0`, `v22.11.0` | exactly that release |
| `22`, `22.x`, `22.11` | the newest matching release |
| `>=20 <23`, `^20`, `~20.1`, `20 \|\| 22` | the newest release in the range |
| `lts/*` | the newest LTS release |
| `lts/jod` | the newest release of that LTS line |
| `lts/-1` | the newest release of the LTS line before the newest |
| `latest`, `current`, `node` | the newest release |

Or read the version from a file with `--node-version-file`:

| File | Read from |
|------|-----------|
| `.nvmrc`, `.node-version` | the version on its own line |
| `.tool-versions` | the `nodejs` or `node` line |
| `mise.toml` | `tools.node` |
| `package.json` | `volta.node`, then the `node` entry in `devEngines.runtime`, then `engines.node`, then the file `volta.extends` names |

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- --node-version-file .nvmrc
```

With neither flag, setup-node uses `.nvmrc`, then `.node-version`, from the
step's working directory, and installs the newest LTS release if there is
neither. `--node-version` wins when both flags are set.

### Global npm installs

Executables from `npm install --global` land on `PATH` too: setup-node points
npm's global prefix at the directory it linked `node` into (its parent on
Linux and macOS, where npm adds `bin`). A prefix you set yourself in
`~/.npmrc` or `$npm_config_prefix` takes precedence.

If that directory is only writable with `sudo`, or is not named `bin`,
setup-node leaves npm's default prefix alone and warns that globally installed
executables will not be on `PATH`.

### Package managers

`install-packages` needs the package manager already on `PATH`. npm comes with
Node.js; install the others with npm:

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- --node-version 24
- run: npm install --global pnpm@10
- run: circleci run github.com/circleci-functions/setup-node@<version> -- install-packages
```

### Mirrors

`--mirror` downloads from any server laid out like `https://nodejs.org/dist`:
an `index.json`, and a `v<version>/` directory per release holding the
archives and `SHASUMS256.txt`. `--mirror-token` is sent verbatim as the
`Authorization` header. The mirror and token are also read from
`$NODE_MIRROR` and `$NODE_MIRROR_TOKEN`, so both can live in a context or in
project environment variables of those names rather than in your config. The
token is never sent to nodejs.org: with no mirror set, setup-node warns and
downloads without it.

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- --node-version 22 --mirror https://mirror.example.com/node
```

### Caching the install

The unpacked install is saved to the job cache under a key made of the OS,
architecture and Node.js version, so later jobs restore it rather than
downloading it again. A cache problem never fails the job. Turn it off with
`--cache=false`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--node-version` | (see above) | Version spec to install |
| `--node-version-file` | (empty) | File to read the version spec from |
| `--mirror` | `https://nodejs.org/dist` | Server to download Node.js from; also read from `$NODE_MIRROR` |
| `--mirror-token` | (empty) | `Authorization` header value for `--mirror`, never sent to nodejs.org; also read from `$NODE_MIRROR_TOKEN` |
| `--cache` | true | Set `--cache=false` to disable caching the install |

## Installing dependencies

`install-packages` detects the package manager from your lockfile, runs a
frozen install, and caches what the package manager downloads, so later steps
that run the package manager pick up the same cache automatically.

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- install-packages --app-dir ./frontend
```

### Choosing a package manager

By default `install-packages` detects the package manager from the lockfile in
`--app-dir`:

| Lockfile | Package manager |
|----------|-----------------|
| `bun.lock` or `bun.lockb` | bun |
| `pnpm-lock.yaml` | pnpm |
| `yarn.lock` | yarn |
| `package-lock.json` or `npm-shrinkwrap.json` | npm |

When more than one is present they are matched in that order. Set
`--package-manager` to pick one explicitly instead:

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- install-packages --package-manager pnpm
```

Each package manager runs a frozen install that fails if the lockfile is out of
sync: `npm ci`, or `<pm> install --frozen-lockfile` for yarn, pnpm and bun.
Replace it entirely with `--override-command`:

```yaml
- run: circleci run github.com/circleci-functions/setup-node@<version> -- install-packages --override-command "npm install --legacy-peer-deps"
```

### Caching packages

Caching is on by default. `install-packages` restores the package manager's
cache directory before installing and saves it back afterwards, keyed on the
OS, architecture, package manager and its version, the cache directory, and a
checksum of the lockfile, so a changed lockfile refreshes the cache on its own.
When nothing is saved for the current lockfile yet, the most recent cache with
everything else the same is restored instead, so only the packages that changed
are downloaded.

Nothing is exported to `$BASH_ENV`: each package manager resolves the same
cache directory on its own, so later steps read and write it whatever shell
they run in.

Turn caching off with `--cache=false`. Cache problems never fail the job: if
the package manager can't report its cache directory or its version, or the
lockfile can't be read (e.g. a `bun.lockb`-only repo, or `--override-command`
with nothing checked out), caching is skipped with a warning while the install
still runs. A package manager missing from `PATH` fails the step, cache or no
cache.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--app-dir` | `.` | Directory containing `package.json` and the lockfile |
| `--package-manager` | (auto) | `npm`, `yarn`, `pnpm` or `bun`. Auto-detected from the lockfile if omitted. |
| `--override-command` | (empty) | Replace the default frozen install command |
| `--cache` | true | Set `--cache=false` to disable caching |

## Platform support

Linux, macOS and Windows, on amd64 and arm64, plus Linux on 32-bit ARM,
ppc64le and s390x, wherever Node.js publishes an official build for the
release you ask for. A version spec only matches releases with a build for the
machine it runs on.

- **Alpine and other musl distros** are not supported: Node.js's official
  Linux builds are linked against glibc.
- **Older glibc distros** may be too old for recent Node.js releases, which
  set a minimum glibc version. setup-node checks the installed `node` runs,
  and fails the step with `node`'s own error if it does not.
- **Windows** gets `node`, `npm`, `npx` and `corepack` shims that work from
  cmd, PowerShell and Git Bash. Where setup-node takes over a preinstalled
  Node.js's directory, its shims replace that install's `npm` scripts too, so
  `npm` is always the one bundled with the Node.js setup-node installed.
