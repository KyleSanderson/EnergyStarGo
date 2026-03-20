VERSION ?= 1.0.0
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)

.PHONY: build build-debug test vet clean cross-compile

# Build for Windows amd64 (release)
build:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS) -s -w" -o bin/energystar.exe ./cmd/energystar/

# Build for Windows arm64
build-arm64:
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS) -s -w" -o bin/energystar-arm64.exe ./cmd/energystar/

# Build with debug info
build-debug:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/energystar-debug.exe ./cmd/energystar/

# Build for Windows 386
build-386:
	GOOS=windows GOARCH=386 go build -ldflags "$(LDFLAGS) -s -w" -o bin/energystar-386.exe ./cmd/energystar/

# Build all architectures
cross-compile: build build-arm64 build-386

# GoReleaser snapshot build (requires goreleaser in PATH)
release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

# GoReleaser release build (requires GITHUB_TOKEN and a git tag)
release:
	goreleaser release --clean

# Run all tests (cross-platform tests only on non-Windows)
test:
	go test -v -count=1 ./internal/config/ ./internal/logger/ ./internal/winapi/

# Run tests targeting Windows (use on Windows or with GOOS=windows for vet only)
test-windows:
	GOOS=windows go test -v -count=1 ./...

# Run vet checks
vet:
	GOOS=windows go vet ./...

# Run vet and tests
check: vet test

# Generate default config
config:
	@echo '{}' | go run -ldflags "$(LDFLAGS)" ./cmd/energystar/ config 2>/dev/null || true

# Clean build artifacts
clean:
	rm -rf bin/ energystar.exe

# Show help
help:
	@echo "EnergyStarGo Build System"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build Windows amd64 release binary"
	@echo "  build-arm64    Build Windows arm64 release binary"
	@echo "  build-debug    Build with debug symbols"
	@echo "  cross-compile  Build for all architectures"
	@echo "  test             Run cross-platform tests"
	@echo "  test-windows     Run all tests (Windows only)"
	@echo "  vet              Run go vet for Windows target"
	@echo "  check            Run vet + tests"
	@echo "  clean            Remove build artifacts"
	@echo "  release-snapshot GoReleaser snapshot (no publish)"
	@echo "  release          GoReleaser tagged release (needs GITHUB_TOKEN)"
