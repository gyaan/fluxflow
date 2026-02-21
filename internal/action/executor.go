package action

import (
	"context"

	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
)

// ActionResult holds the outcome of executing a single action.
type ActionResult struct {
	ActionID string `json:"action_id"`
	Type     string `json:"type"`
	Success  bool   `json:"success"`
	Message  string `json:"message"`
}

// Executor is the interface all action implementations must satisfy.
type Executor interface {
	// Type returns the string key this executor is registered under.
	Type() string
	// Execute runs the action and returns a result.
	Execute(ctx context.Context, actionID string, params map[string]interface{}, evalCtx *dag.EvalContext) (*ActionResult, error)
	// Validate checks params at build time (called by dag/builder).
	Validate(params map[string]interface{}) error
}
