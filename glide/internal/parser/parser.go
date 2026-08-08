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
	// noStruct disables struct literals while parsing control-flow
	// headers (`if c == Red {` must not read `Red {` as a literal —
	// Rust's rule). Parens and argument lists re-enable them.
	noStruct bool
	// loopDepth counts enclosing `for` bodies, so `break`/`continue`
	// outside a loop is a parse error. Closure bodies reset it: a
	// closure is its own function, and a break inside one cannot
	// target the loop it happens to be written in.
	loopDepth int
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
	// The snippet is lexed standalone, so its tokens think they're on
	// line 1; rebase them onto the string's line so AST nodes built
	// from them blame the right place at runtime.
	for i := range toks {
		toks[i].Line += line - 1
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

func (p *parser) peek() lexer.Token {
	if p.pos+1 < len(p.toks) {
		return p.toks[p.pos+1]
	}
	return p.toks[len(p.toks)-1]
}

// headerExpr parses an expression with struct literals disabled.
func (p *parser) headerExpr() (ast.Expr, error) {
	saved := p.noStruct
	p.noStruct = true
	e, err := p.parseExpr()
	p.noStruct = saved
	return e, err
}

// structsOK runs fn with struct literals re-enabled (inside parens,
// argument lists, literal fields — anywhere braces are unambiguous).
func structsOK[T any](p *parser, fn func() (T, error)) (T, error) {
	saved := p.noStruct
	p.noStruct = false
	v, err := fn()
	p.noStruct = saved
	return v, err
}

func isCapitalized(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

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
		p.accept(lexer.KwPub) // visibility is the checker's business; parsed, ignored
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
		case lexer.KwType:
			td, err := p.parseTypeDecl()
			if err != nil {
				return nil, err
			}
			f.Types = append(f.Types, td)
		case lexer.KwImpl:
			im, err := p.parseImpl()
			if err != nil {
				return nil, err
			}
			f.Impls = append(f.Impls, im)
		case lexer.Ident:
			// test/bench are contextual: keywords only at top level
			// followed by a string, so `let test = …` stays legal.
			if p.cur().Text == "test" && p.peek().Kind == lexer.String {
				td, err := p.parseTest()
				if err != nil {
					return nil, err
				}
				f.Tests = append(f.Tests, td)
				continue
			}
			if p.cur().Text == "bench" && p.peek().Kind == lexer.String {
				bd, err := p.parseBench()
				if err != nil {
					return nil, err
				}
				f.Benches = append(f.Benches, bd)
				continue
			}
			return nil, p.errf("expected a declaration at top level, found %q", p.cur().Text)
		default:
			return nil, p.errf("expected a declaration at top level, found %s", p.cur().Kind)
		}
	}
}

// skipGenerics consumes `<...>` type-parameter lists (parsed for
// shape, ignored: the interpreter is dynamically checked).
func (p *parser) skipGenerics() error {
	if p.cur().Kind != lexer.Lt {
		return nil
	}
	depth := 0
	for {
		switch p.cur().Kind {
		case lexer.Lt:
			depth++
		case lexer.Gt:
			depth--
			if depth == 0 {
				p.next()
				return nil
			}
		case lexer.EOF, lexer.LBrace, lexer.Semi:
			return p.errf("unclosed type parameter list")
		}
		p.next()
	}
}

