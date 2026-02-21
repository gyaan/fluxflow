package condition

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// -----------------------------------------------------------------------
// AST nodes
// -----------------------------------------------------------------------

// Expr is the common interface for all AST nodes.
type Expr interface {
	exprNode()
}

// BinaryExpr represents AND / OR.
type BinaryExpr struct {
	Op    string // "AND" | "OR"
	Left  Expr
	Right Expr
}

func (*BinaryExpr) exprNode() {}

// NotExpr represents NOT <expr>.
type NotExpr struct {
	Expr Expr
}

func (*NotExpr) exprNode() {}

// ComparisonExpr represents <operand> <operator> <operand>.
type ComparisonExpr struct {
	Left  Operand
	Op    Operator
	Right Operand
}

func (*ComparisonExpr) exprNode() {}

// -----------------------------------------------------------------------
// Operands
// -----------------------------------------------------------------------

// Operand is either a literal value or a field path.
type Operand interface {
	operandNode()
}

// LiteralOperand holds a pre-parsed constant.
type LiteralOperand struct {
	Value interface{}
}

func (*LiteralOperand) operandNode() {}

// FieldOperand holds a dot-separated path like "payload.amount".
type FieldOperand struct {
	Path []string // ["payload", "amount"]
}

func (*FieldOperand) operandNode() {}

// -----------------------------------------------------------------------
// Tokenizer
// -----------------------------------------------------------------------

type tokenKind int

const (
	tokWord   tokenKind = iota // identifier or keyword
	tokOp                      // ==, !=, >=, <=, >, <
	tokString                  // "…" or '…'
	tokNumber                  // 42 | 3.14
	tokBool                    // true | false
	tokLParen
	tokRParen
	tokEOF
)

type token struct {
	kind tokenKind
	val  string
}

func tokenize(expr string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(expr) {
		ch := expr[i]
		// Skip whitespace.
		if unicode.IsSpace(rune(ch)) {
			i++
			continue
		}
		// Parentheses.
		if ch == '(' {
			tokens = append(tokens, token{tokLParen, "("})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, token{tokRParen, ")"})
			i++
			continue
		}
		// Operators.
		if ch == '=' || ch == '!' || ch == '<' || ch == '>' {
			if i+1 < len(expr) && expr[i+1] == '=' {
				tokens = append(tokens, token{tokOp, string(expr[i:i+2])})
				i += 2
			} else {
				tokens = append(tokens, token{tokOp, string(ch)})
				i++
			}
			continue
		}
		// Arithmetic operators (used in formula expressions).
		// '-' is only arithmetic when not immediately followed by a digit
		// (negative number literals are handled below).
		if ch == '*' || ch == '/' || ch == '+' {
			tokens = append(tokens, token{tokOp, string(ch)})
			i++
			continue
		}
		if ch == '-' && (i+1 >= len(expr) || !unicode.IsDigit(rune(expr[i+1]))) {
			tokens = append(tokens, token{tokOp, string(ch)})
			i++
			continue
		}
		// String literals.
		if ch == '"' || ch == '\'' {
			quote := ch
			j := i + 1
			for j < len(expr) && expr[j] != quote {
				if expr[j] == '\\' {
					j++ // skip escaped char
				}
				j++
			}
			if j >= len(expr) {
				return nil, fmt.Errorf("unterminated string starting at position %d", i)
			}
			// Unescape basic escapes.
			inner := expr[i+1 : j]
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, `\'`, `'`)
			inner = strings.ReplaceAll(inner, `\\`, `\`)
			tokens = append(tokens, token{tokString, inner})
			i = j + 1
			continue
		}
		// Numbers.
		if unicode.IsDigit(rune(ch)) || (ch == '-' && i+1 < len(expr) && unicode.IsDigit(rune(expr[i+1]))) {
			j := i
			if expr[j] == '-' {
				j++
			}
			for j < len(expr) && (unicode.IsDigit(rune(expr[j])) || expr[j] == '.') {
				j++
			}
			tokens = append(tokens, token{tokNumber, expr[i:j]})
			i = j
			continue
		}
		// Words (identifiers, keywords, operators like AND/OR/NOT/contains/matches).
		if unicode.IsLetter(rune(ch)) || ch == '_' {
			j := i
			for j < len(expr) && (unicode.IsLetter(rune(expr[j])) || unicode.IsDigit(rune(expr[j])) || expr[j] == '_' || expr[j] == '.') {
				j++
			}
			word := expr[i:j]
			switch strings.ToLower(word) {
			case "true", "false":
				tokens = append(tokens, token{tokBool, strings.ToLower(word)})
			default:
				tokens = append(tokens, token{tokWord, word})
			}
			i = j
			continue
		}
		return nil, fmt.Errorf("unexpected character %q at position %d", ch, i)
	}
	tokens = append(tokens, token{tokEOF, ""})
	return tokens, nil
}

// -----------------------------------------------------------------------
// Recursive-descent parser
// -----------------------------------------------------------------------

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	return p.tokens[p.pos]
}

func (p *parser) consume() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) expect(kind tokenKind, val string) error {
	t := p.peek()
	if t.kind != kind || (val != "" && t.val != val) {
		return fmt.Errorf("expected %q but got %q", val, t.val)
	}
	p.consume()
	return nil
}

// Parse parses an expression string into an AST.
func Parse(expr string) (Expr, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q after expression", p.peek().val)
	}
	return node, nil
}

// or_expr = and_expr ( "OR" and_expr )*
func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokWord && strings.ToUpper(p.peek().val) == "OR" {
		p.consume()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

// and_expr = not_expr ( "AND" not_expr )*
func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokWord && strings.ToUpper(p.peek().val) == "AND" {
		p.consume()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

// not_expr = [ "NOT" ] comparison | "(" or_expr ")"
func (p *parser) parseNot() (Expr, error) {
	if p.peek().kind == tokWord && strings.ToUpper(p.peek().val) == "NOT" {
		p.consume()
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &NotExpr{Expr: inner}, nil
	}
	if p.peek().kind == tokLParen {
		p.consume()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return inner, nil
	}
	return p.parseComparison()
}

// comparison = operand operator operand
func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	t := p.peek()
	var op Operator
	switch {
	case t.kind == tokOp:
		op = Operator(t.val)
		p.consume()
	case t.kind == tokWord && strings.ToLower(t.val) == "contains":
		op = OpContains
		p.consume()
	case t.kind == tokWord && strings.ToLower(t.val) == "matches":
		op = OpMatches
		p.consume()
	default:
		return nil, fmt.Errorf("expected comparison operator, got %q", t.val)
	}

	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	return &ComparisonExpr{Left: left, Op: op, Right: right}, nil
}

// operand = field_path | literal
func (p *parser) parseOperand() (Operand, error) {
	t := p.peek()
	switch t.kind {
	case tokString:
		p.consume()
		return &LiteralOperand{Value: t.val}, nil
	case tokNumber:
		p.consume()
		if strings.Contains(t.val, ".") {
			f, err := strconv.ParseFloat(t.val, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", t.val)
			}
			return &LiteralOperand{Value: f}, nil
		}
		n, err := strconv.ParseInt(t.val, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q", t.val)
		}
		return &LiteralOperand{Value: float64(n)}, nil
	case tokBool:
		p.consume()
		return &LiteralOperand{Value: t.val == "true"}, nil
	case tokWord:
		p.consume()
		// Field path: split on '.' (already in token since tokenizer includes dots).
		return &FieldOperand{Path: strings.Split(t.val, ".")}, nil
	default:
		return nil, fmt.Errorf("expected operand, got %q", t.val)
	}
}
