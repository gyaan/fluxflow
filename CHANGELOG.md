# Changelog

All notable changes to this project will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Planned
- Kafka and SQS event source adapters
- `startswith` / `endswith` condition operators
- Points ledger persistence (PostgreSQL adapter)
- Distributed hot-reload via etcd/Consul
- Request-level deduplication by `event.id`
- gRPC ingestion endpoint

---

## [0.1.0] — 2026-02-21

Initial release.

### Added

**Core engine**
- DAG-based rule evaluation with depth-first traversal and early branch pruning
- YAML rule configuration (`version`, `scenarios`, `conditions`, `actions`)
- Config hot-reload via `fsnotify` file watcher — zero downtime rule updates
- Atomic DAG swap using `atomic.Pointer[dag.Graph]` — lock-free on the read path

**Expression language**
- Recursive-descent parser producing a typed AST (compiled once at startup)
- Operators: `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`, `matches` (regex)
- Logical combinators: `AND`, `OR`, `NOT`, parentheses for grouping
- Arithmetic operators in `points_formula`: `*`, `/`, `+`, `-`
- Field namespaces: `payload.*`, `meta.*`, `event.type`, `event.source`, `event.actor_id`

**Action system**
- `ActionExecutor` interface — pluggable action types
- `reward_points` action: fixed points or formula-based (`payload.amount * 0.05`)
- Operations: `award` and `deduct`

**HTTP API**
- `POST /v1/events` — synchronous single-event ingestion
- `POST /v1/events/batch` — async batch ingestion (up to 100 events)
- `GET /v1/rules` — list loaded scenarios
- `POST /v1/rules/reload` — hot-reload from disk
- `GET /healthz` — liveness probe
- `GET /readyz` — readiness probe (503 when queue >80% full)
- `GET /metrics` — Prometheus metrics

**Concurrency**
- Fixed worker pool: 32 event goroutines, 16 action goroutines (tunable)
- Bounded event queue (10,000 slots) with non-blocking submit and HTTP 429 backpressure
- Graceful shutdown — drains queues before exit

**Observability**
- Prometheus counters: `ifttt_events_enqueued_total`, `ifttt_events_processed_total`, `ifttt_events_dropped_total`
- Prometheus counters with labels: `ifttt_scenarios_matched_total{scenario_id}`, `ifttt_actions_executed_total{action_type,status}`
- Prometheus histogram: `ifttt_event_processing_duration_ms`
- Prometheus gauge: `ifttt_queue_utilization_ratio`
- Structured logging via `log/slog`

**Developer experience**
- Zero external dependencies beyond `yaml.v3`, `fsnotify`, `prometheus/client_golang`, `google/uuid`
- `CGO_ENABLED=0` — pure Go, cross-compiles without a C toolchain
- 24 unit tests across condition parser and DAG evaluator
- `README.md`, `TEST.md`, `DEEPDIVE.md`, `CONTRIBUTING.md`
- GitHub Actions CI: build + test on Go 1.22 and 1.23, golangci-lint

[Unreleased]: https://github.com/gyaan/fluxflow/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gyaan/fluxflow/releases/tag/v0.1.0
