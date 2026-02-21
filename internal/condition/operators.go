package condition

import (
	"fmt"
	"math"
	"regexp"
)

// Operator represents a comparison operator.
type Operator string

const (
	OpEq       Operator = "=="
	OpNeq      Operator = "!="
	OpGt       Operator = ">"
	OpGte      Operator = ">="
	OpLt       Operator = "<"
	OpLte      Operator = "<="
	OpContains Operator = "contains"
	OpMatches  Operator = "matches"
)

// toFloat64 coerces a numeric value to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// compare applies a binary comparison operator to two values.
func compare(op Operator, left, right interface{}) (bool, error) {
	switch op {
	case OpEq:
		return equal(left, right), nil
	case OpNeq:
		return !equal(left, right), nil
	case OpGt, OpGte, OpLt, OpLte:
		return numericCompare(op, left, right)
	case OpContains:
		return containsOp(left, right)
	case OpMatches:
		return matchesOp(left, right)
	default:
		return false, fmt.Errorf("unknown operator: %s", op)
	}
}

// equal does deep-ish equality: numeric types are compared by value.
func equal(left, right interface{}) bool {
	lf, lok := toFloat64(left)
	rf, rok := toFloat64(right)
	if lok && rok {
		return math.Abs(lf-rf) < 1e-9
	}
	// bool
	if lb, ok := left.(bool); ok {
		if rb, ok := right.(bool); ok {
			return lb == rb
		}
		return false
	}
	// string fallback
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func numericCompare(op Operator, left, right interface{}) (bool, error) {
	lf, lok := toFloat64(left)
	rf, rok := toFloat64(right)
	if !lok || !rok {
		return false, fmt.Errorf("operator %s requires numeric operands, got %T and %T", op, left, right)
	}
	switch op {
	case OpGt:
		return lf > rf, nil
	case OpGte:
		return lf >= rf, nil
	case OpLt:
		return lf < rf, nil
	case OpLte:
		return lf <= rf, nil
	}
	return false, nil
}

func containsOp(left, right interface{}) (bool, error) {
	ls, ok := left.(string)
	if !ok {
		return false, fmt.Errorf("contains: left operand must be a string, got %T", left)
	}
	rs := fmt.Sprintf("%v", right)
	return contains(ls, rs), nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func matchesOp(left, right interface{}) (bool, error) {
	ls, ok := left.(string)
	if !ok {
		return false, fmt.Errorf("matches: left operand must be a string, got %T", left)
	}
	pattern, ok := right.(string)
	if !ok {
		return false, fmt.Errorf("matches: right operand must be a string pattern, got %T", right)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("matches: invalid regex %q: %w", pattern, err)
	}
	return re.MatchString(ls), nil
}
