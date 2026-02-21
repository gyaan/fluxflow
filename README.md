# FluxFlow — IFTTT Rule Engine

<p align="center">
  <a href="https://github.com/gyaan/fluxflow/actions/workflows/ci.yml">
    <img src="https://github.com/gyaan/fluxflow/actions/workflows/ci.yml/badge.svg" alt="CI">
  </a>
  <a href="https://github.com/gyaan/fluxflow/releases/latest">
    <img src="https://img.shields.io/github/v/release/gyaan/fluxflow" alt="Latest release">
  </a>
  <a href="https://pkg.go.dev/github.com/gyaneshwarpardhi/ifttt">
    <img src="https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go" alt="Go version">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/gyaan/fluxflow" alt="License: MIT">
  </a>
  <a href="https://github.com/gyaan/fluxflow/releases">
    <img src="https://img.shields.io/github/downloads/gyaan/fluxflow/total" alt="Downloads">
  </a>
</p>

<p align="center">
  A high-performance, YAML-driven rule engine for loyalty programmes built in Go.<br>
  Events are evaluated against a DAG of configurable rules and trigger actions — with zero-downtime hot-reload.
</p>

---

## Features

- **DAG evaluation** — depth-first traversal with early branch pruning; untouched subtrees cost zero CPU
- **YAML rules** — no redeployment needed; change a file, rules update in under a second
- **Atomic hot-reload** — `atomic.Pointer[dag.Graph]` swap; zero locks on the read path
- **Expression language** — `payload.amount > 1000 AND payload.category == "food"` compiled to AST once at startup
- **Formula actions** — `points_formula: "payload.amount * 0.05"` evaluated at runtime against event payload
- **Worker pool** — 32 fixed goroutines, 10k-slot bounded queue, HTTP 429 backpressure
- **Prometheus metrics** — counters, histograms, queue utilisation gauge; scrape `/metrics`
- **Pluggable actions** — one interface, one registration line; add webhooks, email, CRM with no engine changes

---

## How it works

```
HTTP POST /v1/events
        │
        ▼
┌───────────────────┐
│   API Handler     │  JSON decode · auto-fill ID · validate
└────────┬──────────┘
         │
         ▼  non-blocking channel submit (429 if full)
┌───────────────────────────────────────┐
│   Event Queue  ░░░░░░░░  cap=10,000  │
└────────┬──────────────────────────────┘
         │
    ┌────┴────┐  32 goroutines (pre-allocated, zero alloc per event)
    ▼         ▼
 worker    worker  …
    │
    │  atomic.Pointer[Graph].Load()  — no lock
    ▼
┌──────────────────────────────────────────────┐
│  DAG — Depth-First Evaluation                │
│                                              │
│  ScenarioNode (event.type + source filter)   │
│  └── ConditionNode  (AST eval, short-circuit)│
│       └── ConditionNode                      │
│            └── ActionNode  ← leaf: execute   │
└──────────────────────────────────────────────┘
         │
         ▼
  ActionExecutor.Execute()
  (reward_points, webhook, …)
         │
         ▼
  EventResult → HTTP response
```

**Rules are a YAML tree → compiled to an immutable DAG at startup (or reload). No parsing at eval time.**

---

## Quick start

```bash
git clone https://github.com/gyaan/fluxflow.git
cd fluxflow
CGO_ENABLED=0 go run cmd/server/main.go
```

```bash
# High-value food transaction → 75 bonus points (1500 × 0.05)
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"type":"transaction","source":"pos-system","actor_id":"user_42",
       "payload":{"amount":1500,"category":"food"}}'
```

```json
{
  "event_id": "...",
  "duration_ms": 0,
  "scenarios_matched": ["sc_high_value_food"],
  "actions_executed": [
    {
      "action_id": "act_bonus_points",
      "type": "reward_points",
      "success": true,
      "message": "Awarded 75 points to user_42 — High-value food purchase bonus"
    }
  ]
}
```

