package condition

import (
	"testing"
)

// mockCtx implements EvalContext for tests.
type mockCtx struct {
	data map[string]interface{}
}

func (m *mockCtx) Resolve(path []string) (interface{}, bool) {
	if len(path) == 0 {
		return nil, false
	}
	v, ok := m.data[path[0]]
	if !ok || len(path) == 1 {
		return v, ok
	}
	sub, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return (&mockCtx{data: sub}).Resolve(path[1:])
}

func ctx(kv ...interface{}) *mockCtx {
	m := &mockCtx{data: make(map[string]interface{})}
	for i := 0; i < len(kv)-1; i += 2 {
		m.data[kv[i].(string)] = kv[i+1]
	}
	return m
}

type evalCase struct {
	name    string
	expr    string
	ctx     EvalContext
	want    bool
	wantErr bool
}

func TestEvaluate(t *testing.T) {
	cases := []evalCase{
		// Numeric comparisons
		{
			name: "gt true",
			expr: "amount > 1000",
			ctx:  ctx("amount", float64(1500)),
			want: true,
		},
		{
			name: "gt false",
			expr: "amount > 1000",
			ctx:  ctx("amount", float64(500)),
			want: false,
		},
		{
			name: "gte equal",
			expr: "amount >= 1000",
			ctx:  ctx("amount", float64(1000)),
			want: true,
		},
		{
			name: "lt true",
			expr: "amount < 100",
			ctx:  ctx("amount", float64(50)),
			want: true,
		},
		// String equality
		{
			name: "eq string true",
			expr: `category == "food"`,
			ctx:  ctx("category", "food"),
			want: true,
		},
		{
			name: "eq string false",
			expr: `category == "food"`,
			ctx:  ctx("category", "electronics"),
			want: false,
		},
		{
			name: "neq string",
			expr: `category != "food"`,
			ctx:  ctx("category", "electronics"),
			want: true,
		},
		// Boolean
		{
			name: "bool eq true",
			expr: "is_first_login == true",
			ctx:  ctx("is_first_login", true),
			want: true,
		},
		{
			name: "bool eq false literal",
			expr: "is_first_login == false",
			ctx:  ctx("is_first_login", true),
			want: false,
		},
		// AND / OR
		{
			name: "AND both true",
			expr: `category == "food" AND amount > 500`,
			ctx:  ctx("category", "food", "amount", float64(1000)),
			want: true,
		},
		{
			name: "AND first false",
			expr: `category == "food" AND amount > 500`,
			ctx:  ctx("category", "clothing", "amount", float64(1000)),
			want: false,
		},
		{
			name: "OR first true",
			expr: `category == "food" OR amount > 500`,
			ctx:  ctx("category", "clothing", "amount", float64(1000)),
			want: true,
		},
		{
			name: "OR both false",
			expr: `category == "food" OR amount > 500`,
			ctx:  ctx("category", "clothing", "amount", float64(10)),
			want: false,
		},
		// NOT
		{
			name: "NOT true",
			expr: `NOT amount > 1000`,
			ctx:  ctx("amount", float64(500)),
			want: true,
		},
		// contains
		{
			name: "contains true",
			expr: `tags contains "vip"`,
			ctx:  ctx("tags", "vip-member"),
			want: true,
		},
		{
			name: "contains false",
			expr: `tags contains "vip"`,
			ctx:  ctx("tags", "regular"),
			want: false,
		},
		// matches (regex)
		{
			name: "matches true",
			expr: `email matches ".*@example\\.com"`,
			ctx:  ctx("email", "user@example.com"),
			want: true,
		},
		{
			name: "matches false",
			expr: `email matches ".*@example\\.com"`,
			ctx:  ctx("email", "user@other.com"),
			want: false,
		},
		// Nested field (handled by Resolve in real ctx; mock supports one level)
		// Error cases
		{
			name:    "unknown field",
			expr:    "missing > 10",
			ctx:     ctx("amount", float64(100)),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.expr)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.expr, err)
			}
			got, err := Evaluate(ast, tc.ctx)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Evaluate error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Evaluate(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []string{
		`"unterminated`,
		`amount 1000`, // missing operator
		``,            // empty (will fail at comparison level)
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			_, err := Parse(expr)
			if err == nil {
				t.Errorf("expected parse error for %q, got nil", expr)
			}
		})
	}
}
