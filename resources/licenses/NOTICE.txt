# Third-party notices

PanReel-authored code is licensed under MIT. Third-party code and assets retain their original licenses; the root license does not relicense them. Notices ship in `resources/licenses` and are accessible from Preferences → About → Third-party licenses.

- **[MyGo v0.8.3](https://github.com/egoist/mygo/tree/v0.8.3)**, copyright © 2026 MyGo contributors, MIT: native UI, windowing, packaging and signed updater. Module and component notices are in `resources/licenses/dependencies/github.com_egoist_mygo@v0.8.3/`. Separately licensed components retain their notices; for example, shader code carries Apache-2.0 notices from the Ebiten/Ebitengine Authors.
- **[PanSou](https://github.com/fish2018/pansou/tree/221665fb2c9279f22af4684ea2d8c0c331eac1cc)**, copyright © 2025 fish2018, MIT: a separate bundled executable, initially pinned at `221665fb2c9279f22af4684ea2d8c0c331eac1cc`. Full text: `resources/licenses/PanSou-LICENSE.txt`. `cmd/prepare` and `buildtools/` apply PanReel's loopback/authentication, process lifetime, network, empty-channel and search-progress adaptations. Independent engine updates identify their exact revision in the signed manifest and carry their own license files.
- **[Kivrix](https://github.com/cuikaipeng/kivrix)**, copyright © 2026 Kivrix contributors, MIT: adapted theme/layout controls and updater orchestration. Full text: `resources/licenses/kivrix.txt`; license checked against commit `b6ef5b0e3afee3400a2561db2adf0b7a4d821eeb`.
- **[kero v2.1.31](https://github.com/egoist/kero/tree/v2.1.31)**, copyright © 2026 EGOIST, MIT: portions of the Kivrix-derived desktop layout, controls and palettes. Full text: `resources/licenses/kero.txt`.
- **[Lucide / Feather](https://github.com/lucide-icons/lucide/blob/main/LICENSE)**, ISC and MIT for Feather-derived icons: line-icon geometry and window controls adapted through the Kivrix-based UI. Both notices, including Cole Bemis's copyright, are in `resources/licenses/lucide.txt`.
- **Go**, BSD-3-Clause: runtime and standard library, `resources/licenses/Go-LICENSE.txt`. Other runtime module versions are listed in `resources/licenses/DEPENDENCIES.txt`; exact license, component-notice and authorship files are kept under `dependencies/`.

## Artwork and names

The Floating Cloud logo and app icon are AI-generated artwork created for PanReel. The selected source artwork, editing prompts and exported assets are documented in `docs/brand/README.md`. To the extent PanReel contributors hold rights in this artwork, those rights are provided under the root MIT license. This statement does not establish exclusive copyright, trademark registration or clearance against similar marks.

No Kivrix, kero or Astryx product artwork is included. Cloud-drive and Telegram names identify compatible third-party services; they do not imply affiliation, endorsement or rights in those brands.

## License collection and review

The bundled PanSou is a modified MIT-licensed distribution, not an official upstream binary. Its copyright notice is retained verbatim. The desktop build also uses reviewed dependency versions from `buildtools/pansou.mod` and `pansou.sum`; see [engine build notes](buildtools/README.md). MIT permits modification and redistribution provided the copyright and license text remain included; it does not grant rights to third-party content or service trademarks.

`cmd/licenses` inventories the actual application and engine dependency graphs for macOS arm64, macOS amd64 and Windows amd64. It includes nested component notices and attribution files; some upstream notice bundles also document optional or build-only components. A module mentioned in such a bundle is not necessarily linked into PanReel. In particular, the MPL-2.0 entry in SQLite's third-party inventory describes a module-graph dependency excluded from the shipped runtime.

Run `go run ./cmd/prepare` before `go run ./cmd/licenses -check`. Check mode makes no changes and blocks packaging and automatic engine publication when reviewed notices or dependency versions differ. After reviewing an upstream change, `go run ./cmd/licenses` regenerates the inventory and removes obsolete generated dependency notices; commit the reviewed changes. Manually maintained Kivrix, kero and icon notices are preserved. This checks known notice files and dependency drift; it does not prove upstream ownership or review changes hidden only in source comments or service terms.

## Search content

These licenses grant rights in software, not in films, files, share links, third-party websites or their APIs. PanReel does not bundle media files or a TG-channel directory. Search plugins are included from PanSou and may contact third-party services. Users and operators must have authorization for content they access or distribute and comply with applicable service terms and law. A software license or disclaimer does not grant content rights.
