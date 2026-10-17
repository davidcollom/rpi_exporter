# Experimental direct mailbox firmware transport

Use `--firmware.transport=mailbox` to run the existing GPU and throttling collectors directly through the Raspberry Pi firmware property mailbox, without executing the host `vcgencmd`. Default remains `exec` for backwards compatibility.

This implements the `GET_GENCMD_RESULT` property tag, based on the documented mailbox method of [bcrisp4/pi5_exporter](https://github.com/bcrisp4/pi5_exporter/blob/main/docs/mailbox.md), and is intended for **Pi 3, 4 and 5** on sufficiently recent firmware. It is not hardware-validated yet.

Requirements:
- Linux host exposing `/dev/vcio_gencmd` or `/dev/vcio`.
- The container must have permission to open and issue ioctls against that character device. A hostPath bind mount alone does **not** grant device-cgroup permission in containerd.
- The group owning the host device must be accessible to the container, and a compatible runtime seccomp profile is required.
- Architectures supported by the project's multi-arch build (ARMv7, ARM64); ioctl pointer size is detected at runtime.

Checks on one node:
```sh
ls -l /dev/vcio* 
rpi_exporter --firmware.transport=mailbox
curl -fsS localhost:9243/metrics | grep -E 'rpi_(scrape_collector_success|raspi_throttled)'
```

Do not report collector failure as zero undervoltage. A collector failure should result in `rpi_scrape_collector_success{collector="vcgencmd"} == 0`.

For k3s, test this on a **single opted-in worker** before removing the existing exporter. If accessing the host device still returns EPERM, inspect device-cgroup policy and consider a host-side exporter before resorting to privileged pods.

No changes to deployed DaemonSets or alert rules are included in this PR.
