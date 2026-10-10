# Copyright 2026 David Collom
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0

GIT_COMMIT  ?= $(shell git rev-parse --short HEAD)
GIT_TAG     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo $(GIT_COMMIT))
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GO           ?= go
# Use the major.minor form of the toolchain (e.g. 1.27) so the
# `golang:${GO_VERSION}-alpine` base image can be pulled from Docker Hub
# (it does not publish a go1.27.0-alpine tag, only 1.27-alpine).
GO_VERSION   ?= $(shell $(GO) version | awk '{print $$3}' | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/')
GOOS         ?= $(shell $(GO) env GOOS)
GOARCH       ?= $(shell $(GO) env GOARCH)

DOCKER_REPO      ?= docker.io/davidcollom
DOCKER_IMAGE     ?= $(DOCKER_REPO)/rpi_exporter
DOCKER_TAG       ?= $(GIT_TAG)

PLATFORMS        ?= linux/amd64,linux/arm64,linux/arm/v7

# Inject build-time metadata into the variables published by
# github.com/prometheus/common/version (which our exporter exposes via
# rpi_exporter_build_info{version,revision,branch,...}).
PROM_VERSION_PKG := github.com/prometheus/common/version
LDFLAGS          := \
  -X '$(PROM_VERSION_PKG).Version=$(GIT_TAG)' \
  -X '$(PROM_VERSION_PKG).Revision=$(GIT_COMMIT)' \
  -X '$(PROM_VERSION_PKG).Branch=$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)' \
  -X '$(PROM_VERSION_PKG).BuildUser=$(shell whoami)' \
  -X '$(PROM_VERSION_PKG).BuildDate=$(BUILD_DATE)' \
  -X '$(PROM_VERSION_PKG).GoVersion=$(GO_VERSION)'

.PHONY: all build build-binary test vet lint docker-build docker-push docker-multi-arch clean

all: test build

# Local build (host arch). Prints the artifact to ./rpi_exporter.
build: build-binary
build-binary:
	$(GO) build -trimpath \
	  -ldflags "$(LDFLAGS)" \
	  -o ./rpi_exporter .

# Run unit tests (currently there are none beyond compilation).
test:
	$(GO) test -trimpath -count=1 ./...

# Static checks.
vet:
	$(GO) vet ./...

# Docker build for the host arch (fast local smoke test).
docker-build:
	docker build --rm --tag $(DOCKER_IMAGE):dev-$(GOARCH) \
	  --build-arg VERSION=$(GIT_TAG) \
	  --build-arg COMMIT=$(GIT_COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  --build-arg GO_VERSION=$(GO_VERSION) .

# Push the host-arch image to the configured registry.
docker-push: docker-build
	docker push $(DOCKER_IMAGE):dev-$(GOARCH)

# Multi-arch build + push to the configured registry using buildx.
# Set PLATFORMS to override the defaults.
docker-multi-arch:
	docker buildx build --rm \
	  --platform $(PLATFORMS) \
	  --tag $(DOCKER_IMAGE):$(DOCKER_TAG) \
	  --tag $(DOCKER_IMAGE):latest \
	  --build-arg VERSION=$(GIT_TAG) \
	  --build-arg COMMIT=$(GIT_COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  --build-arg GO_VERSION=$(GO_VERSION) \
	  --push .

clean:
	rm -f ./rpi_exporter
	$(GO) clean -cache -testcache
