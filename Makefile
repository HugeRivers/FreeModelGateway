BINARY_NAME := fmg
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION  := $(shell go version | awk '{print $$3}')

LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GoVersion=$(GO_VERSION)"

.PHONY: all build build-linux build-darwin build-darwin-intel build-windows build-tray build-tray-windows build-all run test test-coverage lint clean docker docker-run package package-darwin package-windows install uninstall init dev help

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
		-v fmg-data:/app/.fmg \
		--name fmg \
		--restart unless-stopped \
		fmg:$(VERSION)

package: build-all
	@echo "Packaging release archives..."
	@mkdir -p dist
	@for bin in bin/fmg-*; do \
		name=$$(basename "$$bin"); \
		archdir="dist/$$name"; \
		mkdir -p "$$archdir"; \
		cp "$$bin" "$$archdir/"; \
		case "$$name" in *windows*) \
			printf '@echo off\nchcp 65001 >nul\ncd /d "%%%%~dp0"\nfmg-windows-amd64.exe -l info\npause\n' > "$$archdir/start.bat";; \
		esac; \
		(cd dist && zip -rq "$$name.zip" "$$name/"); \
		echo "  dist/$$name.zip"; \
	done

build-tray:
	@echo "Building macOS tray app $(VERSION)..."
	@CGO_ENABLED=1 CGO_CFLAGS="-mmacosx-version-min=11.0" CGO_LDFLAGS="-mmacosx-version-min=11.0" go build $(LDFLAGS) -o bin/fmg-tray ./cmd/tray/
	@echo "  -> bin/fmg-tray $$(du -h bin/fmg-tray | cut -f1)"

build-tray-windows:
	@echo "Building Windows tray app $(VERSION)..."
	@GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
		go build $(LDFLAGS) -o bin/fmg-tray-windows-amd64.exe ./cmd/tray/
	@echo "  -> bin/fmg-tray-windows-amd64.exe $$(du -h bin/fmg-tray-windows-amd64.exe | cut -f1)"

package-darwin:
	@./package-darwin.sh

package-windows: build-windows build-tray-windows
	@echo "Packaging Windows app $(VERSION)..."
	@mkdir -p dist/windows-tmp
	@cp bin/fmg-windows-amd64.exe dist/windows-tmp/fmg.exe
	@if [ -d "web-app" ]; then \
		cp -R web-app dist/windows-tmp/web-app; \
		echo "  -> Copied web-app"; \
	fi
	@cp bin/fmg-tray-windows-amd64.exe dist/windows-tmp/fmg-tray.exe
	@cp cmd/tray/assets/tray-running.png dist/windows-tmp/assets/ 2>/dev/null || true
	@cp cmd/tray/assets/tray-stopped.png dist/windows-tmp/assets/ 2>/dev/null || true
	@(cd dist && zip -rq "fmg-$(VERSION)-windows-amd64.zip" "windows-tmp/")
	@echo "  -> dist/fmg-$(VERSION)-windows-amd64.zip"
	@rm -rf dist/windows-tmp

install: build
	@echo "Installing to /usr/local/bin..."
	@sudo cp bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@sudo chmod +x /usr/local/bin/$(BINARY_NAME)
	@echo "Installed. Run: fmg"

uninstall:
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled."

init:
	@echo "FMG now uses SQLite database for configuration."
	@echo "Run 'make build && ./bin/fmg' to start."

dev: build
	@./bin/$(BINARY_NAME) --log-level debug

	help:
	@echo "Free Model Gateway (FMG) - Makefile targets"
	@echo ""
	@echo "  make build              Build for current platform"
	@echo "  make build-all          Cross-compile for linux/darwin/windows"
	@echo "  make build-tray         Build macOS tray app"
	@echo "  make build-tray-windows Build Windows tray app (requires mingw)"
	@echo "  make run                Build and run"
	@echo "  make test               Run unit tests with race detector"
	@echo "  make lint               Run golangci-lint"
	@echo "  make docker             Build Docker image"
	@echo "  make docker-run         Run Docker container (port 10086)"
	@echo "  make package            Cross-compile + zip for all platforms"
	@echo "  make package-darwin     Build macOS .pkg + .dmg installer"
	@echo "  make package-windows    Build Windows .zip with tray"
	@echo "  make init               Show startup instructions"
	@echo "  make dev                Build + start in dev mode (debug logs)"
	@echo "  make install            Install binary to /usr/local/bin"
	@echo "  make clean              Remove build artifacts"
