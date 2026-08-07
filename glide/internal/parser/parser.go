// Package parser builds the AST via recursive descent with a small
// Pratt expression core. The parser is the grammar for now — EBNF
// gets extracted from it later, not written ahead of it.
package parser

import (
	"fmt"
	"strings"

	"glide/internal/ast"
	"glide/internal/lexer"
)

type parser struct {
	toks []lexer.Token
	pos  int
}

func ParseFile(src string) (*ast.File, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parseFile()
}

// parseExprSrc parses an interpolation segment.
func parseExprSrc(src string, line int) (ast.Expr, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, fmt.Errorf("line %d: in interpolation {%s}: %v", line, src, err)
	}
	p := &parser{toks: toks}
	e, err := p.parseExpr()
	if err != nil {
		return nil, fmt.Errorf("line %d: in interpolation {%s}: %v", line, src, err)
	}
	p.skipSemis()
	if p.cur().Kind != lexer.EOF {
		return nil, fmt.Errorf("line %d: in interpolation {%s}: trailing input", line, src)
	}
	return e, nil
}

func (p *parser) cur() lexer.Token  { return p.toks[p.pos] }
func (p *parser) next() lexer.Token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) accept(k lexer.Kind) bool {
	if p.cur().Kind == k {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expect(k lexer.Kind, ctx string) (lexer.Token, error) {
	t := p.cur()
	if t.Kind != k {
		return t, p.errf("expected %s in %s, found %s", k, ctx, t.Kind)
	}
	p.pos++
	return t, nil
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", p.cur().Line, fmt.Sprintf(format, args...))
}

func (p *parser) skipSemis() {
	for p.cur().Kind == lexer.Semi {
		p.pos++
	}
}

// File level

func (p *parser) parseFile() (*ast.File, error) {
	f := &ast.File{}
	for {
		p.skipSemis()
		switch p.cur().Kind {
		case lexer.EOF:
			return f, nil
		case lexer.KwImport:
			p.next()
			t, err := p.expect(lexer.Ident, "import")
			if err != nil {
				return nil, err
			}
			f.Imports = append(f.Imports, t.Text)
		case lexer.KwFn:
			fn, err := p.parseFn()
			if err != nil {
				return nil, err
			}
			f.Funcs = append(f.Funcs, fn)
		default:
			return nil, p.errf("expected 'import' or 'fn' at top level, found %s", p.cur().Kind)
		}
	}
}

func (p *parser) parseFn() (*ast.FuncDecl, error) {
	line := p.cur().Line
	p.next() // fn
	name, err := p.expect(lexer.Ident, "function declaration")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LParen, "function declaration"); err != nil {
		return nil, err
	}
	var params []ast.Param
	for p.cur().Kind != lexer.RParen {
		pn, err := p.expect(lexer.Ident, "parameter list")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.Colon, "parameter list"); err != nil {
			return nil, err
		}
		pt, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params = append(params, ast.Param{Name: pn.Text, Type: pt})
		if !p.accept(lexer.Comma) {
			break
		}
	}
	if _, err := p.expect(lexer.RParen, "function declaration"); err != nil {
		return nil, err
	}
	ret := ""
	if p.accept(lexer.Arrow) {
		ret, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncDecl{Name: name.Text, Params: params, RetType: ret, Body: body, Line: line}, nil
}

// Types are kept as display strings for now (dynamically checked
// interpreter; annotations are documentation until the checker).
func (p *parser) parseType() (string, error) {
	switch p.cur().Kind {
	case lexer.Ident:
		name := p.next().Text
		if p.accept(lexer.Lt) {
			var parts []string
			for p.cur().Kind != lexer.Gt {
				t, err := p.parseType()
				if err != nil {
					return "", err
				}
				parts = append(parts, t)
				if !p.accept(lexer.Comma) {
					break
				}
			}
			if _, err := p.expect(lexer.Gt, "type arguments"); err != nil {
				return "", err
			}
			return name + "<" + strings.Join(parts, ", ") + ">", nil
		}
		return name, nil
	case lexer.LParen:
		p.next()
		if p.accept(lexer.RParen) {
			return "()", nil
		}
		var parts []string
		for {
			t, err := p.parseType()
			if err != nil {
				return "", err
			}
			parts = append(parts, t)
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RParen, "tuple type"); err != nil {
			return "", err
		}
		return "(" + strings.Join(parts, ", ") + ")", nil
	}
	return "", p.errf("expected a type, found %s", p.cur().Kind)
}

// Statements

func (p *parser) parseBlock() (*ast.Block, error) {
	if _, err := p.expect(lexer.LBrace, "block"); err != nil {
		return nil, err
	}
	b := &ast.Block{}
	for {
		p.skipSemis()
		if p.cur().Kind == lexer.RBrace {
			p.next()
			return b, nil
		}
		if p.cur().Kind == lexer.EOF {
			return nil, p.errf("unexpected end of file: unclosed block")
		}
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, s)
	}
}