func (p *parser) parseTypeDecl() (*ast.TypeDecl, error) {
	line := p.next().Line // type
	name, err := p.expect(lexer.Ident, "type declaration")
	if err != nil {
		return nil, err
	}
	if !isCapitalized(name.Text) {
		return nil, p.errf("type names are capitalised: %q", name.Text)
	}
	if err := p.skipGenerics(); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Assign, "type declaration"); err != nil {
		return nil, err
	}
	td := &ast.TypeDecl{Name: name.Text, Line: line}
	if p.cur().Kind == lexer.KwStruct {
		p.next()
		if _, err := p.expect(lexer.LBrace, "struct declaration"); err != nil {
			return nil, err
		}
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				break
			}
			pub := p.accept(lexer.KwPub)
			fn, err := p.expect(lexer.Ident, "struct field")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.Colon, "struct field"); err != nil {
				return nil, err
			}
			ft, err := p.parseType()
			if err != nil {
				return nil, err
			}
			td.Fields = append(td.Fields, ast.FieldDecl{Name: fn.Text, Type: ft, Pub: pub})
			if !p.accept(lexer.Comma) {
				p.skipSemis()
			}
		}
		// derive(...) trails the declaration; meaningless until the
		// compile-time half exists, so parse and drop.
		if p.cur().Kind == lexer.Ident && p.cur().Text == "derive" {
			p.next()
			if _, err := p.expect(lexer.LParen, "derive"); err != nil {
				return nil, err
			}
			for p.cur().Kind != lexer.RParen {
				p.next()
			}
			p.next()
		}
		return td, nil
	}
	if p.cur().Kind == lexer.Ident && p.cur().Text == "distinct" {
		return nil, p.errf("distinct types are not implemented yet (M3)")
	}
	// Sum type: Variant [ (T, T) ] | Variant | …
	for {
		vn, err := p.expect(lexer.Ident, "sum type variant")
		if err != nil {
			return nil, err
		}
		if !isCapitalized(vn.Text) {
			return nil, p.errf("variant names are capitalised: %q", vn.Text)
		}
		v := ast.VariantDecl{Name: vn.Text}
		if p.accept(lexer.LParen) {
			for p.cur().Kind != lexer.RParen {
				if _, err := p.parseType(); err != nil {
					return nil, err
				}
				v.Arity++
				if !p.accept(lexer.Comma) {
					break
				}
			}
			if _, err := p.expect(lexer.RParen, "variant payload"); err != nil {
				return nil, err
			}
		}
		td.Variants = append(td.Variants, v)
		p.skipSemis() // `|` may start the next line
		if !p.accept(lexer.Pipe) {
			return td, nil
		}
		p.skipSemis()
	}
}

func (p *parser) parseImpl() (*ast.ImplBlock, error) {
	line := p.next().Line // impl
	first, err := p.expect(lexer.Ident, "impl")
	if err != nil {
		return nil, err
	}
	if err := p.skipGenerics(); err != nil {
		return nil, err
	}
	im := &ast.ImplBlock{Target: first.Text, Line: line}
	if p.accept(lexer.KwFor) {
		im.Trait = first.Text
		target, err := p.expect(lexer.Ident, "impl … for")
		if err != nil {
			return nil, err
		}
		if err := p.skipGenerics(); err != nil {
			return nil, err
		}
		im.Target = target.Text
	}
	if _, err := p.expect(lexer.LBrace, "impl block"); err != nil {
		return nil, err
	}
	for {
		p.skipSemis()
		if p.accept(lexer.RBrace) {
			return im, nil
		}
		p.accept(lexer.KwPub)
		if p.cur().Kind != lexer.KwFn {
			return nil, p.errf("impl blocks hold fn declarations, found %s", p.cur().Kind)
		}
		fn, err := p.parseFn()
		if err != nil {
			return nil, err
		}
		im.Fns = append(im.Fns, fn)
	}
}

