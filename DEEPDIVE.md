# Deep Dive — Architecture and Design Decisions

This document explains *why* the system is built the way it is, how each component works internally, and what trade-offs were made. Read this if you want to extend, tune, or reason about the system under load.

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Event Model](#2-event-model)
3. [Configuration and Validation](#3-configuration-and-validation)
4. [Condition Expression Engine](#4-condition-expression-engine)
5. [DAG — Data Structure and Construction](#5-dag--data-structure-and-construction)
6. [DAG Evaluation — DFS with Early Pruning](#6-dag-evaluation--dfs-with-early-pruning)
7. [Action System](#7-action-system)
8. [Concurrency Model](#8-concurrency-model)
9. [Hot-Reload](#9-hot-reload)
10. [HTTP API Layer](#10-http-api-layer)
11. [Observability](#11-observability)
12. [Performance Analysis](#12-performance-analysis)
13. [Extension Points](#13-extension-points)
14. [Known Limitations and Future Work](#14-known-limitations-and-future-work)

---

## 1. System Overview

```
┌─────────────────────────────────────────────────────────────┐
│  HTTP Client                                                │
└──────────────────────┬──────────────────────────────────────┘
                       │ POST /v1/events
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  api.Handler                                                │
│  • JSON decode + validate                                   │
│  • Auto-fill ID / ReceivedAt                                │
└──────────────────────┬──────────────────────────────────────┘
                       │ engine.ProcessSync(event)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  engine.Engine                                              │
│  • Non-blocking submit to bounded channel queue             │
│  • 429 if queue full                                        │
└──────────────────────┬──────────────────────────────────────┘
                       │ channel dispatch
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Worker Pool (32 goroutines)                                │
│  • Load DAG via atomic.Pointer (lock-free)                  │
│  • dag.Evaluate(graph, event) → []ActionMatch               │
└──────────────────────┬──────────────────────────────────────┘
                       │
          ┌────────────┴───────────────┐
          ▼                            ▼
┌──────────────────┐        ┌──────────────────────┐
│  ScenarioNode    │        │  ScenarioNode         │
│  sc_food_high    │        │  sc_first_login       │
│  match? ✓        │        │  match? ✗ (wrong type)│
└────────┬─────────┘        └──────────────────────┘
         │ DFS
         ▼
┌──────────────────┐
│  ConditionNode   │
│  cond_is_food    │
│  "food"=="food"✓ │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  ConditionNode   │
│  cond_amount     │
│  1500 > 1000  ✓  │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  ActionNode      │  ──► action.Registry.Get("reward_points")
│  act_bonus_pts   │  ──► RewardPointsAction.Execute()
└──────────────────┘  ──► ActionResult{"Awarded 75 points"}
```

---

## 2. Event Model

```go
// internal/event/event.go
type Event struct {
    ID         string
    Type       string                 // primary dispatch key
    OccurredAt time.Time              // business timestamp
    ReceivedAt time.Time              // server ingestion timestamp (not in JSON)
    Source     string                 // origin system
    ActorID    string                 // who triggered the event
    Payload    map[string]interface{} // arbitrary event data
    Meta       map[string]string      // cross-cutting concerns (tenant, region, tier)
}
```

**Why `map[string]interface{}` for Payload?**

The engine is domain-agnostic. A transaction event has `amount` and `category`; a login event has `is_first_login`; a product-view event might have `sku` and `duration_seconds`. Using a typed struct for each event type would require modifying the engine every time a new event shape appears. The open `map` pushes the schema contract into the YAML rule config, where it belongs.

**Why separate `Meta` (`map[string]string`) from `Payload`?**

`Meta` carries infrastructure-level concerns (tenant ID, region, request ID) that are the same type — strings — and seldom vary. Separating it from `Payload` makes cross-cutting rules (`meta.tenant == "acme"`) visually clearer and avoids polluting business payload fields with ops metadata.

**`ReceivedAt` is excluded from JSON (`json:"-"`)** to avoid confusion with `OccurredAt`. It is set by the HTTP handler and used for latency metrics, never serialised back to the caller.

---

## 3. Configuration and Validation

### Schema design

The config schema (`internal/config/schema.go`) maps one-to-one to the runtime DAG structure. This is intentional: no impedance mismatch between configuration and execution means the builder (`dag/builder.go`) is trivially simple.

`NodeRef` is a **discriminated union** — exactly one of `Condition` or `Action` is non-nil:

```go
type NodeRef struct {
    Condition *ConditionDef `yaml:"condition,omitempty"`
    Action    *ActionDef    `yaml:"action,omitempty"`
}
```

YAML's optional-pointer semantics give us this for free. The validator (`config/validator.go`) enforces the "exactly one" constraint at load time, not at eval time.

### Loader and defaults

`config.Loader` wraps file I/O and deserialisation. Defaults are applied at the loader level:

```go
if cfg.Engine.EventWorkers == 0 {
    cfg.Engine.EventWorkers = 32
}
```

This means an operator can omit the `engine:` block entirely and get sane defaults without the engine code having to handle zero-value structs.

### Validation (`config/validator.go`)

Runs after every load. Checks:

1. **`version` is present** — ensures old/truncated files are rejected early.
2. **All IDs are unique across the entire config** — prevents silent misrouting where two scenarios or conditions share an ID.
3. **`event_types` is non-empty** — a scenario with no event types can never match and is almost certainly a config bug.
4. **`expression` is non-empty** on conditions — an empty expression would need a well-defined default (always-true? always-false?) — better to be explicit.
5. **`type` is non-empty** on actions — prevents a runtime panic when looking up the executor.

Validation is **separate** from parsing and building. This allows the loader to return a structured error with all problems listed (not just the first), which is far friendlier to operators fixing a large config file.

---

## 4. Condition Expression Engine

This is the most algorithmically interesting part of the system.

### Why a custom parser?

Options considered:

| Option | Pros | Cons |
|--------|------|------|
| `expr-lang/expr` library | Feature-rich, tested | External dep, generic, hard to constrain |
| `text/template` | Stdlib | Not a boolean expression language |
| `cel-go` (Common Expression Language) | Standardised | Heavy; complex setup |
| **Custom recursive-descent** | Zero deps, exact feature set, full control | We wrote it |

The required operator set is small and stable. A hand-written parser is ~350 lines, has no dependencies, and makes it easy to add domain-specific operators (`contains`, `matches`) without fighting a library's abstraction.

### Tokenizer (`tokenize`)

Single-pass, character-at-a-time scanner. Produces a flat `[]token` slice. Token kinds:

```
tokWord    → AND, OR, NOT, contains, matches, field.path
tokOp      → ==, !=, >, >=, <, <=, *, /, +, -
tokString  → "…" or '…' (with basic escape handling)
tokNumber  → 42 | 3.14 | -5 (negative only if '-' is immediately followed by digit)
tokBool    → true | false
tokLParen  → (
tokRParen  → )
tokEOF     → sentinel
```

The `tokEOF` sentinel eliminates bounds-checking in the parser; `peek()` always returns a valid token.

**Negative number disambiguation:** `-5` is a number literal only when `-` is immediately followed by a digit. `amount - 5` tokenizes as `[amount][-][5]` (three tokens), while `-5` tokenizes as a single `tokNumber`. This rule handles both cases without lookahead.

### AST nodes

```
Expr
├── BinaryExpr  { Op:"AND"|"OR", Left:Expr, Right:Expr }
├── NotExpr     { Expr:Expr }
└── ComparisonExpr { Left:Operand, Op:Operator, Right:Operand }

Operand
├── LiteralOperand  { Value:interface{} }  ← string, float64, bool
└── FieldOperand    { Path:[]string }      ← ["payload","amount"]
```

The interface-based AST is idiomatic Go and allows the evaluator to use a type switch without reflection.

### Recursive-descent parser

Grammar (top = lowest precedence, bottom = highest):

```
or_expr    = and_expr ( "OR" and_expr )*
and_expr   = not_expr ( "AND" not_expr )*
not_expr   = "NOT" not_expr | "(" or_expr ")" | comparison
comparison = operand operator operand
operand    = field_path | string | number | bool
```

Each grammar rule is a function. The precedence hierarchy is encoded in the call chain: `parseOr` calls `parseAnd`, which calls `parseNot`, which calls `parseComparison`. This is textbook LL(1) parsing — no lookahead tables, no backtracking.

**Short-circuit evaluation** is implemented in the evaluator, not the parser:

```go
case "AND":
    if !left { return false, nil }  // don't evaluate right
    return Evaluate(e.Right, ctx)
case "OR":
    if left { return true, nil }    // don't evaluate right
    return Evaluate(e.Right, ctx)
```

This matters for conditions that access potentially missing fields — a false left-hand side of AND prevents the right from erroring on a missing field.

### Compile-once, evaluate-many

`dag/builder.go` calls `condition.Parse(expr)` for every condition node at startup. The resulting `condition.Expr` AST is stored inside the `ConditionNode` and reused for every event. There is **zero string parsing at evaluation time** — evaluation is a tree walk.

```go
// dag/builder.go — called once
ast, err := condition.Parse(c.Expression)
cn := NewConditionNode(c.ID, ast)

// dag/node.go — called for every event
func (n *ConditionNode) Evaluate(ctx *EvalContext) (bool, error) {
    return condition.Evaluate(n.expr, ctx) // n.expr is the pre-compiled AST
}
```

---

## 5. DAG — Data Structure and Construction

### Why a DAG?

An IFTTT rule engine is, at its core, a decision tree. A DAG (Directed Acyclic Graph) generalises this to allow shared subtrees — in future, a common condition (e.g., "is the user a loyalty member?") could be referenced from multiple scenarios without being duplicated. The YAML config currently produces a tree (each node has exactly one parent), but the runtime data structure supports arbitrary fan-in.

### Graph representation

```go
// internal/dag/graph.go
type Graph struct {
    nodes    map[string]Node   // id → Node (O(1) lookup)
    children map[string][]Node // parent id → ordered children list
    roots    []*ScenarioNode   // DFS entry points
}
```

This is an **adjacency list** representation. Nodes are stored once in `nodes`; edges are stored as slices in `children`. The `roots` slice pre-filters scenario nodes so the evaluator does not need to scan all nodes to find entry points.

**Immutability after construction:** `Graph` has no mutating methods after `Build` returns. This is what makes the `atomic.Pointer` hot-reload pattern safe — readers hold the old graph pointer until their evaluation finishes; the new graph pointer is only ever stored and loaded, never partially modified.

### Node hierarchy

```
Node (interface)
├── ScenarioNode — root; evaluates event.Type ∩ event.Source
├── ConditionNode — internal; holds compiled Expr AST
└── ActionNode — leaf; holds actionType + params
```

All three implement `Evaluate(ctx *EvalContext) (bool, error)`. The evaluator treats them uniformly during DFS. `ActionNode.Evaluate` always returns `true` — it is the signal to the evaluator that this branch has fully passed and an action should be dispatched. The actual execution is done outside the DFS loop by the engine.

### Builder (`dag/builder.go`)

`Build` is a single-pass depth-first traversal of the config tree:

```
Build(cfg):
  for each scenario:
    if not enabled: skip
    create ScenarioNode, add to graph as root
    buildChildren(graph, scenarioID, scenario.Children)

buildChildren(graph, parentID, refs):
  for each ref:
    if condition:
      parse expression → AST (fails fast on syntax error)
      create ConditionNode, add to graph
      add edge: parentID → conditionNode
      recurse: buildChildren(graph, conditionID, condition.Children)
    if action:
      create ActionNode, add to graph
      add edge: parentID → actionNode
```

**Expression parse errors fail the entire build.** This is deliberate: a rule with a typo in its expression would silently never match at runtime, which is far worse than a startup failure that forces the operator to fix the config.

---

## 6. DAG Evaluation — DFS with Early Pruning

```go
// dag/evaluator.go
func Evaluate(g *Graph, ev *event.Event) ([]ActionMatch, []string, error) {
    ctx := &EvalContext{Event: ev, Results: make(map[string]interface{})}

    for _, root := range g.Roots() {
        ok, _ := root.Evaluate(ctx)   // check event.Type + event.Source
        if !ok { continue }            // ← scenario-level prune
        actions, _ := dfs(g, ctx, root.ID(), root.ID())
        if len(actions) > 0 {
            scenariosMatched = append(scenariosMatched, root.ID())
        }
    }
    return matches, scenariosMatched, ...
}

func dfs(g *Graph, ctx *EvalContext, parentID, scenarioID string) ([]ActionMatch, error) {
    for _, child := range g.Children(parentID) {
        ok, err := child.Evaluate(ctx)
        if !ok || err != nil { continue }  // ← condition-level prune
        if isAction(child) {
            results = append(results, ActionMatch{...})
        } else {
            sub := dfs(g, ctx, child.ID(), scenarioID)
            results = append(results, sub...)
        }
    }
}
```

**Why DFS over BFS?**

DFS naturally prunes subtrees: once a condition fails, `g.Children(failedNodeID)` is never accessed. With BFS you would need to maintain a "live" set and filter it on each level, which is more complex and has worse locality.

**Fail-open behaviour:** When `child.Evaluate` returns an error (e.g., a condition references a field that doesn't exist in the payload), the error is recorded in `ctx.Errors` and the branch is *skipped* rather than terminating evaluation. This means other scenarios can still fire even if one has a bug. The first error is surfaced in the HTTP response so the caller knows something went wrong, but they still get partial results.

**`EvalContext.Results`** accumulates action outputs during a single event's evaluation. This allows later actions to reference results of earlier actions within the same event (future feature, groundwork already in place).

---

## 7. Action System

### Interface

```go
type Executor interface {
    Type() string
    Execute(ctx context.Context, actionID string,
        params map[string]interface{}, evalCtx *dag.EvalContext) (*ActionResult, error)
    Validate(params map[string]interface{}) error
}
```

**`Validate` is called by the builder at startup**, not at evaluation time. This catches configuration errors (missing `operation` field, unknown operation type) before the server accepts any traffic.

**`Execute` receives the full `dag.EvalContext`**, not just the params. This allows actions to:
- Read event payload values for dynamic computation (e.g., `points_formula`)
- Write results back to `ctx.Results` for downstream actions to consume
- Access `ctx.Event.ActorID` without the YAML needing to pass it as a param

### Registry

```go
type Registry struct {
    mu        sync.RWMutex
    executors map[string]Executor
}
```

`Register` panics on duplicate type strings. This is intentional: a duplicate registration is a programming error that should be caught at process startup, not silently ignored.

`Get` uses a read lock — safe for millions of concurrent lookups with zero contention under normal operation.

### `reward_points` executor

Supports two modes:

**Fixed points:**
```yaml
params:
  operation: award
  points: 100
```

**Formula-based:**
```yaml
params:
  operation: award
  points_formula: "payload.amount * 0.05"
```

The formula is parsed by the same `condition.Parse` function used for conditions — reusing the tokenizer, operator set, and field resolver. The evaluator (`evalNumericExpr`) intercepts `ComparisonExpr` nodes with arithmetic operators (`*`, `/`, `+`, `-`) and returns their computed numeric value rather than a boolean comparison result.

**Rounding:** `math.Round(pts*100) / 100` ensures points are rounded to 2 decimal places before recording, preventing floating-point accumulation errors.

---

## 8. Concurrency Model

```
┌─────────────────────────────────────────────────────┐
│  HTTP goroutines (one per request, managed by net/http)│
│  • JSON decode, validate                             │
│  • engine.ProcessSync() → blocks on resultC channel │
└───────────────────────┬─────────────────────────────┘
                        │ non-blocking send to buffered chan
                        ▼
                 ┌──────────────┐
                 │ event queue  │  cap = 10,000
                 │ chan job[T]   │  ← HTTP 429 when full
                 └──────┬───────┘
                        │
          ┌─────────────┴─────────────┐
          ▼             ▼             ▼
    worker[0]      worker[1]  … worker[31]   (32 goroutines)
    dag.Evaluate   dag.Evaluate   dag.Evaluate
    runAction      runAction      runAction
          │             │
          └──── resultC channel (buffered 1) ────► HTTP response
```

### `workerPool[T, R any]`

The generic worker pool uses Go 1.18+ type parameters:

```go
type workerPool[T, R any] struct {
    queue   chan job[T]
    process func(ctx context.Context, t T) (R, error)
    wg      sync.WaitGroup
}
```

`T` = the work item type, `R` = the result type. The same pool implementation is used for both event workers (`*eventWork → *EventResult`) and action workers (`*actionWork → *ActionResult`).

**Submit is non-blocking:**
```go
func (p *workerPool[T, R]) Submit(t T) bool {
    select {
    case p.queue <- job[T]{payload: t}:
        return true
    default:
        return false  // queue full → caller returns 429
    }
}
```

This is the backpressure mechanism. A `select` with a `default` branch is the idiomatic Go pattern for a non-blocking channel send. No mutex, no spin-loop.

### Lock-free graph access

```go
// engine.go
type Engine struct {
    graph atomic.Pointer[dag.Graph]
    ...
}

// Read path (every event, 32 concurrent workers)
g := e.graph.Load()  // atomic, no lock

// Write path (hot-reload, rare)
e.graph.Store(newGraph)  // atomic, no lock
```

`atomic.Pointer[T]` (Go 1.19+) provides compare-and-swap semantics on a pointer. Readers load the current pointer; they hold a reference to the graph for the duration of their evaluation, even if the pointer is swapped during that time. The old graph is garbage-collected once no goroutine holds a reference to it.

### Why 32 event workers / 16 action workers?

At 1,000 events/second, average processing latency of 5 ms:
- Events in-flight simultaneously: `1000 × 0.005 = 5`
- 32 workers provides `32 / 5 = 6.4×` headroom for latency spikes

For I/O-bound action executors (webhook POSTs, database writes), the action pool allows blocking network calls without stalling event evaluation. These are tunable via `configs/rules.yaml`.

### ProcessSync vs ProcessAsync

| | `ProcessSync` | `ProcessAsync` |
|-|---------------|----------------|
| Used by | `POST /v1/events` | `POST /v1/events/batch` |
| Blocks | Yes — waits for result via channel | No — fire and forget |
| Returns | Full `EventResult` | Job ID + queue counts |
| Timeout | Configurable (`event_timeout_ms`) | None |
| 429 | Queue full | Queue full |

`ProcessSync` creates a `chan *EventResult` buffered to 1 and passes it as part of the work item. The worker writes the result back and the HTTP goroutine unblocks. The channel capacity of 1 prevents the worker from blocking even if the HTTP goroutine has already timed out.

---

## 9. Hot-Reload

The hot-reload system has two triggers:

### 1. File watcher (automatic)

```go
// config/loader.go
watcher.Add(l.path)

// On fs.Write or fs.Create event:
cfg := l.load()
l.current = cfg                     // update loader's cache
for _, fn := range l.onChange {
    fn(cfg)                         // notify all registered callbacks
}
```

### 2. HTTP endpoint (manual)

```
POST /v1/rules/reload
→ loader.Reload()
→ dag.Build(newCfg)
→ engine.SwapGraph(newGraph)
```

### Callback chain

```
config.Loader.OnChange callback (registered in main.go)
    config.Validate(newCfg)     ← reject invalid config; keep old graph
    dag.Build(newCfg)           ← compile all expressions into new ASTs
    engine.SwapGraph(newGraph)  ← atomic pointer swap
```

**Safety:** If validation or building fails, the swap never happens. The engine continues running with the old (valid) graph. Operators see a log warning but traffic is unaffected.

**Consistency:** Because `atomic.Pointer.Store` is a single machine instruction (on all architectures Go supports), workers either see the old graph or the new graph — never a partially-initialised one. There is no window where a worker could read a half-swapped graph.

---

## 10. HTTP API Layer

### Routing — Go 1.22 `net/http`

Go 1.22 added method+path routing to the standard library:

```go
h.mux.HandleFunc("POST /v1/events", h.ingestEvent)
h.mux.HandleFunc("GET /metrics", promhttp.Handler())
```

No third-party router is needed. This eliminates a dependency and keeps the binary small.

### Middleware chain

```
loggingMiddleware → mux (routes to handlers)
```

`loggingMiddleware` wraps `http.ResponseWriter` with a `responseWriter` that captures the status code so it can be logged after the handler returns:

```go
type responseWriter struct {
    http.ResponseWriter
    status int
}
func (rw *responseWriter) WriteHeader(code int) {
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
}
```

### Readiness probe — `/readyz`

The readiness probe checks queue utilisation, not just process liveness:

```go
if util > 0.8 {
    writeJSON(w, http.StatusServiceUnavailable, ...)
}
```

This integrates with Kubernetes: when the event queue is >80% full, Kubernetes stops routing new traffic to this pod, allowing the queue to drain before more requests arrive. This is **load-shedding at the infrastructure level** — it backs off upstream load without dropping requests that are already in the queue.

---

## 11. Observability

### Metrics design

All metrics use `promauto` which auto-registers with the default Prometheus registry and panics if registration fails — ensuring misconfiguration is caught at startup.

| Metric type | Used for | Why |
|-------------|----------|-----|
| `Counter` | enqueued, processed, dropped, matched, executed | Monotonically increasing — suitable for rate() in dashboards |
| `CounterVec` | scenarios_matched, actions_executed | Per-label breakdown without separate metrics for each scenario/action |
| `Histogram` | processing_duration_ms | Captures latency distribution (p50, p95, p99) not just average |
| `Gauge` | queue_utilization | Point-in-time ratio — appropriate for level values |

### Structured logging

`log/slog` (stdlib, Go 1.21+) is used throughout. Text format for development; JSON format can be enabled by changing the handler:

```go
// For JSON logs (structured, machine-parseable):
slog.New(slog.NewJSONHandler(os.Stdout, nil))
```

All HTTP requests are logged with method, path, status, duration, and remote address. All config events (DAG built, hot-reload, watcher errors) are logged at appropriate levels.

---

## 12. Performance Analysis

### Where time is spent per event

```
HTTP decode         ~0.1 ms  (JSON unmarshal of small payload)
Queue dispatch      ~0 ms    (non-blocking channel send)
Queue wait          ~0 ms    (32 workers, typical queue near-empty)
DAG evaluation      ~0.01 ms (tree walk, O(nodes), no I/O)
  ├── ScenarioNode  map lookup × 2 (eventTypes, sources)
  ├── ConditionNode AST walk, field resolve, operator call
  └── ActionNode    condition.Evaluate always true
Action execution    ~0.1 ms  (in-memory, no network)
JSON encode         ~0.1 ms
```

**Total: < 1 ms at low load.** The zero allocation per-event design (worker goroutines are pre-allocated, `EvalContext` is stack-allocated) means GC pressure is minimal.

### Throughput ceiling

The ceiling is the worker pool size × goroutine throughput:
- 32 workers × ~10,000 evaluations/sec/goroutine ≈ 320,000 events/sec theoretical maximum
- Actual ceiling dominated by HTTP parsing and JSON encode/decode: ~10,000–50,000 req/sec on a single core
- Target of >1,000 req/sec has ~50× headroom

### Memory footprint

- DAG graph: ~1 KB per node (ID string + map pointers + AST) × typical ~100 nodes = ~100 KB
- `EvalContext`: ~256 bytes per event × 32 concurrent events = ~8 KB
- Event queue (10,000 slots × pointer size): ~80 KB
- Total steady-state memory: well under 50 MB

---

## 13. Extension Points

### New action type

Implement `action.Executor`, register in `main.go`. No changes to engine, DAG, or API.

```go
type WebhookAction struct{ client *http.Client }

func (w *WebhookAction) Type() string { return "webhook_post" }
func (w *WebhookAction) Validate(params map[string]interface{}) error { ... }
func (w *WebhookAction) Execute(ctx context.Context, id string,
    params map[string]interface{}, evalCtx *dag.EvalContext) (*action.ActionResult, error) {
    url := params["url"].(string)
    // POST to url with event data
}
```

### New event source (Kafka, SQS, gRPC)

Implement an `EventSource` interface and call `engine.ProcessAsync(ev)`:

```go
type KafkaSource struct{ consumer *kafka.Consumer }

func (k *KafkaSource) Start(ctx context.Context, out chan<- *event.Event) error {
    for {
        msg := k.consumer.Poll(100)
        ev := unmarshal(msg.Value)
        out <- ev
    }
}
```

The engine's worker pool is source-agnostic — it just processes `*event.Event` values.

### New condition operator

Add the operator constant to `internal/condition/operators.go`, implement its logic in `compare()`, and add it to the tokenizer/parser as a `tokWord` (like `contains`) or `tokOp` (like `>=`).

### Multi-tenant support

`Meta` already supports `meta.tenant`. Add a `tenant` filter field to `Scenario`:

```yaml
- id: sc_acme_food
  tenant: acme-retail          # new field
  event_types: [transaction]
  ...
```

Add the filter to `ScenarioNode.Evaluate` — no other changes needed.

---

## 14. Known Limitations and Future Work

### Arithmetic in condition expressions

The current expression language supports arithmetic operators (`*`, `/`, `+`, `-`) only in `points_formula` params, evaluated by a separate `evalNumericExpr` path. Arithmetic in condition expressions (e.g., `payload.price * payload.qty > 10000`) is not supported — the parser would produce a `ComparisonExpr` with op=`*` which the boolean evaluator doesn't understand. A proper two-level parser (arithmetic expr then boolean comparison) would address this.

### No expression pre-validation for action params

`ActionDef.Params` values are `interface{}`. `reward_points.Validate` is called by the builder, but other action types may not validate `points_formula` syntax at build time. Adding a `condition.Parse` call inside every action's `Validate` would catch formula syntax errors at startup.

### Sync ProcessSync uses a per-request channel

`ProcessSync` allocates a `chan *EventResult` per request. This is safe and correct, but creates GC pressure at very high request rates. A `sync.Pool` of pre-allocated result channels would eliminate this allocation at the cost of a small pool management overhead.

### No persistence

`EvalContext.Results` is in-memory and discarded after the event is processed. A production loyalty system needs a durable points ledger. The `Execute` method signature is already designed for this: add a `LedgerClient` dependency to `RewardPointsAction` and call it inside `Execute`.

### No deduplication

If the same `event.ID` is received twice (e.g., retried HTTP POST), both will be evaluated and both will trigger actions. Idempotency keys or a short-lived Redis dedup cache would address this.

### Single-process only

The worker pool and atomic graph pointer work within one process. Horizontal scaling requires either: (a) a message queue (Kafka/SQS) in front, with each process consuming its partition, or (b) a shared config store (etcd/Consul) replacing the file watcher for distributed hot-reload.
