# Testing Guide

This document covers running the test suite, understanding what each test validates, writing new tests, and performing manual end-to-end verification.

---

## Running tests

```bash
# All packages
CGO_ENABLED=0 go test ./...

# Verbose output
CGO_ENABLED=0 go test ./... -v

# Specific package
CGO_ENABLED=0 go test ./internal/condition/... -v
CGO_ENABLED=0 go test ./internal/dag/... -v

# With race detector
CGO_ENABLED=0 go test -race ./...

# With coverage report
CGO_ENABLED=0 go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

> `CGO_ENABLED=0` is required on macOS if Xcode Command Line Tools are not installed.

---

## Test inventory

### `internal/condition` — Expression parser and evaluator

File: `internal/condition/expression_test.go`

#### `TestEvaluate` (19 sub-cases)

Tests the full parse → AST → evaluate pipeline against a mock `EvalContext`.

| Sub-test | Expression | Context | Expected |
|----------|------------|---------|----------|
| `gt true` | `amount > 1000` | amount=1500 | true |
| `gt false` | `amount > 1000` | amount=500 | false |
| `gte equal` | `amount >= 1000` | amount=1000 | true |
| `lt true` | `amount < 100` | amount=50 | true |
| `eq string true` | `category == "food"` | category="food" | true |
| `eq string false` | `category == "food"` | category="electronics" | false |
| `neq string` | `category != "food"` | category="electronics" | true |
| `bool eq true` | `is_first_login == true` | is_first_login=true | true |
| `bool eq false literal` | `is_first_login == false` | is_first_login=true | false |
| `AND both true` | `category == "food" AND amount > 500` | category="food", amount=1000 | true |
| `AND first false` | `category == "food" AND amount > 500` | category="clothing", amount=1000 | false |
| `OR first true` | `category == "food" OR amount > 500` | category="clothing", amount=1000 | true |
| `OR both false` | `category == "food" OR amount > 500` | category="clothing", amount=10 | false |
| `NOT true` | `NOT amount > 1000` | amount=500 | true |
| `contains true` | `tags contains "vip"` | tags="vip-member" | true |
| `contains false` | `tags contains "vip"` | tags="regular" | false |
| `matches true` | `email matches ".*@example\\.com"` | email="user@example.com" | true |
| `matches false` | `email matches ".*@example\\.com"` | email="user@other.com" | false |
| `unknown field` | `missing > 10` | amount=100 | error |

#### `TestParse_Errors` (3 sub-cases)

Confirms the parser returns an error for malformed expressions.

| Input | Why it should fail |
|-------|--------------------|
| `"unterminated` | Unclosed string literal |
| `amount 1000` | Missing operator between operands |
| `` (empty string) | No expression to parse |

---

### `internal/dag` — DAG builder and DFS evaluator

File: `internal/dag/evaluator_test.go`

All tests share a helper `buildTestGraph` that constructs the following graph from inline config:

```
sc_food_high (transaction, pos-system)
└── cond_food: payload.category == "food"
    └── cond_amount: payload.amount > 1000
        └── act_bonus: reward_points{points:100}

sc_login (login, any source)
└── act_welcome: reward_points{points:50}
```

#### `TestEvaluate_ScenarioMatch`

- **Input:** `transaction / pos-system / {amount:1500, category:"food"}`
- **Asserts:** `scenarios_matched = ["sc_food_high"]`, `actions = [act_bonus]`
- **Why:** Both conditions pass; action node is reached.

#### `TestEvaluate_ConditionPrune`

- **Input:** `transaction / pos-system / {amount:500, category:"food"}`
- **Asserts:** no scenarios matched, no actions
- **Why:** `cond_food` passes but `cond_amount` (amount > 1000) fails → branch pruned, `act_bonus` never reached.

#### `TestEvaluate_WrongEventType`

- **Input:** `login / (no source) / nil payload`
- **Asserts:** `scenarios_matched = ["sc_login"]`, `actions = [act_welcome]`
- **Why:** `sc_login` matches `login` events; `act_welcome` is a direct child with no condition gates.

