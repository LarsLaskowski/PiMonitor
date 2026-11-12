# 0004: The final history write waits for the running one

- **Status:** Accepted
- **Date:** 2026-11-12
- **Area:** History
- **Source:** Issue #198
- **Supersedes:** —

## Context

History writes run in the background behind a try-lock: a write that falls due while another is still running is
skipped, because the next one carries newer data anyway. The final write on a clean shutdown used the same
try-lock, so when a slow-tick write was still running at shutdown, the final write was skipped and the file kept
that earlier write's state; the points collected since were lost, although a clean stop was meant to lose none.
The process bounds its whole shutdown by one grace period shared with the HTTP server, so a stalled write must
not hold the process up.

## Options considered

1. **The final write waits for the running write, then writes the current state** — the try-lock is taken by
   waiting instead of trying; the process's existing shutdown deadline still bounds the wait.
2. **The final write runs at once, beside the running write** — no waiting, but two writers rename over the same
   file and the older snapshot can land last, so the file may still hold the earlier state.
3. **A shutdown method with its own context, called by `main`** — moves the deadline into the collector, but
   changes the collector's API and splits the final write from `Run`, which then returns before the history is
   saved, for a bound `main` already enforces.

## Decision

On shutdown the collector waits until no history write is running, then encodes and writes the history as it is
at that moment (option 1); `main` keeps bounding the wait by the shutdown grace period. The rules are in
[History](../areas/history.md#persistence-when-it-is-written).

## Consequences

- A clean stop with a healthy disk leaves the latest state in `history.bin`.
- A stalled write delays the final one until the grace period ends; the process then exits without it, and the
  atomic replacement keeps the previous file intact.
- Writes during operation keep skipping a write that falls due while one is running.