func (p *parser) parseStmt() (ast.Stmt, error) {
	switch p.cur().Kind {
	case lexer.KwLet:
		return p.parseLet()
	case lexer.KwFor:
		return p.parseFor()
	case lexer.KwReturn:
		line := p.next().Line
		if p.cur().Kind == lexer.Semi || p.cur().Kind == lexer.RBrace {
			return &ast.ReturnStmt{Line: line}, nil
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.ReturnStmt{E: e, Line: line}, nil
	case lexer.KwImport:
		return nil, p.errf("imports are only allowed at the top of the file")
	}
	line := p.cur().Line
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	switch p.cur().Kind {
	case lexer.Assign, lexer.PlusEq, lexer.MinusEq:
		op := p.next().Text
		if err := validAssignTarget(e, op); err != nil {
			return nil, fmt.Errorf("line %d: %v", line, err)
		}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Target: e, Op: op, Value: v, Line: line}, nil
	}
	return &ast.ExprStmt{E: e, Line: line}, nil
}

func validAssignTarget(e ast.Expr, op string) error {
	switch t := e.(type) {
	case *ast.IdentExpr:
		if t.Name == "_" && op != "=" {
			return fmt.Errorf("cannot use %s with the discard target _", op)
		}
		return nil
	case *ast.Index:
		return nil
	}
	return fmt.Errorf("invalid assignment target: assign to a name or an index")
}

func (p *parser) parseLet() (ast.Stmt, error) {
	line := p.next().Line // let
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	typ := ""
	if p.accept(lexer.Colon) {
		typ, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	if _, err := p.expect(lexer.Assign, "let"); err != nil {
		return nil, err
	}
	init, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	var elseB *ast.Block
	if p.accept(lexer.KwElse) {
		elseB, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
	}
	return &ast.LetStmt{Pat: pat, Type: typ, Init: init, Else: elseB, Line: line}, nil
}

func (p *parser) parseFor() (ast.Stmt, error) {
	line := p.next().Line // for
	if p.cur().Kind == lexer.LBrace {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Body: body, Line: line}, nil
	}
	// Try `for <pattern> in`; backtrack to a conditional loop otherwise.
	save := p.pos
	if pat, err := p.parsePattern(); err == nil && p.cur().Kind == lexer.KwIn {
		p.next() // in
		iter, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Pat: pat, Iter: iter, Body: body, Line: line}, nil
	}
	p.pos = save
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.ForStmt{Cond: cond, Body: body, Line: line}, nil
}

// Patterns

