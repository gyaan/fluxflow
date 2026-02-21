# Contributing

Thank you for your interest in contributing! This document explains how to get started, what the development workflow looks like, and the conventions to follow.

---

## Getting started

### Prerequisites

- Go 1.22 or later
- Git

No other tools are required to run the server or tests. `golangci-lint` is only needed if you want to run the linter locally.

### Fork and clone

```bash
# Fork on GitHub, then:
git clone https://github.com/<your-username>/fluxflow.git
cd fluxflow
CGO_ENABLED=0 go mod download
```

### Run the tests

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test -race ./...   # with race detector
```

### Run the server locally

```bash
CGO_ENABLED=0 go run cmd/server/main.go
```

---

## How to contribute

### Reporting a bug

Open a [Bug Report](https://github.com/gyaan/fluxflow/issues/new?template=bug_report.yml). Include your Go version, the minimal YAML config that triggers the bug, and the exact curl command or test case.

### Requesting a feature

Open a [Feature Request](https://github.com/gyaan/fluxflow/issues/new?template=feature_request.yml). Describe the problem you are solving, not just the solution you have in mind.

### Submitting a pull request

1. **Open an issue first** for anything beyond a trivial fix — this avoids wasted effort if the direction needs discussion.
2. Create a branch from `master`:
   ```bash
   git checkout -b feat/my-feature
   ```
3. Make your changes following the conventions below.
4. Ensure all checks pass:
   ```bash
   CGO_ENABLED=0 go build ./...
   go vet ./...
   CGO_ENABLED=0 go test -race ./...
   ```
5. Open a PR using the pull request template. Reference the related issue with `Closes #<number>`.

---

## Development conventions

### Keep changes focused

- Fix one thing per PR. A bug fix should not include unrelated refactors or style cleanups.
- Do not add docstrings, comments, or error handling to code you did not change.
- Do not add abstractions for a single use case.

### Adding a new action type

1. Create `internal/action/<name>/<name>.go` implementing `action.Executor`.
2. Register it in `cmd/server/main.go`.
3. Add it to the `configs/rules.yaml` example if it is general-purpose.
4. Document it in `README.md` under *Extending with a new action type*.
5. Write at least one unit test validating `Validate()` and `Execute()`.

### Adding a new condition operator

1. Add the operator constant to `internal/condition/operators.go`.
2. Implement its logic in the `compare()` function.
3. Add it to the tokenizer in `internal/condition/expression.go` (as `tokOp` or a `tokWord` keyword).
4. Wire it in `parseComparison()`.
5. Add table-driven test cases to `internal/condition/expression_test.go`.
6. Document the operator in the table in `README.md`.

### Naming

- Use `snake_case` for YAML keys and Go struct tags.
- Use `CamelCase` for exported Go identifiers following standard Go conventions.
- Node IDs in YAML follow `sc_` (scenario), `cond_` (condition), `act_` (action) prefixes by convention — this is not enforced by the engine but makes configs self-documenting.

### Commit messages

Use the imperative mood in the subject line, 72 characters max:

```
Add startswith operator to condition expression language

Implements the `startswith` operator as a tokWord keyword.
Adds 3 table-driven test cases covering string and non-string operands.

Closes #42
```

---

## Project structure reminder

```
internal/condition/   ← expression parser, AST, evaluator, operators
internal/dag/         ← graph data structure, builder, DFS evaluator
internal/engine/      ← worker pool, atomic graph swap, ProcessSync/Async
internal/action/      ← executor interface, registry, points implementation
internal/api/         ← HTTP handlers, middleware
internal/config/      ← schema, YAML loader, validator
internal/metrics/     ← Prometheus metrics
cmd/server/           ← entry point, wiring
```

Read [DEEPDIVE.md](DEEPDIVE.md) for a full explanation of every component's design rationale before making structural changes.

---

## Code of conduct

Be respectful and constructive. Issues and PRs that are rude, dismissive, or off-topic will be closed without comment.
