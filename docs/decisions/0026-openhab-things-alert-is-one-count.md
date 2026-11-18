# 0026: The openHAB Things alert is one count with a capped list

- **Status:** Accepted
- **Date:** 2026-11-18
- **Area:** Alerts
- **Source:** Issue #184
- **Supersedes:** —

## Context

An openHAB install can hold hundreds of Things, and a dropped bridge takes all of its child Things offline at once.
The operator needs to know that Things are offline and which ones. Thing UIDs and labels come from openHAB and are
untrusted text.

## Options considered

1. **One metric per Thing (`resource` = UID)** — names each Thing in its own event, but the state grows with the
   install, a bridge outage sends one notification per child, and a removed Thing needs pruning like a filesystem.
2. **One metric, `openhab_things_offline`, whose value is the count, with the first 10 counted Things by UID in its
   events** — bounded state and one notification per change; a Thing that goes offline while others already are
   changes no level and sends nothing.

## Decision

The Things rule is one metric whose value is the number of counted Things; each of its events carries up to 10 of
them, sorted by UID (option 2). Which Things count and the list's fields are in [Alerts](../areas/alerts.md).

## Consequences

- At most one state and 10 Things per event, each string cut to 256 bytes; with the 100-event cap the engine holds
  at most 1,000 Things, about 1 MiB of Thing text, whatever a label's length in openHAB.
- An operator who wants one alert per Thing uses openHAB's own rules or the Prometheus Thing counts.
