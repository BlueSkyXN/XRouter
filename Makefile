.PHONY: run test build fmt vet clean docker package package-current release-snapshot checksum

GO ?= go
BINARY ?= xrouter
CONFIG ?= config.example.json
DIST_DIR ?= dist
PACKAGE_DIR ?= $(DIST_DIR)/packages
CGO_ENABLED ?= 0
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
BIN_EXT :=
ifeq ($(GOOS),windows)
BIN_EXT := .exe
endif
PACKAGE_NAME := xrouter_$(VERSION)_$(GOOS)_$(GOARCH)

run:
	$(GO) run . -config $(CONFIG)

fmt:
	gofmt -w *.go

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)$(BIN_EXT) .

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t xrouter:$(VERSION) .

clean:
	rm -rf $(DIST_DIR)

package: clean test package-current

package-current: build
	rm -rf $(PACKAGE_DIR)/$(PACKAGE_NAME)
	mkdir -p $(PACKAGE_DIR)/$(PACKAGE_NAME)
	cp $(DIST_DIR)/$(BINARY)$(BIN_EXT) $(PACKAGE_DIR)/$(PACKAGE_NAME)/
	cp README.md LICENSE config.example.json $(PACKAGE_DIR)/$(PACKAGE_NAME)/
	cp -R docs examples $(PACKAGE_DIR)/$(PACKAGE_NAME)/
	if [ "$(GOOS)" = "windows" ]; then \
		(cd $(PACKAGE_DIR) && zip -qr $(PACKAGE_NAME).zip $(PACKAGE_NAME)); \
	else \
		tar -C $(PACKAGE_DIR) -czf $(PACKAGE_DIR)/$(PACKAGE_NAME).tar.gz $(PACKAGE_NAME); \
	fi

release-snapshot: clean test
	for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; \
		goarch=$${platform#*/}; \
		$(MAKE) GOOS=$$goos GOARCH=$$goarch package-current; \
	done
	$(MAKE) checksum

checksum:
	cd $(PACKAGE_DIR) && shasum -a 256 *.tar.gz *.zip > SHA256SUMS