#### `TestEvaluate_WrongSource`

- **Input:** `transaction / mobile-app / {amount:1500, category:"food"}`
- **Asserts:** `sc_food_high` is NOT in `scenarios_matched`
- **Why:** `sc_food_high` restricts sources to `pos-system`; `mobile-app` is not in that list.

#### `TestEvaluate_DisabledScenario`

- **Input:** `transaction / (any) / {}` with `enabled: false` on the only scenario
- **Asserts:** no scenarios matched
- **Why:** `dag.Build` skips disabled scenarios; they are never added to the graph roots.

---

## What is NOT yet covered by automated tests

The following areas are exercised by the manual smoke tests below but do not yet have automated tests. These are good candidates for future test additions.

| Area | Gap |
|------|-----|
| `config/loader.go` | File load, YAML parse, defaults, hot-reload callback |
| `config/validator.go` | Duplicate IDs, missing fields, empty event_types |
| `engine/engine.go` | ProcessSync, ProcessAsync, queue-full 429, timeout, SwapGraph |
| `action/points/reward.go` | Fixed points, formula points, invalid operation |
| `api/handler.go` | All HTTP endpoints, batch ingestion, /readyz thresholds |
| Concurrency | Race-free graph swap under load |

---

## Manual end-to-end tests

Start the server first:
```bash
CGO_ENABLED=0 go run cmd/server/main.go -addr :8080
```

### 1. High-value food transaction — scenario matches, formula points awarded

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "evt_food_001",
    "type": "transaction",
    "source": "pos-system",
    "actor_id": "user_42",
    "payload": { "amount": 1500, "category": "food" }
  }'
```

Expected:
```json
{
  "event_id": "evt_food_001",
  "scenarios_matched": ["sc_high_value_food"],
  "actions_executed": [{ "success": true, "message": "Awarded 75 points to user_42 — High-value food purchase bonus" }]
}
```
Points = `1500 * 0.05 = 75`. Confirm the formula was evaluated at runtime.

---

### 2. Low-value food transaction — branch pruned at amount condition

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "transaction", "source": "pos-system", "actor_id": "u1", "payload": { "amount": 500, "category": "food" } }'
```

Expected: `"scenarios_matched": null, "actions_executed": []`

---

### 3. Food purchase from wrong source — scenario source filter rejects it

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "transaction", "source": "erp-system", "actor_id": "u1", "payload": { "amount": 2000, "category": "food" } }'
```

Expected: `"scenarios_matched": null` — `erp-system` is not in `sc_high_value_food.sources`.

---

### 4. First login — fixed points awarded

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "login", "actor_id": "new_user_99", "payload": { "is_first_login": true } }'
```

Expected: `"scenarios_matched": ["sc_first_login"]`, `"message": "Awarded 100 points to new_user_99 — Welcome bonus"`

---

### 5. Repeat login — condition blocks action

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "login", "actor_id": "old_user_1", "payload": { "is_first_login": false } }'
```

Expected: `"scenarios_matched": null` — `is_first_login == true` fails.

---

### 6. Unknown event type — no scenario matches

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "page_view", "actor_id": "u1", "payload": {} }'
```

Expected: `"scenarios_matched": null, "actions_executed": []`

---

### 7. Auto-generated ID — server fills missing id field

```bash
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "type": "login", "actor_id": "u1", "payload": { "is_first_login": true } }'
```

Expected: `event_id` is a UUID like `"3f2504e0-..."` — not empty.

---

### 8. Batch ingestion — mixed event types

```bash
curl -s -X POST http://localhost:8080/v1/events/batch \
  -H 'Content-Type: application/json' \
  -d '[
    { "type": "transaction", "source": "pos-system", "actor_id": "u1", "payload": { "amount": 2000, "category": "food" } },
    { "type": "login", "actor_id": "u2", "payload": { "is_first_login": true } },
    { "type": "page_view", "actor_id": "u3", "payload": {} }
  ]'
```

Expected:
```json
{ "job_id": "...", "total": 3, "queued": 3, "rejected": 0 }
```

---

### 9. Hot-reload — change rules without restart

