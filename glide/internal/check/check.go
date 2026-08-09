package check

import (
	"glide/internal/ast"
	"glide/internal/program"
	"glide/internal/source"
	"glide/internal/types"
)

// Info is what checking produces: types attached to AST nodes, not a
// second tree. go/types.Info's shape, and for its reason — a lowered
// IR would double the node definitions and double the eventual port to
// Glide, and Glide's first backend emits Go, whose own compiler does
// the optimising. See glide/DESIGN-DECISIONS.md.
//
// Nothing in the evaluator requires Info today; it is the seam the
// `?`-conversion and shorthand-resolution work plugs into next, and
// the one the code generator will read.
type Info struct {
	// Types is every expression's type. An expression the checker
	// could not type is absent rather than mapped to Unknown, so a
	// consumer can tell "no information" from "information: unknown".
	Types map[ast.Expr]types.Type

	// Shorthand records what each `.Variant` resolved to. M1-M3
	// resolved these in a global namespace because there was no
	// expected type to resolve them in; this is that expected type,
	// recorded.
	Shorthand map[*ast.DotName]*types.Variant

	// Funcs is each declared function's resolved signature, keyed by
	// declaration so methods and free functions share one map.
	Funcs map[*ast.FuncDecl]*types.Func
}

// local is one binding in a lexical scope.
type local struct {
	t   types.Type
	mut bool
}

type scope struct {
	parent   *scope
	vars     map[string]*local
	fnBound  bool
	inLoop   bool
	generator bool
}

type checker struct {
	tab  *program.Table
	bag  *source.Bag
	info *Info

	named   map[string]*types.Named
	fns     map[string]*types.Func
	consts  map[string]types.Type
	methods map[string]map[string]*types.Func
	tparams map[string]*types.Var

	scope *scope
	ret   types.Type // enclosing function's declared return type
}

// File checks one program. It returns Info even when checking fails,
// so a caller that wants partial information (an editor, a later
// pass) gets what was learned before the errors.
func File(f *ast.File, tab *program.Table) (*Info, error) {
	c := &checker{
		tab: tab,
		bag: &source.Bag{File: tab.File},
		info: &Info{
			Types:     map[ast.Expr]types.Type{},
			Shorthand: map[*ast.DotName]*types.Variant{},
			Funcs:     map[*ast.FuncDecl]*types.Func{},
		},
		named:   map[string]*types.Named{},
		fns:     map[string]*types.Func{},
		consts:  map[string]types.Type{},
		methods: map[string]map[string]*types.Func{},
		tparams: map[string]*types.Var{},
		ret:     types.Unit,
	}
	c.declareTypes(tab)
	c.declareFns(tab)
	c.checkFile(f)
	return c.info, c.bag.Err()
}

// declareFns resolves every function and method signature before any
// body is checked, because module-level declarations are
// order-independent and a body may call a function declared below it.
func (c *checker) declareFns(tab *program.Table) {
	for name, fd := range tab.Fns {
		c.fns[name] = c.record(fd, c.signature(fd))
	}
	for tn, ms := range tab.Methods {
		set := map[string]*types.Func{}
		c.withParams(c.paramsOf(tn), func() {
			for name, fd := range ms {
				set[name] = c.record(fd, c.signature(fd))
			}
		})
		c.methods[tn] = set
	}
	// Trait defaults fill in where a type does not override, exactly
	// as the evaluator's findMethod does. Verifying that an impl
	// actually satisfies its trait is M4c; this only makes the calls
	// resolve.
	for tn, traits := range tab.TypeTraits {
		for _, trName := range traits {
			tr := tab.Traits[trName]
			if tr == nil {
				continue
			}
			for _, fd := range tr.Fns {
				if c.methods[tn] == nil {
					c.methods[tn] = map[string]*types.Func{}
				}
				if _, override := c.methods[tn][fd.Name]; override {
					continue
				}
				c.methods[tn][fd.Name] = c.record(fd, c.signature(fd))
			}
		}
	}
}

