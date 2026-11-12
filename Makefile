.PHONY: dev test vet race integration screenshots build package release-check

dev:
	go run ./cmd/prepare
	go tool mygo dev

test:
	go test ./...

vet:
	go vet ./...
	go tool mygo vet ./internal/app

race:
	go test -race ./...

integration:
	PANREEL_ENGINE_TEST=1 go test -count=1 -v ./internal/engine

screenshots:
	PANREEL_UI_ARTIFACTS=$(CURDIR)/build/test-artifacts go test -run TestRenderDesktopArtifacts ./internal/app

build:
	go build ./...

package:
	go run ./cmd/package $(ARGS)

release-check:
	go run ./cmd/package -check
