# syntax=docker/dockerfile:1.7
#
# Multi-stage Dockerfile for davidcollom/rpi_exporter.
#
# Stage 1: build the static binary with a pinned Go toolchain so the build
#          is reproducible. The exporter is fully static (no cgo) so we can
#          ship a scratch+distroless image for the runtime.
#
# Stage 2: distroless nonroot. Tiny, no shell, no package manager.
#          The exporter only needs to read /sys, /proc and call
#          /usr/bin/vcgencmd from the host (mounted by the daemonset).
#

ARG GO_VERSION=1.27

# --------------------------- build stage ---------------------------
FROM docker.io/library/golang:${GO_VERSION}-alpine AS build

ARG COMMIT
ARG BUILD_DATE
ARG VERSION
ARG GO_VERSION

WORKDIR /src

# Cache the module download separately from the source tree so we don't
# have to redownload on every code change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download -x

COPY . .

# Build a fully static binary. CGO_ENABLED=0 ensures no glibc/musl
# dependency so the binary runs on scratch or distroless:anywhere.
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS="-trimpath"

# Build ldflags are injected by the Makefile via buildx --build-arg; if
# buildx is not used we still get reasonable defaults from the
# prometheus/common/version package.
ARG LDFLAGS_EXTRA="-X 'github.com/prometheus/common/version.Version=${VERSION}' \
                  -X 'github.com/prometheus/common/version.Revision=${COMMIT}' \
                  -X 'github.com/prometheus/common/version.Branch=master' \
                  -X 'github.com/prometheus/common/version.BuildUser=docker' \
                  -X 'github.com/prometheus/common/version.BuildDate=${BUILD_DATE}' \
                  -X 'github.com/prometheus/common/version.GoVersion=go${GO_VERSION}'"

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "${LDFLAGS_EXTRA}" \
        -o /out/rpi_exporter .

# --------------------------- runtime stage --------------------------
# distroless "static" uses user 65532. We use /etc/passwd-group with a
# uid:gid mapping so the binary can write its log to stderr (which
# `kubectl logs` already plumbs). No other filesystems are needed.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/rpi_exporter /bin/rpi_exporter

# /metrics is exported on :9243 by default.
EXPOSE 9243

USER 65532:65532
ENTRYPOINT ["/bin/rpi_exporter"]