func (p *parser) parseTest() (*ast.TestDecl, error) {
	line := p.next().Line // "test" ident
	name := p.next()      // string; caller checked
	td := &ast.TestDecl{Name: strLitText(name), Line: line}
	if p.accept(lexer.LParen) {
		for p.cur().Kind != lexer.RParen {
			pn, err := p.expect(lexer.Ident, "test parameters")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.Colon, "test parameters"); err != nil {
				return nil, err
			}
			pt, err := p.parseType()
			if err != nil {
				return nil, err
			}
			td.Params = append(td.Params, ast.Param{Name: pn.Text, Type: pt})
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RParen, "test parameters"); err != nil {
			return nil, err
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	td.Body = body
	return td, nil
}

func (p *parser) parseBench() (*ast.BenchDecl, error) {
	line := p.next().Line // "bench" ident
	name := p.next()
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.BenchDecl{Name: strLitText(name), Body: body, Line: line}, nil
}

// strLitText flattens a string token that names a test/bench —
// interpolation makes no sense there, so literal parts only.
func strLitText(t lexer.Token) string {
	var sb strings.Builder
	for _, part := range t.Parts {
		if !part.IsExpr {
			sb.WriteString(part.S)
		}
	}
	return sb.String()
}

func (p *parser) parseFn() (*ast.FuncDecl, error) {
	line := p.cur().Line
	p.next() // fn
	name, err := p.expect(lexer.Ident, "function declaration")
	if err != nil {
		return nil, err
	}
	if err := p.skipGenerics(); err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LParen, "function declaration"); err != nil {
		return nil, err
	}
	selfMode := ast.NoSelf
	if p.cur().Kind == lexer.KwMut && p.peek().Kind == lexer.Ident && p.peek().Text == "self" {
		p.next()
		p.next()
		selfMode = ast.MutSelf
		p.accept(lexer.Comma)
	} else if p.cur().Kind == lexer.Ident && p.cur().Text == "self" {
		p.next()
		selfMode = ast.Self
		p.accept(lexer.Comma)
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
	return &ast.FuncDecl{Name: name.Text, Self: selfMode, Params: params, RetType: ret, Body: body, Line: line}, nil
}

// Types are kept as display strings for now (dynamically checked
// interpreter; annotations are documentation until the checker).
func (p *parser) parseType() (string, error) {
	t, err := p.parseTypeCore()
	if err != nil {
		return "", err
	}
	if p.accept(lexer.Question) {
		return t + "?", nil
	}
	return t, nil
}

func (p *parser) parseTypeCore() (string, error) {
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
	case lexer.KwBreak:
		if p.loopDepth == 0 {
			return nil, p.errf("break outside a loop")
		}
		return &ast.BreakStmt{Line: p.next().Line}, nil
	case lexer.KwContinue:
		if p.loopDepth == 0 {
			return nil, p.errf("continue outside a loop")
		}
		return &ast.ContinueStmt{Line: p.next().Line}, nil
	case lexer.KwYield:
		line := p.next().Line
		from := false
		if p.cur().Kind == lexer.Ident && p.cur().Text == "from" {
			// Contextual: `yield from expr` delegates. A variable
			// literally named "from" cannot be yielded bare.
			p.next()
			from = true
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.YieldStmt{E: e, From: from, Line: line}, nil
	}
	line := p.cur().Line
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	switch p.cur().Kind {
	case lexer.Assign, lexer.PlusEq, lexer.MinusEq,
		lexer.StarEq, lexer.SlashEq, lexer.PercentEq:
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
	case *ast.Field:
		return nil
	}
	return fmt.Errorf("invalid assignment target: assign to a name, a field, or an index")
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
		body, err := p.parseLoopBody()
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Body: body, Line: line}, nil
	}
	// Try `for <pattern> in`; backtrack to a conditional loop otherwise.
	save := p.pos
	if pat, err := p.parsePattern(); err == nil && p.cur().Kind == lexer.KwIn {
		p.next() // in
		iter, err := p.headerExpr()
		if err != nil {
			return nil, err
		}
		body, err := p.parseLoopBody()
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Pat: pat, Iter: iter, Body: body, Line: line}, nil
	}
	p.pos = save
	cond, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseLoopBody()
	if err != nil {
		return nil, err
	}
	return &ast.ForStmt{Cond: cond, Body: body, Line: line}, nil
}

