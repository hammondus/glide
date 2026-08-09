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

	// Wrap marks the expressions where the implicit T -> T? coercion
	// applies. Option is boxed in the runtime, so `let x: Int? = 5`
	// has to build a Some around the 5, and this is the only record of
	// where: the expression's own type says `Int`, and nothing else
	// distinguishes it from an `Int` going into an `Int`.
	Wrap map[ast.Expr]bool
}

// local is one binding in a lexical scope.
type local struct {
	t   types.Type
	mut bool
}

type scope struct {
	parent    *scope
	vars      map[string]*local
	fnBound   bool
	inLoop    bool
	generator bool
}

type checker struct {
	tab  *program.Table
	file *ast.File
	bag  *source.Bag
	info *Info

	named   map[string]*types.Named
	fns     map[string]*types.Func
	consts  map[string]types.Type
	methods map[string]map[string]*types.Func
	traits  map[string]map[string]*types.Func // trait -> method -> signature, Self-bound
	tparams map[string]*types.Var

	scope *scope
	ret   types.Type // enclosing function's declared return type

	// spawned is non-nil while a closure handed to `spawn` is being
	// checked, and collects the `mut` bindings it captures. DESIGN.md:
	// a closure crossing a task boundary must not capture one — the
	// parent going on to mutate it is the data-race archetype, and it
	// is statically visible because mut-ness is known and spawn is a
	// known boundary.
	spawned map[string]source.Span

	// yields is the element type of the enclosing generator: the T of
	// its declared Iterator<T>. nil outside a generator, and nil
	// inside one whose declared return the checker could not read as
	// an Iterator — in which case yields are inferred and not asserted.
	yields types.Type

	// selfT is what `Self` means in the declaration being resolved:
	// the concrete type inside an impl, and the trait's own type
	// variable inside a trait. nil outside both, where `Self` is
	// simply an unknown type name.
	selfT types.Type
}

// withSelf binds `Self` for the duration of f. Paired with withParams
// rather than folded into it because the two are independent: an impl
// on a non-generic type binds Self and no parameters.
func (c *checker) withSelf(t types.Type, f func()) {
	saved := c.selfT
	c.selfT = t
	f()
	c.selfT = saved
}

// File checks one program. It returns Info even when checking fails,
// so a caller that wants partial information (an editor, a later
// pass) gets what was learned before the errors.
func File(f *ast.File, tab *program.Table) (*Info, error) {
	c := &checker{
		tab:  tab,
		file: f,
		bag:  &source.Bag{File: tab.File},
		info: &Info{
			Types:     map[ast.Expr]types.Type{},
			Shorthand: map[*ast.DotName]*types.Variant{},
			Funcs:     map[*ast.FuncDecl]*types.Func{},
			Wrap:      map[ast.Expr]bool{},
		},
		named:   map[string]*types.Named{},
		fns:     map[string]*types.Func{},
		consts:  map[string]types.Type{},
		methods: map[string]map[string]*types.Func{},
		traits:  map[string]map[string]*types.Func{},
		tparams: map[string]*types.Var{},
		ret:     types.Unit,
	}
	c.declareTypes(tab)
	c.declareFns(tab)
	c.checkConformance()
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
		c.withSelf(c.selfType(tn), func() {
			c.withParams(c.paramsOf(tn), func() {
				for name, fd := range ms {
					set[name] = c.record(fd, c.signature(fd))
				}
			})
		})
		c.methods[tn] = set
	}
	// Every trait's own signatures, resolved once with `Self` bound to
	// the trait's type variable. This is what a bound consults: on a
	// `T: Ord`, `t.cmp(u)` is this `cmp` with Self := T.
	for name, tr := range tab.Traits {
		c.traits[name] = c.traitSigs(tr)
	}
	// Trait defaults fill in where a type does not override, exactly
	// as the evaluator's findMethod does. Conformance — that the type
	// actually satisfies what it declared — is checked separately, in
	// checkConformance.
	for tn, traits := range tab.TypeTraits {
		for _, trName := range traits {
			tr := tab.Traits[trName]
			if tr == nil {
				continue
			}
			c.withSelf(c.selfType(tn), func() {
				c.withParams(c.paramsOf(tn), func() {
					for _, fd := range tr.Fns {
						if c.methods[tn] == nil {
							c.methods[tn] = map[string]*types.Func{}
						}
						if _, override := c.methods[tn][fd.Name]; override {
							continue
						}
						// Only a *default* (bodied) method is inherited.
						// A required signature is a demand on the type,
						// not a method it acquires; treating it as one is
						// what let `impl Greet for Foo {}` resolve a
						// `hello` that was never written.
						if fd.Body == nil {
							continue
						}
						c.methods[tn][fd.Name] = c.record(fd, c.signature(fd))
					}
				})
			})
		}
	}
}