func (c *checker) record(fd *ast.FuncDecl, f *types.Func) *types.Func {
	c.info.Funcs[fd] = f
	return f
}

func (c *checker) paramsOf(typeName string) []*types.Var {
	if n := c.named[typeName]; n != nil {
		return n.Params
	}
	return nil
}

func (c *checker) checkFile(f *ast.File) {
	// Consts in file order: a const may refer to one declared above
	// it, which is exactly the order the evaluator uses.
	c.pushFn()
	for _, cd := range f.Consts {
		c.consts[cd.Name] = types.Default(c.infer(cd.E))
	}
	c.pop()
	for _, fd := range f.Funcs {
		c.fnBody(fd, c.fns[fd.Name], nil)
	}
	for _, im := range f.Impls {
		self := c.selfType(im.Target)
		c.withParams(c.paramsOf(im.Target), func() {
			for _, fd := range im.Fns {
				c.fnBody(fd, c.methods[im.Target][fd.Name], self)
			}
		})
	}
	for _, tr := range f.Traits {
		for _, fd := range tr.Fns {
			if fd.Body != nil {
				// A default body has no concrete self; checking it
				// properly needs the trait's own type variable, which
				// arrives with M4c. Until then it is skipped rather
				// than checked against a wrong self.
				continue
			}
		}
	}
	for _, td := range f.Tests {
		c.pushFn()
		for _, prm := range td.Params {
			c.declare(prm.Name, c.resolve(prm.Type), false, prm.Span)
		}
		c.block(td.Body, nil)
		c.pop()
	}
	for _, bd := range f.Benches {
		c.pushFn()
		c.block(bd.Body, nil)
		c.pop()
	}
}

// selfType is the receiver type inside `impl T { … }`: the declared
// type applied to its own parameters, so `self.root` on a `Tree<T>`
// has type T rather than Unknown.
func (c *checker) selfType(name string) types.Type {
	n := c.named[name]
	if n == nil {
		return types.Unknown
	}
	if len(n.Params) == 0 {
		return n
	}
	args := make([]types.Type, len(n.Params))
	for i, v := range n.Params {
		args[i] = v
	}
	return n.Instantiate(args)
}

func (c *checker) fnBody(fd *ast.FuncDecl, sig *types.Func, self types.Type) {
	if fd == nil || fd.Body == nil || sig == nil {
		return
	}
	c.withParams(sig.TypeParams, func() {
		savedRet := c.ret
		c.ret = sig.Ret
		c.pushFn()
		c.scope.generator = isGenerator(fd.Body)
		if self != nil && fd.Self != ast.NoSelf {
			c.declare("self", self, fd.Self == ast.MutSelf, fd.Span)
		}
		for i, prm := range fd.Params {
			if prm.Default != nil {
				c.checkExpr(prm.Default, sig.Params[i].Type)
			}
			c.declare(prm.Name, sig.Params[i].Type, false, prm.Span)
		}
		// The tail-value rule's static half: a function that declares a
		// return type and ends in a value expression must end in a
		// value of that type. Pushing the return type in as the body's
		// expectation is what makes `{ if c { Ok(x) } else { Err(e) } }`
		// check — each arm is checked against Result<T, E> rather than
		// synthesised and then compared, which is the difference
		// between the two directions of a bidirectional checker.
		//
		// Generators are exempt: their declared type describes what
		// they yield, and typing that is later work.
		want := sig.Ret
		if c.scope.generator || want == types.Unit {
			want = nil
		}
		tail := c.block(fd.Body, want)
		if want != nil && !types.IsOpaque(tail) && !types.IsNever(tail) &&
			!types.AssignableTo(tail, want) {
			c.errf(tailSpan(fd.Body), "this function returns %s, but its body ends with %s",
				sig.Ret, types.Default(tail))
		}
		c.pop()
		c.ret = savedRet
	})
}

