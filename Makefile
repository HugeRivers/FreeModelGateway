BINARY_NAME := fmg
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION  := $(shell go version | awk '{print $$3}')

LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GoVersion=$(GO_VERSION)"

.PHONY: all build build-linux build-darwin build-darwin-intel build-windows build-tray build-all run test test-coverage lint clean docker docker-run dist package package-darwin install uninstall init dev check help

all: build

build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	@go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/fmg/
	@echo "  -> bin/$(BINARY_NAME) $$(du -h bin/$(BINARY_NAME) | cut -f1)"

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/fmg/

build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/fmg/

build-darwin-intel:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/fmg/

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/fmg/

build-all: build-linux build-darwin build-darwin-intel build-windows

run: build
	./bin/$(BINARY_NAME)

test:
	go test -v -race -count=1 ./...

test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ coverage.out coverage.html dist/

docker:
	docker build -t fmg:$(VERSION) .

docker-run:
	docker run -d \
		-p 10086:10086 \
		-v ./config.yaml:/app/config.yaml \
		-e OPENCODE_API_KEY=$${OPENCODE_API_KEY} \
		-e OPENROUTER_API_KEY=$${OPENROUTER_API_KEY} \
		-e AIHUBMIX_API_KEY=$${AIHUBMIX_API_KEY} \
		-e KILO_API_KEY=$${KILO_API_KEY} \
		-e ZENMUX_API_KEY=$${ZENMUX_API_KEY} \
		--name fmg \
		--restart unless-stopped \
		fmg:$(VERSION)

dist:
	@./build.sh

package:
	@./build.sh --release

build-tray:
	@echo "Building tray app $(VERSION)..."
	@CGO_ENABLED=1 go build $(LDFLAGS) -o bin/fmg-tray ./cmd/tray/
	@echo "  -> bin/fmg-tray $$(du -h bin/fmg-tray | cut -f1)"

package-darwin:
	@./package-darwin.sh

install: build
	@echo "Installing to /usr/local/bin..."
	@sudo cp bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@sudo chmod +x /usr/local/bin/$(BINARY_NAME)
	@echo "Installed. Run: fmg"

uninstall:
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled."

init:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env (please edit)"; fi
	@if [ ! -f config.yaml ]; then cp config.example.yaml config.yaml && echo "Created config.yaml"; fi

dev: build
	@./start.sh --dev

check:
	@./start.sh --check

help:
	@echo "Free Model Gateway (FMG) - Makefile targets"
	@echo ""
	@echo "  make build          Build for current platform"
	@echo "  make build-all      Cross-compile for linux/darwin/windows"
	@echo "  make build-tray     Build macOS tray app"
	@echo "  make run            Build and run"
	@echo "  make test           Run unit tests with race detector"
	@echo "  make lint           Run golangci-lint"
	@echo "  make docker         Build Docker image"
	@echo "  make docker-run     Run Docker container (port 10086)"
	@echo "  make dist           Cross-compile via build.sh"
	@echo "  make package        Cross-compile + zip for all platforms"
	@echo "  make package-darwin Build macOS .pkg + .dmg installer"
	@echo "  make init           Create .env and config.yaml from templates"
	@echo "  make dev            Build + start in dev mode (debug logs)"
	@echo "  make check          Check environment readiness"
	@echo "  make install        Install binary to /usr/local/bin"
	@echo "  make clean          Remove build artifacts"
