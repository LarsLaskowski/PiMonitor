# Tasks: Read available apt updates from a stable source

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `dpkgStatusPath`, `maxAptOutputBytes`, `maxAptLineBytes`, `updatesTimeout`, `UpdatesCollector` (`aptCachePath`, `statusPath`, `timeout`, `maxOutput`), `NewUpdatesCollector`, `Collect`, `pkgKey`, `installedSet`, `debianVersion`, `parseDebianVersion`, `compareDebianVersions`, `compareVersionPart`, `scanControlRecords`, `readInstalled`, `findUpgrades`, `newCappedReader`, `stateStamp`, `aptStateStamp`, `newestListModTime`, `errOutputTooLarge`, `aptCacheAge`; remove `aptPath`, `parseAptListUpgradable`, `packageUpdateLine` | `internal/collector/updates.go` | AC1–AC5 | — | Dev | [ ] |
| 2 | G1 | Fixtures trimmed and checked for private values from the PM's Trixie arm64 samples | `internal/collector/testdata/apt/*` | AC1 | 1 | Tester | [ ] |
| 3 | G1 | Tests first: *Tests per criterion*, deletion of `TestParseAptListUpgradable*`, adaptation of the existing call sites | `internal/collector/updates_test.go`, `internal/collector/collector_test.go` | AC1–AC5 | 1, 2 | Tester | [ ] |
| 4 | G1 | Implement readers, version comparison, capped reader, stamp and `Collect` until the tests and the *Coverage gate* pass | `internal/collector/updates.go` | AC1–AC5 | 3 | Dev | [ ] |
| 5 | G1 | Documentation updates of the plan; report the exact text for `.squad/project.md` and `.squad/stack.md` | `docs/areas/metrics.md`, `docs/areas/deployment.md`, `SECURITY.md`, `README.md`, `docs/ARCHITECTURE.md`, `docs/CONTRIBUTING.md`, `packaging/pimonitor.service`, `packaging/pimonitor.example.yaml` | AC5 | 4 | Dev | [ ] |
| 6 | G1 | Write the reported text into `.squad/project.md` and `.squad/stack.md` | `.squad/project.md`, `.squad/stack.md` | AC5 | 5 | Orchestrator | [ ] |
| 7 | PM | Manual check of AC1 on a Bookworm Pi (armhf if available) with a hold and a pin, as *Approach* says | — | AC1 | 4 | Product Manager | [ ] |
