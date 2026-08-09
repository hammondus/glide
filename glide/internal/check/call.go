package check

import (
	"sort"

	"glide/internal/ast"
	"glide/internal/source"
	"glide/internal/types"
)

// call types a call expression. The shape of the callee decides
// everything, exactly as it does in the evaluator — the two switch on
// the same cases in the same order so a call that resolves one way in
// one tier cannot resolve another way in the other.
func (c *checker) call(x *ast.Call, want types.Type) types.Type {
	if id, ok := x.Fn.(*ast.IdentExpr); ok && c.lookup(id.Name) == nil {
		if t, handled := c.specialForm(id.Name, x, want); handled {
			return t
		}
	}
	switch fn := x.Fn.(type) {
	case *ast.Field:
		return c.dotCall(fn, x, want)
	case *ast.DotName:
		// `.Green(1)` — a variant constructor resolved in the
		// expected type.
		owner := c.shorthand(fn, want, nil)
		return c.variantCall(fn.Name, owner, x)
	}
	callee := c.infer(x.Fn)
	// A distinct type is constructed by calling its own name:
	// `Code("hi")`. This is the only place a *type* is callable, and
	// it is a conversion, not a function — which is why it takes
	// exactly the base type and nothing implicitly convertible to it.
	if m, ok := callee.(*types.Meta); ok {
		if b, isBasic := m.T.(*types.Basic); isBasic {
			return c.conversion(b, x)
		}
		if n, isNamed := m.T.(*types.Named); isNamed && n.IsDistinct() {
			base := types.Base(n)
			if len(x.Args) != 1 {
				c.inferArgs(x)
				c.errf(x.Span, "%s wraps exactly one %s, got %d arguments", n.Name, base, len(x.Args))
				return n
			}
			if got := c.typeOf(x.Args[0], base); !types.AssignableTo(got, base) && !types.IsOpaque(got) {
				c.errf(span(x.Args[0]), "%s wraps %s, got %s (no implicit conversion)", n.Name, base, got)
			}
			return n
		}
	}
	sig, ok := callee.(*types.Func)
	if !ok {
		c.inferArgs(x)
		if !types.IsOpaque(callee) {
			c.errf(x.Span, "%s is not callable", callee)
		}
		return types.Unknown
	}
	return c.checkArgs(sig, x, x.Span)
}

// conversion checks `dst(v)` — the explicit numeric conversion, Go's
// spelling. It is the only way between widths: DESIGN.md forbids the
// implicit kind, and until M4c there was no explicit kind either, so
// a sized value could only ever come from a literal.
//
// The argument is *inferred*, never checked against the target: the
// whole point is that the source type differs. The one thing pushed
// inward is the range check on a literal, so `u8(300)` fails here
// rather than at runtime — the checker already knows.
func (c *checker) conversion(dst *types.Basic, x *ast.Call) types.Type {
	if !types.Numeric(dst) {
		c.inferArgs(x)
		c.errf(x.Span, "%s is not a conversion (conversion is defined between numbers and Rune only)", dst)
		return dst
	}
	if len(x.Args) != 1 {
		c.inferArgs(x)
		c.errf(x.Span, "%s converts exactly one value, got %d arguments", dst, len(x.Args))
		return dst
	}
	if len(x.Names) > 0 && x.Names[0] != "" {
		c.errf(x.Span, "a conversion takes one positional value, not a named argument")
	}
	src := c.infer(x.Args[0])
	if !types.ConvertibleTo(src, dst) {
		c.errf(span(x.Args[0]), "cannot convert %s to %s (conversion is defined between numbers and Rune only)",
			types.Default(src), dst)
		return dst
	}
	// A constant that cannot fit is a compile error, not a trap:
	// `u8(300)` is as knowable as `let x: u8 = 300`, and so is
	// `Rune(-1)`.
	c.rangeCheck(x.Args[0], dst)
	if dst.IsRune() {
		if mag, neg, isConst := constInt(x.Args[0]); isConst && !types.ValidCodePoint(mag, neg) {
			c.errf(span(x.Args[0]), "%s is not a Unicode code point", signed(mag, neg))
		}
	}
	c.info.Types[x.Args[0]] = types.Default(src)
	return dst
}