// traitSigs resolves a trait's method signatures with `Self` bound to
// the trait's own type variable, and the trait's type parameters in
// scope. The Var carries the trait as its bound so that `Self` inside
// a default body is usable the same way a bounded `T` is.
func (c *checker) traitSigs(tr *ast.TraitDecl) map[string]*types.Func {
	self := &types.Var{Name: "Self", Bounds: []string{tr.Name}}
	out := map[string]*types.Func{}
	c.withSelf(self, func() {
		var params []*types.Var
		for _, tp := range tr.TypeParams {
			params = append(params, &types.Var{Name: tp.Name, Bounds: boundNames(tp.Bounds)})
		}
		c.withParams(params, func() {
			for _, fd := range tr.Fns {
				out[fd.Name] = c.signature(fd)
			}
		})
	})
	return out
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
		c.withSelf(self, func() {
			c.withParams(c.paramsOf(im.Target), func() {
				for _, fd := range im.Fns {
					c.fnBody(fd, c.methods[im.Target][fd.Name], self)
				}
			})
		})
	}
	// A trait's default bodies are checked against the trait's own
	// type variable as self — `Self: ThisTrait` — which is exactly the
	// generality a default has: it may call the trait's other methods
	// and nothing else. Skipped before M4c because there was no such
	// variable to check them against.
	for _, tr := range f.Traits {
		self := &types.Var{Name: "Self", Bounds: []string{tr.Name}}
		sigs := c.traits[tr.Name]
		c.withSelf(self, func() {
			var params []*types.Var
			for _, tp := range tr.TypeParams {
				params = append(params, &types.Var{Name: tp.Name, Bounds: boundNames(tp.Bounds)})
			}
			c.withParams(params, func() {
				for _, fd := range tr.Fns {
					if fd.Body == nil {
						continue
					}
					c.fnBody(fd, sigs[fd.Name], self)
				}
			})
		})
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
		// A generator is exempt from the tail-value rule — its declared
		// type describes what it *yields*, not what its body evaluates
		// to — but its yields are checked against that element type.
		savedYields := c.yields
		c.yields = nil
		want := sig.Ret
		if c.scope.generator {
			c.yields = c.elementOf(sig.Ret, fd)
			want = nil
		} else if want == types.Unit {
			want = nil
		}
		defer func() { c.yields = savedYields }()
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
	b, _ := c.lookupCrossing(name)
	return b
}

// lookupCrossing is lookup plus whether the binding was found on the
// far side of a function boundary — that is, whether reading the name
// here is a *capture*. Used to enforce the rule that a closure crossing
// a task boundary may not capture a `mut` binding.
func (c *checker) lookupCrossing(name string) (*local, bool) {
	crossed := false
	for s := c.scope; s != nil; s = s.parent {
		if b, ok := s.vars[name]; ok {
			return b, crossed
		}
		if s.fnBound {
			// A closure captures, so lookup continues past a closure
			// boundary; a nested `fn` does not, but the evaluator
			// enforces that and duplicating it here would need the
			// distinction threaded through. Left to the evaluator on
			// purpose — see DESIGN.md's open question on whether the
			// dynamic checks stay.
			crossed = true
			continue
		}
	}
	return nil, crossed
}

// elementOf reads a generator's declared return as `Iterator<T>` and
// returns the T. A generator that declares anything else is an error:
// `yield` produces an iterator, and saying otherwise is not a shape
// the language has.
func (c *checker) elementOf(ret types.Type, fd *ast.FuncDecl) types.Type {
	if types.IsOpaque(ret) {
		return nil
	}
	if app, ok := ret.(*types.App); ok && app.C == types.Iterator {
		return app.Elem()
	}
	c.errf(fd.Span, "this function yields, so it returns Iterator<T> — it declares %s", ret)
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
