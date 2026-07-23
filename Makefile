# generate all the make targets to build cmd/server.go
GOROOT=$(shell go env GOROOT)
GO=$(GOROOT)/bin/go
GOFLAGS = ''
PROJECT = go-fitter
VERSION = $(shell git describe --tags --always --dirty="-dev")
LDFLAGS = -X 'main.Version=$(VERSION)' -X 'main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'

GCLOUD_PROJECT = triathlon-480307

BUILD_DIR = ./build
BUILD_TARGET = $(BUILD_DIR)/go-fitter

dev:
	$(GO) run ./main.go

default: help

all: clean build

version:
	echo $(VERSION)

.PHONY: help
## help: prints this help message
help:
	@echo "Usage: \n"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' |  sed -e 's/^/ /'


.PHONY: build
## build: builds for linux/mac on arm64 & arm
build: build-linux-amd64 build-linux-armv7 build-darwin-amd64 build-darwin-arm64

build-linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-server-amd64-linux ./main.go
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-sync-amd64-linux ./cmd/sync.go

build-linux-armv7:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-server-armv7-linux ./main.go
	GOOS=linux GOARCH=arm $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-sync-armv7-linux ./cmd/sync.go

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-server-amd64-darwin ./main.go
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-sync-amd64-darwin ./cmd/sync.go

build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-server-arm64-darwin ./main.go
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags $(GOFLAGS) -o $(BUILD_DIR)/$(PROJECT)-sync-arm64-darwin ./cmd/sync.go

docker-publish:
	@docker buildx build --push --platform linux/amd64 -t eu.gcr.io/$(GCLOUD_PROJECT)/sync:latest -f Dockerfile .

.PHONY: clean
## clean: call Felix ;)
clean:
	$(GO) mod tidy
	rm -rf $(BUILD_DIR)/*