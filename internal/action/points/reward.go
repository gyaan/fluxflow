package points

import (
	"context"
	"fmt"
	"math"

	"github.com/gyaneshwarpardhi/ifttt/internal/action"
	"github.com/gyaneshwarpardhi/ifttt/internal/condition"
	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
)

// RewardPointsAction handles "reward_points" actions.
// It supports two param modes:
//   - points: <fixed number>
//   - points_formula: <expression evaluated against event context>
type RewardPointsAction struct{}

func New() *RewardPointsAction { return &RewardPointsAction{} }

func (r *RewardPointsAction) Type() string { return "reward_points" }

func (r *RewardPointsAction) Validate(params map[string]interface{}) error {
	op, _ := params["operation"].(string)
	if op != "award" && op != "deduct" {
		return fmt.Errorf("reward_points: operation must be 'award' or 'deduct', got %q", op)
	}
	_, hasFixed := params["points"]
	_, hasFormula := params["points_formula"]
	if !hasFixed && !hasFormula {
		return fmt.Errorf("reward_points: one of 'points' or 'points_formula' is required")
	}
	return nil
}

func (r *RewardPointsAction) Execute(
	ctx context.Context,
	actionID string,
	params map[string]interface{},
	evalCtx *dag.EvalContext,
) (*action.ActionResult, error) {
	op, _ := params["operation"].(string)
	reason, _ := params["reason"].(string)

	pts, err := resolvePoints(params, evalCtx)
	if err != nil {
		return &action.ActionResult{
			ActionID: actionID,
			Type:     r.Type(),
			Success:  false,
			Message:  err.Error(),
		}, err
	}

	pts = math.Round(pts*100) / 100 // round to 2 dp

	msg := fmt.Sprintf("%s %.0f points to %s", capitalize(op)+"ed", pts, evalCtx.Event.ActorID)
	if reason != "" {
		msg += " — " + reason
	}

	// In a real system, persist to a points ledger here.
	// For now we record in EvalContext.Results.
	evalCtx.Results[actionID] = map[string]interface{}{
		"operation": op,
		"points":    pts,
		"actor_id":  evalCtx.Event.ActorID,
	}

	return &action.ActionResult{
		ActionID: actionID,
		Type:     r.Type(),
		Success:  true,
		Message:  msg,
	}, nil
}

// resolvePoints returns the point value from either a fixed param or a formula.
func resolvePoints(params map[string]interface{}, evalCtx *dag.EvalContext) (float64, error) {
	if formula, ok := params["points_formula"].(string); ok && formula != "" {
		ast, err := condition.Parse(formula)
		if err != nil {
			return 0, fmt.Errorf("points_formula parse error: %w", err)
		}
		// Wrap the formula in a fake comparison to extract its numeric value.
		// We evaluate "formula > -1" and access the left operand directly.
		// Simpler: evaluate via a numeric resolver.
		val, err := evalNumericExpr(ast, evalCtx)
		if err != nil {
			return 0, fmt.Errorf("points_formula eval error: %w", err)
		}
		return val, nil
	}
	if pts, ok := toFloat64(params["points"]); ok {
		return pts, nil
	}
	return 0, fmt.Errorf("cannot resolve points value")
}

// evalNumericExpr evaluates a simple arithmetic-like expression by resolving
// field paths and computing the result. It handles the common case of
// "payload.amount * 0.05" by recursively walking BinaryExpr with * / + -.
func evalNumericExpr(expr condition.Expr, ctx *dag.EvalContext) (float64, error) {
	switch e := expr.(type) {
	case *condition.ComparisonExpr:
		// For formulas like "payload.amount * 0.05", the parser will read it
		// as a field * literal. We special-case the arithmetic operators here.
		left, err := resolveNumericOperand(e.Left, ctx)
		if err != nil {
			return 0, err
		}
		right, err := resolveNumericOperand(e.Right, ctx)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case "*":
			return left * right, nil
		case "/":
			if right == 0 {
				return 0, fmt.Errorf("division by zero in points_formula")
			}
			return left / right, nil
		case "+":
			return left + right, nil
		case "-":
			return left - right, nil
		default:
			return 0, fmt.Errorf("unsupported operator %q in points_formula", e.Op)
		}
	default:
		return 0, fmt.Errorf("unsupported expression type %T in points_formula", expr)
	}
}

func resolveNumericOperand(op condition.Operand, ctx *dag.EvalContext) (float64, error) {
	switch o := op.(type) {
	case *condition.LiteralOperand:
		if f, ok := toFloat64(o.Value); ok {
			return f, nil
		}
		return 0, fmt.Errorf("literal %v is not numeric", o.Value)
	case *condition.FieldOperand:
		val, ok := ctx.Resolve(o.Path)
		if !ok {
			return 0, fmt.Errorf("field %v not found", o.Path)
		}
		if f, ok := toFloat64(val); ok {
			return f, nil
		}
		return 0, fmt.Errorf("field %v value %v is not numeric", o.Path, val)
	default:
		return 0, fmt.Errorf("unknown operand type %T", op)
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-32) + s[1:]
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