// specialForm handles the builtins whose type depends on the call
// site rather than on a signature. They are deliberately not in the
// signature table: giving `Ok` a signature would mean inventing a
// return type it does not have until the expectation says so.
func (c *checker) specialForm(name string, x *ast.Call, want types.Type) (types.Type, bool) {
	switch name {
	case "expect":
		// expect(a == b) is a special form: it keeps the argument's
		// AST so a failure can report both sides.
		for _, a := range x.Args {
			c.infer(a)
		}
		return types.Unit, true

	case "Some":
		if len(x.Args) != 1 {
			c.inferArgs(x)
			c.errf(x.Span, "Some takes exactly one argument")
			return types.Unknown, true
		}
		if o, ok := want.(*types.App); ok && o.C == types.Option {
			c.checkExpr(x.Args[0], o.Elem())
			return o, true
		}
		return types.Opt(types.Default(c.checkExpr(x.Args[0], nil))), true

	case "Ok", "Err":
		idx := 0
		if name == "Err" {
			idx = 1
		}
		if len(x.Args) != 1 {
			c.inferArgs(x)
			c.errf(x.Span, "%s takes exactly one argument", name)
			return types.Unknown, true
		}
		// The expected type supplies both halves; without one, the
		// side not written stays Unknown and the caller's
		// expectation fills it in later.
		if r, ok := want.(*types.App); ok && r.C == types.Result {
			c.checkExpr(x.Args[0], r.Arg(idx))
			// The expectation is the answer: the payload was checked
			// against it, and rebuilding a Result from the argument's
			// own type would report `Result<Int, String>` where
			// `Result<Int, Error>` was both wanted and satisfied.
			return r, true
		}
		res := [2]types.Type{types.Unknown, types.Unknown}
		res[idx] = types.Default(c.checkExpr(x.Args[0], nil))
		return types.Apply(types.Result, res[0], res[1]), true

	case "channel":
		// channel() / channel(cap: n) -> (Sender<T>, Receiver<T>).
		for _, a := range x.Args {
			c.checkExpr(a, types.Int)
		}
		if len(x.Names) > 0 && (len(x.Names) != 1 || x.Names[0] != "cap") {
			c.errf(x.Span, "channel takes no arguments or (cap: n)")
		}
		elem := types.Type(types.Unknown)
		if tup, ok := want.(*types.Tuple); ok && len(tup.Elems) == 2 {
			if s, isApp := tup.Elems[0].(*types.App); isApp && s.C == types.Sender {
				elem = s.Elem()
			}
		}
		return &types.Tuple{Elems: []types.Type{
			types.Apply(types.Sender, elem),
			types.Apply(types.Receiver, elem),
		}}, true
	}
	return nil, false
}

