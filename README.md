# rpi_exporter

A Raspberry Pi hardware exporter for Prometheus — fork of
[lukasmalkmus/rpi_exporter](https://github.com/lukasmalkmus/rpi_exporter)
maintained by **David Collom**.

Builds for `linux/amd64`, `linux/arm64` and `linux/arm/v7` and ships as a
distroless image for minimal surface area.

---

## Metrics exposed

All metrics are namespaced under `rpi_`. Each collector can be enabled or
disabled individually via the `--collector.<name>` flag (or omitted
entirely via `--collect[]=` query parameter to Prometheus).

| collector    | metrics                                                                                     | source                                                              |
|--------------|---------------------------------------------------------------------------------------------|---------------------------------------------------------------------|
| `cpu`        | `rpi_cpu_temperature_celsius`, `rpi_cpu_frequency_hertz`, `rpi_cpu_frequency_{min,max}_hertz` | `/sys/class/thermal/thermal_zone0/temp`, `/sys/devices/system/cpu` |
| `gpu`        | `rpi_gpu_temperature_celsius`, `rpi_gpu_frequency_hertz`, `rpi_gpu_voltage_volts`             | `vcgencmd measure_temp / measure_clock / measure_volts`              |
| `vcgencmd`   | `rpi_raspi_voltage_volts`, `rpi_raspi_throttled_raw`, `rpi_raspi_throttled{name,kind}`       | `vcgencmd measure_volts / get_throttled`                            |
| `textfile`   | any metrics in `*.prom` files under `--collector.textfile.directory`                        | scraped text files                                                   |

The `vcgencmd` collector is the new bit and is what makes it possible to
alert on Pi firmware warnings:

```
rpi_raspi_voltage_volts                         0.85
rpi_raspi_throttled_raw                         0x50005        # sticky bits in upper word
rpi_raspi_throttled{kind="current",name="under_voltage"}                1
rpi_raspi_throttled{kind="current",name="throttled"}                      1
rpi_raspi_throttled{kind="occurred",name="under_voltage_occurred"}        1
rpi_raspi_throttled{kind="occurred",name="throttling_occurred"}           1
```

The `kind` label is one of:
* `current` — latched, will clear when the condition clears.
* `occurred` — sticky since boot, set once and cleared on reboot.

Bit layout for the `throttled` register follows the upstream
[videocore firmware](https://github.com/raspberrypi/firmware):

| bit    | name                                | description                                       |
|-------:|-------------------------------------|---------------------------------------------------|
| `0x1`     | `under_voltage`                  | core voltage dropped below ~0.84V                 |
| `0x2`     | `arm_frequency_capped`           | ARM clock capped due to PSU                        |
| `0x4`     | `throttled`                      | currently throttled                               |
| `0x8`     | `soft_temperature_limit`         | soft thermal limit reached (Pi 4+)                 |
| `0x10000` | `under_voltage_occurred`         | has happened since boot                            |
| `0x20000` | `arm_frequency_capping_occurred` | has happened since boot                            |
| `0x40000` | `throttling_occurred`            | has happened since boot                            |
| `0x80000` | `soft_temperature_limit_occurred`| has happened since boot                            |

## Build & run locally

```bash
make build                          # host arch, ./rpi_exporter
./rpi_exporter --web.listen-address=":9243"
```

## Release pipeline

Releases are **maintainer-triggered** via a single GitHub Actions
workflow (`.github/workflows/release.yaml`) using [svu][svu] for
version resolution and [GoReleaser][goreleaser] for binary packaging
+ GitHub Release + SBOM. The multi-arch Docker image is built and
pushed in a separate job of the same workflow (the Docker pipeline
is intentionally *not* part of GoReleaser so the CI workflow and the
release workflow share the same `docker/build-push-action` invocation
and stay in lock-step).

[svu]: https://github.com/caarlos0/svu
[goreleaser]: https://goreleaser.com

### Triggering a release

1. Make sure `master` is clean (CI green, working tree clean).
2. GitHub → **Actions** → **release** → **Run workflow**:
   - `release_type`:
     - `patch` / `minor` / `major` / `prerelease` — version is computed
       from the current tag via `svu`.
     - `custom` — supply the explicit `vX.Y.Z` in the `version` input.
   - `publish` — uncheck to run a `--snapshot` dry-run locally-equivalent.
   - `prerelease` — mark the GitHub Release as pre-release.
3. The workflow will:
   1. Resolve the next version via `svu` and tag the commit.
   2. Run `goreleaser release` to build multi-platform binaries
      (linux/darwin × amd64/arm64/arm/v7), tar.gz archives, source
      tarball, SPDX SBOM, and SHA256SUMS, and publish a GitHub Release
      with an auto-generated changelog grouped by Conventional Commits
      type.
   3. Build a multi-arch OCI image (`linux/amd64`, `linux/arm64`,
      `linux/arm/v7`) with `docker/build-push-action` and push to
      `docker.io/davidcollom/rpi_exporter` with semver tags.
   4. Sign the image with keyless cosign.

### Required GitHub secrets

| secret               | purpose                                                |
|----------------------|--------------------------------------------------------|
| `DOCKERHUB_USERNAME` | Docker Hub user used to push the image                |
| `DOCKERHUB_TOKEN`    | Docker Hub access token (Settings → Security)         |
| `GITHUB_TOKEN`       | built-in; used to push the GitHub Release and tag      |

## CI

`.github/workflows/ci.yaml` runs on every push and PR to `master` and
combines everything that used to live in separate workflows:

* `lint` — `go vet`, `go test -race`, and `goreleaser check` (config
  validation). Reads the Go version from `go.mod` via
  `actions/setup-go@v5`'s `go-version-file` input, so bumping the
  `go` directive in `go.mod` is the **only** edit needed.
* `build` — multi-arch binary matrix (linux/darwin × amd64/arm64,
  plus linux/arm/v7); each binary is uploaded as a workflow
  artifact.
* `docker` — multi-platform matrix build with `docker/build-push-action`,
  with a smoke test (`/health` + `/metrics`) for the `linux/amd64`
  image.

## Docker image

```bash
make docker-build                   # host-arch local smoke test
make docker-multi-arch              # buildx, all 3 platforms, pushes
```

The image is built in two stages. The build stage uses pinned
`golang:1.27-alpine` and the runtime stage uses
`gcr.io/distroless/static-debian12:nonroot` (uid/gid 65532), so the
final image has no shell and no package manager.

Configuration is environment-driven via flags. Make sure
`/usr/bin/vcgencmd` (host) is mounted into the container — the standard
way to do this in Kubernetes is a hostPath mount from the node:

```yaml
volumeMounts:
  - name: vcgencmd
    mountPath: /usr/bin/vcgencmd
    readOnly: true
volumes:
  - name: vcgencmd
    hostPath:
      path: /usr/bin/vcgencmd
```

## License

© Lukas Malkmus, 2019 (original work).
© David Collom, 2026 (this fork).

Distributed under the Apache License, Version 2.0. See `LICENSE` for
the full text.
