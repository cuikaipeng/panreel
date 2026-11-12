# PanSou desktop build

The engine source is pinned by `cmd/prepare`. The `pansou-*.go.txt` files add loopback authentication, process lifetime and search progress support; they do not import upstream example TG channels.

## Reviewed dependency manifest

`pansou.mod` and `pansou.sum` are the desktop engine's dependency manifest and checksums. Builds, integration tests and license collection use them with `-modfile` and `-mod=readonly`, without rewriting the upstream `go.mod`. Go 1.27.4 replaces the older Go 1.25.0 engine toolchain.

The app and engine require macOS 13 or later, matching [Go 1.27's supported minimum](https://go.dev/doc/go1.27#linker).

The initial release updates Brotli to 1.2.7, JWT to 5.2.2, `x/net` to 0.62.0 and `x/text` to 0.43.0 to address known vulnerabilities. Sonic 1.15.5 supports the newer toolchain; its transitive dependencies are pinned here too. Examples of the reviewed advisories: [Brotli memory exhaustion](https://pkg.go.dev/vuln/GO-2026-6888), [HTML parsing complexity](https://pkg.go.dev/vuln/GO-2026-6860), and [JWT header allocation](https://pkg.go.dev/vuln/GO-2025-3553).

An upstream update requiring different dependencies must update these manifests and reviewed licenses before publication. Do not silently replace them with the upstream files or fetch unpinned latest versions during packaging.

## Validate changes

```sh
go run ./cmd/prepare -platform darwin/arm64,darwin/amd64,windows/amd64
go run ./cmd/licenses -check
PANREEL_ENGINE_TEST=1 go test -count=1 ./internal/engine ./internal/app
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck ./...
GOFLAGS='-modfile=../../../buildtools/pansou.mod -mod=readonly' GOTOOLCHAIN=go1.27.4 govulncheck -C .cache/pansou/source .
```

These shell examples use POSIX syntax. Both desktop CI and independent engine publication check for reachable known vulnerabilities and fail before distributing affected builds. A clean scan covers the current vulnerability database; it is not a guarantee about undiscovered bugs or third-party service behavior.