// dotCall handles `recv.name(args)`: module functions, associated
// functions, variant constructors, distinct unwrapping, user methods
// and builtin methods.
func (c *checker) dotCall(f *ast.Field, x *ast.Call, want types.Type) types.Type {
	recv := c.infer(f.X)

	if mod, ok := recv.(*types.Module); ok {
		set, known := modules[mod.Name]
		if !known {
			c.inferArgs(x)
			return types.Unknown
		}
		sig := set[f.Name]
		if sig == nil {
			c.inferArgs(x)
			c.errf(x.Span, "module %s has no function %q", mod.Name, f.Name)
			return types.Unknown
		}
		return c.checkArgs(sig, x, x.Span)
	}

	if m, ok := recv.(*types.Meta); ok {
		n, isNamed := m.T.(*types.Named)
		if !isNamed {
			c.inferArgs(x)
			return types.Unknown
		}
		if _, isVariant := n.Variant(f.Name); isVariant {
			return c.variantCall(f.Name, n, x)
		}
		sig := c.methods[n.Name][f.Name]
		if sig == nil {
			c.inferArgs(x)
			c.errf(x.Span, "type %s has no associated function %q", n.Name, f.Name)
			return types.Unknown
		}
		if sig.Self != ast.NoSelf {
			c.inferArgs(x)
			c.errf(x.Span, "%s.%s is a method; call it on a value", n.Name, f.Name)
			return types.Unknown
		}
		// The expectation binds the type parameters an associated
		// function's arguments cannot: `Box.new()` takes nothing, so
		// `let c: Box<Int> = Box.new()` is the only thing that can say
		// what T is. Before M4c this leaned on erase turning T into
		// Unknown and the annotation winning by wildcard — which
		// happened to work and said nothing when it did not.
		return c.checkArgsWanting(sig, x, x.Span, want)
	}

	// value() unwraps a distinct type — the one built-in escape
	// hatch, explicit so the conversion is visible at the site.
	if n, ok := recv.(*types.Named); ok && n.IsDistinct() && f.Name == "value" &&
		c.methods[n.Name]["value"] == nil {
		c.inferArgs(x)
		if len(x.Args) != 0 {
			c.errf(x.Span, "value takes no arguments")
		}
		return types.Base(n)
	}

	// `s.spawn(|| …)` is a task boundary. Checking the closure with
	// `spawned` armed collects the mut bindings it captures, and the
	// rule is enforced once the argument has been typed — the
	// diagnostic wants to point at the capture, which is only known
	// after the body is walked.
	if app, ok := recv.(*types.App); ok && app.C == types.Scope && f.Name == "spawn" {
		return c.spawnCall(x)
	}
	sig, modelled := c.methodOf(recv, f.Name)
	if sig == nil {
		c.inferArgs(x)
		// `modelled` carries the type-parameter case on its own: an
		// unbounded T reports false and stays silent, a bounded one
		// reports true and lands in the branch below. So this guard
		// tests Unknown rather than IsOpaque — using IsOpaque here
		// would silence exactly the bounded receivers that bounds
		// exist to make checkable.
		if modelled && !types.IsUnknown(recv) {
			if v, isVar := recv.(*types.Var); isVar {
				c.errf(x.Span, "%s has no method %q: it is bounded by %s, which does not declare one",
					v.Name, f.Name, boundsOf(v))
			} else if hint, ok := methodHints[typeCtorName(types.Default(recv))+"."+f.Name]; ok {
				c.errf(x.Span, "%s", hint)
			} else {
				// Defaulted, so an untyped literal is named by the
				// type it would become. "untyped integer has no
				// method" leaks a name that appears nowhere in the
				// language — the reader wrote `5.sqrt()` and needs to
				// be told about Int.
				c.errf(x.Span, "%s has no method %q", types.Default(recv), f.Name)
			}
		}
		return types.Unknown
	}
	if sig.Self == ast.NoSelf {
		c.inferArgs(x)
		c.errf(x.Span, "%s.%s is an associated function; call it as %s.%s(…)",
			recv, f.Name, recv, f.Name)
		return types.Unknown
	}
	if sig.Self == ast.MutSelf {
		c.requireMutRoot(f.X, x.Span)
	}
	return c.checkArgs(sig, x, x.Span)
}

// spawnCall checks `s.spawn(f)` with the mut-capture rule armed.
// DESIGN.md: a closure crossing a task boundary must not capture a
// `mut` binding, because the parent going on to mutate it is the
// data-race archetype — and unlike most races this one is statically
// visible, since mut-ness is known and spawn is a known boundary.
func (c *checker) spawnCall(x *ast.Call) types.Type {
	saved := c.spawned
	c.spawned = map[string]source.Span{}
	sig := ctorMethods[types.Scope]["spawn"]
	ret := c.checkArgs(sig, x, x.Span)
	names := make([]string, 0, len(c.spawned))
	for name := range c.spawned {
		names = append(names, name)
	}
	sort.Strings(names) // stable diagnostics, not map order
	for _, name := range names {
		c.errf(c.spawned[name],
			"a spawned closure cannot capture the mutable binding %q — the parent may still be writing it. "+
				"Freeze it first (`let %s_now = %s`) or send it over a channel",
			name, name, name)
	}
	c.spawned = saved
	return ret
}

// methodOf finds a method on a receiver. modelled reports whether the
// checker knows this receiver's method set at all: false means
// silence, because complaining about a method on a type the checker
// does not model would be a false positive.
func (c *checker) methodOf(recv types.Type, name string) (sig *types.Func, modelled bool) {
	// A type parameter answers through its bounds. Unbounded, it
	// answers nothing and the caller stays silent.
	if v, ok := recv.(*types.Var); ok {
		return c.boundMethod(v, name)
	}
	if n, ok := recv.(*types.Named); ok {
		m := c.methods[n.Name][name]
		if m == nil {
			return nil, true
		}
		if b := namedBinding(n); b != nil {
			if f, ok := types.Subst(m, b).(*types.Func); ok {
				return f, true
			}
		}
		return m, true
	}
	return builtinMethod(recv, name)
}

