# Plan: Report a filesystem bind-mounted at several mountpoints once

Source: Issue #279
Tier: standard — a behavior change of the disk collector (fewer `disks` entries) that creates, changes or removes no
security control.
Security areas touched: Parsing of local input (`internal/collector/disk.go` collapses the parsed `/proc/mounts`
entries; the parser itself is unchanged; `statfs_linux.go` adds one `stat(2)` per mountpoint)

## Problem / root cause

`dedupeMounts` (`internal/collector/disk.go:262-272`) collapses `/proc/mounts` entries only by mountpoint, and
`Collect` (`disk.go:229-255`) turns every remaining entry whose `statfs` succeeds into its own `Disk`. The bind mounts
systemd creates for `pimonitor.service` (`packaging/pimonitor.service:51-57`: `ProtectSystem=strict`,
`StateDirectory=pimonitor`, `ReadWritePaths=/var/lib/pimonitor`, `PrivateTmp=true`) carry the root filesystem's
device and type in `/proc/mounts`, so `/var/tmp` and `/var/lib/pimonitor` are reported next to `/` with its figures.
Nothing in the collector knows which filesystem is at a path: `fsStats` (`disk.go:144-149`) holds only sizes.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Entries are collapsed only by mountpoint, last entry wins, first-seen order | `disk.go:262-272` | confirmed |
| 2 | The unit sets `ProtectSystem=strict`, `StateDirectory`, `ReadWritePaths=/var/lib/pimonitor`, `PrivateTmp=true` | `packaging/pimonitor.service:51-57` | confirmed |
| 3 | A bind mount is listed in `/proc/mounts` with its source's device and type | probe: `unshare -rm` + `mount --bind a/sub b` in the scratchpad → `/dev/vda …/b ext4 …` | confirmed |
| 4 | A bind mount and its source report the same `st_dev`, and the same `statfs` figures | same probe: `stat -c %d` → `65024` for both; `stat -f` → equal blocks and fsid | confirmed |
| 5 | Two distinct filesystems with identical `statfs` figures report different `st_dev` | probe: two `tmpfs size=1m` mounts → `dev=40`, `dev=41`, both 256 × 4096 blocks | confirmed |
| 6 | `syscall.Stat_t.Dev` is `uint64` on linux/arm, arm64 and amd64 (no conversion, no G115) | `$GOROOT/src/syscall/ztypes_linux_{arm,arm64,amd64}.go` | confirmed |
| 7 | `statfs` failures and the 2 s timeout skip the mountpoint, no error, no crash | `disk.go:198-223`, `277-293` | confirmed |
| 8 | Disk alert states and history series of a mountpoint that leaves `disks` end by the existing vanished-resource rules | `docs/areas/alerts.md:136-139`, `docs/areas/history.md` (*Series of vanished devices*) | confirmed |
| 9 | `platformStatfs` has no test today | `grep platformStatfs internal/collector/*_test.go` → none | confirmed |

## Acceptance criteria

- AC1: Mounts of one filesystem at `/`, `/var/tmp` and `/var/lib/pimonitor` (same device ID) next to
  `/boot/firmware` (another device ID) yield exactly two `Disk` entries, `/` (its device, type and figures) then
  `/boot/firmware`.
- AC2: Two mountpoints with different device IDs and identical `statfs` figures (and the same device name) are both
  reported.
- AC3: Of the mountpoints of one filesystem, the shortest (in bytes, decoded) is kept wherever it stands in
  `/proc/mounts`, the first of equally long ones; the result is the list without the dropped entries, in the order
  of the existing rule (first appearance of each mountpoint).
- AC4: `platformStatfs` reports in `Dev` the `st_dev` of the path's filesystem — equal for a directory and its
  subdirectory, equal to `os.Stat`'s `st_dev` — and returns an error for a path that does not exist.
