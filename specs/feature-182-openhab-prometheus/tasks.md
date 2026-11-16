# Tasks: Expose local openHAB status as Prometheus metrics

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `OpenHABThingStatuses()` (plan, *Signatures*) and the unexported `renderOpenHABMetrics` returning nil | internal/collector/openhab.go, internal/httpapi/prometheus.go | AC1–AC6 | — | Dev | [x] |
| 2 | G1 | Tests of *Tests per criterion* and `TestOpenHABThingStatuses` | internal/httpapi/prometheus_test.go, internal/httpapi/handlers_test.go, internal/collector/openhab_test.go | AC1–AC6 | 1 | Tester | [x] |
| 3 | G1 | Implement the accessor, the openHAB families (plan, *Approach*) and the join in `handlePrometheusMetrics` | internal/collector/openhab.go, internal/httpapi/prometheus.go, internal/httpapi/handlers.go | AC1–AC6 | 2 | Dev | [x] |
| 4 | G2 | Documentation updates of the plan (API.md, the two area documents, the integration guide, README, SECURITY, ARCHITECTURE) and the reported `.squad/project.md` text | docs/API.md, docs/areas/http-api.md, docs/areas/openhab.md, docs/integrations/openhab.md, README.md, SECURITY.md, docs/ARCHITECTURE.md | AC1–AC6 | 3 | Dev | [x] |