// namedBinding maps a Named instance's type parameters to its
// arguments, for substituting into a method signature.
func namedBinding(n *types.Named) map[string]types.Type {
	if len(n.Params) == 0 || len(n.Args) != len(n.Params) {
		return nil
	}
	m := make(map[string]types.Type, len(n.Params))
	for i, v := range n.Params {
		m[v.Name] = n.Args[i]
	}
	return m
}

// requireMutRoot enforces the `mut` path rule statically: a `mut self`
// method, and a builtin that mutates, need a mutable root binding.
func (c *checker) requireMutRoot(target ast.Expr, at source.Span) {
	_, name := rootBinding(target)
	if name == "" {
		return
	}
	if b := c.lookup(name); b != nil && !b.mut {
		c.errf(at, "cannot mutate through immutable binding %q (declare it with `let mut`)", name)
	}
}

// variantCall types `Colour.Green(1)` and `.Green(1)`.
func (c *checker) variantCall(name string, owner *types.Named, x *ast.Call) types.Type {
	if owner == nil {
		c.inferArgs(x)
		return types.Unknown
	}
	v, ok := owner.Variant(name)
	if !ok {
		c.inferArgs(x)
		c.errf(x.Span, "%s has no variant %q", owner.Name, name)
		return owner
	}
	if len(v.Fields) > 0 {
		c.inferArgs(x)
		c.errf(x.Span, "%s has named fields; construct it with %s{ … }", name, name)
		return owner
	}
	if len(x.Args) != len(v.Args) {
		c.inferArgs(x)
		c.errf(x.Span, "%s takes %d argument(s), got %d", name, len(v.Args), len(x.Args))
		return owner
	}
	for i, a := range x.Args {
		c.checkExpr(a, v.Args[i])
	}
	return owner
}

// checkArgs matches a call's arguments to a signature: named
// arguments to their parameters, defaults where an argument is
// absent, and type-parameter inference from what was passed.
func (c *checker) checkArgs(sig *types.Func, x *ast.Call, at source.Span) types.Type {
	return c.checkArgsWanting(sig, x, at, nil)
}

// checkArgsWanting is checkArgs with the call's own expected type
// available to seed the type-parameter binding.
func (c *checker) checkArgsWanting(sig *types.Func, x *ast.Call, at source.Span, want types.Type) types.Type {
	slots := c.argSlots(sig, x, at)
	bind := map[string]types.Type{}
	if want != nil {
		unify(sig.Ret, want, bind)
	}
	for i, prm := range sig.Params {
		if i >= len(slots) || slots[i] == nil {
			continue
		}
		want := types.Subst(prm.Type, bind)
		if hasVar(want) {
			// The expectation still has unbound parameters, so it is
			// pushed inward for its known parts but not asserted:
			// asserting it would compare a concrete type against a
			// variable and always fail.
			got := c.typeOf(slots[i], want)
			unify(want, got, bind)
			continue
		}
		c.checkExpr(slots[i], want)
	}
	if sig.Variadic {
		for i := len(sig.Params); i < len(x.Args); i++ {
			c.infer(x.Args[i])
		}
	}
	// Bounds are checked here, once the arguments have said what each
	// type parameter is. This is the half the *caller* sees; the body
	// was already checked once against the bounds at its declaration.
	c.checkBounds(sig, bind, at)
	ret := types.Subst(sig.Ret, bind)
	c.requireBound(sig, bind, ret, at)
	return c.erase(ret)
}

// requireBound reports a call whose type parameters the arguments
// could not determine and no expectation supplied — `Box.new()`, where
// nothing says what a Box of. Erasing to Unknown silently was the M4b
// behaviour, and it means a later `add(1)` then `add("s")` both pass.
//
// Rust's answer to the same call is "type annotations needed", and it
// is the right one here for the same reason: the alternative is
// inferring T from a *later* statement, which needs a constraint store
// this checker deliberately does not have (DESIGN.md: no
// cross-function unification, no occurs check). An annotation costs
// one line and says what the erasure was hiding.
func (c *checker) requireBound(sig *types.Func, bind map[string]types.Type, ret types.Type, at source.Span) {
	if !hasVar(ret) {
		return
	}
	// Walked off the *return type* rather than sig.TypeParams: an
	// associated function takes its parameters from the impl header
	// (`impl Box<T> { fn new() -> Box<T> }`), so its own TypeParams
	// list is empty and the T is the Named's. This is the same
	// condition `erase` keys on, one step earlier — the free variables
	// it is about to replace with Unknown.
	loose := freeVars(ret, bind, c.tparams)
	if len(loose) == 0 {
		return
	}
	sort.Strings(loose)
	c.errf(at, "cannot tell what %s is in %s here — annotate the binding", list(loose), ret)
}

