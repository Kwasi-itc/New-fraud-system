// Package expressionassist implements a bounded local expression compiler and
// structural explainer. It does not call or impersonate a language model.
package expressionassist

import (
	"encoding/json"
	"errors"
	"fmt"
	ast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/scanner"
)

var ErrExpression = errors.New("invalid local expression; use fields, JSON literals, comparisons, and/or/not and parentheses")

type parser struct {
	scanner      scanner.Scanner
	token        rune
	text         string
	invalid      bool
	nodes, depth int
}

func Compile(expression string) (ast.Node, error) {
	if len(expression) == 0 || len(expression) > 8192 {
		return ast.Node{}, ErrExpression
	}
	p := &parser{}
	p.scanner.Init(strings.NewReader(expression))
	p.scanner.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats | scanner.ScanStrings
	p.scanner.Error = func(*scanner.Scanner, string) { p.invalid = true }
	p.next()
	node, err := p.binary(1)
	if err != nil || p.invalid || p.token != scanner.EOF {
		return ast.Node{}, ErrExpression
	}
	return node, nil
}
func (p *parser) next() { p.token = p.scanner.Scan(); p.text = p.scanner.TokenText() }
func (p *parser) binary(level int) (ast.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return ast.Node{}, ErrExpression
	}
	var left ast.Node
	var err error
	if level < 3 {
		left, err = p.binary(level + 1)
	} else {
		left, err = p.atom()
	}
	if err != nil {
		return left, err
	}
	for {
		op := ""
		if level == 1 && p.text == "or" {
			op = "or"
		} else if level == 2 && p.text == "and" {
			op = "and"
		} else if level == 3 && strings.Contains("=<>!", p.text) && len(p.text) == 1 {
			first := p.text
			p.next()
			operator := first
			if p.text == "=" {
				operator += "="
				p.next()
			}
			op = map[string]string{"==": "eq", "!=": "neq", ">": "gt", ">=": "gte", "<": "lt", "<=": "lte"}[operator]
			if op == "" {
				return left, ErrExpression
			}
			right, err := p.atom()
			if err != nil {
				return left, err
			}
			left = ast.Node{Function: op, Children: []ast.Node{left, right}}
			p.nodes++
			break
		}
		if op == "" {
			break
		}
		p.next()
		right, err := p.binary(level + 1)
		if err != nil {
			return left, err
		}
		left = ast.Node{Function: op, Children: []ast.Node{left, right}}
		p.nodes++
		if p.nodes > 256 {
			return left, ErrExpression
		}
	}
	return left, nil
}
func (p *parser) atom() (ast.Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	p.nodes++
	if p.depth > 32 || p.nodes > 256 || p.invalid {
		return ast.Node{}, ErrExpression
	}
	if p.text == "not" {
		p.next()
		n, err := p.atom()
		return ast.Node{Function: "not", Children: []ast.Node{n}}, err
	}
	if p.token == '(' {
		p.next()
		n, err := p.binary(1)
		if err != nil || p.token != ')' {
			return ast.Node{}, ErrExpression
		}
		p.next()
		return n, nil
	}
	negative := false
	if p.token == '-' {
		negative = true
		p.next()
	}
	var n ast.Node
	switch p.token {
	case scanner.Int, scanner.Float:
		value, err := strconv.ParseFloat(p.text, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return n, ErrExpression
		}
		if negative {
			value = -value
		}
		n.Constant = value
	case scanner.String:
		if negative {
			return n, ErrExpression
		}
		var value string
		if err := json.Unmarshal([]byte(p.text), &value); err != nil {
			return n, ErrExpression
		}
		n.Constant = value
	case scanner.Ident:
		if negative {
			return n, ErrExpression
		}
		switch p.text {
		case "true":
			n.Constant = true
		case "false":
			n.Constant = false
		case "null":
			n.Constant = nil
		case "and", "or", "not":
			return n, ErrExpression
		default:
			n = ast.Node{Function: "Payload", Children: []ast.Node{{Constant: p.text}}}
		}
	default:
		return n, ErrExpression
	}
	p.next()
	return n, nil
}

// Describe preserves every argument and sorts named arguments so repeated
// explanations are stable. Budgets apply before recursive model validation.
func Describe(node ast.Node) (string, error) { nodes := 0; return describe(node, 0, &nodes) }
func describe(n ast.Node, depth int, nodes *int) (string, error) {
	*nodes++
	if depth > 32 || *nodes > 256 {
		return "", ErrExpression
	}
	if n.Function == "" || n.Function == "constant" {
		raw, err := json.Marshal(n.Constant)
		if err != nil || len(raw) > 8192 {
			return "", ErrExpression
		}
		return string(raw), nil
	}
	args := make([]string, 0, len(n.Children)+len(n.NamedChildren))
	if cap(args) > 256 {
		return "", ErrExpression
	}
	for _, child := range n.Children {
		v, err := describe(child, depth+1, nodes)
		if err != nil {
			return "", err
		}
		args = append(args, v)
	}
	keys := make([]string, 0, len(n.NamedChildren))
	for key := range n.NamedChildren {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v, err := describe(n.NamedChildren[key], depth+1, nodes)
		if err != nil {
			return "", err
		}
		args = append(args, strconv.Quote(key)+"="+v)
	}
	out := n.Function + "(" + strings.Join(args, ", ") + ")"
	if n.Function == "Payload" && len(args) == 1 {
		out = "payload[" + args[0] + "]"
	}
	if operator := map[string]string{"eq": "equals", "neq": "does not equal", "gt": "is greater than", "gte": "is at least", "lt": "is less than", "lte": "is at most"}[n.Function]; operator != "" && len(args) == 2 {
		out = fmt.Sprintf("(%s %s %s)", args[0], operator, args[1])
	}
	if (n.Function == "and" || n.Function == "or") && len(args) > 0 {
		out = "(" + strings.Join(args, " "+n.Function+" ") + ")"
	}
	if n.Function == "not" && len(args) == 1 {
		out = "not (" + args[0] + ")"
	}
	if len(out) > 16384 {
		return "", ErrExpression
	}
	return out, nil
}
