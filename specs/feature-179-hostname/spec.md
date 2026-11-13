# Spec: Show the host name in the dashboard header and browser tab

Source: Issue #179
Status: Approved by Lead

## Problem

An operator who runs PiMonitor on several Raspberry Pis cannot tell from a dashboard which Pi it belongs to: the
header reads only the PiMonitor brand, every browser tab is titled `PiMonitor`, and the System card shows Model,
Distribution and Kernel but no host name. Integrations (openHAB, Home Assistant) cannot label a device by its host
name either, because `GET /api/v1/metrics` does not carry it. Details and the proposed design are in the issue.

## Scenarios

- S1: Given a Pi whose host name is `raspberrypi` and a default configuration, when the operator opens the dashboard
  and the first snapshot arrives, then the header reads the brand followed by ` (raspberrypi)`, the host name set
  apart in a muted, normal-weight style and in its original case, the browser tab is titled `raspberrypi – PiMonitor`
  (en dash), and the System card's first row is `Hostname` with `raspberrypi`.
- S2: Given the same Pi, when an integration calls `GET /api/v1/metrics`, then the `system` object carries
  `"hostname": "raspberrypi"` next to its existing fields, which are unchanged.
- S3: Given `hostname_enabled: false` in the configuration, when the dashboard loads or an integration polls, then
  the response has no `system.hostname` key, the header looks exactly as it does today, the tab title stays
  `PiMonitor` and the System card has no `Hostname` row.
- S4: Given the dashboard has not yet received a snapshot (header "Loading…", or "API key required" after a `401`),
  then no host name is shown and the tab title is `PiMonitor`; once a snapshot arrives, S1 applies.
- S5: Given the host name is changed on the Pi while the service runs, then the dashboard and the API keep showing
  the name read when the service started, until the service restarts.

## Errors and edge cases

- E1: The host name source cannot be read (not Linux, as with `make run` on a development machine, or the file is
  missing or unreadable): the API omits `system.hostname`, the dashboard shows no host name as in S3, everything
  else works, and the service logs one warning per process start, never per poll. With `hostname_enabled: false`
  the source is not read and nothing is logged about it.
- E2: The source holds surrounding whitespace or a trailing newline: they are removed. A source that is empty, only
  whitespace, or the kernel's placeholder `(none)` for an unset name counts as no host name (as E1, without a
  warning for an empty value).
- E3: A host name of the kernel's maximum length (64 characters) on a phone-width screen: the header still fits next
  to its buttons without horizontal scrolling; the host name is cut with an ellipsis and its full text is the
  element's tooltip. The System card row wraps or truncates like the other rows.
- E4: A host name that contains markup characters (`<b>pi</b>`): it is shown literally, as text, in the header, tab
  title, tooltip and System card; nothing of it is interpreted as HTML.
- E5: A metrics poll fails after the host name was shown ("Connection error" in the header, or a `401` after a key
  was rotated on the server): the host name and the tab title stay as last received, like every card's values.
- E6: A snapshot taken before the service's startup read finished (the API serves while collection starts) has no
  host name: the dashboard shows none for that poll and shows it from the next snapshot that has it.

## Non-functional requirements

- N1: New configuration option `hostname_enabled` (bool, default `true`), beside `distro_info_enabled` and
  `pi_model_enabled`. Existing configuration files without the key keep working and get the default.
- N2: Privacy: with the default the host name is published on the API, which is unauthenticated unless `api_key` is
  set — the same exposure as the distribution and the model, with the same opt-out (N1).
- N3: Compatibility: the key is additive to `/api/v1` (new optional key, existing keys unchanged), as earlier
  additions such as `wireless` were; no `/api/v2`. Prometheus output and webhook payloads are unchanged.
- N4: The host name is read once at service start, like the other `system` fields; no periodic reading.

## Acceptance criteria

- AC1: `GET /api/v1/metrics` carries `system.hostname` with the host name read at startup, trimmed of surrounding
  whitespace, and every existing field unchanged; a later host name change appears only after a restart (S2, S5, E2,
  N3, N4).
- AC2: The key `system.hostname` is omitted from the response when the source is unreadable, empty, only whitespace
  or `(none)`; an unreadable source logs one warning per process start, never per poll, and the service runs on (E1,
  E2).
- AC3: `hostname_enabled` defaults to `true`; with `false` the response omits `system.hostname`, the source is not
  read and no warning about it is logged (S3, E1, N1, N2).
- AC4: Evaluated on every snapshot: with a host name, the dashboard header shows the brand followed by the host name
  in parentheses, in a separate element with a muted, normal-weight style, in its original case; without one the
  header renders exactly as today — no parentheses, no extra text, no extra space, no empty tooltip. A failed poll
  changes nothing (S1, S3, S4, E5, E6).