// freeVars collects the type parameters in t that neither the call
// bound nor the enclosing declaration has in scope — the ones erase
// would silently turn into Unknown.
func freeVars(t types.Type, bind map[string]types.Type, inScope map[string]*types.Var) []string {
	seen := map[string]bool{}
	var walk func(types.Type)
	walk = func(t types.Type) {
		switch x := t.(type) {
		case *types.Var:
			if _, bound := bind[x.Name]; bound {
				return
			}
			if _, lexical := inScope[x.Name]; lexical {
				return
			}
			seen[x.Name] = true
		case *types.App:
			for _, a := range x.Args {
				walk(a)
			}
		case *types.Named:
			for _, a := range x.Args {
				walk(a)
			}
		case *types.Tuple:
			for _, e := range x.Elems {
				walk(e)
			}
		case *types.Func:
			for _, p := range x.Params {
				walk(p.Type)
			}
			walk(x.Ret)
		}
	}
	walk(t)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	return out
}

// mentions reports whether a type parameter appears anywhere in t.
func mentions(t types.Type, name string) bool {
	switch x := t.(type) {
	case *types.Var:
		return x.Name == name
	case *types.App:
		return mentionsAny(x.Args, name)
	case *types.Named:
		return mentionsAny(x.Args, name)
	case *types.Tuple:
		return mentionsAny(x.Elems, name)
	case *types.Func:
		for _, p := range x.Params {
			if mentions(p.Type, name) {
				return true
			}
		}
		return mentions(x.Ret, name)
	}
	return false
}

func mentionsAny(ts []types.Type, name string) bool {
	for _, t := range ts {
		if mentions(t, name) {
			return true
		}
	}
	return false
}

// erase replaces type parameters the call site could not bind with
// Unknown. `Tree.new()` returns `Tree<T>` where T is the impl's
// parameter, and at a call site outside that impl there is nothing
// for T to mean — inferring it from later use is call-site inference,
// which is M4c. Until then the honest answer is "a Tree of something",
// not a leaked variable that would then fail to equal anything.
//
// A variable that *is* lexically in scope survives: inside a generic
// body, T is a real type and erasing it would throw away checking.
func (c *checker) erase(t types.Type) types.Type {
	switch x := t.(type) {
	case *types.Var:
		if _, inScope := c.tparams[x.Name]; inScope {
			return x
		}
		return types.Unknown
	case *types.App:
		return &types.App{C: x.C, Args: c.eraseAll(x.Args)}
	case *types.Named:
		if len(x.Args) == 0 {
			return x
		}
		return x.Instantiate(c.eraseAll(x.Args))
	case *types.Tuple:
		return &types.Tuple{Elems: c.eraseAll(x.Elems)}
	}
	return t
}

func (c *checker) eraseAll(ts []types.Type) []types.Type {
	out := make([]types.Type, len(ts))
	for i, t := range ts {
		out[i] = c.erase(t)
	}
	return out
}

