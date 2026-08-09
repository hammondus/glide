// Package parser builds the AST via recursive descent with a small
// Pratt expression core. The parser is the grammar for now — EBNF
// gets extracted from it later, not written ahead of it.
package parser

import (
	"errors"
	"fmt"
	"strings"

	"glide/internal/ast"
	"glide/internal/lexer"
	"glide/internal/source"
)

type parser struct {
	toks []lexer.Token
	pos  int
	file *source.File // nil when parsing an interpolation segment
	// noStruct disables struct literals while parsing control-flow
	// headers (`if c == Red {` must not read `Red {` as a literal —
	// Rust's rule). Parens and argument lists re-enable them.
	noStruct bool
	// loopDepth counts enclosing `for` bodies, so `break`/`continue`
	// outside a loop is a parse error. Closure bodies reset it: a
	// closure is its own function, and a break inside one cannot
	// target the loop it happens to be written in.
	loopDepth int
	// loopLabels stacks the labels of enclosing loops ("" for
	// unlabeled), validated by `break label`. Reset with loopDepth.
	loopLabels []string
}

// ParseFile parses src as a complete file. name appears in
// diagnostics and is otherwise unused; the resulting ast.File carries
// the source.File that every node's Span indexes into.
func ParseFile(name, src string) (*ast.File, error) {
	sf := source.NewFile(name, src)
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, wrap(sf, err)
	}
	p := &parser{toks: toks, file: sf}
	f, err := p.parseFile()
	if err != nil {
		return nil, wrap(sf, err)
	}
	f.Source = sf
	return f, nil
}

// wrap attaches a source file to a positioned diagnostic so it can
// render itself with a caret. Errors that carry no position (or are
// already wrapped) pass through untouched.
func wrap(sf *source.File, err error) error {
	var d source.Diagnostic
	if errors.As(err, &d) {
		return &source.Error{File: sf, Diags: []source.Diagnostic{d}}
	}
	return err
}

// parseExprSrc parses an interpolation segment. pos is the segment's
// byte offset in the enclosing file, so the tokens — and every node
// built from them — carry file coordinates rather than offsets into a
// snippet that exists nowhere on disk.
func parseExprSrc(src string, pos, line int) (ast.Expr, error) {
	toks, err := lexer.LexAt(src, pos, line)
	if err != nil {
		return nil, inInterp(src, err)
	}
	p := &parser{toks: toks}
	e, err := p.parseExpr()
	if err != nil {
		return nil, inInterp(src, err)
	}
	p.skipSemis()
	if p.cur().Kind != lexer.EOF {
		return nil, inInterp(src, p.errf("trailing input"))
	}
	return e, nil
}

