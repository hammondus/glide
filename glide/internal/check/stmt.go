package check

import (
	"glide/internal/ast"
	"glide/internal/source"
	"glide/internal/types"
)

// block checks a block and returns the type of its tail value. want is
// the expected type when the block sits in an expression position with
// one (an `if` arm whose sibling is already typed, a function body
// whose return type is declared); nil means synthesise.
func (c *checker) block(b *ast.Block, want types.Type) types.Type {
	if b == nil {
		return types.Unit
	}
	c.push()
	defer c.pop()

	// Nested fns are hoisted to block entry so siblings can be
	// mutually recursive, which means their signatures must be in
	// scope before any statement is checked.
	for _, s := range b.Stmts {
		if fs, ok := s.(*ast.FnStmt); ok {
			c.declare(fs.Decl.Name, c.record(fs.Decl, c.signature(fs.Decl)), false, fs.Span)
		}
	}

	tail := types.Type(types.Unit)
	for i, s := range b.Stmts {
		if es, isExpr := s.(*ast.ExprStmt); isExpr && i == len(b.Stmts)-1 {
			if want != nil {
				tail = c.checkExpr(es.E, want)
			} else {
				tail = c.infer(es.E)
			}
			continue
		}
		c.stmt(s)
	}
	// A block whose last statement transfers control has no tail
	// value — it has no end. Saying Unit here would make every
	// `fn f() -> T { … ; return x }` look like it fell off the end
	// returning nothing.
	if n := len(b.Stmts); n > 0 {
		switch b.Stmts[n-1].(type) {
		case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
			return types.Never
		}
	}
	return tail
}

func (c *checker) stmt(s ast.Stmt) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		c.infer(st.E)

	case *ast.LetStmt:
		c.letStmt(st)

	case *ast.AssignStmt:
		c.assign(st)

	case *ast.ReturnStmt:
		if st.E == nil {
			if c.ret != types.Unit && !types.IsOpaque(c.ret) {
				c.errf(st.Span, "this function returns %s, but this `return` has no value", c.ret)
			}
			return
		}
		c.checkExpr(st.E, c.ret)

	case *ast.ForStmt:
		c.forStmt(st)

	case *ast.YieldStmt:
		switch {
		case st.E == nil:
			// `yield` with no value: only meaningful for Iterator<()>.
			if c.yields != nil && !types.AssignableTo(types.Unit, c.yields) {
				c.errf(st.Span, "this generator yields %s, so `yield` needs a value", c.yields)
			}
		case st.From:
			// `yield from it` delegates, so the operand is an iterator
			// of the same element type, not an element.
			want := types.Type(nil)
			if c.yields != nil {
				want = types.Apply(types.Iterator, c.yields)
			}
			c.checkExpr(st.E, want)
		default:
			c.checkExpr(st.E, c.yields)
		}

	case *ast.FnStmt:
		c.fnBody(st.Decl, c.info.Funcs[st.Decl], nil)

	case *ast.DeferStmt:
		c.block(st.Body, nil)

	case *ast.BreakStmt, *ast.ContinueStmt:
		// Loop-shape rules (breaking outside a loop, an unknown label)
		// are the parser's and the evaluator's; nothing here is about
		// types.
	}
}

func (c *checker) letStmt(st *ast.LetStmt) {
	var t types.Type
	switch {
	case st.Type != nil:
		t = c.resolve(st.Type)
		if st.Init != nil {
			c.checkExpr(st.Init, t)
		}
	case st.Init != nil:
		// No annotation: the initialiser decides, and an untyped
		// constant settles into its default here — `let n = 1` is an
		// Int, not a constant that stays polymorphic.
		t = types.Default(c.infer(st.Init))
	default:
		t = types.Unknown
	}
	if st.Else != nil {
		c.block(st.Else, nil)
	}
	c.bind(st.Pat, t)
}

func (c *checker) forStmt(st *ast.ForStmt) {
	c.push()
	defer c.pop()
	saved := c.scope.inLoop
	c.scope.inLoop = true
	defer func() { c.scope.inLoop = saved }()

	switch {
	case st.Iter != nil:
		elem := c.elemType(c.infer(st.Iter), st.Iter)
		c.bind(st.Pat, elem)
	case st.Cond != nil:
		c.cond(st.Cond, "for")
	}
	c.block(st.Body, nil)
}