// argSlots places each argument at its parameter's index, applying
// named arguments and leaving nil where a default fills in. It
// reports arity and naming problems; a nil slot for a parameter with
// no default is one of them.
func (c *checker) argSlots(sig *types.Func, x *ast.Call, at source.Span) []ast.Expr {
	slots := make([]ast.Expr, len(sig.Params))
	npos := len(x.Args)
	for i, n := range x.Names {
		if n != "" {
			npos = i
			break
		}
	}
	// Only a declared function's signature carries parameter names, so
	// only a declared function can take named arguments.
	if npos < len(x.Args) && sig.Decl == nil {
		c.inferArgs(x)
		c.errf(at, "named arguments work on declared functions and methods, not closures, builtins, and function values")
		return nil
	}
	if !sig.Variadic && len(x.Args) > len(sig.Params) {
		c.inferArgs(x)
		c.errf(at, "%s takes %d argument(s), got %d", callee(sig), len(sig.Params), len(x.Args))
		return nil
	}
	for i := 0; i < npos && i < len(slots); i++ {
		slots[i] = x.Args[i]
	}
	filled := map[string]bool{}
	for i := npos; i < len(x.Args); i++ {
		name := ""
		if i < len(x.Names) {
			name = x.Names[i]
		}
		idx := -1
		for j, prm := range sig.Params {
			if prm.Name == name && name != "" {
				idx = j
			}
		}
		if idx < 0 {
			c.infer(x.Args[i])
			c.errf(span(x.Args[i]), "%s has no parameter %q", callee(sig), name)
			continue
		}
		if slots[idx] != nil || filled[name] {
			c.infer(x.Args[i])
			c.errf(span(x.Args[i]), "parameter %q is given twice", name)
			continue
		}
		filled[name] = true
		slots[idx] = x.Args[i]
	}
	for i, prm := range sig.Params {
		if slots[i] != nil || prm.HasDefault {
			continue
		}
		// Same wording the evaluator uses. A message that changed
		// only because the *stage* moved would read as a different
		// diagnostic to anyone who has seen the old one.
		if prm.Name != "" {
			c.errf(at, "%s is missing its %q argument", callee(sig), prm.Name)
		} else {
			c.errf(at, "%s takes %d argument(s), got %d", callee(sig), len(sig.Params), len(x.Args))
		}
	}
	return slots
}

func callee(sig *types.Func) string {
	if sig.Name != "" {
		return sig.Name
	}
	return "this call"
}

func (c *checker) inferArgs(x *ast.Call) {
	for _, a := range x.Args {
		c.infer(a)
	}
}

// hasVar reports whether t still mentions an unbound type parameter.
func hasVar(t types.Type) bool {
	switch x := t.(type) {
	case *types.Var:
		return true
	case *types.App:
		return anyVar(x.Args)
	case *types.Named:
		return anyVar(x.Args)
	case *types.Tuple:
		return anyVar(x.Elems)
	case *types.Func:
		for _, p := range x.Params {
			if hasVar(p.Type) {
				return true
			}
		}
		return hasVar(x.Ret)
	}
	return false
}

func anyVar(ts []types.Type) bool {
	for _, t := range ts {
		if hasVar(t) {
			return true
		}
	}
	return false
}

// unify binds the type parameters in declared from the corresponding
// positions in actual. It is one-directional and never fails: a
// mismatch simply teaches it nothing, and the mismatch itself is
// reported by the ordinary assignability check.
//
// This is the whole of Glide's inference. It is bounded by a single
// call because signatures are mandatory — there is no cross-function
// unification, no occurs check, and no solver.
func unify(declared, actual types.Type, bind map[string]types.Type) {
	if declared == nil || actual == nil {
		return
	}
	switch d := declared.(type) {
	case *types.Var:
		// Unknown must not bind — it would poison the parameter for
		// every later use. A type *parameter* must: `outer<T: Ord>`
		// calling `inner<U: Ord>(a)` binds U := T, which is how
		// generic code composes and how the bound on U gets something
		// to check against. Before M4c both were skipped, so passing
		// an unbounded T where an Ord was required went unnoticed.
		if _, done := bind[d.Name]; !done && !types.IsUnknown(actual) {
			bind[d.Name] = types.Default(actual)
		}
	case *types.App:
		if a, ok := actual.(*types.App); ok && a.C == d.C {
			unifyAll(d.Args, a.Args, bind)
		}
	case *types.Named:
		if a, ok := actual.(*types.Named); ok && a.Name == d.Name {
			unifyAll(d.Args, a.Args, bind)
		}
	case *types.Tuple:
		if a, ok := actual.(*types.Tuple); ok {
			unifyAll(d.Elems, a.Elems, bind)
		}
	case *types.Func:
		a, ok := actual.(*types.Func)
		if !ok {
			return
		}
		for i := range d.Params {
			if i < len(a.Params) {
				unify(d.Params[i].Type, a.Params[i].Type, bind)
			}
		}
		unify(d.Ret, a.Ret, bind)
	}
}

func unifyAll(ds, as []types.Type, bind map[string]types.Type) {
	for i := range ds {
		if i < len(as) {
			unify(ds[i], as[i], bind)
		}
	}
}
