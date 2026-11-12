# Areas

What the software does, area by area. Each area has one document here that states the behavior a user, an
operator or another component can rely on — formats, limits, error behavior, guarantees. The
[decision records](../decisions/README.md) explain *why* individual choices were made; the area documents say
*what holds today*. `ARCHITECTURE.md` shows how the parts fit together and links the areas instead of repeating them.

## Rules

- **One document per area**, `docs/areas/<slug>.md`, created from [`_template.md`](_template.md) and listed in the
  index below. The squad keeps it in step with the code (the Lead plans the update, the Dev writes it against the code as
  built): a change in behavior updates the area document in the same pull request.
- **Normative and stable.** Write what must hold, not how one language or library does it: the test is whether
  the document would still be true if the component were rewritten in another language. No class, package or
  function names as a contract (mention them only as a pointer to where it is implemented).
- **One home per behavior.** A cross-cutting topic (security, offline behavior) lives in the area that owns it; the
  others link there.
- **Size.** One readable document per area: split one that grows past `AREA_MAX_WORDS` words
  (`.squad/tools/squad_settings.py`; `decision-check.py` warns and names the split) into narrower areas — a Lead
  decision like any new area (below) — and fold one that says only a few sentences into a neighbor. Development and
  release process are not areas (see `CONTRIBUTING.md`).
- **A new area is a Lead decision** made in the plan (`plan.md`, *Areas*), with its name and scope; the Dev writes
  the document and its index row with the plan's documentation updates.
- Every decision record names its area in its `Area:` field (`—` for a record about the process). Area documents are
  not frozen by a release; their history is Git.

## Index

<!-- project:begin area-index -->
| Area | Scope | Not here |
| ---- | ----- | -------- |
| [Metrics](metrics.md) | Which metrics are collected, their sources and units, the fast and slow collection ticks, top-N processes, apt and Pi-hole update state, behavior when a source is missing | Retention and persistence ([History](history.md)), thresholds ([Alerts](alerts.md)), JSON shapes ([`API.md`](../API.md)) |
| [History](history.md) | The time series kept per metric: retention window and capacity, eviction of vanished devices, the `history.bin` file and its restore at startup | The history endpoint's shape ([`API.md`](../API.md)), sparklines ([Dashboard](dashboard.md)) |
| [Alerts](alerts.md) | Thresholds, debounce, `ok`/`warn`/`crit` states, `fired`/`cleared` events, vanished resources, what `GET /api/v1/alerts` reports | Delivery to webhooks ([Notifications](notifications.md)) |
| [Notifications](notifications.md) | Webhook delivery: payload and templates, severity filter, per-webhook queues, retries, rate limit, timeouts, logging | When an alert fires ([Alerts](alerts.md)) |
| [HTTP API](http-api.md) | Routes and server-wide rules: versioning, API key, compression, caching, concurrency limit, security headers, TLS, `/healthz`, `/metrics`, server stats, timeouts | Per-endpoint JSON shapes ([`API.md`](../API.md)), the web UI ([Dashboard](dashboard.md)) |
| [Dashboard](dashboard.md) | The web UI: asset serving, polling, hidden-tab pause, API key prompt and browser storage, rendering, value coloring, theme, card layout | Headers and CSP ([HTTP API](http-api.md)), data shapes ([`API.md`](../API.md)) |
| [Configuration](configuration.md) | Config sources and precedence (file, `PIMONITOR_API_KEY`, flags), parsing rules, every option with type, default and valid range, startup failure | What an option does in its area (the area document) |
| [Deployment](deployment.md) | Release archive, `download-latest.sh`, `install.sh`, the systemd units and their privileges, the root apt timer, data directory, process lifecycle, upgrades | Release builds and tags ([`CONTRIBUTING.md`](../CONTRIBUTING.md)), threat model ([`SECURITY.md`](../../SECURITY.md)) |
<!-- project:end area-index -->