// elemType is what `for x in it` binds. Reporting a non-iterable is
// worth doing — it is a common typo and the evaluator only finds it if
// the loop is reached — but only for types the checker is sure about.
func (c *checker) elemType(t types.Type, at ast.Expr) types.Type {
	if types.IsOpaque(t) {
		return types.Unknown
	}
	switch x := t.(type) {
	case *types.App:
		switch x.C {
		case types.List, types.Iterator, types.Receiver:
			return x.Elem()
		case types.Map:
			return &types.Tuple{Elems: []types.Type{x.Arg(0), x.Arg(1)}}
		case types.Range:
			return types.Int
		}
	case *types.Named:
		// A user type is iterable when it has an `iter()` method —
		// that is the whole protocol.
		if m := c.methods[x.Name]["iter"]; m != nil {
			return c.elemType(m.Ret, at)
		}
	case *types.Basic:
		if x == types.Int || x == types.UntypedInt {
			// `for i in 0..n` types the range, not the endpoint; a
			// bare Int is not iterable.
			break
		}
	}
	c.errf(span(at), "%s is not iterable", t)
	return types.Unknown
}

// assign checks `x = v`, `xs[i] += 1`, and friends: the target must
// name a mutable path, and the value must fit what lives there.
func (c *checker) assign(st *ast.AssignStmt) {
	if id, ok := st.Target.(*ast.IdentExpr); ok && id.Name == "_" {
		c.infer(st.Value)
		return
	}
	target := c.inferAssignable(st.Target)
	if st.Op == "=" {
		c.checkExpr(st.Value, target)
		return
	}
	// Compound assignment is the binary operator plus a store, so it
	// obeys exactly the operator's rules.
	v := c.infer(st.Value)
	c.binaryResult(st.Op[:1], target, v, st.Span)
}

// inferAssignable types an assignment target and reports an immutable
// one. The `mut` rule is a path property: it is the *root* binding
// that must be mutable, which is why this walks to the root rather
// than asking about the field or element.
func (c *checker) inferAssignable(e ast.Expr) types.Type {
	if root, name := rootBinding(e); root != nil {
		if b := c.lookup(name); b != nil && !b.mut {
			c.errf(span(e), "cannot mutate through immutable binding %q (declare it with `let mut`)", name)
		} else if b == nil {
			// Not a local: a const, a function, an unknown name. The
			// ident path reports unknown names; assignment to a const
			// is caught there too.
			c.infer(root)
		}
	}
	// `m[k] = v` stores a V. Reading `m[k]` yields a V? because the
	// key may be absent, but the slot being written is not optional —
	// this is the one place indexing means something different on the
	// left of an `=` than on the right.
	if idx, ok := e.(*ast.Index); ok {
		if recv, isApp := c.infer(idx.X).(*types.App); isApp && recv.C == types.Map {
			c.checkExpr(idx.I, recv.Arg(0))
			return recv.Arg(1)
		}
	}
	return c.infer(e)
}

// rootBinding walks an lvalue back to the identifier it is rooted at:
// `a.b[i].c` is rooted at `a`. Returns nil for an lvalue with no root
// binding (an assignment to a call result, which the parser rejects).
func rootBinding(e ast.Expr) (ast.Expr, string) {
	for {
		switch x := e.(type) {
		case *ast.IdentExpr:
			return x, x.Name
		case *ast.Index:
			e = x.X
		case *ast.Field:
			e = x.X
		case *ast.TupleIndex:
			e = x.X
		default:
			return nil, ""
		}
	}
}

