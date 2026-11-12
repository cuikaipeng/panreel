# Contributing

PanReel uses Go and MyGo native UI. PanSou is an independent, pinned executable bundled with the application. Keep UI changes in `internal/app`, search behavior in `internal/search` and engine lifecycle in `internal/engine`.

## Local workflow

Use Go 1.27.4+ and Git. `pnpm dev` prepares the matching engine and opens the desktop window in one terminal. pnpm is optional and has no frontend dependencies. `make dev` provides the same workflow on POSIX systems. PanSou builds use Go 1.27.4 with the reviewed dependency manifest in `buildtools/pansou.mod`; the upstream source remains pinned. See [engine build notes](buildtools/README.md).

Before opening a PR, run `pnpm test`, `pnpm check`, and `pnpm release:check`. For concurrency, persistence or process changes, also run `pnpm test:race` and the real engine integration test:

```sh
pnpm engine:prepare
go run ./cmd/licenses -check
PANREEL_ENGINE_TEST=1 go test -count=1 ./internal/engine ./internal/app
```

On Windows PowerShell, set `$env:PANREEL_ENGINE_TEST='1'` before the `go test` command, then remove it with `Remove-Item Env:PANREEL_ENGINE_TEST`. UI changes should be exercised in both languages and appearances at the minimum window size. `make screenshots` renders the real native views into `build/test-artifacts`; run `pnpm icon` to regenerate the [brand assets](docs/brand/README.md).

Use feature branches for focused PRs; `dev` can be used for integration and `main` for release preparation. These are repository conventions, not changes to remote branch settings. Do not publish a release or rotate its update signing key as part of routine development.

## Architecture and local data

`cmd/panreel` starts the native UI in `internal/app`. Search and link checks live in `internal/search`; `internal/watch` handles favorite directory scans, while `internal/storage` persists settings, history and favorites. `internal/engine` owns the PanSou process and `internal/engineupdate` verifies independent engine updates.

`cmd/prepare` fetches the [pinned PanSou revision](https://github.com/fish2018/pansou/tree/221665fb2c9279f22af4684ea2d8c0c331eac1cc), applies the desktop patches in `buildtools/`, and generates `internal/catalog/catalog.json`. It imports registered plugins, not upstream example channels. The engine listens on a random loopback port with a temporary bearer token; closing the parent pipe on exit or crash stops the engine. Its source and build cache are under `.cache/pansou/`, excluded from Git.

Open the data directory through Preferences → General. SQLite stores favorites, history and settings; search settings use AES-GCM with a local `secrets.key`. Back up both `panreel.db` and `secrets.key`. The key is protected by account permissions, not an OS keychain. Logs redact configured credentials and are limited to 8 MiB per generation. Use `PANREEL_DATA_DIR` for an isolated development profile.

Design and localization details are in [the design notes](docs/design/README.md), [Kivrix attribution](docs/KIVRIX_REFERENCE.md) and [I18N](docs/I18N.md).

## Boundaries

- Preserve favorites, history, custom channels and existing preferences during upgrades. Failed decryption must never reset the user's configuration.
- Pass search choices by stable values, never by translated labels. Store message templates and arguments so existing messages change language too.
- UI state changes from background work go through `app.update`. Cancel work at shutdown and ignore stale results.
- Appearance and language changes must not restart PanSou. Source and engine settings save automatically; only settings that affect the running process trigger a grouped restart.
- Add meaningful regression tests for lifecycle, migration, updates, translations and layout defects. Avoid tests that merely repeat implementation details.
- Keep Kivrix and kero attribution on adapted code. Do not copy unrelated Redis functionality or unlicensed assets.
- Preserve third-party notices, including Lucide/Feather icon attribution. After preparing the engine, run `go run ./cmd/licenses -check`; a new dependency or changed license requires review before regenerating and committing notices with `go run ./cmd/licenses`.

Release procedures and signing boundaries are in [docs/RELEASING.md](docs/RELEASING.md).