// inInterp adds "which interpolation" context while *keeping the
// span*. Wrapping with fmt.Errorf would flatten the diagnostic to a
// string and lose the position, which is the whole point of the
// segment being lexed in file coordinates.
func inInterp(src string, err error) error {
	var d source.Diagnostic
	if errors.As(err, &d) {
		d.Msg = fmt.Sprintf("in interpolation {%s}: %s", src, d.Msg)
		return d
	}
	return fmt.Errorf("in interpolation {%s}: %w", src, err)
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

// errf reports at the current token. That is the right place for
// nearly every parse error: the parser fails on the token it could not
// use, which is exactly what the reader needs to see underlined.
func (p *parser) errf(format string, args ...any) error {
	return p.errAt(p.cur().Span, format, args...)
}

func (p *parser) errAt(sp source.Span, format string, args ...any) error {
	return source.Diagnostic{Span: sp, Msg: fmt.Sprintf(format, args...)}
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
			f.Imports = append(f.Imports, ast.Import{Span: t.Span, Name: t.Text})
		case lexer.KwFn:
			fn, err := p.parseFn(false)
			if err != nil {
				return nil, err
			}
			f.Funcs = append(f.Funcs, fn)
		case lexer.KwTrait:
			tr, err := p.parseTrait()
			if err != nil {
				return nil, err
			}
			f.Traits = append(f.Traits, tr)
		case lexer.KwConst:
			at := p.next().Span
			name, err := p.expect(lexer.Ident, "const declaration")
			if err != nil {
				return nil, err
			}
			if isCapitalized(name.Text) {
				return nil, p.errf("const names are lowercase bindings: %q (capitalised names are types/variants)", name.Text)
			}
			if _, err := p.expect(lexer.Assign, "const declaration"); err != nil {
				return nil, err
			}
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			f.Consts = append(f.Consts, &ast.ConstDecl{Name: name.Text, E: e, Span: at})
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

// parseTypeParams parses a declaration-site list: `<T>`, `<A, B>`,
// `<T: Ord + Hash>`. Returns nil when there is no `<`.
//
// Note there is no ambiguity to resolve here. DESIGN.md's `f<T>(x)`
// vs `(f < T) > (x)` problem is a *use*-site problem; a declaration's
// `<` always follows the declared name, so this is a plain parse.
func (p *parser) parseTypeParams() ([]ast.TypeParam, error) {
	if !p.accept(lexer.Lt) {
		return nil, nil
	}
	var params []ast.TypeParam
	for {
		name, err := p.expect(lexer.Ident, "type parameter list")
		if err != nil {
			return nil, err
		}
		if !isCapitalized(name.Text) {
			return nil, p.errf("type parameter names are capitalised: %q", name.Text)
		}
		tp := ast.TypeParam{Name: name.Text, Span: name.Span}
		if p.accept(lexer.Colon) {
			// Inline colon bounds only, `T: Ord + Hash`. No `where`
			// clause in v0 — two ways to write bounds is a house-rule
			// violation (DESIGN.md, Generics syntax).
			for {
				b, err := p.parseType()
				if err != nil {
					return nil, err
				}
				tp.Bounds = append(tp.Bounds, b)
				if !p.accept(lexer.Plus) {
					break
				}
			}
		}
		params = append(params, tp)
		if !p.accept(lexer.Comma) {
			break
		}
	}
	if _, err := p.expect(lexer.Gt, "type parameter list"); err != nil {
		return nil, err
	}
	return params, nil
}

// parseTypeArgs parses a use-site list: `<Int>`, `<T>`, `<A, B>`.
// Returns nil when there is no `<`.
func (p *parser) parseTypeArgs() ([]*ast.TypeExpr, error) {
	if !p.accept(lexer.Lt) {
		return nil, nil
	}
	var args []*ast.TypeExpr
	for {
		t, err := p.parseType()
		if err != nil {
			return nil, err
		}
		args = append(args, t)
		if !p.accept(lexer.Comma) {
			break
		}
	}
	if _, err := p.expect(lexer.Gt, "type arguments"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parseTypeDecl() (*ast.TypeDecl, error) {
	at := p.next().Span // type
	name, err := p.expect(lexer.Ident, "type declaration")
	if err != nil {
		return nil, err
	}
	if !isCapitalized(name.Text) {
		return nil, p.errf("type names are capitalised: %q", name.Text)
	}
	tps, err := p.parseTypeParams()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.Assign, "type declaration"); err != nil {
		return nil, err
	}
	td := &ast.TypeDecl{Name: name.Text, TypeParams: tps, Span: at.To(name.Span)}
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
		// `type NoteId = distinct Int`: a nominal wrapper — explicit
		// construction, no implicit conversion, no inherited operators.
		p.next()
		base, err := p.parseType()
		if err != nil {
			return nil, err
		}
		td.Distinct = base
		return td, nil
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
		if p.cur().Kind == lexer.LBrace {
			// Named-field variant: NotFound{ id: Int }
			p.next()
			for {
				p.skipSemis()
				if p.accept(lexer.RBrace) {
					break
				}
				fn, err := p.expect(lexer.Ident, "variant field")
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(lexer.Colon, "variant field"); err != nil {
					return nil, err
				}
				ft, err := p.parseType()
				if err != nil {
					return nil, err
				}
				v.Fields = append(v.Fields, ast.FieldDecl{Name: fn.Text, Type: ft})
				if !p.accept(lexer.Comma) {
					p.skipSemis()
				}
			}
			if len(v.Fields) == 0 {
				return nil, p.errf("named-field variant %s{} needs at least one field (a bare variant has no braces)", vn.Text)
			}
		} else if p.accept(lexer.LParen) {
			for p.cur().Kind != lexer.RParen {
				pt, err := p.parseType()
				if err != nil {
					return nil, err
				}
				v.Payload = append(v.Payload, pt)
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
	at := p.next().Span // impl
	first, err := p.expect(lexer.Ident, "impl")
	if err != nil {
		return nil, err
	}
	// The first `<...>` belongs to whichever role `first` turns out
	// to have, which `for` decides: `impl Tree<T>` names a target,
	// `impl Iterable<T> for Tree<T>` names a trait.
	firstArgs, err := p.parseTypeArgs()
	if err != nil {
		return nil, err
	}
	im := &ast.ImplBlock{Target: first.Text, TargetArgs: firstArgs, Span: at.To(first.Span)}
	if p.accept(lexer.KwFor) {
		im.Trait, im.TraitArgs = first.Text, firstArgs
		target, err := p.expect(lexer.Ident, "impl … for")
		if err != nil {
			return nil, err
		}
		im.TargetArgs, err = p.parseTypeArgs()
		if err != nil {
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
		fn, err := p.parseFn(false)
		if err != nil {
			return nil, err
		}
		im.Fns = append(im.Fns, fn)
	}
}

// parseTrait parses `trait Name { fn sig(self) -> T  fn dflt(self) { … } }`.
// Bodies are default methods; body-less fns are required signatures.
func (p *parser) parseTrait() (*ast.TraitDecl, error) {
	at := p.next().Span // trait
	name, err := p.expect(lexer.Ident, "trait declaration")
	if err != nil {
		return nil, err
	}
	if !isCapitalized(name.Text) {
		return nil, p.errf("trait names are capitalised: %q", name.Text)
	}
	tps, err := p.parseTypeParams()
	if err != nil {
		return nil, err
	}
	tr := &ast.TraitDecl{Name: name.Text, TypeParams: tps, Span: at.To(name.Span)}
	if _, err := p.expect(lexer.LBrace, "trait body"); err != nil {
		return nil, err
	}
	for {
		p.skipSemis()
		if p.accept(lexer.RBrace) {
			return tr, nil
		}
		if p.cur().Kind != lexer.KwFn {
			return nil, p.errf("trait bodies hold fn declarations, found %s", p.cur().Kind)
		}
		fn, err := p.parseFn(true)
		if err != nil {
			return nil, err
		}
		if fn.Self == ast.NoSelf {
			return nil, p.errf("trait method %q needs a self receiver", fn.Name)
		}
		tr.Fns = append(tr.Fns, fn)
	}
}

func (p *parser) parseTest() (*ast.TestDecl, error) {
	at := p.next().Span // "test" ident
	name := p.next()    // string; caller checked
	td := &ast.TestDecl{Name: strLitText(name), Span: at}
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
	at := p.next().Span // "bench" ident
	name := p.next()
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.BenchDecl{Name: strLitText(name), Body: body, Span: at.To(name.Span)}, nil
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

// parseFn parses a fn declaration; bodyOptional permits the
// body-less required-method form inside trait blocks.
func (p *parser) parseFn(bodyOptional bool) (*ast.FuncDecl, error) {
	at := p.cur().Span
	p.next() // fn
	name, err := p.expect(lexer.Ident, "function declaration")
	if err != nil {
		return nil, err
	}
	tps, err := p.parseTypeParams()
	if err != nil {
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
		prm := ast.Param{Name: pn.Text, Type: pt}
		if p.accept(lexer.Assign) {
			prm.Default, err = p.parseExpr()
			if err != nil {
				return nil, err
			}
		}
		params = append(params, prm)
		if !p.accept(lexer.Comma) {
			break
		}
	}
	if _, err := p.expect(lexer.RParen, "function declaration"); err != nil {
		return nil, err
	}
	var ret *ast.TypeExpr
	if p.accept(lexer.Arrow) {
		ret, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	var body *ast.Block
	if !bodyOptional || p.cur().Kind == lexer.LBrace {
		body, err = p.parseBlock()
		if err != nil {
			return nil, err
		}
	}
	return &ast.FuncDecl{Name: name.Text, TypeParams: tps, Self: selfMode, Params: params, RetType: ret, Body: body, Span: at.To(name.Span)}, nil
}

// parseType builds a real ast.TypeExpr. The `?` suffix binds loosest,
// so `List<Int>?` is an optional list and `List<Int?>` is a list of
// optionals — the recursion through parseTypeCore's argument list is
// what keeps those apart.
func (p *parser) parseType() (*ast.TypeExpr, error) {
	t, err := p.parseTypeCore()
	if err != nil {
		return nil, err
	}
	if p.accept(lexer.Question) {
		t.Optional = true
	}
	return t, nil
}

func (p *parser) parseTypeCore() (*ast.TypeExpr, error) {
	at := p.cur().Span
	switch p.cur().Kind {
	case lexer.Ident:
		t := &ast.TypeExpr{Kind: ast.TypeName, Name: p.next().Text, Span: at}
		if p.accept(lexer.Lt) {
			for p.cur().Kind != lexer.Gt {
				arg, err := p.parseType()
				if err != nil {
					return nil, err
				}
				t.Args = append(t.Args, arg)
				if !p.accept(lexer.Comma) {
					break
				}
			}
			if _, err := p.expect(lexer.Gt, "type arguments"); err != nil {
				return nil, err
			}
		}
		return t, nil
	case lexer.LParen:
		p.next()
		if p.accept(lexer.RParen) {
			return &ast.TypeExpr{Kind: ast.TypeUnit, Span: at}, nil
		}
		t := &ast.TypeExpr{Kind: ast.TypeTuple, Span: at}
		for {
			elem, err := p.parseType()
			if err != nil {
				return nil, err
			}
			t.Elems = append(t.Elems, elem)
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RParen, "tuple type"); err != nil {
			return nil, err
		}
		// The grammar never said what `(T)` means — a 1-tuple, or a
		// parenthesised type? M1-M3 quietly produced the string
		// "(T)", which is a 1-tuple nobody can construct. Reject it
		// rather than pick: an unconstructable Elems of length 1 is
		// exactly the kind of hole that surfaces as a baffling
		// checker bug later. Nothing in the corpus writes it.
		if len(t.Elems) == 1 {
			return nil, p.errf("(%s) is not a type: write %s for the type itself, or add a second member for a tuple",
				t.Elems[0].String(), t.Elems[0].String())
		}
		return t, nil
	}
	return nil, p.errf("expected a type, found %s", p.cur().Kind)
}

// Statements

func (p *parser) parseBlock() (*ast.Block, error) {
	open, err := p.expect(lexer.LBrace, "block")
	if err != nil {
		return nil, err
	}
	b := &ast.Block{Span: open.Span}
	for {
		p.skipSemis()
		if p.cur().Kind == lexer.RBrace {
			b.Span = open.Span.To(p.next().Span)
			return b, nil
		}
		if p.cur().Kind == lexer.EOF {
			return nil, p.errf("unexpected end of file: unclosed block")
		}
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		switch s.(type) {
		case *ast.DeferStmt:
			b.HasDefer = true
		case *ast.FnStmt:
			b.HasFns = true
		}
		b.Stmts = append(b.Stmts, s)
	}
}

func (p *parser) parseStmt() (ast.Stmt, error) {
	switch p.cur().Kind {
	case lexer.KwLet:
		return p.parseLet()
	case lexer.KwFor:
		return p.parseFor("")
	case lexer.Ident:
		// `search: for … { … }` — a label names the loop it prefixes;
		// labels attach to loops only, nothing else.
		if p.peek().Kind == lexer.Colon && p.pos+2 < len(p.toks) &&
			p.toks[p.pos+2].Kind == lexer.KwFor && !isCapitalized(p.cur().Text) {
			label := p.next().Text
			p.next() // :
			for _, l := range p.loopLabels {
				if l == label {
					return nil, p.errf("label %q already names an enclosing loop", label)
				}
			}
			return p.parseFor(label)
		}
	case lexer.KwReturn:
		at := p.next().Span
		if p.cur().Kind == lexer.Semi || p.cur().Kind == lexer.RBrace {
			return &ast.ReturnStmt{Span: at}, nil
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.ReturnStmt{E: e, Span: at}, nil
	case lexer.KwImport:
		return nil, p.errf("imports are only allowed at the top of the file")
	case lexer.KwFn:
		// Nested fn: its own function — reset loop context.
		outer, outerLabels := p.loopDepth, p.loopLabels
		p.loopDepth, p.loopLabels = 0, nil
		fn, err := p.parseFn(false)
		p.loopDepth, p.loopLabels = outer, outerLabels
		if err != nil {
			return nil, err
		}
		return &ast.FnStmt{Decl: fn}, nil
	case lexer.KwDefer, lexer.KwErrdefer:
		t := p.next()
		// The body runs at scope exit, outside loop flow — an
		// enclosing loop is out of reach, like a closure body.
		outer, outerLabels := p.loopDepth, p.loopLabels
		p.loopDepth, p.loopLabels = 0, nil
		body, err := p.parseBlock()
		p.loopDepth, p.loopLabels = outer, outerLabels
		if err != nil {
			return nil, err
		}
		return &ast.DeferStmt{Body: body, Err: t.Kind == lexer.KwErrdefer, Span: t.Span}, nil
	case lexer.KwBreak:
		if p.loopDepth == 0 {
			return nil, p.errf("break outside a loop")
		}
		at := p.next().Span
		label, err := p.loopLabel()
		if err != nil {
			return nil, err
		}
		return &ast.BreakStmt{Label: label, Span: at}, nil
	case lexer.KwContinue:
		if p.loopDepth == 0 {
			return nil, p.errf("continue outside a loop")
		}
		at := p.next().Span
		label, err := p.loopLabel()
		if err != nil {
			return nil, err
		}
		return &ast.ContinueStmt{Label: label, Span: at}, nil
	case lexer.KwYield:
		at := p.next().Span
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
		return &ast.YieldStmt{E: e, From: from, Span: at}, nil
	}
	at := p.cur().Span
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	switch p.cur().Kind {
	case lexer.Assign, lexer.PlusEq, lexer.MinusEq,
		lexer.StarEq, lexer.SlashEq, lexer.PercentEq:
		op := p.next().Text
		if err := validAssignTarget(e, op); err != nil {
			return nil, p.errAt(at, "%v", err)
		}
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{Target: e, Op: op, Value: v, Span: at}, nil
	}
	return &ast.ExprStmt{E: e, Span: at}, nil
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
	at := p.next().Span // let
	pat, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	var typ *ast.TypeExpr
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
	return &ast.LetStmt{Pat: pat, Type: typ, Init: init, Else: elseB, Span: at}, nil
}

func (p *parser) parseFor(label string) (ast.Stmt, error) {
	at := p.next().Span // for
	if p.cur().Kind == lexer.LBrace {
		body, err := p.parseLoopBody(label)
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Body: body, Label: label, Span: at}, nil
	}
	// Try `for <pattern> in`; backtrack to a conditional loop otherwise.
	save := p.pos
	if pat, err := p.parsePattern(); err == nil && p.cur().Kind == lexer.KwIn {
		p.next() // in
		iter, err := p.headerExpr()
		if err != nil {
			return nil, err
		}
		body, err := p.parseLoopBody(label)
		if err != nil {
			return nil, err
		}
		return &ast.ForStmt{Pat: pat, Iter: iter, Body: body, Label: label, Span: at}, nil
	}
	p.pos = save
	cond, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseLoopBody(label)
	if err != nil {
		return nil, err
	}
	return &ast.ForStmt{Cond: cond, Body: body, Label: label, Span: at}, nil
}

func (p *parser) parseLoopBody(label string) (*ast.Block, error) {
	p.loopDepth++
	p.loopLabels = append(p.loopLabels, label)
	b, err := p.parseBlock()
	p.loopLabels = p.loopLabels[:len(p.loopLabels)-1]
	p.loopDepth--
	return b, err
}

// loopLabel parses the optional label after break/continue and
// checks it names an enclosing loop.
func (p *parser) loopLabel() (string, error) {
	if p.cur().Kind != lexer.Ident || isCapitalized(p.cur().Text) {
		return "", nil
	}
	label := p.next().Text
	for _, l := range p.loopLabels {
		if l == label {
			return label, nil
		}
	}
	return "", p.errf("no enclosing loop is labeled %q", label)
}

// Patterns

// parsePattern parses one pattern and stamps its span. Spans are
// attached here, once, rather than in each of the twelve pattern
// constructors: a pattern always spans exactly the tokens it
// consumed, so the wrapper knows the answer and the cases do not have
// to remember to say it.
func (p *parser) parsePattern() (ast.Pattern, error) {
	start := p.cur().Span
	pat, err := p.parsePatternCore()
	if err != nil {
		return nil, err
	}
	end := start
	if p.pos > 0 {
		end = p.toks[p.pos-1].Span
	}
	setPatSpan(pat, start.To(end))
	return pat, nil
}

func setPatSpan(pat ast.Pattern, sp source.Span) {
	switch x := pat.(type) {
	case *ast.IdentPat:
		x.Span = sp
	case *ast.WildPat:
		x.Span = sp
	case *ast.TuplePat:
		x.Span = sp
	case *ast.ListPat:
		x.Span = sp
	case *ast.CtorPat:
		x.Span = sp
	case *ast.StructPat:
		x.Span = sp
	case *ast.IntPat:
		x.Span = sp
	case *ast.StrPat:
		x.Span = sp
	case *ast.BoolPat:
		x.Span = sp
	case *ast.RangePat:
		x.Span = sp
	case *ast.RunePat:
		x.Span = sp
	case *ast.RuneRangePat:
		x.Span = sp
	}
}

func (p *parser) parsePatternCore() (ast.Pattern, error) {
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
			return p.parseCtorTail(t)
		}
		return &ast.IdentPat{Name: t.Text}, nil
	case lexer.Dot:
		// `.Variant` pattern — same shorthand as in expressions.
		p.next()
		t, err := p.expect(lexer.Ident, "pattern")
		if err != nil {
			return nil, err
		}
		if !isCapitalized(t.Text) {
			return nil, p.errf("`.%s`: dot shorthand names a variant (capitalised)", t.Text)
		}
		return p.parseCtorTail(t)
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
		if p.cur().Kind == lexer.DotDot || p.cur().Kind == lexer.DotDotEq {
			incl := p.next().Kind == lexer.DotDotEq
			hi, err := p.patInt()
			if err != nil {
				return nil, err
			}
			return &ast.RangePat{Lo: lo, Hi: hi, Incl: incl}, nil
		}
		return &ast.IntPat{V: lo}, nil
	case lexer.Rune:
		t := p.next()
		if p.cur().Kind == lexer.DotDot || p.cur().Kind == lexer.DotDotEq {
			incl := p.next().Kind == lexer.DotDotEq
			hi, err := p.expect(lexer.Rune, "rune range pattern")
			if err != nil {
				return nil, err
			}
			return &ast.RuneRangePat{Lo: rune(t.Num), Hi: rune(hi.Num), Incl: incl}, nil
		}
		return &ast.RunePat{V: rune(t.Num)}, nil
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

// parseCtorTail finishes a constructor pattern whose (capitalised)
// name token is consumed: `Name`, `Name(pats…)`, or `Name{ fields }`.
func (p *parser) parseCtorTail(t lexer.Token) (ast.Pattern, error) {
	if p.cur().Kind == lexer.LBrace {
		return p.parseStructPat(t.Text, t.Span)
	}
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

// parseStructPat parses `Type{ field, other: pat, .. }` (the `{` is
// current). `..` must come last; `mut name` is binding shorthand.
func (p *parser) parseStructPat(typeName string, at source.Span) (ast.Pattern, error) {
	p.next() // {
	sp := &ast.StructPat{Type: typeName, Span: at}
	seen := map[string]bool{}
	for {
		p.skipSemis()
		if p.accept(lexer.RBrace) {
			return sp, nil
		}
		if p.cur().Kind == lexer.DotDot {
			p.next()
			sp.Rest = true
			p.skipSemis()
			p.accept(lexer.Comma)
			p.skipSemis()
			if _, err := p.expect(lexer.RBrace, "struct pattern (`..` must be last)"); err != nil {
				return nil, err
			}
			return sp, nil
		}
		mut := p.accept(lexer.KwMut)
		t, err := p.expect(lexer.Ident, "struct pattern field")
		if err != nil {
			return nil, err
		}
		if seen[t.Text] {
			return nil, p.errf("field %q appears twice in struct pattern", t.Text)
		}
		seen[t.Text] = true
		var fp ast.Pattern = &ast.IdentPat{Name: t.Text, Mut: mut}
		if !mut && p.accept(lexer.Colon) {
			fp, err = p.parsePattern()
			if err != nil {
				return nil, err
			}
		}
		sp.Fields = append(sp.Fields, ast.FieldPat{Name: t.Text, Pat: fp, Span: t.Span})
		if !p.accept(lexer.Comma) {
			p.skipSemis()
			if _, err := p.expect(lexer.RBrace, "struct pattern"); err != nil {
				return nil, err
			}
			return sp, nil
		}
	}
}

// patInt parses an optionally negated integer literal in a pattern.
func (p *parser) patInt() (int64, error) {
	neg := p.accept(lexer.Minus)
	t, err := p.expect(lexer.Int, "pattern")
	if err != nil {
		return 0, err
	}
	// Patterns match a *value*, so the magnitude has to fit an Int
	// here. `-9223372036854775808` is the one case where the negated
	// magnitude fits and the bare one does not, which is why this
	// tests the two signs separately rather than converting first.
	if neg {
		if t.Num > 1<<63 {
			return 0, p.errAt(t.Span, "%s is out of range for an Int pattern", t.Text)
		}
		return -int64(t.Num), nil
	}
	if t.Num > 1<<63-1 {
		return 0, p.errAt(t.Span, "%s is out of range for an Int pattern", t.Text)
	}
	return int64(t.Num), nil
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
	case *ast.StructPat:
		for _, f := range q.Fields {
			if patternBinds(f.Pat) {
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
	case lexer.DotDot, lexer.DotDotEq:
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
		if k == lexer.DotDot || k == lexer.DotDotEq {
			left = &ast.RangeExpr{Lo: left, Hi: right, Incl: k == lexer.DotDotEq, Span: opTok.Span}
		} else {
			left = &ast.Binary{Op: opTok.Text, L: left, R: right, Span: opTok.Span}
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
		// The span covers the operand too: a diagnostic about `-129`
		// should underline `-129`, not just the minus.
		return &ast.Unary{Op: opTok.Text, X: x, Span: opTok.Span.To(exprSpan(x))}, nil
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
			at := p.next().Span
			call := &ast.Call{Fn: e, Span: at}
			_, err := structsOK(p, func() (ast.Expr, error) {
				seenNamed := false
				used := map[string]bool{}
				for p.cur().Kind != lexer.RParen {
					nm := ""
					// `ident:` is a named argument (params are
					// lowercase; a capitalised ident:… would be a
					// variant, which cannot be a name here).
					if p.cur().Kind == lexer.Ident && p.peek().Kind == lexer.Colon && !isCapitalized(p.cur().Text) {
						nm = p.next().Text
						p.next() // :
						if used[nm] {
							return nil, p.errf("argument %q named twice", nm)
						}
						used[nm] = true
						seenNamed = true
					} else if seenNamed {
						return nil, p.errf("positional arguments go before named ones")
					}
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					call.Args = append(call.Args, a)
					call.Names = append(call.Names, nm)
					if !p.accept(lexer.Comma) {
						break
					}
				}
				if !seenNamed {
					call.Names = nil
				}
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RParen, "call"); err != nil {
				return nil, err
			}
			e = call
		case lexer.LBrack:
			at := p.next().Span
			idx, err := structsOK(p, p.parseExpr)
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.RBrack, "index"); err != nil {
				return nil, err
			}
			e = &ast.Index{X: e, I: idx, Span: at}
		case lexer.Dot:
			at := p.next().Span
			switch p.cur().Kind {
			case lexer.Ident:
				e = &ast.Field{X: e, Name: p.next().Text, Span: at}
			case lexer.Int:
				e = &ast.TupleIndex{X: e, N: int(p.next().Num), Span: at}
			default:
				return nil, p.errf("expected a name or tuple index after '.', found %s", p.cur().Kind)
			}
		case lexer.Question:
			at := p.next().Span
			e = &ast.Try{X: e, Span: at}
		default:
			return e, nil
		}
	}
}

// exprSpan is any expression's position. Every node embeds a
// source.Span, so this is one assertion rather than a switch over the
// whole expression set.
func exprSpan(e ast.Expr) source.Span {
	if s, ok := e.(interface{ Position() source.Span }); ok {
		return s.Position()
	}
	return source.Span{}
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	t := p.cur()
	switch t.Kind {
	case lexer.Int:
		p.next()
		return &ast.IntLit{V: t.Num, Span: t.Span}, nil
	case lexer.Float:
		p.next()
		return &ast.FloatLit{V: t.Float, Span: t.Span}, nil
	case lexer.Rune:
		p.next()
		return &ast.RuneLit{V: rune(t.Num), Span: t.Span}, nil
	case lexer.KwTrue:
		p.next()
		return &ast.BoolLit{V: true, Span: t.Span}, nil
	case lexer.KwFalse:
		p.next()
		return &ast.BoolLit{V: false, Span: t.Span}, nil
	case lexer.String:
		p.next()
		lit := &ast.StrLit{Span: t.Span}
		for _, part := range t.Parts {
			if part.IsExpr {
				e, err := parseExprSrc(part.S, part.Pos, part.Line)
				if err != nil {
					return nil, err
				}
				lit.Parts = append(lit.Parts, ast.StrPart{IsExpr: true, E: e, Spec: part.Spec, Span: source.Span{Pos: part.Pos, End: part.Pos + len(part.S)}})
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
		return &ast.IdentExpr{Name: t.Text, Span: t.Span}, nil
	case lexer.Dot:
		// `.Variant` — Swift's dot shorthand (variants only; fields
		// and methods are lowercase and need a value on the left).
		at := p.next().Span
		t, err := p.expect(lexer.Ident, "dot shorthand")
		if err != nil {
			return nil, err
		}
		if !isCapitalized(t.Text) {
			return nil, p.errf("`.%s`: dot shorthand names a variant (capitalised)", t.Text)
		}
		if p.cur().Kind == lexer.LBrace && !p.noStruct {
			return p.parseStructLit(t)
		}
		return &ast.DotName{Name: t.Text, Span: at}, nil
	case lexer.KwIf:
		return p.parseIf()
	case lexer.KwMatch:
		return p.parseMatch()
	case lexer.KwScope:
		return p.parseScope()
	case lexer.KwSelect:
		return p.parseSelect()
	case lexer.Pipe, lexer.OrOr:
		return p.parseClosure()
	case lexer.LParen:
		open := p.next().Span
		if p.cur().Kind == lexer.RParen {
			return &ast.UnitLit{Span: open.To(p.next().Span)}, nil
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
			end, err := p.expect(lexer.RParen, "tuple")
			if err != nil {
				return nil, err
			}
			tl.Span = open.To(end.Span)
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
		return &ast.BlockExpr{Body: body, Span: t.Span}, nil
	}
	return nil, p.errf("expected an expression, found %s", t.Kind)
}

// parseScope: `scope [(key: expr, …)] [handle] { body }`. Config keys
// are timeout and deadline; the handle is needed only to spawn. The
// body is parsed in the enclosing loop context — break/continue/
// return leave the scope legally (the exit path cancels children).
func (p *parser) parseScope() (ast.Expr, error) {
	at := p.next().Span // scope
	sc := &ast.ScopeExpr{Span: at}
	if p.accept(lexer.LParen) {
		for p.cur().Kind != lexer.RParen {
			t, err := p.expect(lexer.Ident, "scope config")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.Colon, "scope config"); err != nil {
				return nil, err
			}
			e, err := structsOK(p, p.parseExpr)
			if err != nil {
				return nil, err
			}
			switch t.Text {
			case "timeout":
				if sc.Timeout != nil {
					return nil, p.errf("scope config %q given twice", t.Text)
				}
				sc.Timeout = e
			case "deadline":
				if sc.Deadline != nil {
					return nil, p.errf("scope config %q given twice", t.Text)
				}
				sc.Deadline = e
			default:
				return nil, p.errf("unknown scope config %q (timeout and deadline exist)", t.Text)
			}
			if !p.accept(lexer.Comma) {
				break
			}
		}
		if _, err := p.expect(lexer.RParen, "scope config"); err != nil {
			return nil, err
		}
	}
	if p.cur().Kind == lexer.Ident {
		if isCapitalized(p.cur().Text) {
			return nil, p.errf("a scope handle is a binding (lowercase), found %q", p.cur().Text)
		}
		sc.Handle = p.next().Text
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	sc.Body = body
	return sc, nil
}

// parseSelect: match's clothes on Go's engine. Arms, line-separated:
//
//	pat = rx.recv() [if guard] => expr
//	tx.send(v)      [if guard] => expr
//	else                       => expr
//
// The op must literally be a .recv()/.send(v) call — select waits on
// channel operations, not arbitrary expressions.
func (p *parser) parseSelect() (ast.Expr, error) {
	at := p.next().Span // select
	if _, err := p.expect(lexer.LBrace, "select"); err != nil {
		return nil, err
	}
	sel := &ast.SelectExpr{Span: at}
	seenElse := false
	return structsOK(p, func() (ast.Expr, error) {
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				if len(sel.Arms) == 0 {
					return nil, p.errf("select needs at least one arm (a zero-arm select would block forever)")
				}
				return sel, nil
			}
			armAt := p.cur().Span
			arm := ast.SelectArm{Span: armAt}
			if p.accept(lexer.KwElse) {
				if seenElse {
					return nil, p.errf("select has two else arms")
				}
				seenElse = true
				arm.Else = true
			} else {
				// A pattern followed by `=` marks a recv arm; anything
				// else re-parses as the operation expression (send).
				save := p.pos
				if q, err := p.parsePattern(); err == nil && p.cur().Kind == lexer.Assign {
					p.next() // =
					arm.Pat = q
				} else {
					p.pos = save
				}
				op, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				call, ok := op.(*ast.Call)
				var fld *ast.Field
				if ok {
					fld, ok = call.Fn.(*ast.Field)
				}
				if !ok {
					return nil, p.errf("a select arm waits on rx.recv() or tx.send(v)")
				}
				switch fld.Name {
				case "recv":
					if arm.Pat == nil {
						return nil, p.errf("a recv arm binds its value: `pat = rx.recv() => …`")
					}
					if len(call.Args) != 0 {
						return nil, p.errf("recv takes no arguments")
					}
					arm.Ch = fld.X
				case "send":
					if arm.Pat != nil {
						return nil, p.errf("send returns nothing; a send arm has no pattern")
					}
					if len(call.Args) != 1 {
						return nil, p.errf("send takes exactly one value")
					}
					arm.Ch, arm.SendVal = fld.X, call.Args[0]
				default:
					return nil, p.errf("a select arm waits on rx.recv() or tx.send(v), not %q", fld.Name)
				}
				if p.accept(lexer.KwIf) {
					arm.Guard, err = p.parseExpr()
					if err != nil {
						return nil, err
					}
				}
			}
			if _, err := p.expect(lexer.FatArrow, "select arm"); err != nil {
				return nil, err
			}
			p.skipSemis() // body may start on the next line
			body, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			arm.Body = body
			sel.Arms = append(sel.Arms, arm)
		}
	})
}

func (p *parser) parseListOrMap() (ast.Expr, error) {
	open := p.next().Span // [
	if p.cur().Kind == lexer.Colon {
		p.next()
		end, err := p.expect(lexer.RBrack, "empty map literal")
		if err != nil {
			return nil, err
		}
		return &ast.MapLit{Span: open.To(end.Span)}, nil
	}
	if p.cur().Kind == lexer.RBrack {
		return &ast.ListLit{Span: open.To(p.next().Span)}, nil
	}
	if p.cur().Kind == lexer.DotDot {
		// Leading spread: this is a list, no map ambiguity.
		el, err := p.listElem()
		if err != nil {
			return nil, err
		}
		return p.finishListLit(open, el)
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
		end, err := p.expect(lexer.RBrack, "map literal")
		if err != nil {
			return nil, err
		}
		m.Span = open.To(end.Span)
		return m, nil
	}
	return p.finishListLit(open, first)
}

func (p *parser) finishListLit(open source.Span, first ast.Expr) (ast.Expr, error) {
	l := &ast.ListLit{Elems: []ast.Expr{first}}
	for p.accept(lexer.Comma) {
		if p.cur().Kind == lexer.RBrack {
			break
		}
		e, err := p.listElem()
		if err != nil {
			return nil, err
		}
		l.Elems = append(l.Elems, e)
	}
	end, err := p.expect(lexer.RBrack, "list literal")
	if err != nil {
		return nil, err
	}
	l.Span = open.To(end.Span)
	return l, nil
}

// listElem parses one list-literal element: an expression, or a
// `..xs` spread of any iterable.
func (p *parser) listElem() (ast.Expr, error) {
	if p.cur().Kind == lexer.DotDot {
		at := p.next().Span
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &ast.Spread{E: e, Span: at}, nil
	}
	return p.parseExpr()
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
	outer, outerLabels := p.loopDepth, p.loopLabels
	p.loopDepth, p.loopLabels = 0, nil
	defer func() { p.loopDepth, p.loopLabels = outer, outerLabels }()
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
	at := p.next().Span // if
	if p.accept(lexer.KwLet) {
		return p.parseIfLet(at)
	}
	cond, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	node := &ast.If{Cond: cond, Then: then, Span: at}
	node.ElseIf, node.ElseBlock, err = p.parseElse()
	if err != nil {
		return nil, err
	}
	return node, nil
}

// parseElse handles the tail of any if-form: nothing, `else if …`
// (plain or `if let`, chained), or a final `else { }`.
func (p *parser) parseElse() (ast.Expr, *ast.Block, error) {
	if !p.accept(lexer.KwElse) {
		return nil, nil, nil
	}
	if p.cur().Kind == lexer.KwIf {
		ei, err := p.parseIf()
		return ei, nil, err
	}
	eb, err := p.parseBlock()
	return nil, eb, err
}

// parseIfLet: `if let <pattern> = <expr> { … } [else { … }]` —
// checks the Option and unwraps in one move.
func (p *parser) parseIfLet(at source.Span) (ast.Expr, error) {
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
	node := &ast.IfLet{Pat: pat, X: x, Then: then, Span: at}
	node.ElseIf, node.ElseBlock, err = p.parseElse()
	if err != nil {
		return nil, err
	}
	return node, nil
}

// parseStructLit: name token consumed, cur is `{`.
func (p *parser) parseStructLit(name lexer.Token) (ast.Expr, error) {
	p.next() // {
	lit := &ast.StructLit{Type: name.Text, Span: name.Span}
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
	at := p.next().Span // match
	if p.cur().Kind == lexer.LBrace {
		return p.parseCondMatch(at)
	}
	x, err := p.headerExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LBrace, "match"); err != nil {
		return nil, err
	}
	m := &ast.Match{X: x, Span: at}
	return structsOK(p, func() (ast.Expr, error) {
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				if len(m.Arms) == 0 {
					return nil, p.errf("match needs at least one arm")
				}
				return m, nil
			}
			armAt := p.cur().Span
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
			m.Arms = append(m.Arms, ast.MatchArm{Pats: pats, Guard: guard, Body: body, Span: armAt})
		}
	})
}

// parseCondMatch parses subjectless `match { cond => … }`. The `{`
// belongs to the match — a block expression cannot be the subject.
func (p *parser) parseCondMatch(at source.Span) (ast.Expr, error) {
	p.next() // {
	m := &ast.CondMatch{Span: at}
	return structsOK(p, func() (ast.Expr, error) {
		for {
			p.skipSemis()
			if p.accept(lexer.RBrace) {
				if len(m.Arms) == 0 {
					return nil, p.errf("match needs at least one arm")
				}
				return m, nil
			}
			armAt := p.cur().Span
			var cond ast.Expr
			if p.cur().Kind == lexer.Ident && p.cur().Text == "_" {
				p.next()
			} else {
				var err error
				cond, err = p.parseExpr()
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
			m.Arms = append(m.Arms, ast.CondArm{Cond: cond, Body: body, Span: armAt})
		}
	})
}