// bind introduces a pattern's bindings at type t.
//
// It reports one class of error and one only: a constructor pattern
// naming something that is not a constructor. That case is worth a
// diagnostic because the evaluator's answer is *silence* — a
// mistyped capitalised name is a pattern that simply never matches,
// and the program runs to completion doing the wrong thing. Every
// other pattern/type disagreement is left to exhaustiveness checking,
// which is later work and needs to be right rather than early.
func (c *checker) bind(p ast.Pattern, t types.Type) {
	switch x := p.(type) {
	case *ast.IdentPat:
		c.declare(x.Name, t, x.Mut, x.Span)

	case *ast.WildPat:

	case *ast.TuplePat:
		elems := tupleElems(t, len(x.Elems))
		for i, e := range x.Elems {
			c.bind(e, elems[i])
		}

	case *ast.ListPat:
		elem := types.Type(types.Unknown)
		if app, ok := t.(*types.App); ok && app.C == types.List {
			elem = app.Elem()
		}
		for _, e := range x.Elems {
			c.bind(e, elem)
		}
		if x.Rest >= 0 && x.RestName != "" && x.RestName != "_" {
			c.declare(x.RestName, types.Apply(types.List, elem), false, x.Span)
		}

	case *ast.CtorPat:
		c.bindCtor(x, t)

	case *ast.StructPat:
		c.bindStruct(x, t)
	}
}

func (c *checker) bindCtor(x *ast.CtorPat, t types.Type) {
	switch x.Name {
	case "None":
		return
	case "Some":
		inner, _ := types.Unwrap(t)
		c.bindArgs(x, []types.Type{inner})
		return
	case "Ok", "Err":
		i := 0
		if x.Name == "Err" {
			i = 1
		}
		payload := types.Type(types.Unknown)
		if app, ok := t.(*types.App); ok && app.C == types.Result {
			payload = app.Arg(i)
		}
		c.bindArgs(x, []types.Type{payload})
		return
	}
	// A distinct type destructures by its own name: `NoteId(n)` binds
	// n to the base type. The wrapper is nominal, so the pattern names
	// the wrapper, never the base.
	if n, ok := c.named[x.Name]; ok && n.IsDistinct() {
		c.bindArgs(x, []types.Type{types.Base(n)})
		return
	}
	// A user variant. Prefer the scrutinee's own type so a generic
	// sum resolves its payload; fall back to the file-wide variant
	// index, which is what makes the name legal at all.
	if n, ok := t.(*types.Named); ok {
		if v, found := n.Variant(x.Name); found {
			c.bindArgs(x, v.Args)
			return
		}
	}
	vi, known := c.tab.Variants[x.Name]
	if !known {
		c.errf(x.Span, "%s is not a constructor (a capitalised name in a pattern names a variant; lowercase binds)", x.Name)
		c.bindArgs(x, nil)
		return
	}
	if n := c.named[vi.Type]; n != nil {
		if v, found := n.Variant(x.Name); found {
			c.bindArgs(x, v.Args)
			return
		}
	}
	c.bindArgs(x, nil)
}

// bindArgs binds a constructor pattern's sub-patterns, using Unknown
// wherever the payload types are not known.
func (c *checker) bindArgs(x *ast.CtorPat, payload []types.Type) {
	for i, sub := range x.Args {
		t := types.Type(types.Unknown)
		if i < len(payload) {
			t = payload[i]
		}
		c.bind(sub, t)
	}
}

func (c *checker) bindStruct(x *ast.StructPat, t types.Type) {
	var n *types.Named
	if b, ok := types.Base(t).(*types.Named); ok {
		n = b
	} else if d := c.named[x.Type]; d != nil {
		n = d
	}
	for _, f := range x.Fields {
		ft := types.Type(types.Unknown)
		if n != nil {
			if fd, ok := n.Field(f.Name); ok {
				ft = fd.Type
			} else if v, ok := n.Variant(x.Type); ok {
				for _, vf := range v.Fields {
					if vf.Name == f.Name {
						ft = vf.Type
					}
				}
			}
		}
		c.bind(f.Pat, ft)
	}
}

// tupleElems splits t into n element types, padding with Unknown when
// t is not a tuple of that width — destructuring something that is not
// a tuple is the evaluator's error to report, not a reason to
// manufacture n more here.
func tupleElems(t types.Type, n int) []types.Type {
	out := make([]types.Type, n)
	tup, ok := t.(*types.Tuple)
	for i := range out {
		if ok && i < len(tup.Elems) {
			out[i] = tup.Elems[i]
		} else {
			out[i] = types.Unknown
		}
	}
	return out
}

// span extracts a node's position. Every AST node embeds source.Span,
// so this is a type assertion rather than a switch over 65 cases.
func span(n any) source.Span {
	type spanned interface{ Position() source.Span }
	if s, ok := n.(spanned); ok {
		return s.Position()
	}
	return source.Span{}
}
