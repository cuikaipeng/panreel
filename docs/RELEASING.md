# Release workflow

Packages include the app, PanSou and third-party notices. Supported distributions are macOS universal and Windows x64. GitHub Releases contain installers, signed update archives and checksums, and are created as drafts for review.

## Version and validation

1. Update `mygo.json`, `package.json` and `appVersion` in `internal/app/app.go` together. Add bilingual notes at `docs/releases/vX.Y.Z.md`.
2. Run `pnpm release:check`, `pnpm test`, `pnpm check`, and `pnpm test:race`.
3. Prepare and test PanSou: `pnpm engine:prepare`, then `go run ./cmd/licenses -check` and `PANREEL_ENGINE_TEST=1 go test -count=1 -v ./internal/engine` (PowerShell: set `$env:PANREEL_ENGINE_TEST='1'` first). New dependencies or license changes must be reviewed before running `go run ./cmd/licenses` and committing the resulting notices.
4. Run `pnpm build:all` on macOS. NSIS (`makensis`) is needed for a Windows installer when cross-compiling. CI uses native macOS and Windows runners.
5. Install the full app on both operating systems. On a fresh profile, verify all plugins are enabled, TG channels are empty and searching is blocked until channels are added and automatically applied. Check comma/newline batch input, clearing all channels, preservation of existing saved channels, search, favorites, both languages, both themes, restart persistence, and exit cleanup.

`go run ./cmd/package` is the shared packaging entry point behind pnpm and Make. It validates versions, prepares platform engines, checks reviewed licenses, validates the signing key, then invokes the pinned MyGo tool. `-platform darwin/universal,windows/amd64` selects both packages. No packaging command publishes anything.

## Update signing

PanReel has its **own Ed25519 update key**, separate from Kivrix. Only the public key is in `mygo.json` and `internal/engineupdate/update.go`; the release check ensures they match. The local private key is `.local/update-keys/mygo-update.key` (ignored by Git, permissions 0600). Back it up in a secure password manager. Never commit, print, share, or regenerate it casually: installed clients trust this specific public key.

For CI, set the repository secret `MYGO_UPDATER_PRIVATE_KEY` to the contents of that private-key file. Local packaging loads the same key automatically, or accepts the environment variable. `-require-key` fails when the key is absent or does not match the public key.

A release contains the DMG, the NSIS installer, `panreel-<version>-<platform>.tar.gz`, the matching `update-<platform>.json`, and `SHA256SUMS.txt`. Do not rename archives without updating the manifest URL. Verify an assembled release directory with:

```sh
go run ./cmd/package -tag v0.1.0 -verify build/release-flat
```

Verification checks version, repository URL, archive size and Ed25519 signature before writing checksums. The app checks the GitHub release feed; download/preparation is explicit, signature verification is performed by MyGo, and restart is explicit and flushes pending automatic settings saves. Development builds report that updates are unavailable. A repository without a published manifest reports no published update.

## GitHub Actions

Pushes to `dev`/`main`, PRs and manual dispatch run tests and produce test installers. A `v*` tag additionally requires the signing secret, builds update archives on both runners, verifies them, and creates a **draft** Release using the checked-in notes. Publishing that draft is a separate maintainer action. Retrying a draft is supported; already-published releases are never overwritten.

Running local validation does not create tags, set repository secrets or publish a release.

## Independent engine updates

PanSou uses the separate `engine.yml` workflow and the non-latest `pansou-engine` Release. This channel automatically publishes verified engine archives, while application releases remain drafts. Setup and recovery are documented in [ENGINE_UPDATES.md](ENGINE_UPDATES.md).

## OS signing

Ed25519 update signatures are distinct from OS trust. Version 0.1.0 uses ad hoc macOS signing and has no Apple Developer ID, notarization or Windows Authenticode certificate. Keep the [macOS first-launch instructions](../README.md#macos-first-launch) in the release documentation; do not describe these packages as notarized. Windows installation/UI acceptance and macOS downloaded-package acceptance are separate from automated tests.

When certificates become available, configure MyGo's macOS Developer ID and notarization settings and the Windows signing process using protected CI credentials. `PANREEL_MACOS_SIGN_IDENTITY` passes a configured local identity through the package command. Do not commit certificate files or passwords.

Action usage references: [checkout](https://github.com/actions/checkout), [setup-go](https://github.com/actions/setup-go), [upload-artifact](https://github.com/actions/upload-artifact), [download-artifact](https://github.com/actions/download-artifact).