- AC5: A mountpoint whose `statfs` fails, hangs or is cooling down is still skipped without an error and takes no
  part in the comparison: the other mountpoints of the same filesystem are compared among themselves.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestDiskCollector_Collect_DeduplicatesFilesystems/AC1 systemd bind mounts of root are reported once` | `internal/collector/disk_test.go` |
| AC2 | `TestDiskCollector_Collect_DeduplicatesFilesystems/AC2 distinct filesystems with equal sizes are kept` | `internal/collector/disk_test.go` |
| AC3 | `TestDiskCollector_Collect_DeduplicatesFilesystems/AC3 shortest mountpoint wins regardless of position`, `…/AC3 first of equally long mountpoints wins` | `internal/collector/disk_test.go` |
| AC4 | `TestPlatformStatfs/AC4 reports the device ID of the path`, `TestPlatformStatfs/AC4 missing path is an error` | `internal/collector/statfs_linux_test.go` (`//go:build linux`) |
| AC5 | `TestDiskCollector_Collect_DeduplicatesFilesystems/AC5 failing statfs takes no part` | `internal/collector/disk_test.go` |

Cases: AC1 fixture with `/dev/mmcblk0p2` at `/`, `/var/tmp`, `/var/lib/pimonitor` and `/dev/mmcblk0p1` at
`/boot/firmware`, the fake `statfs` keyed by path (Dev `179<<8|2` for the three, `179<<8|1`). AC2: `/dev/sda1` at
`/mnt/a` and `/mnt/b` with Devs 1 and 2. AC3: order `/srv/data` (A), `/mnt/x` (B), `/srv` (A) → `/mnt/x`, `/srv`;
`/aa` (A) before `/bb` (A) → `/aa`. AC5: `/` fails, `/var/lib/pimonitor` and `/var/tmp` (same Dev) succeed →
`/var/tmp` only. AC4 uses `t.TempDir()` (the file system the test already writes fixtures to).

## Approach

1. `fsStats` gets `Dev`. `platformStatfs` (`statfs_linux.go`) calls `syscall.Stat(path, &st)` after
   `syscall.Statfs`, in the same function, and sets `Dev: st.Dev`; a `Stat` error returns
   `fmt.Errorf("stat %s: %w", path, err)` (never user-visible: `collectDisk` drops it). Both calls run inside the
   goroutine `statfsWithTimeout` already starts, so the bound stays one timeout and at most one parked goroutine per
   mountpoint and cooldown; the seam the existing `TestDiskCollector_Collect_HungStatfsTimesOutAndCoolsDown` observes
   it through is `statfsFunc`, unchanged. `statfs_other.go` is unchanged.
2. `collectDisk` returns an `fsDisk` (the `Disk` plus the `Dev` of its `fsStats`) instead of a `Disk`.
3. `Collect` gathers the `fsDisk`s in the order `dedupeMounts` gives and returns `dedupeFilesystems(candidates)`:
   pass 1 picks per `dev` the index of the shortest mountpoint (`len`, strict `<`, so the first of equal ones stays);
   pass 2 appends, in order, each candidate that is its device's pick. The result is allocated with `make` (non-nil,
   `[]` in JSON, as today). Excluded types are filtered before `collectDisk`, as today.
4. `dedupeMounts` and the mountpoint-keyed cooldown stay as they are; every unique mountpoint is still statted once.

No new user-visible output (fewer entries only); the `disks` JSON shape is unchanged.

Early draft PR: no

## Options considered

The four options of [0027](../../docs/decisions/0027-filesystem-identified-by-device-id.md): device ID from
`stat(2)` (chosen), `/proc/self/mountinfo` with `major:minor` and root (a second parser, btrfs subvolumes share
`major:minor`; it would let the original mount win instead of the shortest, which the issue does not ask), `statfs`
`f_fsid` (filesystem-defined, may be zero), equal device name plus equal figures (merges coincidences the issue says to
keep). Also the issue's "shortest mountpoint wins" versus "first-seen wins": shortest is the issue's expected
behavior and keeps `/` for the systemd case whatever the mount order.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `disk.go`: `fsStats`, `fsDisk`, `collectDisk`, `Collect`, `dedupeFilesystems` | device ID, filesystem dedupe |
| `internal/collector` | `statfs_linux.go`: `platformStatfs` | adds `stat(2)` |

