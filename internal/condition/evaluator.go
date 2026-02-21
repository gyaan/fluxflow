package condition

import (
	"fmt"
	"strings"
)

// EvalContext provides data for expression evaluation.
// It mirrors dag.EvalContext but is kept here to avoid an import cycle.
type EvalContext interface {
	Resolve(path []string) (interface{}, bool)
}

// Evaluate walks the AST and returns true/false or an error.
func Evaluate(expr Expr, ctx EvalContext) (bool, error) {
	switch e := expr.(type) {
	case *BinaryExpr:
		return evalBinary(e, ctx)
	case *NotExpr:
		v, err := Evaluate(e.Expr, ctx)
		if err != nil {
			return false, err
		}
		return !v, nil
	case *ComparisonExpr:
		return evalComparison(e, ctx)
	default:
		return false, fmt.Errorf("unknown expr type %T", expr)
	}
}

func evalBinary(e *BinaryExpr, ctx EvalContext) (bool, error) {
	left, err := Evaluate(e.Left, ctx)
	if err != nil {
		return false, err
	}
	switch strings.ToUpper(e.Op) {
	case "AND":
		if !left {
			return false, nil // short-circuit
		}
		return Evaluate(e.Right, ctx)
	case "OR":
		if left {
			return true, nil // short-circuit
		}
		return Evaluate(e.Right, ctx)
	default:
		return false, fmt.Errorf("unknown binary op %q", e.Op)
	}
}

func evalComparison(e *ComparisonExpr, ctx EvalContext) (bool, error) {
	left, err := resolveOperand(e.Left, ctx)
	if err != nil {
		return false, err
	}
	right, err := resolveOperand(e.Right, ctx)
	if err != nil {
		return false, err
	}
	return compare(e.Op, left, right)
}

func resolveOperand(op Operand, ctx EvalContext) (interface{}, error) {
	switch o := op.(type) {
	case *LiteralOperand:
		return o.Value, nil
	case *FieldOperand:
		val, ok := ctx.Resolve(o.Path)
		if !ok {
			return nil, fmt.Errorf("field %q not found", strings.Join(o.Path, "."))
		}
		return val, nil
	default:
		return nil, fmt.Errorf("unknown operand type %T", op)
	}
}
