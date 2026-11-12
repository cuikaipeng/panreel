# PanReel

**English** · [简体中文](README.zh-CN.md)

A native cloud-drive search app for macOS and Windows, built with Go and [MyGo](https://github.com/egoist/mygo). The installer includes [PanSou](https://github.com/fish2018/pansou); no Docker, separate server or development tools are needed to use the app.

## Features

- 75 search plugins enabled by default, plus configurable public Telegram channels.
- 14 result types: Baidu, Aliyun, Quark, Guangya, Tianyi, UC, China Mobile, 115, PikPak, Xunlei, 123, magnet, ed2k and other links. Choose which types to search with checkboxes.
- Results arrive automatically, with deduplication, filters, extraction codes and link checks. **Valid only** is the default; use **All results** to see links whose status is unknown or unsupported.
- Favorites, local search history and optional favorite update reminders.
- English/Chinese, light/dark themes, automatic settings saving and system proxy support.
- Independent PanSou updates: checks every 30 minutes while running; installation requires your click. Availability depends on the maintainer publishing the [engine channel](docs/ENGINE_UPDATES.md).

The app searches public shares. It does not provide drive account login, file transfers, downloading or playback. Third-party sources may require credentials or become unavailable.

## Install

Download packages from [Releases](https://github.com/cuikaipeng/panreel/releases).

- **macOS 13+**, Apple Silicon / Intel: open `panreel-0.1.0.dmg` and drag **PanReel.app** into **Applications**.
- **Windows 10/11 x64**: run `panreel-setup-0.1.0-amd64.exe`. Keep the bundled engine installed with the app.

### macOS first launch

The macOS package has no Apple Developer ID certificate or notarization. If macOS blocks it because the developer cannot be verified, first confirm that you downloaded it from this repository. After copying it to Applications, open Terminal and run:

```sh
xattr -dr com.apple.quarantine "/Applications/PanReel.app"
```

Then open PanReel again. If the command reports a permission error, run it again with `sudo` before `xattr`; adjust the path if you installed elsewhere. This removes the quarantine attribute only from this app. You can also use **System Settings → Privacy & Security → Open Anyway**, as described by [Apple](https://support.apple.com/en-us/102445). For corruption or malware alerts, check the download instead of assuming a certificate issue.

Windows builds also lack a code-signing certificate, so Windows may show an unknown-publisher warning.

## Configure TG channels first

**No Telegram channels are included. Add and enable at least one public channel before searching, even in Plugins only mode.** All plugins remain enabled by default.

1. Open **Search sources → TG channels**.
2. Enter public channel usernames or links. Add several at once using commas, spaces, newlines or semicolons:

   ```text
   @channel_one, @channel_two, https://t.me/channel_three
   https://t.me/s/channel_four，channel_five
   ```

   Replace these examples with real channels. Usernames are deduplicated regardless of case. Private channels, invite links and individual post links are unsupported.
3. Click **Add channels**. They are enabled and saved automatically; wait for **Engine ready**, then search.

Uncheck a channel to disable it or remove it from this page. Disabling every channel blocks searching again. Public-channel search needs no Telegram login. If Telegram is unreachable, check your network; PanReel follows the system proxy by default. Enable your proxy app's system proxy if needed, or change **Preferences → Search engine → Network proxy**.

## Favorite update reminders

Select a favorite and enable **Update reminders** in its details. Choose an interval from 30 minutes to one day. The first successful scan establishes a baseline; later additions or file-content changes trigger a system notification and an unread badge.

Checks run **only while PanReel is open**; allow system notifications. Eleven drive types have directory adapters, but login requirements, verification challenges or provider changes can prevent scanning. See [watch behavior and support limits](docs/update-watches.md).

## Develop

Requires Git, Go **1.27.4+** and pnpm for the aliases below. The first run downloads the pinned PanSou source and its engine dependencies.

```sh
pnpm dev            # prepare the engine and launch the app in one terminal
pnpm test
pnpm check
pnpm release:check
pnpm build          # package for the current platform
```

pnpm is optional: `go run ./cmd/prepare` followed by `go tool mygo dev` also works. See [Contributing](CONTRIBUTING.md) for tests and architecture, and [Releasing](docs/RELEASING.md) for cross-platform packages and update signing.

## License

PanReel-authored code is [MIT](LICENSE). PanSou and other dependencies retain their own licenses and copyright notices; see [Third-party notices](THIRD_PARTY_NOTICES.md). Redistribution must retain the bundled `resources/licenses/` notices.

Software licenses do not grant rights to films or shared files. Use only content you are authorized to access and follow the relevant service terms. This repository includes no media files or preset TG channels.