## Signatures (for the Dev's skeleton)

- `fsStats` gains the field `Dev uint64 // device ID of the filesystem at the path (stat(2) st_dev)`.
- `type fsDisk struct { disk Disk; dev uint64 }`
- `func (c *DiskCollector) collectDisk(e mountEntry, now time.Time) (candidate fsDisk, ok bool)` (was `(disk Disk, ok bool)`)
- `func dedupeFilesystems(candidates []fsDisk) []Disk`

`Collect`, `dedupeMounts`, `statfsFunc` and `platformStatfs` keep their signatures. Names follow `go vet`/golangci-lint
defaults (`.golangci.yml` adds only `gocognit` 15; both new functions stay two plain loops).

## Test files

`internal/collector/disk_test.go`, new `internal/collector/statfs_linux_test.go`.

Existing tests whose fake `statfs` returns the same `fsStats` for every path now collapse into one entry; each gets a
fake returning a distinct `Dev` per path, assertions untouched — adapted by the **Tester** in step 5 (old and new
code both compile against them): `TestDiskCollector_Collect` (`/boot/firmware`, `/mnt/usb`),
`TestDiskCollector_Collect_DeduplicatesMountpoints` (`/data`, `/`),
`TestDiskCollector_Collect_HungStatfsTimesOutAndCoolsDown` (`/`, `/mnt/usb`). Unchanged: `…_ExcludesNetworkFS`,
`…_UsedPercentMatchesDf`, `…_ZeroDenominator` (one surviving mountpoint), `…_SkipsFailingStatfs`,
`…_NoMountsReturnsEmptyNotNilSlice` (none survives), `collector_test.go` (no `statfs` fake with several mounts).

Files no unit test can cover: none (the `Stat`-error branch after a successful `Statfs` is one unreachable line).

## Areas

- `docs/areas/metrics.md (+60 words)` (2936 words now): *Filesystems and disk I/O*, first bullet — a filesystem
  mounted at several mountpoints (bind mounts, such as the service's own systemd ones) is reported once, at its
  shortest mountpoint, the first of equally long ones, identified by the device ID at the path; the others are left
  out, the rest keep their order; distinct filesystems stay even with equal sizes; a mountpoint skipped in a tick
  takes no part. Links 0027.

## Documentation updates

- `docs/areas/metrics.md` as above (Dev).
- `docs/API.md` (notes on `disks`, line 253): "at most one entry per mountpoint" gains "and per filesystem: a
  filesystem mounted at several mountpoints appears once, at its shortest mountpoint" (Dev).
- `README.md` feature bullet *Filesystem usage* (line 20): add that a filesystem bind-mounted at several places is
  listed once (Dev).
- Sweep: `docs/ARCHITECTURE.md` (lines 144-157, 234-237: per-mountpoint keys and vanished-mountpoint rules, still
  true), `docs/areas/alerts.md`, `history.md`, `dashboard.md`, `notifications.md`, `docs/integrations/openhab.md`
  (`/` stays listed) — no change. No changelog file exists; the PR carries its release-notes label.

## Architecture check

The no-fabricated-reading guarantee holds: every reported entry is a real `statfs` of its mountpoint; a failed
`stat` skips the mountpoint like a failed `statfs`. Process isolation and the unprivileged service are unchanged
(`stat(2)` needs no privilege beyond the path lookup `statfs` already does). No `docs/ARCHITECTURE.md` change.

## Security considerations

- [x] No limits: no new parsed input; `/proc/mounts` parsing is unchanged.
- [x] None: `stat` and `statfs` errors are dropped in `collectDisk`, never logged or served.

## Decision records

- `docs/decisions/0027-filesystem-identified-by-device-id.md` (Proposed)

## Out of scope / follow-ups

- Preferring the original mount over the shortest mountpoint (needs `/proc/self/mountinfo`, 0027 option 2).