func (p *parser) parseLoopBody() (*ast.Block, error) {
	p.loopDepth++
	b, err := p.parseBlock()
	p.loopDepth--
	return b, err
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
		if isCapitalized(t.Text) {
			return nil, p.errf("%q is a constructor (capitalised names match; lowercase names bind)", t.Text)
		}
		return &ast.IdentPat{Name: t.Text, Mut: true}, nil
	case lexer.Ident:
		t := p.next()
		if t.Text == "_" {
			return &ast.WildPat{}, nil
		}
		// Case is load-bearing: Circle(r) matches, point binds.
		if isCapitalized(t.Text) {
			cp := &ast.CtorPat{Name: t.Text}
			if p.accept(lexer.LParen) {
				for p.cur().Kind != lexer.RParen {
					arg, err := p.parsePattern()
					if err != nil {
						return nil, err
					}
					cp.Args = append(cp.Args, arg)
					if !p.accept(lexer.Comma) {
						break
					}
				}
				if _, err := p.expect(lexer.RParen, "constructor pattern"); err != nil {
					return nil, err
				}
			}
			return cp, nil
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
	case lexer.Int, lexer.Minus:
		lo, err := p.patInt()
		if err != nil {
			return nil, err
		}
		if p.accept(lexer.DotDot) {
			hi, err := p.patInt()
			if err != nil {
				return nil, err
			}
			return &ast.RangePat{Lo: lo, Hi: hi}, nil
		}
		return &ast.IntPat{V: lo}, nil
	case lexer.String:
		t := p.next()
		var s strings.Builder
		for _, part := range t.Parts {
			if part.IsExpr {
				return nil, p.errf("a string pattern must be a plain literal (no interpolation)")
			}
			s.WriteString(part.S)
		}
		return &ast.StrPat{V: s.String()}, nil
	case lexer.KwTrue:
		p.next()
		return &ast.BoolPat{V: true}, nil
	case lexer.KwFalse:
		p.next()
		return &ast.BoolPat{V: false}, nil
	}
	return nil, p.errf("expected a pattern, found %s", p.cur().Kind)
}

// patInt parses an optionally negated integer literal in a pattern.
func (p *parser) patInt() (int64, error) {
	neg := p.accept(lexer.Minus)
	t, err := p.expect(lexer.Int, "pattern")
	if err != nil {
		return 0, err
	}
	if neg {
		return -t.Int, nil
	}
	return t.Int, nil
}

// patternBinds reports whether a pattern introduces any binding —
// multi-pattern arms are Go-style value alternatives and must not.
func patternBinds(pt ast.Pattern) bool {
	switch q := pt.(type) {
	case *ast.IdentPat:
		return true
	case *ast.TuplePat:
		for _, el := range q.Elems {
			if patternBinds(el) {
				return true
			}
		}
	case *ast.ListPat:
		if q.Rest >= 0 && q.RestName != "_" {
			return true
		}
		for _, el := range q.Elems {
			if patternBinds(el) {
				return true
			}
		}
	case *ast.CtorPat:
		for _, el := range q.Args {
			if patternBinds(el) {
				return true
			}
		}
	}
	return false
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
			left = &ast.RangeExpr{Lo: left, Hi: right, Line: opTok.Line}
		} else {
			left = &ast.Binary{Op: opTok.Text, L: left, R: right, Line: opTok.Line}
		}
	}
}

