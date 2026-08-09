package check

import (
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
		return c.dotCall(fn, x)
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
func (c *checker) dotCall(f *ast.Field, x *ast.Call) types.Type {
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
		return c.checkArgs(sig, x, x.Span)
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

	sig, modelled := c.methodOf(recv, f.Name)
	if sig == nil {
		c.inferArgs(x)
		if modelled && !types.IsOpaque(recv) {
			if hint, ok := methodHints[typeCtorName(recv)+"."+f.Name]; ok {
				c.errf(x.Span, "%s", hint)
			} else {
				c.errf(x.Span, "%s has no method %q", recv, f.Name)
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

// methodOf finds a method on a receiver. modelled reports whether the
// checker knows this receiver's method set at all: false means
// silence, because complaining about a method on a type the checker
// does not model would be a false positive.
func (c *checker) methodOf(recv types.Type, name string) (sig *types.Func, modelled bool) {
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
	slots := c.argSlots(sig, x, at)
	bind := map[string]types.Type{}
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
	return c.erase(types.Subst(sig.Ret, bind))
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
		if _, done := bind[d.Name]; !done && !types.IsOpaque(actual) {
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
