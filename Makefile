.DEFAULT_GOAL := help

.PHONY: help fmt fmt-check vet test test-race test-e2e lint vuln license build build-cross sanitize check release-snapshot

help:
	@printf '%s\n' 'Rungrid developer workflow'
	@printf '%s\n' ''
	@printf '%s\n' '  make check             check format, vet, test, race, licenses, and builds'
	@printf '%s\n' '  make test-e2e          run the real Process Compose lifecycle suite'
	@printf '%s\n' '  make lint              run golangci-lint'
	@printf '%s\n' '  make vuln              run govulncheck'
	@printf '%s\n' '  make license           verify dependency license material'
	@printf '%s\n' '  make release-snapshot  validate a local GoReleaser snapshot'

fmt:
	gofmt -w main.go cmd internal *_test.go

fmt-check:
	@test -z "$$(gofmt -l main.go cmd internal *_test.go)"

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

test-e2e:
	RUNGRID_E2E=1 go test -run TestHeadlessLifecycleEndToEnd -count=1 ./tests/end-to-end/local

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

license:
	tests/licenses/check.sh

build:
	go build ./...

build-cross:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /tmp/rungrid-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/rungrid-darwin-arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/rungrid-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/rungrid-linux-arm64 .

sanitize:
	go test -run 'TestCLISpec(IsNeutral|DefinesV1Contract)' .

check: fmt-check vet test test-race sanitize license build build-cross

release-snapshot:
	goreleaser check
	@if command -v syft >/dev/null 2>&1; then \
		goreleaser release --snapshot --clean --skip=sign; \
	else \
		goreleaser release --snapshot --clean --skip=sign,sbom; \
	fi