func (p *parser) parseUnary() (ast.Expr, error) {
	switch p.cur().Kind {
	case lexer.Not, lexer.Minus:
		opTok := p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.Unary{Op: opTok.Text, X: x, Line: opTok.Line}, nil
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
			args, err := structsOK(p, func() ([]ast.Expr, error) {
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
				return args, nil
			})
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RParen, "call"); err != nil {
				return nil, err
			}
			e = &ast.Call{Fn: e, Args: args, Line: line}
		case lexer.LBrack:
			line := p.next().Line
			idx, err := structsOK(p, p.parseExpr)
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
				e = &ast.TupleIndex{X: e, N: int(p.next().Int), Line: line}
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
		lit := &ast.StrLit{Line: t.Line}
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
		// Capitalised name + `{` = struct literal, except in control
		// headers where the brace belongs to the body.
		if isCapitalized(t.Text) && p.cur().Kind == lexer.LBrace && !p.noStruct {
			return p.parseStructLit(t)
		}
		return &ast.IdentExpr{Name: t.Text, Line: t.Line}, nil
	case lexer.KwIf:
		return p.parseIf()
	case lexer.KwMatch:
		return p.parseMatch()
	case lexer.Pipe, lexer.OrOr:
		return p.parseClosure()
	case lexer.LParen:
		p.next()
		if p.accept(lexer.RParen) {
			return &ast.UnitLit{}, nil
		}
		first, err := structsOK(p, p.parseExpr)
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
	case lexer.LBrace:
		// A bare block is an expression: its tail is its value, its
		// bindings die at the }. Not in control headers — there the
		// brace belongs to the body (same rule as struct literals).
		if p.noStruct {
			break
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ast.BlockExpr{Body: body, Line: t.Line}, nil
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
	// The body is a new function: an enclosing loop is out of reach.
	outer := p.loopDepth
	p.loopDepth = 0
	defer func() { p.loopDepth = outer }()
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
	if p.accept(lexer.KwLet) {
		return p.parseIfLet(line)
	}
	cond, err := p.headerExpr()
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
			chained, ok := ei.(*ast.If)
			if !ok {
				return nil, p.errf("`else if let` is not supported yet; nest the if-let in an else block")
			}
			node.ElseIf = chained
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

// parseIfLet: `if let <pattern> = <expr> { … } [else { … }]` —
// checks the Option and unwraps in one move.
func (p *parser) parseIfLet(line int) (ast.Expr, error) {
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Assign, "if let"); err != nil {
		return nil, err
	}
	x, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	node := &ast.IfLet{Pat: pat, X: x, Then: then, Line: line}
	if p.accept(lexer.KwElse) {
		eb, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		node.ElseBlock = eb
	}
	return node, nil
}

// parseStructLit: name token consumed, cur is `{`.
func (p *parser) parseStructLit(name lexer.Token) (ast.Expr, error) {
	p.next() // {
	lit := &ast.StructLit{Type: name.Text, Line: name.Line}
	return structsOK(p, func() (ast.Expr, error) {
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				return lit, nil
			}
			if p.accept(lexer.DotDot) {
				base, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				lit.Base = base
				p.skipSemis()
				if _, err := p.expect(lexer.RBrace, "struct literal (`..base` comes last)"); err != nil {
					return nil, err
				}
				return lit, nil
			}
			fn, err := p.expect(lexer.Ident, "struct literal field")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.Colon, "struct literal field"); err != nil {
				return nil, err
			}
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			lit.Names = append(lit.Names, fn.Text)
			lit.Vals = append(lit.Vals, v)
			if !p.accept(lexer.Comma) {
				p.skipSemis()
			}
		}
	})
}

func (p *parser) parseMatch() (ast.Expr, error) {
	line := p.next().Line // match
	x, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LBrace, "match"); err != nil {
		return nil, err
	}
	m := &ast.Match{X: x, Line: line}
	return structsOK(p, func() (ast.Expr, error) {
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				if len(m.Arms) == 0 {
					return nil, p.errf("match needs at least one arm")
				}
				return m, nil
			}
			armLine := p.cur().Line
			pat, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			pats := []ast.Pattern{pat}
			for p.accept(lexer.Comma) {
				q, err := p.parsePattern()
				if err != nil {
					return nil, err
				}
				pats = append(pats, q)
			}
			if len(pats) > 1 {
				for _, q := range pats {
					if patternBinds(q) {
						return nil, p.errf("alternatives in a multi-pattern arm match values only; they cannot bind names")
					}
				}
			}
			var guard ast.Expr
			if p.accept(lexer.KwIf) {
				guard, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
			}
			if _, err := p.expect(lexer.FatArrow, "match arm"); err != nil {
				return nil, err
			}
			p.skipSemis() // body may start on the next line
			body, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			m.Arms = append(m.Arms, ast.MatchArm{Pats: pats, Guard: guard, Body: body, Line: armLine})
		}
	})
}
