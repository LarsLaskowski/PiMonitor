# 0027: A filesystem is identified by the device ID of its mountpoint and reported once

- **Status:** Accepted
- **Date:** 2026-11-25
- **Area:** Metrics
- **Source:** Issue #279
- **Supersedes:** —

## Context

`pimonitor.service` runs with `ProtectSystem=strict`, `PrivateTmp=true`, `StateDirectory=pimonitor` and
`ReadWritePaths=/var/lib/pimonitor`, which systemd implements as bind mounts in the service's mount namespace. A bind
mount appears in `/proc/mounts` with the same device and type as the filesystem it shows, and `statfs` returns that
filesystem's figures, so the disk collector, which collapsed entries only by mountpoint, listed the root filesystem
three times (`/`, `/var/tmp`, `/var/lib/pimonitor`). `/proc/mounts` alone cannot tell a bind mount from a second,
distinct filesystem on an equally named device, and two distinct filesystems can have identical sizes.

## Options considered

1. **The device ID of the filesystem at each mountpoint (`stat(2)` `st_dev`), read in the same call as `statfs`** —
   a bind mount shares the ID of the filesystem it shows, distinct filesystems never share one (measured: a bind
   mount and its source both report `dev=65024`, two `tmpfs` mounts of equal size `40` and `41`). No new parser,
   one more syscall per mountpoint inside the existing timeout. Cannot tell which of several mountpoints is the
   original mount.
2. **`/proc/self/mountinfo`, keyed by `major:minor` and the mount's root** — knows the source root of every bind
   mount, so the original mount could win instead of the shortest path. A new reader of a second kernel format, and
   `major:minor` is the superblock's device, shared by every btrfs subvolume, so it needs a root-containment rule on
   top to keep separately mounted subvolumes apart.
3. **`statfs` `f_fsid`** — no extra syscall, but its content is filesystem-defined (`statfs(2)`: "Nobody knows what
   `f_fsid` is supposed to contain"), and a filesystem that leaves it zero would merge with every other such one.
4. **Equal device name and equal `statfs` figures** — needs no syscall, but merges two distinct filesystems that happen
   to match and splits a bind mount when a write lands between two `statfs` calls.

## Decision

The collector identifies each mountpoint's filesystem by the device ID `stat(2)` reports for the path and reports a
filesystem once, at its shortest mountpoint (the first of equally long ones); the rule is in
[Metrics](../areas/metrics.md#filesystems-and-disk-io).

## Consequences

- The service's own systemd bind mounts disappear from `disks`, `GET /metrics`, the history and the alert states;
  their series and states end by the existing rules for a vanished mountpoint.
- A filesystem bind-mounted by the operator is shown at its shortest mountpoint, which need not be the mount the
  operator made first; reporting the original mount instead would take option 2.
- A mountpoint left out of a tick (failed or hung `statfs`, cooldown) takes no part in that tick's comparison, so in
  that tick another mountpoint of the same filesystem can stand in for it.
- Separately mounted btrfs subvolumes, for which `stat(2)` is expected to report a device ID each (not measured),
  stay separate entries, each with the figures of the whole filesystem.
