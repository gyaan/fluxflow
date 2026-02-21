# IFTTT Rule Engine

A high-performance, YAML-driven rule engine for loyalty programmes built in Go. Events (transactions, logins, etc.) are ingested over HTTP, evaluated against a DAG of configurable rules, and trigger actions such as rewarding loyalty points — with zero downtime rule updates via hot-reload.

---

## Features

- **DAG-based rule evaluation** with early branch pruning (DFS)
- **YAML configuration** — no redeployment needed for rule changes
- **Hot-reload** — file watcher swaps the DAG atomically while the server is running
- **Expression language** — human-readable conditions (`payload.amount > 1000 AND payload.category == "food"`)
- **Formula-based actions** — point values can be computed at runtime (`payload.amount * 0.05`)
- **Worker pool concurrency** — fixed goroutine pools, bounded queues, backpressure via HTTP 429
- **Prometheus metrics** — counters, histograms, and queue utilisation gauge out of the box
- **Pluggable actions** — add new action types by implementing a single interface

---

## Requirements

| Tool | Version |
|------|---------|
| Go   | 1.22+   |

No external services required. All dependencies are Go modules.

---

## Quick Start

```bash
# Clone / enter the project
cd ifttt

# Download dependencies
CGO_ENABLED=0 go mod tidy

# Run the server (defaults: :8080, configs/rules.yaml)
CGO_ENABLED=0 go run cmd/server/main.go

# Send a test event
curl -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "evt_01",
    "type": "transaction",
    "source": "pos-system",
    "actor_id": "user_42",
    "payload": { "amount": 1500, "category": "food" }
  }'
```

Expected response:
```json
{
  "event_id": "evt_01",
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

> **Note:** `CGO_ENABLED=0` is required on macOS if Xcode Command Line Tools are not installed.

---

## Project Layout

```
ifttt/
├── cmd/server/main.go                  # Entry point — wires everything together
├── internal/
│   ├── event/event.go                  # Canonical Event struct
│   ├── config/
│   │   ├── schema.go                   # YAML-mapped Go structs
│   │   ├── loader.go                   # Load + fsnotify hot-reload watcher
│   │   └── validator.go                # Duplicate ID and required-field checks
│   ├── condition/
│   │   ├── expression.go               # Tokenizer + recursive-descent parser → AST
│   │   ├── evaluator.go                # AST evaluation against EvalContext
│   │   └── operators.go                # Operator implementations (==, >, contains, matches, …)
│   ├── dag/
│   │   ├── node.go                     # Node interface + ScenarioNode, ConditionNode, ActionNode
│   │   ├── graph.go                    # Immutable Graph: adjacency list, roots, children
│   │   ├── builder.go                  # Builds Graph from config; compiles all expressions at startup
│   │   └── evaluator.go                # DFS traversal with early branch pruning
│   ├── action/
│   │   ├── executor.go                 # ActionExecutor interface + ActionResult
│   │   ├── registry.go                 # String → ActionExecutor map
│   │   └── points/reward.go            # reward_points action (fixed or formula-based)
│   ├── engine/
│   │   ├── engine.go                   # Core: atomic.Pointer[dag.Graph], ProcessSync/Async
│   │   └── worker_pool.go              # Generic fixed goroutine pool with bounded channel
│   ├── api/
│   │   ├── handler.go                  # HTTP handlers (Go 1.22 method+path routing)
│   │   ├── middleware.go               # Request logging middleware
│   │   └── response.go                 # JSON helpers
│   └── metrics/metrics.go              # Prometheus counters, histograms, gauges
├── configs/rules.yaml                  # Example rule configuration
├── go.mod / go.sum
├── README.md
├── TEST.md
└── DEEPDIVE.md
```

---

## Configuration

### Server flags

| Flag      | Default              | Description               |
|-----------|----------------------|---------------------------|
| `-addr`   | `:8080`              | HTTP listen address        |
| `-config` | `configs/rules.yaml` | Path to rules YAML config  |

```bash
CGO_ENABLED=0 go run cmd/server/main.go -addr :9090 -config /etc/ifttt/rules.yaml
```

### Engine tuning (`configs/rules.yaml`)

```yaml
engine:
  event_workers: 32      # goroutines processing events
  action_workers: 16     # goroutines executing I/O-bound actions
  queue_depth: 10000     # bounded event queue (429 when full)
  event_timeout_ms: 5000 # sync processing timeout per event
  fail_open: true        # on condition error, skip branch (vs. fail event)
```

### Writing rules

Rules live entirely in YAML. No redeployment is needed — edit the file and either wait for the watcher to pick it up or call `POST /v1/rules/reload`.

```yaml
version: v1
scenarios:
  - id: sc_vip_cashback
    description: "10% cashback for VIP members on electronics"
    enabled: true
    event_types: [transaction]
    sources: [mobile-app, web]      # omit to match all sources
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
                        expiry_days: 60