func (p *parser) parsePattern() (ast.Pattern, error) {
	switch p.cur().Kind {
	case lexer.KwMut:
		p.next()
		t, err := p.expect(lexer.Ident, "pattern")
		if err != nil {
			return nil, err
		}
		if t.Text == "_" {
			return nil, p.errf("mut _ makes no sense")
		}
		return &ast.IdentPat{Name: t.Text, Mut: true}, nil
	case lexer.Ident:
		t := p.next()
		if t.Text == "_" {
			return &ast.WildPat{}, nil
		}
		return &ast.IdentPat{Name: t.Text}, nil
	case lexer.LParen:
		p.next()
		tp := &ast.TuplePat{}
		for {
			el, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			tp.Elems = append(tp.Elems, el)
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RParen, "tuple pattern"); err != nil {
			return nil, err
		}
		if len(tp.Elems) < 2 {
			return nil, p.errf("tuple pattern needs at least two elements")
		}
		return tp, nil
	case lexer.LBrack:
		p.next()
		lp := &ast.ListPat{Rest: -1}
		for p.cur().Kind != lexer.RBrack {
			if p.cur().Kind == lexer.DotDot {
				if lp.Rest >= 0 {
					return nil, p.errf("a list pattern may have at most one ..rest")
				}
				p.next()
				lp.Rest = len(lp.Elems)
				lp.RestName = "_"
				if p.cur().Kind == lexer.Ident {
					lp.RestName = p.next().Text
				}
			} else {
				el, err := p.parsePattern()
				if err != nil {
					return nil, err
				}
				lp.Elems = append(lp.Elems, el)
			}
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RBrack, "list pattern"); err != nil {
			return nil, err
		}
		return lp, nil
	}
	return nil, p.errf("expected a pattern, found %s", p.cur().Kind)
}

// Expressions — Pratt.

func prec(k lexer.Kind) int {
	switch k {
	case lexer.QQ:
		return 1
	case lexer.DotDot:
		return 2
	case lexer.OrOr:
		return 3
	case lexer.AndAnd:
		return 4
	case lexer.Eq, lexer.Ne:
		return 5
	case lexer.Lt, lexer.Le, lexer.Gt, lexer.Ge:
		return 6
	case lexer.Plus, lexer.Minus:
		return 7
	case lexer.Star, lexer.Slash, lexer.Percent:
		return 8
	}
	return 0
}

func (p *parser) parseExpr() (ast.Expr, error) {
	return p.parseBinary(1)
}

func (p *parser) parseBinary(min int) (ast.Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		k := p.cur().Kind
		pr := prec(k)
		if pr < min || pr == 0 {
			return left, nil
		}
		opTok := p.next()
		right, err := p.parseBinary(pr + 1)
		if err != nil {
			return nil, err
		}
		if k == lexer.DotDot {
			left = &ast.RangeExpr{Lo: left, Hi: right}
		} else {
			left = &ast.Binary{Op: opTok.Text, L: left, R: right, Line: opTok.Line}
		}
	}
}

func (p *parser) parseUnary() (ast.Expr, error) {
	switch p.cur().Kind {
	case lexer.Not, lexer.Minus:
		op := p.next().Text
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{Op: op, X: x}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (ast.Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.cur().Kind {
		case lexer.LParen:
			line := p.next().Line
			var args []ast.Expr
			for p.cur().Kind != lexer.RParen {
				a, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if !p.accept(lexer.Comma) {
					break
				}
			}
			if _, err := p.expect(lexer.RParen, "call"); err != nil {
				return nil, err
			}
			e = &ast.Call{Fn: e, Args: args, Line: line}
		case lexer.LBrack:
			line := p.next().Line
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RBrack, "index"); err != nil {
				return nil, err
			}
			e = &ast.Index{X: e, I: idx, Line: line}
		case lexer.Dot:
			line := p.next().Line
			switch p.cur().Kind {
			case lexer.Ident:
				e = &ast.Field{X: e, Name: p.next().Text, Line: line}
			case lexer.Int:
				e = &ast.TupleIndex{X: e, N: int(p.next().Int)}
			default:
				return nil, p.errf("expected a name or tuple index after '.', found %s", p.cur().Kind)
			}
		case lexer.Question:
			line := p.next().Line
			e = &ast.Try{X: e, Line: line}
		default:
			return e, nil
		}
	}
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.cur()
	switch t.Kind {
	case lexer.Int:
		p.next()
		return &ast.IntLit{V: t.Int}, nil
	case lexer.Float:
		p.next()
		return &ast.FloatLit{V: t.Float}, nil
	case lexer.KwTrue:
		p.next()
		return &ast.BoolLit{V: true}, nil
	case lexer.KwFalse:
		p.next()
		return &ast.BoolLit{V: false}, nil
	case lexer.String:
		p.next()
		lit := &ast.StrLit{}
		for _, part := range t.Parts {
			if part.IsExpr {
				e, err := parseExprSrc(part.S, part.Line)
				if err != nil {
					return nil, err
				}
				lit.Parts = append(lit.Parts, ast.StrPart{IsExpr: true, E: e, Spec: part.Spec})
			} else {
				lit.Parts = append(lit.Parts, ast.StrPart{Lit: part.S})
			}
		}
		return lit, nil
	case lexer.Ident:
		p.next()
		return &ast.IdentExpr{Name: t.Text, Line: t.Line}, nil
	case lexer.KwIf:
		return p.parseIf()
	case lexer.Pipe, lexer.OrOr:
		return p.parseClosure()
	case lexer.LParen:
		p.next()
		if p.accept(lexer.RParen) {
			return &ast.UnitLit{}, nil
		}
		first, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.accept(lexer.Comma) {
			tl := &ast.TupleLit{Elems: []ast.Expr{first}}
			for p.cur().Kind != lexer.RParen {
				e, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				tl.Elems = append(tl.Elems, e)
				if !p.accept(lexer.Comma) {
					break
				}
			}
			if _, err := p.expect(lexer.RParen, "tuple"); err != nil {
				return nil, err
			}
			return tl, nil
		}
		if _, err := p.expect(lexer.RParen, "parenthesised expression"); err != nil {
			return nil, err
		}
		return first, nil
	case lexer.LBrack:
		return p.parseListOrMap()
	}
	return nil, p.errf("expected an expression, found %s", t.Kind)
}

