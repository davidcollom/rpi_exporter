# Copyright 2026 David Collom
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0

GIT_COMMIT  ?= $(shell git rev-parse --short HEAD)
GIT_BRANCH  ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unset)
GIT_TAG     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo $(GIT_COMMIT))
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GO           ?= go
GO_VERSION   ?= $(shell $(GO) version | awk '{print $$3}' | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/')
GOOS         ?= $(shell $(GO) env GOOS)
GOARCH       ?= $(shell $(GO) env GOARCH)

DOCKER_REPO      ?= docker.io/davidcollom
DOCKER_IMAGE     ?= $(DOCKER_REPO)/rpi_exporter

PLATFORMS        ?= linux/amd64,linux/arm64,linux/arm/v7

# Inject build-time metadata into the variables published by
# github.com/prometheus/common/version (which the exporter exposes via
# rpi_exporter_build_info{version,revision,...}). Release builds go through
# GoReleaser which sets the same flags; the Makefile target here is only
# for local dev builds.
PROM_VERSION_PKG := github.com/prometheus/common/version
LDFLAGS          := \
  -X '$(PROM_VERSION_PKG).Version=$(GIT_TAG)' \
  -X '$(PROM_VERSION_PKG).Revision=$(GIT_COMMIT)' \
  -X '$(PROM_VERSION_PKG).Branch=$(GIT_BRANCH)' \
  -X '$(PROM_VERSION_PKG).BuildUser=$(shell whoami)' \
  -X '$(PROM_VERSION_PKG).BuildDate=$(BUILD_DATE)' \
  -X '$(PROM_VERSION_PKG).GoVersion=$(GO_VERSION)'

GORELEASER  ?= $(shell command -v goreleaser 2>/dev/null || echo $(HOME)/go/bin/goreleaser)
SVU         ?= $(shell command -v svu 2>/dev/null || echo $(HOME)/go/bin/svu)
SYFT        ?= $(shell command -v syft 2>/dev/null || echo $(HOME)/go/bin/syft)

.PHONY: all build build-binary test vet lint \
        docker-build docker-push docker-multi-arch \
        release-check release-snapshot release \
        install-tools clean

all: test build

# Local build (host arch). ./rpi_exporter artefact.
build: build-binary
build-binary:
	$(GO) build -trimpath \
	  -ldflags "$(LDFLAGS)" \
	  -o ./rpi_exporter .

# Run unit tests.
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
docker-multi-arch:
	docker buildx build --rm \
	  --platform $(PLATFORMS) \
	  --tag $(DOCKER_IMAGE):$(GIT_TAG) \
	  --tag $(DOCKER_IMAGE):latest \
	  --build-arg VERSION=$(GIT_TAG) \
	  --build-arg COMMIT=$(GIT_COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  --build-arg GO_VERSION=$(GO_VERSION) \
	  --push .

# ---- GoReleaser (release) -----------------------------------------------

# Validate the goreleaser config without producing any artifacts.
release-check:
	$(GORELEASER) check

# Build distributables (binaries, archives, image, SBOM, checksums) but
# DO NOT publish them. Use this for a dry-run locally before opening a
# release PR or running the GitHub release workflow.
release-snapshot:
	GO_VERSION=$(GO_VERSION) GIT_BRANCH=$(GIT_BRANCH) \
	  $(GORELEASER) release --clean --snapshot --skip=publish,announce

# Full release: tag the commit, then publish to GitHub + Docker Hub.
# Requires `release_type` (patch|minor|major|prerelease|custom) and
# optional `version` (only used when release_type=custom).
release:
ifndef release_type
	$(error release_type is required: patch|minor|major|prerelease|custom)
endif
	@case "$(release_type)" in \
	  patch)      ver=$$($(SVU) patch) ;; \
	  minor)      ver=$$($(SVU) minor) ;; \
	  major)      ver=$$($(SVU) major) ;; \
	  prerelease) ver=$$($(SVU) prerelease) ;; \
	  custom)     ver=$(version) ;; \
	  *)          echo "unknown release_type=$(release_type)" >&2; exit 2 ;; \
	 esac; \
	case "$$ver" in v*) ;; *) ver="v$$ver" ;; esac; \
	echo "→ releasing $$ver"; \
	GO_VERSION=$(GO_VERSION) GIT_BRANCH=$(GIT_BRANCH) \
	  $(GORELEASER) release --clean --release-version "$$ver"

# Install goreleaser, svu, syft (Homebrew is preferred on macOS).
install-tools:
	go install github.com/goreleaser/goreleaser/v2@latest
	go install github.com/caarlos0/svu/v3@latest
	@command -v syft >/dev/null 2>&1 || { \
	  echo "Installing syft via Homebrew..."; \
	  brew install syft; \
	}

clean:
	rm -f ./rpi_exporter
	$(GO) clean -cache -testcache
	rm -rf ./dist