```

#### Expression language

| Operator   | Types            | Example |
|------------|------------------|---------|
| `==`       | any              | `payload.category == "food"` |
| `!=`       | any              | `payload.status != "refund"` |
| `>`  `>=`  | numeric          | `payload.amount > 1000` |
| `<`  `<=`  | numeric          | `payload.quantity <= 5` |
| `contains` | string           | `payload.tags contains "vip"` |
| `matches`  | string (regex)   | `payload.email matches ".*@corp\\.com"` |
| `AND`      | boolean          | `A AND B` |
| `OR`       | boolean          | `A OR B` |
| `NOT`      | boolean          | `NOT payload.is_test == true` |
| `()`       | grouping         | `(A OR B) AND C` |

Resolvable field namespaces:

| Prefix    | Fields available |
|-----------|-----------------|
| `payload` | any key in the event's `payload` map |
| `meta`    | any key in the event's `meta` map |
| `event`   | `event.type`, `event.source`, `event.actor_id`, `event.id` |

Formula expressions (for `points_formula`) support `*`, `/`, `+`, `-`:
```
points_formula: "payload.amount * 0.05"
points_formula: "payload.quantity * payload.unit_price * 0.02"
```

---

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/events` | Ingest one event (synchronous — returns full result) |
| `POST` | `/v1/events/batch` | Ingest up to 100 events (async — returns job summary) |
| `GET`  | `/v1/rules` | List currently loaded scenarios |
| `POST` | `/v1/rules/reload` | Hot-reload rules from disk and rebuild DAG |
| `GET`  | `/healthz` | Liveness probe — always 200 |
| `GET`  | `/readyz` | Readiness probe — 503 if queue >80% full |
| `GET`  | `/metrics` | Prometheus metrics endpoint |

### POST /v1/events

**Request:**
```json
{
  "id": "evt_01",
  "type": "transaction",
  "occurred_at": "2026-02-21T10:30:00Z",
  "source": "pos-system",
  "actor_id": "user_42",
  "payload": { "amount": 1500, "category": "food" },
  "meta": { "tenant": "acme-retail", "tier": "gold" }
}
```
`id` and `occurred_at` are optional — the server auto-fills them with UUID and server time.

**Response (200 OK):**
```json
{
  "event_id": "evt_01",
  "duration_ms": 1,
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

**Response (429):** queue full — apply backpressure.

### POST /v1/events/batch

```json
[
  { "type": "transaction", "actor_id": "u1", "payload": { "amount": 200 } },
  { "type": "login", "actor_id": "u2", "payload": { "is_first_login": true } }
]
```

**Response (202 Accepted):**
```json
{ "job_id": "550e8400-...", "total": 2, "queued": 2, "rejected": 0 }
```

### POST /v1/rules/reload

No body required. Returns:
```json
{ "reloaded": true, "scenarios_count": 3 }
```

---

## Extending with a new action type

1. Create `internal/action/webhook/notify.go`:

```go
package webhook

import (
    "context"
    "github.com/gyaneshwarpardhi/ifttt/internal/action"
    "github.com/gyaneshwarpardhi/ifttt/internal/dag"
)

type NotifyAction struct{}

func New() *NotifyAction { return &NotifyAction{} }
func (n *NotifyAction) Type() string { return "webhook_notify" }

func (n *NotifyAction) Validate(params map[string]interface{}) error {
    if _, ok := params["url"].(string); !ok {
        return fmt.Errorf("webhook_notify: url is required")
    }
    return nil
}

func (n *NotifyAction) Execute(ctx context.Context, actionID string,
    params map[string]interface{}, evalCtx *dag.EvalContext) (*action.ActionResult, error) {
    // ... POST to params["url"]
}
```

2. Register in `cmd/server/main.go`:
```go
reg.Register(webhook.New())
```

3. Use in YAML:
```yaml
- action:
    id: act_notify
    type: webhook_notify
    params:
      url: "https://hooks.example.com/loyalty"
```

No other changes needed.

---

## Observability

### Prometheus metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ifttt_events_enqueued_total` | Counter | Events placed on queue |
| `ifttt_events_processed_total` | Counter | Events fully processed |
| `ifttt_events_dropped_total` | Counter | Events rejected (queue full) |
| `ifttt_scenarios_matched_total{scenario_id}` | Counter | Matches per scenario |
| `ifttt_actions_executed_total{action_type,status}` | Counter | Actions by type and outcome |
| `ifttt_event_processing_duration_ms` | Histogram | End-to-end latency |
| `ifttt_queue_utilization_ratio` | Gauge | Queue fill ratio (0–1) |

```bash
curl http://localhost:8080/metrics
```

### Structured logging

The server emits structured logs via `log/slog` in text format:
```
time=2026-02-21T10:30:01.234+05:30 level=INFO msg="request" method=POST path=/v1/events status=200 duration_ms=1 remote_addr=127.0.0.1:54321
```

---

## Dependencies

| Module | Purpose |
|--------|---------|
| `gopkg.in/yaml.v3` | YAML config parsing |
| `github.com/fsnotify/fsnotify` | Config file hot-reload |
| `github.com/prometheus/client_golang` | Metrics |
| `github.com/google/uuid` | Auto-generated event IDs |

No web framework — Go 1.22's stdlib `net/http` with `METHOD /path` pattern routing.

---

## Performance notes

- All condition expressions are compiled into ASTs **once at startup** — zero regex parsing at evaluation time.
- The DAG is held in `atomic.Pointer[dag.Graph]` — hot-reload is a single pointer swap; no locks on the read path.
- Worker goroutines are pre-allocated at startup; events are dispatched via a buffered channel — zero goroutine allocation per event.
- At 1 k events/sec with 5 ms avg latency: ~5 events in-flight simultaneously against 32 workers — enormous headroom.