Or download a pre-built binary from the [Releases page](https://github.com/gyaan/fluxflow/releases/latest):

```bash
# Linux amd64
curl -Lo fluxflow https://github.com/gyaan/fluxflow/releases/latest/download/fluxflow-v0.1.0-linux-amd64
chmod +x fluxflow && ./fluxflow
```

> **macOS without Xcode tools:** prefix all `go` commands with `CGO_ENABLED=0`.

---

## Installation

### From source

```bash
CGO_ENABLED=0 go install github.com/gyaneshwarpardhi/ifttt/cmd/server@latest
```

### Pre-built binaries

Available for Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`) on the [Releases page](https://github.com/gyaan/fluxflow/releases). SHA-256 checksums are included.

---

## Project layout

```
fluxflow/
├── cmd/server/main.go                  # Entry point
├── internal/
│   ├── event/event.go                  # Canonical Event struct
│   ├── config/                         # YAML schema · loader · validator
│   ├── condition/                      # Tokenizer · AST parser · evaluator
│   ├── dag/                            # Graph · builder · DFS evaluator
│   ├── action/                         # Executor interface · registry · reward_points
│   ├── engine/                         # Worker pool · atomic graph swap
│   ├── api/                            # HTTP handlers · middleware
│   └── metrics/                        # Prometheus instrumentation
├── configs/rules.yaml                  # Example rules
├── README.md · TEST.md · DEEPDIVE.md · CHANGELOG.md · CONTRIBUTING.md
└── go.mod
```

---

## Configuration

### Server flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |
| `-config` | `configs/rules.yaml` | Path to YAML rules file |

### Engine tuning

```yaml
# configs/rules.yaml
engine:
  event_workers: 32       # goroutines evaluating events
  action_workers: 16      # goroutines running I/O-bound actions
  queue_depth: 10000      # max events buffered (429 when full)
  event_timeout_ms: 5000  # sync response timeout
  fail_open: true         # on condition error, skip branch (don't fail event)
```

### Writing rules

```yaml
version: v1
scenarios:
  - id: sc_vip_cashback
    description: "10% cashback for VIP members on electronics"
    enabled: true
    event_types: [transaction]
    sources: [mobile-app, web]        # omit to match all sources
    children:
      - condition:
          id: cond_is_vip
          expression: 'meta.tier == "vip"'
          children:
            - condition:
                id: cond_is_electronics
                expression: 'payload.category == "electronics"'
                children:
                  - action:
                      id: act_vip_cashback
                      type: reward_points
                      params:
                        operation: award
                        points_formula: "payload.amount * 0.10"
                        reason: "VIP electronics cashback"
```

No restart required — save the file or call `POST /v1/rules/reload`.

### Expression language

| Operator | Types | Example |
|----------|-------|---------|
| `==` `!=` | any | `payload.category == "food"` |
| `>` `>=` `<` `<=` | numeric | `payload.amount > 1000` |
| `contains` | string | `payload.tags contains "vip"` |
| `matches` | string (regex) | `payload.email matches ".*@corp\\.com"` |
| `AND` `OR` `NOT` | boolean | `A AND (B OR NOT C)` |

Field namespaces: `payload.*` · `meta.*` · `event.type` · `event.source` · `event.actor_id`

Formula arithmetic: `*` `/` `+` `-` (used in `points_formula` params)

---

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/events` | Ingest one event — synchronous, returns full result |
| `POST` | `/v1/events/batch` | Ingest up to 100 events — async, returns job summary |
| `GET` | `/v1/rules` | List loaded scenarios |
| `POST` | `/v1/rules/reload` | Hot-reload rules from disk |
| `GET` | `/healthz` | Liveness probe (always 200) |
| `GET` | `/readyz` | Readiness probe (503 if queue >80%) |
| `GET` | `/metrics` | Prometheus metrics |

<details>
<summary>Request / response examples</summary>

**POST /v1/events**

```json
// Request
{
  "id": "evt_01",
  "type": "transaction",
  "source": "pos-system",
  "actor_id": "user_42",
  "payload": { "amount": 1500, "category": "food" },
  "meta": { "tenant": "acme-retail" }
}

// Response 200
{
  "event_id": "evt_01",
  "duration_ms": 1,
  "scenarios_matched": ["sc_high_value_food"],
  "actions_executed": [
    { "action_id": "act_bonus_points", "type": "reward_points",
      "success": true, "message": "Awarded 75 points to user_42 — High-value food purchase bonus" }
  ]
}

// Response 429 — queue full
{ "error": "event queue full (capacity 10000)" }
```

**POST /v1/events/batch**

```json
// Request — array of up to 100 events
[{ "type": "login", "actor_id": "u1", "payload": { "is_first_login": true } }]

// Response 202
{ "job_id": "550e8400-...", "total": 1, "queued": 1, "rejected": 0 }
```

</details>

---

## Adding a new action type

Implement one interface, register it — no other changes:

```go
// internal/action/webhook/notify.go
type NotifyAction struct{ client *http.Client }

func (n *NotifyAction) Type() string { return "webhook_notify" }

func (n *NotifyAction) Validate(params map[string]interface{}) error {
    if _, ok := params["url"].(string); !ok {
        return fmt.Errorf("url is required")
    }
    return nil
}

func (n *NotifyAction) Execute(ctx context.Context, id string,
    params map[string]interface{}, evalCtx *dag.EvalContext) (*action.ActionResult, error) {
    // POST to params["url"]
}
```

```go
// cmd/server/main.go
reg.Register(webhook.New())
```

```yaml
# configs/rules.yaml
- action:
    id: act_notify
    type: webhook_notify
    params:
      url: "https://hooks.example.com/loyalty"
```

---

## Observability

### Prometheus metrics

| Metric | Type | Labels |
|--------|------|--------|
| `ifttt_events_enqueued_total` | Counter | — |
| `ifttt_events_processed_total` | Counter | — |
| `ifttt_events_dropped_total` | Counter | — |
| `ifttt_scenarios_matched_total` | Counter | `scenario_id` |
| `ifttt_actions_executed_total` | Counter | `action_type`, `status` |
| `ifttt_event_processing_duration_ms` | Histogram | — |
| `ifttt_queue_utilization_ratio` | Gauge | — |

### Structured logs (`log/slog`)

```
time=2026-02-21T10:30:01.234Z level=INFO msg="request" method=POST path=/v1/events status=200 duration_ms=1
time=2026-02-21T10:30:02.100Z level=INFO msg="DAG hot-reloaded" nodes=12
```

---

## Dependencies

| Module | Purpose |
|--------|---------|
| [`gopkg.in/yaml.v3`](https://pkg.go.dev/gopkg.in/yaml.v3) | YAML parsing |
| [`github.com/fsnotify/fsnotify`](https://pkg.go.dev/github.com/fsnotify/fsnotify) | Config hot-reload |
| [`github.com/prometheus/client_golang`](https://pkg.go.dev/github.com/prometheus/client_golang) | Metrics |
| [`github.com/google/uuid`](https://pkg.go.dev/github.com/google/uuid) | Auto-generated event IDs |

Zero web frameworks — Go 1.22 `net/http` with method+path routing.

---

## Documentation

| Doc | Contents |
|-----|----------|
| [README](README.md) | Setup · API · expression language · extension guide |
| [TEST.md](TEST.md) | Test inventory · manual end-to-end verification |
| [DEEPDIVE.md](DEEPDIVE.md) | Architecture · design decisions · performance analysis |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Fork workflow · conventions · commit style |
| [CHANGELOG.md](CHANGELOG.md) | Release history |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Bug reports and feature requests go through [GitHub Issues](https://github.com/gyaan/fluxflow/issues).

## License

[MIT](LICENSE) © 2026 Gyaneshwar Pardhi
