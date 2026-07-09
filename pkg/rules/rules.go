// Package rules implements GoForge's access-rule and filter expression
// language, compiled to parameterized SQL so every check runs in the
// database with consistent semantics.
//
// Examples:
//
//	""                                              → public (matches all)
//	@request.auth.id != ''                          → any authenticated user
//	owner = @request.auth.id                        → record owner
//	@request.auth.roles ~ 'admin'                   → role check
//	status = 'published' && created >= '2025-01-01' → field conditions
//	author.plan = 'pro'                             → single-hop relation
//
// A nil rule (as opposed to an empty string) is "locked": only superusers.
package rules

import (
	"fmt"
	"strings"
	"unicode"
)

// ---- AST ----

type Expr interface{ node() }

// Logic is && / ||.
type Logic struct {
	Op   string
	L, R Expr
}

// Cmp is a comparison: = != > >= < <= ~ !~.
type Cmp struct {
	Op   string
	L, R Expr
}

// Ident is a field reference (posts.title) or placeholder (@request.auth.id).
type Ident struct{ Name string }

// Lit is a literal: string, float64, bool or nil.
type Lit struct{ Val any }

func (Logic) node() {}
func (Cmp) node()   {}
func (Ident) node() {}
func (Lit) node()   {}

// ---- Lexer ----

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tString
	tNumber
	tOp // = != > >= < <= ~ !~ && ||
	tLParen
	tRParen
)

type tok struct {
	kind tokKind
	val  string
	pos  int
}

const maxRuleLen = 2000

func lex(input string) ([]tok, error) {
	if len(input) > maxRuleLen {
		return nil, fmt.Errorf("rules: expression too long")
	}
	var toks []tok
	i := 0
	for i < len(input) {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, tok{tLParen, "(", i})
			i++
		case c == ')':
			toks = append(toks, tok{tRParen, ")", i})
			i++
		case c == '\'' || c == '"':
			quote := c
			j := i + 1
			var sb strings.Builder
			closed := false
			for j < len(input) {
				if input[j] == '\\' && j+1 < len(input) {
					sb.WriteByte(input[j+1])
					j += 2
					continue
				}
				if input[j] == quote {
					closed = true
					break
				}
				sb.WriteByte(input[j])
				j++
			}
			if !closed {
				return nil, fmt.Errorf("rules: unterminated string at %d", i)
			}
			toks = append(toks, tok{tString, sb.String(), i})
			i = j + 1
		case c == '&' || c == '|':
			if i+1 < len(input) && input[i+1] == c {
				toks = append(toks, tok{tOp, string(c) + string(c), i})
				i += 2
			} else {
				return nil, fmt.Errorf("rules: unexpected %q at %d", string(c), i)
			}
		case c == '=':
			toks = append(toks, tok{tOp, "=", i})
			i++
		case c == '!':
			if i+1 < len(input) && input[i+1] == '=' {
				toks = append(toks, tok{tOp, "!=", i})
				i += 2
			} else if i+1 < len(input) && input[i+1] == '~' {
				toks = append(toks, tok{tOp, "!~", i})
				i += 2
			} else {
				return nil, fmt.Errorf("rules: unexpected '!' at %d", i)
			}
		case c == '~':
			toks = append(toks, tok{tOp, "~", i})
			i++
		case c == '>' || c == '<':
			op := string(c)
			if i+1 < len(input) && input[i+1] == '=' {
				op += "="
				i++
			}
			toks = append(toks, tok{tOp, op, i})
			i++
		case c >= '0' && c <= '9' || c == '-' && i+1 < len(input) && input[i+1] >= '0' && input[i+1] <= '9':
			j := i + 1
			for j < len(input) && (input[j] >= '0' && input[j] <= '9' || input[j] == '.') {
				j++
			}
			toks = append(toks, tok{tNumber, input[i:j], i})
			i = j
		case isIdentStart(rune(c)):
			j := i + 1
			for j < len(input) && isIdentPart(rune(input[j])) {
				j++
			}
			toks = append(toks, tok{tIdent, input[i:j], i})
			i = j
		default:
			return nil, fmt.Errorf("rules: unexpected character %q at %d", string(c), i)
		}
	}
	toks = append(toks, tok{tEOF, "", len(input)})
	return toks, nil
}

func isIdentStart(r rune) bool {
	return r == '@' || r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '.' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ---- Parser (recursive descent) ----

type parser struct {
	toks []tok
	pos  int
}

// Parse parses a rule expression. Empty input returns (nil, nil), meaning
// "matches everything".
func Parse(input string) (Expr, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("rules: unexpected %q at %d", p.cur().val, p.cur().pos)
	}
	return expr, nil
}

func (p *parser) cur() tok  { return p.toks[p.pos] }
func (p *parser) next() tok { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tOp && p.cur().val == "||" {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = Logic{Op: "||", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tOp && p.cur().val == "&&" {
		p.next()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = Logic{Op: "&&", L: left, R: right}
	}
	return left, nil
}

var cmpOps = map[string]bool{"=": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true, "~": true, "!~": true}

func (p *parser) parseCmp() (Expr, error) {
	if p.cur().kind == tLParen {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tRParen {
			return nil, fmt.Errorf("rules: missing ')' at %d", p.cur().pos)
		}
		p.next()
		return inner, nil
	}
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	if p.cur().kind == tOp && cmpOps[p.cur().val] {
		op := p.next().val
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return Cmp{Op: op, L: left, R: right}, nil
	}
	return nil, fmt.Errorf("rules: expected comparison operator at %d", p.cur().pos)
}

func (p *parser) parseOperand() (Expr, error) {
	t := p.cur()
	switch t.kind {
	case tString:
		p.next()
		return Lit{Val: t.val}, nil
	case tNumber:
		p.next()
		var f float64
		if _, err := fmt.Sscanf(t.val, "%g", &f); err != nil {
			return nil, fmt.Errorf("rules: bad number %q", t.val)
		}
		return Lit{Val: f}, nil
	case tIdent:
		p.next()
		switch t.val {
		case "true":
			return Lit{Val: true}, nil
		case "false":
			return Lit{Val: false}, nil
		case "null":
			return Lit{Val: nil}, nil
		}
		return Ident{Name: t.val}, nil
	}
	return nil, fmt.Errorf("rules: unexpected %q at %d", t.val, t.pos)
}