- AC5: Evaluated on every snapshot: with a host name the tab title is `<hostname> – PiMonitor`, without one
  `PiMonitor`; a failed poll changes nothing (S1, S3, S4, E5, E6).
- AC6: Evaluated on every snapshot: the System card's first row is `Hostname` with the host name when the snapshot
  has one, and the row is not shown when it has none (S1, S3, E6).
- AC7: The host name reaches the page only as text (text nodes, `document.title`, the `title` attribute), never
  through `innerHTML`; a host name with markup characters is displayed literally (E4).
- AC8: At phone width a 64-character host name is truncated with an ellipsis, carries its full text as tooltip, and
  the header does not overflow its row next to the buttons (E3).

## Out of scope

- The host name in alert webhook payloads or in Prometheus labels or metrics (separate change, own compatibility
  questions).
- A configurable free-text display name instead of the host name.
- Detecting a host name change at runtime (S5 is the specified behavior).
- A new per-metric sub-resource: no `GET /api/v1/metrics/system` exists today, and none is added.
- Exposing `hostname_enabled` in `GET /api/v1/config`: the dashboard decides from the presence of the key.

## Open questions

- Does a per-metric sub-resource expose `system`? Answered: no — `metricsSubResources` in
  `internal/httpapi/routes.go` lists cpu, temperature, memory, disks, network, updates and pihole only.
- Is a new key in `system` a breaking `/api/v1` change? Answered: no — `docs/API.md` excludes only breaking shape
  changes, and `wireless` and `pihole` were added to `v1` as optional keys before.
- Does the service see the real host name under its systemd hardening? Answered: from the unit, `ProtectHostname=true`
  in `packaging/pimonitor.service` gives the service its own UTS namespace, initialized with the host's name at
  start, and `ProtectKernelTunables=true` leaves `/proc/sys` readable. This also makes S5 the only possible
  behavior. Confirmation on a real Pi is part of the plan's verification.
- With the option off, does the System card show `unknown` (as Model and Distribution do today) or no row? Answered:
  the Lead decides, from the issue's criterion that the host name is "absent" from the card, that no row is shown (AC6).
- The issue asks to update the Configuration section of `README.md`. Answered: from the code, that section lists no
  individual options (`README.md`, *Configuration*); the option's home is `docs/areas/configuration.md` and
  `packaging/pimonitor.example.yaml`, and the README *Features* list ("System identity") names the host name. The
  plan lists the exact documentation updates.
- The issue names a 63-character limit. Answered: the kernel allows 64 bytes, so E3 and AC8 use 64.

## Area documents

- `docs/areas/metrics.md`, *System identity and uptime*: `system.hostname` — its source, trimming, the empty cases
  and `(none)`, read once at startup, **omitted** (not empty, unlike its sibling keys) when unknown or when
  `hostname_enabled` is off; *Failures*: its one warning per start.
- `docs/areas/configuration.md`: the `hostname_enabled` row (bool, default `true`).
- `docs/areas/dashboard.md`, *Rendering*: the host name in the header and the tab title and when they are absent,
  the `Hostname` row of the System card, truncation with tooltip, and the host name added to the list of server
  text inserted only as text.
- `docs/API.md` (the shape document, not an area): `hostname` in the example `system` object with a short note that
  says "omitted", and the note on zero values after process start extended to a key that is briefly absent.

## Challenge

The Devil's Advocate raised five minor objections and no major one; all are accepted.

1. AC4 tied to the markup (a static empty element would break "markup exactly as today") — accepted: AC4 now says
   the header *renders* as today (no parentheses, text, space or tooltip).
2. S4 and E5 disagreed on a `401` after a host name was shown — accepted: S4 covers only a dashboard without any
   snapshot yet; E5 now includes the `401` after a key rotation, which keeps host name and title.
3. Option off plus unreadable source would still warn — accepted: AC3 and E1 say the source is not read and nothing
   is logged with the option off; AC2 and E1 say one warning per process start, never per poll.
4. AC4–AC8 cannot be checked by running code (no JS test framework) — accepted: `plan.md`, *Tests per criterion*,
   names a static scan or a manual browser check, with its build, for each.
5. `hostname` is the only `system` key omitted rather than empty, and an early snapshot can lack it — accepted:
   *Area documents* requires "omitted" in `docs/areas/metrics.md` and `docs/API.md` and the zero-values note
   extended; new E6, and AC4–AC6 are evaluated on every snapshot.