1. Edit `configs/rules.yaml` to disable `sc_high_value_food` (`enabled: false`).
2. Either save the file (watcher fires in ~1s) or call:
   ```bash
   curl -s -X POST http://localhost:8080/v1/rules/reload
   ```
3. Resend the high-value food event (test 1 above).
4. Expected: `"scenarios_matched": null` — the scenario was disabled at runtime.

---

### 10. Liveness and readiness probes

```bash
curl -s http://localhost:8080/healthz
# {"status":"ok"}

curl -s http://localhost:8080/readyz
# {"status":"ready","queue_utilization":0}
```

---

### 11. Bad request — validation errors

```bash
# Missing type field
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d '{ "actor_id": "u1", "payload": {} }'
# {"error":"event type is required"}

# Malformed JSON
curl -s -X POST http://localhost:8080/v1/events \
  -H 'Content-Type: application/json' \
  -d 'not-json'
# {"error":"invalid JSON: ..."}

# Batch over 100 events
python3 -c "import json; print(json.dumps([{'type':'login','actor_id':'u'}]*101))" | \
  curl -s -X POST http://localhost:8080/v1/events/batch -H 'Content-Type: application/json' -d @-
# {"error":"batch size 101 exceeds max 100"}
```

---

### 12. Prometheus metrics — counters increment

```bash
# Send a few events, then check metrics
curl -s http://localhost:8080/metrics | grep ifttt_
```

Expected output (values depend on how many events you sent):
```
ifttt_events_enqueued_total 5
ifttt_events_processed_total 5
ifttt_events_dropped_total 0
ifttt_scenarios_matched_total{scenario_id="sc_high_value_food"} 2
ifttt_actions_executed_total{action_type="reward_points",status="success"} 3
ifttt_queue_utilization_ratio 0
```

---

## Load test

Requires [`hey`](https://github.com/rakyll/hey) (`go install github.com/rakyll/hey@latest`).

```bash
# 100k requests, 100 concurrent connections
hey -n 100000 -c 100 -m POST \
  -H 'Content-Type: application/json' \
  -d '{"type":"transaction","source":"pos-system","actor_id":"u1","payload":{"amount":1500,"category":"food"}}' \
  http://localhost:8080/v1/events
```

Targets to look for:
- **RPS:** > 1,000
- **p99 latency:** < 50 ms
- **Error rate:** 0% (no 5xx; 429s only if queue saturates)

---

## Writing new tests

### Unit test for a new condition operator

Add a case to `TestEvaluate` in `internal/condition/expression_test.go`:

```go
{
    name: "startswith custom",
    expr: `payload.sku startswith "FOOD-"`,
    ctx:  ctx("payload.sku", "FOOD-001"),
    want: true,
},
```

### Integration test for a new scenario

Add a test to `internal/dag/evaluator_test.go` using the existing `buildTestGraph` pattern or inline a custom `config.RuleConfig`:

```go
func TestEvaluate_VIPDiscount(t *testing.T) {
    cfg := &config.RuleConfig{
        Version: "v1",
        Scenarios: []config.Scenario{
            {
                ID: "sc_vip", Enabled: true,
                EventTypes: []string{"transaction"},
                Children: []config.NodeRef{
                    {Condition: &config.ConditionDef{
                        ID: "cond_vip",
                        Expression: `meta.tier == "vip"`,
                        Children: []config.NodeRef{
                            {Action: &config.ActionDef{
                                ID: "act_vip_bonus", Type: "reward_points",
                                Params: map[string]interface{}{"operation": "award", "points": float64(200)},
                            }},
                        },
                    }},
                },
            },
        },
    }
    g, _ := dag.Build(cfg)
    ev := &event.Event{
        Type: "transaction",
        Meta: map[string]string{"tier": "vip"},
    }
    _, scenarios, err := dag.Evaluate(g, ev)
    if err != nil { t.Fatal(err) }
    if len(scenarios) != 1 || scenarios[0] != "sc_vip" {
        t.Errorf("expected sc_vip, got %v", scenarios)
    }
}
```
