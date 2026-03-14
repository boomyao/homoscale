VERSION ?= $(shell if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git describe --tags --always --dirty; else printf dev; fi)
COMMIT ?= $(shell if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git rev-parse --short HEAD; else printf none; fi)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
TARGETS ?= $(shell go env GOOS)/$(shell go env GOARCH)

.PHONY: build test build-android apk-debug dist dist-android clean

build:
	@mkdir -p bin
	go build -trimpath -ldflags "-s -w -X homoscale/internal/homoscale.version=$(VERSION) -X homoscale/internal/homoscale.commit=$(COMMIT) -X homoscale/internal/homoscale.date=$(BUILD_DATE)" -o ./bin/homoscale ./cmd/homoscale

test:
	go test ./...

build-android:
	@mkdir -p bin
	GOOS=android GOARCH=arm64 go build -trimpath -ldflags "-s -w -X homoscale/internal/homoscale.version=$(VERSION) -X homoscale/internal/homoscale.commit=$(COMMIT) -X homoscale/internal/homoscale.date=$(BUILD_DATE)" -o ./bin/homoscale-android-arm64 ./cmd/homoscale

apk-debug:
	cd android && ./gradlew assembleDebug

dist:
	VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILD_DATE="$(BUILD_DATE)" TARGETS="$(TARGETS)" ./scripts/dist.sh

dist-android:
	VERSION="$(VERSION)" COMMIT="$(COMMIT)" BUILD_DATE="$(BUILD_DATE)" TARGETS="android/arm64" ./scripts/dist.sh

clean:
	rm -rf ./bin ./dist