// Scopes

func (c *checker) pushFn() {
	c.scope = &scope{parent: c.scope, vars: map[string]*local{}, fnBound: true}
}

func (c *checker) push() {
	inLoop := c.scope != nil && c.scope.inLoop
	gen := c.scope != nil && c.scope.generator
	c.scope = &scope{parent: c.scope, vars: map[string]*local{}, inLoop: inLoop, generator: gen}
}

func (c *checker) pop() { c.scope = c.scope.parent }

func (c *checker) declare(name string, t types.Type, mut bool, at source.Span) {
	if name == "" || name == "_" || c.scope == nil {
		return
	}
	c.scope.vars[name] = &local{t: t, mut: mut}
}

func (c *checker) lookup(name string) *local {
	for s := c.scope; s != nil; s = s.parent {
		if b, ok := s.vars[name]; ok {
			return b
		}
		if s.fnBound {
			// A closure captures, so lookup continues past a closure
			// boundary; a nested `fn` does not, but the evaluator
			// enforces that and duplicating it here would need the
			// distinction threaded through. Left to the evaluator on
			// purpose — see DESIGN.md's open question on whether the
			// dynamic checks stay.
			continue
		}
	}
	return nil
}

// isGenerator reports whether a body contains a `yield`, which is what
// makes a function a generator.
func isGenerator(b *ast.Block) bool {
	found := false
	walkBlock(b, func(s ast.Stmt) {
		if _, ok := s.(*ast.YieldStmt); ok {
			found = true
		}
	})
	return found
}

// walkBlock visits every statement in a block and its nested blocks,
// but not into nested function declarations or closures — a `yield`
// inside a closure makes the closure a generator, not its host.
func walkBlock(b *ast.Block, f func(ast.Stmt)) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		f(s)
		switch x := s.(type) {
		case *ast.ForStmt:
			walkBlock(x.Body, f)
		case *ast.DeferStmt:
			walkBlock(x.Body, f)
		case *ast.LetStmt:
			walkBlock(x.Else, f)
			walkExprBlocks(x.Init, f)
		case *ast.ExprStmt:
			walkExprBlocks(x.E, f)
		case *ast.ReturnStmt:
			walkExprBlocks(x.E, f)
		case *ast.YieldStmt:
			walkExprBlocks(x.E, f)
		}
	}
}

func walkExprBlocks(e ast.Expr, f func(ast.Stmt)) {
	switch x := e.(type) {
	case *ast.BlockExpr:
		walkBlock(x.Body, f)
	case *ast.If:
		walkBlock(x.Then, f)
		walkBlock(x.ElseBlock, f)
		walkExprBlocks(x.ElseIf, f)
	case *ast.IfLet:
		walkBlock(x.Then, f)
		walkBlock(x.ElseBlock, f)
		walkExprBlocks(x.ElseIf, f)
	case *ast.Match:
		for _, a := range x.Arms {
			walkExprBlocks(a.Body, f)
		}
	case *ast.CondMatch:
		for _, a := range x.Arms {
			walkExprBlocks(a.Body, f)
		}
	case *ast.ScopeExpr:
		walkBlock(x.Body, f)
	}
}

// tailSpan is where to point a "wrong tail value" diagnostic: at the
// tail expression, not at the whole function.
func tailSpan(b *ast.Block) source.Span {
	if b != nil && len(b.Stmts) > 0 {
		if es, ok := b.Stmts[len(b.Stmts)-1].(*ast.ExprStmt); ok {
			return es.Span
		}
	}
	if b != nil && b.Span.IsValid() {
		// No tail statement at all: point at the closing brace, which
		// is where the missing value should have been, rather than at
		// the whole body.
		return source.Span{Pos: b.Span.End - 1, End: b.Span.End}
	}
	return source.Span{}
}