func (p *parser) parseListOrMap() (ast.Expr, error) {
	p.next() // [
	if p.cur().Kind == lexer.Colon {
		p.next()
		if _, err := p.expect(lexer.RBrack, "empty map literal"); err != nil {
			return nil, err
		}
		return &ast.MapLit{}, nil
	}
	if p.accept(lexer.RBrack) {
		return &ast.ListLit{}, nil
	}
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.accept(lexer.Colon) {
		m := &ast.MapLit{}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		m.Keys = append(m.Keys, first)
		m.Vals = append(m.Vals, v)
		for p.accept(lexer.Comma) {
			if p.cur().Kind == lexer.RBrack {
				break
			}
			k, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.Colon, "map literal"); err != nil {
				return nil, err
			}
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			m.Keys = append(m.Keys, k)
			m.Vals = append(m.Vals, v)
		}
		if _, err := p.expect(lexer.RBrack, "map literal"); err != nil {
			return nil, err
		}
		return m, nil
	}
	l := &ast.ListLit{Elems: []ast.Expr{first}}
	for p.accept(lexer.Comma) {
		if p.cur().Kind == lexer.RBrack {
			break
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		l.Elems = append(l.Elems, e)
	}
	if _, err := p.expect(lexer.RBrack, "list literal"); err != nil {
		return nil, err
	}
	return l, nil
}

func (p *parser) parseClosure() (ast.Expr, error) {
	c := &ast.Closure{}
	if p.cur().Kind == lexer.OrOr {
		p.next() // || — zero-parameter closure
	} else {
		p.next() // |
		for p.cur().Kind != lexer.Pipe {
			t, err := p.expect(lexer.Ident, "closure parameters")
			if err != nil {
				return nil, err
			}
			c.Params = append(c.Params, t.Text)
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.Pipe, "closure parameters"); err != nil {
			return nil, err
		}
	}
	if p.cur().Kind == lexer.LBrace {
		b, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		c.BodyBlock = b
		return c, nil
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	c.BodyExpr = e
	return c, nil
}

func (p *parser) parseIf() (ast.Expr, error) {
	line := p.next().Line // if
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	node := &ast.If{Cond: cond, Then: then, Line: line}
	if p.accept(lexer.KwElse) {
		if p.cur().Kind == lexer.KwIf {
			ei, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			node.ElseIf = ei.(*ast.If)
		} else {
			eb, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			node.ElseBlock = eb
		}
	}
	return node, nil
}
