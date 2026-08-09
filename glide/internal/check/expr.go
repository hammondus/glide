package check

import (
	"strconv"

	"glide/internal/ast"
	"glide/internal/source"
	"glide/internal/types"
)

// infer synthesises an expression's type with no expectation.
func (c *checker) infer(e ast.Expr) types.Type { return c.typeOf(e, nil) }

// checkExpr pushes an expected type inward and then verifies the
// result. This is the checking half of bidirectional checking; the
// inward push is what makes `.Shorthand` resolvable and what lets a
// literal land in a sized type.
func (c *checker) checkExpr(e ast.Expr, want types.Type) types.Type {
	if want == nil {
		return c.infer(e)
	}
	if hasVar(want) {
		// The expectation still mentions an unbound type parameter, so
		// it is worth pushing inward (a closure learns its parameter
		// types from it) but not worth asserting: comparing a concrete
		// type against a variable that is about to be bound *to* it
		// would fail every time.
		return c.typeOf(e, want)
	}
	got := c.typeOf(e, want)
	if types.AssignableTo(got, want) {
		c.rangeCheck(e, want)
		c.settle(e, got, want)
		return got
	}
	if types.IsOpaque(got) || types.IsOpaque(want) {
		return got
	}
	c.errf(span(e), "expected %s, found %s", want, types.Default(got))
	// Return the expectation, not the mismatch: one wrong argument
	// should produce one diagnostic, not a cascade down the
	// expression it sits in.
	return want
}

// rangeCheck rejects a literal that does not fit the type it landed
// in — DESIGN.md: `let x: u8 = 300` is a compile error, not a wrap.
// The diagnostic points at the literal, which is where the fix is.
func (c *checker) rangeCheck(e ast.Expr, want types.Type) {
	mag, neg, ok := constInt(e)
	if !ok {
		return
	}
	b, ok := types.Base(want).(*types.Basic)
	if !ok {
		if o, isOpt := want.(*types.App); isOpt && o.C == types.Option {
			c.rangeCheck(e, o.Elem())
		}
		return
	}
	if !types.FitsIn(mag, neg, b) {
		c.errf(span(e), "%s does not fit in %s", signed(mag, neg), b)
	}
}

// constInt reads an integer constant as a magnitude and a sign,
// seeing through a unary minus — `-129` is a negation of the literal
// 129 in the tree, and the range check has to see what was written.
func constInt(e ast.Expr) (mag uint64, neg bool, ok bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.V, false, true
	case *ast.Unary:
		if x.Op == "-" {
			if m, n, found := constInt(x.X); found {
				return m, !n, true
			}
		}
	}
	return 0, false, false
}

// signed renders a magnitude/sign pair for a diagnostic. It exists
// because the one value that matters most here — i64's minimum — has
// no int64 to be formatted from.
func signed(mag uint64, neg bool) string {
	s := strconv.FormatUint(mag, 10)
	if neg {
		return "-" + s
	}
	return s
}

// settle records the type an untyped constant landed in. The node's
// own type is "untyped integer", which is true and useless to a
// backend: it has to know that the 5 in `let f: Float = 5` is a
// Float and the 5 in `let n: u64 = 5` is a u64. Recording it here —
// rather than rewriting the node — keeps the AST purely syntactic.
//
// Both of those were wrong before M4b, silently: `let f: Float = 5`
// built an integer, so `f / 2` did integer division and answered 2.
func (c *checker) settle(e ast.Expr, got, want types.Type) {
	gb, ok := got.(*types.Basic)
	if !ok || !gb.IsUntyped() {
		return
	}
	if wb, ok := want.(*types.Basic); ok && !wb.IsUntyped() {
		c.info.Types[e] = wb
	}
}

func (c *checker) typeOf(e ast.Expr, want types.Type) types.Type {
	t := c.typeOfRaw(e, want)
	if t == nil {
		t = types.Unknown
	}
	if !types.IsOpaque(t) {
		c.info.Types[e] = t
	}
	return t
}

func (c *checker) typeOfRaw(e ast.Expr, want types.Type) types.Type {
	switch x := e.(type) {
	case *ast.IntLit:
		return types.UntypedInt
	case *ast.FloatLit:
		return types.UntypedFloat
	case *ast.BoolLit:
		return types.Bool
	case *ast.RuneLit:
		return types.Rune
	case *ast.UnitLit:
		return types.Unit

	case *ast.StrLit:
		for i := range x.Parts {
			if x.Parts[i].IsExpr {
				c.infer(x.Parts[i].E)
			}
		}
		return types.String

	case *ast.IdentExpr:
		return c.ident(x, want)

	case *ast.DotName:
		// Returned as *types.Named, which may be nil — assigning a
		// typed nil into the Type interface would produce a non-nil
		// interface holding a nil pointer, and the next method call on
		// it is a crash rather than an error.
		if n := c.shorthand(x, want, nil); n != nil {
			return n
		}
		return types.Unknown

	case *ast.Binary:
		return c.binary(x)

	case *ast.Unary:
		return c.unary(x)

	case *ast.RangeExpr:
		c.checkExpr(x.Lo, types.Int)
		c.checkExpr(x.Hi, types.Int)
		return types.Apply(types.Range)

	case *ast.TupleLit:
		wants := tupleElems(want, len(x.Elems))
		if want == nil {
			wants = make([]types.Type, len(x.Elems))
		}
		elems := make([]types.Type, len(x.Elems))
		for i, el := range x.Elems {
			got := c.checkExpr(el, wants[i])
			// Where a slot has an expectation and the element meets
			// it, that expectation is the element's type — the same
			// rule listLit already applies. Defaulting instead would
			// make `(250, -300)` an (Int, Int) that then fails to be
			// the (u8, i16) each of its elements had just satisfied.
			if wants[i] != nil && !types.IsUnknown(wants[i]) && types.AssignableTo(got, wants[i]) {
				elems[i] = wants[i]
				continue
			}
			elems[i] = types.Default(got)
		}
		return &types.Tuple{Elems: elems}

	case *ast.ListLit:
		return c.listLit(x, want)

	case *ast.MapLit:
		return c.mapLit(x, want)

	case *ast.Spread:
		// A spread's own type is its element type; the list literal
		// that contains it does the joining.
		return c.elemType(c.infer(x.E), x.E)

	case *ast.StructLit:
		return c.structLit(x)

	case *ast.Call:
		return c.call(x, want)

	case *ast.Field:
		return c.field(x)

	case *ast.Index:
		return c.index(x)

	case *ast.TupleIndex:
		t := c.infer(x.X)
		if tup, ok := t.(*types.Tuple); ok {
			if x.N < len(tup.Elems) {
				return tup.Elems[x.N]
			}
			c.errf(x.Span, "%s has %d elements; .%d is out of range", t, len(tup.Elems), x.N)
			return types.Unknown
		}
		if !types.IsOpaque(t) {
			c.errf(x.Span, ".%d requires a tuple, found %s", x.N, t)
		}
		return types.Unknown

	case *ast.Try:
		return c.try(x)

	case *ast.Closure:
		return c.closure(x, want)

	case *ast.BlockExpr:
		return c.block(x.Body, want)

	case *ast.If:
		return c.ifExpr(x, want)

	case *ast.IfLet:
		return c.ifLet(x, want)

	case *ast.Match:
		return c.match(x, want)

	case *ast.CondMatch:
		return c.condMatch(x, want)

	case *ast.ScopeExpr:
		return c.scopeExpr(x, want)

	case *ast.SelectExpr:
		return c.selectExpr(x, want)
	}
	return types.Unknown
}

// Names

func (c *checker) ident(x *ast.IdentExpr, want types.Type) types.Type {
	switch x.Name {
	case "_":
		c.errf(x.Span, "_ discards; it cannot be read")
		return types.Unknown
	case "None":
		// A bare None takes its element type from the expectation:
		// `let x: Int? = None` is an Option<Int>, not an Option<?>.
		if o, ok := want.(*types.App); ok && o.C == types.Option {
			return o
		}
		return types.Opt(types.Unknown)
	}
	if b := c.lookup(x.Name); b != nil {
		return b.t
	}
	if t, ok := c.consts[x.Name]; ok {
		return t
	}
	if f, ok := c.fns[x.Name]; ok {
		return f
	}
	if c.tab.Imports[x.Name] {
		return &types.Module{Name: x.Name}
	}
	if n, ok := c.named[x.Name]; ok {
		return &types.Meta{T: n}
	}
	// A primitive numeric type's name is callable as a conversion:
	// `u8(n)`, `Float(k)`, `Int(c)`. Reached after the local lookup on
	// purpose — a `let u8 = 5` shadows it, exactly as a local shadows
	// a predeclared identifier in Go, and the failure is the loud
	// "Int is not callable" rather than a silent reinterpretation.
	//
	// Every primitive resolves here, not just the convertible ones, so
	// that `String(65)` reports what is actually wrong with it rather
	// than claiming `String` is an undefined name.
	if b, ok := types.Primitives[x.Name]; ok {
		return &types.Meta{T: b}
	}
	if vi, ok := c.tab.Variants[x.Name]; ok {
		c.errf(x.Span, "variants are namespaced: write .%s or %s.%s (bare variant names are pattern-only)",
			x.Name, vi.Type, x.Name)
		return types.Unknown
	}
	if f, ok := freeBuiltins[x.Name]; ok {
		return f
	}
	if expectedTypeBuiltins[x.Name] {
		// Ok/Err/Some/channel have no signature of their own — they
		// are typed at the call site from the expected type.
		return types.Unknown
	}
	c.errf(x.Span, "undefined name %q", x.Name)
	return types.Unknown
}

var expectedTypeBuiltins = map[string]bool{
	"Ok": true, "Err": true, "Some": true, "channel": true,
}

// shorthand resolves `.Variant` against the expected type. M1-M3
// resolved these in a file-wide namespace because there was no
// expected type to resolve them in; this is the ratified behaviour
// (docs/reference/language.md) finally implemented.
//
// owner, when non-nil, is an explicit namespace (`Colour.Green`).
func (c *checker) shorthand(x *ast.DotName, want types.Type, owner *types.Named) *types.Named {
	if owner == nil {
		owner, _ = types.Base(stripOption(want)).(*types.Named)
	}
	if owner != nil {
		if v, ok := owner.Variant(x.Name); ok {
			c.info.Shorthand[x] = v
			return owner
		}
		c.errf(x.Span, "%s has no variant %q", owner.Name, x.Name)
		return nil
	}
	// No expectation to resolve in. Falling back to the file-wide
	// variant index keeps every program that ran under M1-M3 running,
	// and variant names are file-unique, so the answer is the same
	// one — it is only the *justification* that is weaker.
	vi, ok := c.tab.Variants[x.Name]
	if !ok {
		c.errf(x.Span, "no variant named %q in scope", x.Name)
		return nil
	}
	n := c.named[vi.Type]
	if n != nil {
		if v, found := n.Variant(x.Name); found {
			c.info.Shorthand[x] = v
		}
	}
	return n
}

func stripOption(t types.Type) types.Type {
	if o, ok := t.(*types.App); ok && o.C == types.Option {
		return o.Elem()
	}
	return t
}

// cond checks something that must be a Bool and says which construct
// wanted one. "expected Bool, found Int" is true but anonymous; the
// reader of a long `if` wants to be told it was the condition.
func (c *checker) cond(e ast.Expr, what string) {
	t := c.typeOf(e, types.Bool)
	if !types.AssignableTo(t, types.Bool) && !types.IsOpaque(t) {
		c.errf(span(e), "%s condition must be Bool, got %s", what, types.Default(t))
	}
}

// Operators

func (c *checker) binary(x *ast.Binary) types.Type {
	switch x.Op {
	case "&&", "||":
		c.boolOperand(x.L, x.Op)
		c.boolOperand(x.R, x.Op)
		return types.Bool
	case "??":
		l := c.infer(x.L)
		inner := types.Type(types.Unknown)
		if app, ok := l.(*types.App); ok && (app.C == types.Option || app.C == types.Result) {
			inner = app.Arg(0)
		} else if !types.IsOpaque(l) {
			c.errf(span(x.L), "?? needs an Option or a Result on the left, found %s", l)
		}
		r := c.checkExpr(x.R, inner)
		if types.IsOpaque(inner) {
			return types.Default(r)
		}
		return inner
	}
	l := c.infer(x.L)
	r := c.infer(x.R)
	// An untyped operand adopts the other side's type: in `a - 1`
	// where a is a u64, the 1 is a u64 too. Without this the literal
	// stays "untyped integer" in Info and the evaluator builds it at
	// the default width, which is a type the other operand refuses.
	if j, ok := types.Default(types.Join(l, r)).(*types.Basic); ok {
		c.settle(x.L, l, j)
		c.rangeCheck(x.L, j)
		c.settle(x.R, r, j)
		c.rangeCheck(x.R, j)
	}
	return c.binaryResult(x.Op, l, r, x.Span)
}

// binaryResult is the operator table. It is shared with compound
// assignment, so `xs[i] += 1` and `xs[i] + 1` cannot disagree.
func (c *checker) binaryResult(op string, l, r types.Type, at source.Span) types.Type {
	if types.IsOpaque(l) || types.IsOpaque(r) {
		if isComparison(op) {
			return types.Bool
		}
		return types.Unknown
	}
	switch op {
	case "==", "!=":
		// Structural equality, but only between types that could ever
		// be equal. Comparing an Int with a String is a bug the
		// evaluator reports as "not comparable"; here it is caught
		// whether or not the line runs.
		if types.Join(l, r) == nil {
			c.errf(at, "%s and %s can never be equal", l, r)
		}
		return types.Bool
	}
	if t := c.timeOp(op, l, r); t != nil {
		return t
	}
	j := types.Join(l, r)
	b, _ := j.(*types.Basic)
	if b != nil {
		switch op {
		case "+":
			if b.IsNumeric() || b == types.String {
				return j
			}
		case "-", "*", "/":
			if b.IsNumeric() {
				return j
			}
		case "%":
			// Float has no remainder operator, matching the evaluator
			// and Go: `%` on floats is a C-ism nobody wants.
			if b.IsInteger() {
				return j
			}
		case "<", "<=", ">", ">=":
			if b.IsOrdered() {
				return types.Bool
			}
		}
	}
	c.errf(at, "operator %s is not defined for %s and %s", op, l, r)
	if isComparison(op) {
		return types.Bool
	}
	return types.Unknown
}

func (c *checker) boolOperand(e ast.Expr, op string) {
	t := c.typeOf(e, types.Bool)
	if !types.AssignableTo(t, types.Bool) && !types.IsOpaque(t) {
		c.errf(span(e), "%s requires Bool, got %s", op, types.Default(t))
	}
}

func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// timeOp is the ratified Duration/Instant arithmetic — the exceptions
// to "both sides the same type". Returns nil when neither side is a
// time type, so the ordinary rules apply.
func (c *checker) timeOp(op string, l, r types.Type) types.Type {
	ld, li := isCtor(l, types.Duration), isCtor(l, types.Instant)
	rd, ri := isCtor(r, types.Duration), isCtor(r, types.Instant)
	if !ld && !li && !rd && !ri {
		return nil
	}
	isInt := func(t types.Type) bool {
		b, ok := t.(*types.Basic)
		return ok && b.IsInteger()
	}
	switch {
	case ld && rd:
		switch op {
		case "+", "-":
			return l
		case "<", "<=", ">", ">=":
			return types.Bool
		}
	case ld && isInt(r):
		if op == "*" || op == "/" {
			return l
		}
	case isInt(l) && rd:
		if op == "*" {
			return r // commutative convenience, per the stdlib reference
		}
	case li && ri:
		switch op {
		case "-":
			return tDuration
		case "<", "<=", ">", ">=":
			return types.Bool
		}
	case li && rd:
		if op == "+" || op == "-" {
			return l
		}
	}
	return nil
}

func isCtor(t types.Type, c types.Ctor) bool {
	a, ok := t.(*types.App)
	return ok && a.C == c
}

func (c *checker) unary(x *ast.Unary) types.Type {
	switch x.Op {
	case "!":
		c.checkExpr(x.X, types.Bool)
		return types.Bool
	case "-":
		t := c.infer(x.X)
		if b, ok := t.(*types.Basic); ok && !b.IsNumeric() {
			c.errf(x.Span, "unary - is not defined for %s", t)
			return types.Unknown
		}
		return t
	}
	return c.infer(x.X)
}

// Containers

func (c *checker) listLit(x *ast.ListLit, want types.Type) types.Type {
	elemWant := types.Type(nil)
	if app, ok := want.(*types.App); ok && app.C == types.List {
		elemWant = app.Elem()
	}
	elem := types.Type(types.Unknown)
	for _, el := range x.Elems {
		t := c.checkExpr(el, elemWant)
		if elemWant != nil {
			continue
		}
		if j := types.Join(elem, t); j != nil {
			elem = j
		} else {
			c.errf(span(el), "list elements are %s, but this one is %s",
				types.Default(elem), types.Default(t))
		}
	}
	if elemWant != nil {
		return types.Apply(types.List, elemWant)
	}
	return types.Apply(types.List, types.Default(elem))
}

func (c *checker) mapLit(x *ast.MapLit, want types.Type) types.Type {
	var keyWant, valWant types.Type
	if app, ok := want.(*types.App); ok && app.C == types.Map {
		keyWant, valWant = app.Arg(0), app.Arg(1)
	}
	key, val := types.Type(types.Unknown), types.Type(types.Unknown)
	for i := range x.Keys {
		kt := c.checkExpr(x.Keys[i], keyWant)
		vt := c.checkExpr(x.Vals[i], valWant)
		if keyWant == nil {
			if j := types.Join(key, kt); j != nil {
				key = j
			} else {
				c.errf(span(x.Keys[i]), "map keys are %s, but this one is %s",
					types.Default(key), types.Default(kt))
			}
		}
		if valWant == nil {
			if j := types.Join(val, vt); j != nil {
				val = j
			} else {
				c.errf(span(x.Vals[i]), "map values are %s, but this one is %s",
					types.Default(val), types.Default(vt))
			}
		}
	}
	if keyWant != nil {
		return types.Apply(types.Map, keyWant, valWant)
	}
	return types.Apply(types.Map, types.Default(key), types.Default(val))
}

func (c *checker) structLit(x *ast.StructLit) types.Type {
	n := c.named[x.Type]
	if n != nil && n.Fields != nil {
		return c.structFields(x, n)
	}
	// A named-field variant wears the same syntax: NotFound{ id: 7 }.
	if vi, ok := c.tab.Variants[x.Type]; ok {
		owner := c.named[vi.Type]
		if owner != nil {
			if v, found := owner.Variant(x.Type); found {
				c.variantFields(x, v, owner)
				return owner
			}
		}
		return types.Unknown
	}
	for i := range x.Vals {
		c.infer(x.Vals[i])
	}
	if n == nil {
		c.errf(x.Span, "unknown type %q", x.Type)
	} else {
		c.errf(x.Span, "%s is not a struct type", x.Type)
	}
	return types.Unknown
}

// structFields checks a struct literal and, for a generic type,
// works out what it was instantiated at. `Node{ value: 1, … }` is a
// Node<Int> because its `value` field is declared `T`; the same
// literal inside `impl Tree<T>` is a Node<T>, because there T is a
// real type parameter in scope and not something to be guessed.
func (c *checker) structFields(x *ast.StructLit, n *types.Named) types.Type {
	bind := map[string]types.Type{}
	for _, v := range n.Params {
		if lexical, inScope := c.tparams[v.Name]; inScope {
			bind[v.Name] = lexical
		}
	}
	if x.Base != nil {
		bt := c.infer(x.Base)
		if bn, ok := bt.(*types.Named); ok && bn.Name == n.Name {
			for i, v := range n.Params {
				if _, done := bind[v.Name]; !done && i < len(bn.Args) {
					bind[v.Name] = bn.Args[i]
				}
			}
		} else if !types.IsOpaque(bt) {
			c.errf(span(x.Base), "..base must be a %s, found %s", n.Name, bt)
		}
	}
	given := map[string]bool{}
	for i, name := range x.Names {
		f, ok := n.Field(name)
		if !ok {
			c.infer(x.Vals[i])
			c.errf(span(x.Vals[i]), "%s has no field %q", n.Name, name)
			continue
		}
		given[name] = true
		want := types.Subst(f.Type, bind)
		got := c.checkExpr(x.Vals[i], want)
		if hasVar(want) {
			unify(want, got, bind)
		}
	}
	// Mandatory initialisation: every field accounted for, by the
	// literal or by ..base. No zero values, ever.
	if x.Base == nil {
		for _, f := range n.Fields {
			if !given[f.Name] {
				c.errf(x.Span, "missing field %q in %s literal (no zero values)", f.Name, n.Name)
			}
		}
	}
	if len(n.Params) == 0 {
		return n
	}
	args := make([]types.Type, len(n.Params))
	for i, v := range n.Params {
		args[i] = types.Unknown
		if t, ok := bind[v.Name]; ok {
			args[i] = t
		}
	}
	return n.Instantiate(args)
}

func (c *checker) variantFields(x *ast.StructLit, v *types.Variant, owner *types.Named) {
	if x.Base != nil {
		c.errf(x.Span, "..base is for structs; %s is a variant of %s", x.Type, owner.Name)
	}
	given := map[string]bool{}
	for i, name := range x.Names {
		found := false
		for _, f := range v.Fields {
			if f.Name == name {
				c.checkExpr(x.Vals[i], f.Type)
				found, given[name] = true, true
			}
		}
		if !found {
			c.infer(x.Vals[i])
			c.errf(span(x.Vals[i]), "%s has no field %q", x.Type, name)
		}
	}
	for _, f := range v.Fields {
		if !given[f.Name] {
			c.errf(x.Span, "%s is missing field %q (no zero values — every field is named)", x.Type, f.Name)
		}
	}
}

// Access

func (c *checker) field(x *ast.Field) types.Type {
	recv := c.infer(x.X)
	switch r := types.Base(recv).(type) {
	case *types.Named:
		if f, ok := r.Field(x.Name); ok {
			return f.Type
		}
		// A named-field variant's fields are readable through the sum
		// type: `e.id` on an ApiError. Statically only the sum is
		// known, so the answer is the field's type when every variant
		// that has it agrees, and Unknown when they disagree — never
		// a guess, and never a complaint about a field that exists.
		if t, found := variantField(r, x.Name); found {
			return t
		}
	case *types.Basic:
		// Duration suffix properties: `250.ms`, `0.5.s`.
		if r.IsNumeric() && durationSuffixes[x.Name] {
			return tDuration
		}
	}
	if m, ok := recv.(*types.Meta); ok {
		if n, isNamed := m.T.(*types.Named); isNamed {
			if _, found := n.Variant(x.Name); found {
				return n
			}
			if fn := c.methods[n.Name][x.Name]; fn != nil {
				return fn
			}
		}
		return types.Unknown
	}
	if types.IsOpaque(recv) {
		return types.Unknown
	}
	// A method used as a value: not supported by the evaluator, so
	// nothing is claimed about it here either.
	if fn, _ := c.methodOf(recv, x.Name); fn != nil {
		return types.Unknown
	}
	if _, isMod := recv.(*types.Module); isMod {
		return types.Unknown
	}
	c.errf(x.Span, "%s has no field %q", recv, x.Name)
	return types.Unknown
}

// variantField looks a field name up across every variant of a sum
// type.
func variantField(n *types.Named, name string) (types.Type, bool) {
	var found types.Type
	for _, v := range n.Variants {
		for _, f := range v.Fields {
			if f.Name != name {
				continue
			}
			if found == nil {
				found = f.Type
			} else if !types.Identical(found, f.Type) {
				return types.Unknown, true
			}
		}
	}
	return found, found != nil
}

func (c *checker) index(x *ast.Index) types.Type {
	recv := c.infer(x.X)
	switch r := recv.(type) {
	case *types.App:
		switch r.C {
		case types.List:
			c.checkExpr(x.I, types.Int)
			return r.Elem()
		case types.Map:
			c.checkExpr(x.I, r.Arg(0))
			// An absent key is None, which is why map reads pair with
			// `??` rather than panicking.
			return types.Opt(r.Arg(1))
		}
	case *types.Basic:
		if r == types.String {
			c.infer(x.I)
			c.errf(x.Span, "strings have no index operator: use bytes(), runes(), or explicit byte-offset slicing")
			return types.Unknown
		}
	}
	c.infer(x.I)
	if types.IsOpaque(recv) {
		return types.Unknown
	}
	c.errf(x.Span, "%s cannot be indexed", recv)
	return types.Unknown
}

// try checks `x?`: unwrap an Ok, or propagate the Err to the caller —
// converting it first if the enclosing function's error type differs
// and declares a `from`.
func (c *checker) try(x *ast.Try) types.Type {
	t := c.infer(x.X)
	app, ok := t.(*types.App)
	if !ok || app.C != types.Result {
		if !types.IsOpaque(t) {
			c.errf(x.Span, "? requires a Result, found %s", t)
		}
		return types.Unknown
	}
	c.checkPropagation(app.Arg(1), x.Span)
	return app.Arg(0)
}

// checkPropagation verifies the error type can reach the caller. This
// retires interp.resultErrType's string slicing: the target error type
// is now a resolved type, so a generic error type no longer silently
// fails to match its own method table.
func (c *checker) checkPropagation(errType types.Type, at source.Span) {
	want, ok := c.ret.(*types.App)
	if !ok || want.C != types.Result {
		if !types.IsOpaque(c.ret) {
			c.errf(at, "? propagates an error, but this function returns %s, not a Result", c.ret)
		}
		return
	}
	target := want.Arg(1)
	if types.AssignableTo(errType, target) || types.IsOpaque(errType) || types.IsOpaque(target) {
		return
	}
	// Conversion at the propagation point: Rust's From, in the
	// trait-less associated-fn form until traits are verified.
	if n, isNamed := target.(*types.Named); isNamed {
		if from := c.methods[n.Name]["from"]; from != nil && from.Self == ast.NoSelf {
			if len(from.Params) == 1 && types.AssignableTo(errType, from.Params[0].Type) {
				return
			}
		}
	}
	c.errf(at, "cannot propagate %s: this function returns %s, and %s has no `from` that accepts it",
		errType, c.ret, target)
}

func (c *checker) closure(x *ast.Closure, want types.Type) types.Type {
	sig, _ := want.(*types.Func)
	c.pushFn()
	defer c.pop()
	params := make([]types.Param, len(x.Params))
	for i, name := range x.Params {
		t := types.Type(types.Unknown)
		if sig != nil && i < len(sig.Params) {
			t = sig.Params[i].Type
		}
		params[i] = types.Param{Name: name, Type: t}
		c.declare(name, t, false, x.Span)
	}
	var retWant types.Type
	if sig != nil {
		retWant = sig.Ret
	}
	savedRet := c.ret
	if retWant != nil {
		c.ret = retWant
	}
	var ret types.Type
	switch {
	case x.BodyExpr != nil:
		ret = c.typeOf(x.BodyExpr, retWant)
	default:
		ret = c.block(x.BodyBlock, retWant)
	}
	// The closure's own diagnostic, rather than the generic
	// "expected X, found Y": a mismatch here is about what the
	// closure returns, and the caller (filter, sort_by) is the one
	// that cares.
	if retWant != nil && !hasVar(retWant) && !types.AssignableTo(ret, retWant) &&
		!types.IsOpaque(ret) && !types.IsOpaque(retWant) {
		c.errf(closureBodySpan(x), "this closure must return %s, got %s", retWant, types.Default(ret))
	}
	c.ret = savedRet
	return &types.Func{Params: params, Ret: types.Default(ret)}
}

func closureBodySpan(x *ast.Closure) source.Span {
	if x.BodyExpr != nil {
		return span(x.BodyExpr)
	}
	return tailSpan(x.BodyBlock)
}

// Control flow. Each of these is an expression, so each produces the
// join of its arms — and reports when the arms genuinely disagree.

func (c *checker) ifExpr(x *ast.If, want types.Type) types.Type {
	c.cond(x.Cond, "if")
	then := c.block(x.Then, want)
	switch {
	case x.ElseIf != nil:
		return c.joinArms(then, c.typeOf(x.ElseIf, want), want, x.Span)
	case x.ElseBlock != nil:
		return c.joinArms(then, c.block(x.ElseBlock, want), want, x.Span)
	}
	// No else: the construct is a statement, and its value is unit.
	// "value-position if requires else" is a rule about where the if
	// sits, which the parser knows and the checker does not need to
	// re-derive.
	return types.Unit
}

func (c *checker) ifLet(x *ast.IfLet, want types.Type) types.Type {
	scrut := c.infer(x.X)
	c.push()
	// `if let` is a one-armed match, not an Option unwrapper: the
	// pattern decides what it is matching. A *binding* pattern
	// (`if let root = self.root`) is the Option-unwrapping form —
	// nothing else would be refutable. A constructor pattern
	// (`if let Err(Timeout) = r`) matches the scrutinee as written.
	subject := scrut
	if _, binds := x.Pat.(*ast.IdentPat); binds {
		inner, ok := types.Unwrap(scrut)
		if !ok && !types.IsOpaque(scrut) {
			c.errf(span(x.X), "`if let name = …` unwraps an Option, but this is %s", scrut)
		}
		subject = inner
	}
	c.bind(x.Pat, subject)
	then := c.block(x.Then, want)
	c.pop()
	switch {
	case x.ElseIf != nil:
		return c.joinArms(then, c.typeOf(x.ElseIf, want), want, x.Span)
	case x.ElseBlock != nil:
		return c.joinArms(then, c.block(x.ElseBlock, want), want, x.Span)
	}
	return types.Unit
}

func (c *checker) match(x *ast.Match, want types.Type) types.Type {
	scrut := c.infer(x.X)
	result := types.Type(types.Unknown)
	for i := range x.Arms {
		arm := &x.Arms[i]
		c.push()
		for _, pat := range arm.Pats {
			c.bind(pat, scrut)
		}
		if arm.Guard != nil {
			c.cond(arm.Guard, "match guard")
		}
		t := c.typeOf(arm.Body, want)
		c.pop()
		result = c.joinArms(result, t, want, arm.Span)
	}
	return result
}

func (c *checker) condMatch(x *ast.CondMatch, want types.Type) types.Type {
	result := types.Type(types.Unknown)
	for i := range x.Arms {
		arm := &x.Arms[i]
		if arm.Cond != nil {
			c.cond(arm.Cond, "match arm")
		}
		result = c.joinArms(result, c.typeOf(arm.Body, want), want, arm.Span)
	}
	return result
}

// joinArms combines two arms of a branching expression. With an
// expectation in hand, both arms were already checked against it and
// the expectation is the answer; without one, the arms have to agree
// among themselves.
func (c *checker) joinArms(a, b, want types.Type, at source.Span) types.Type {
	if want != nil {
		return want
	}
	if j := types.Join(a, b); j != nil {
		return j
	}
	c.errf(at, "these branches produce different types: %s and %s",
		types.Default(a), types.Default(b))
	return types.Unknown
}

func (c *checker) scopeExpr(x *ast.ScopeExpr, want types.Type) types.Type {
	// "must be a Duration" rather than "expected Duration": units are
	// mandatory here and `timeout: 5` is the mistake worth naming.
	if x.Timeout != nil {
		if t := c.typeOf(x.Timeout, tDuration); !types.AssignableTo(t, tDuration) && !types.IsOpaque(t) {
			c.errf(span(x.Timeout), "a scope timeout must be a Duration, got %s (write 5.s, not 5)", types.Default(t))
		}
	}
	if x.Deadline != nil {
		if t := c.typeOf(x.Deadline, tInstant); !types.AssignableTo(t, tInstant) && !types.IsOpaque(t) {
			c.errf(span(x.Deadline), "a scope deadline must be an Instant, got %s", types.Default(t))
		}
	}
	c.push()
	if x.Handle != "" {
		c.declare(x.Handle, types.Apply(types.Scope), false, x.Span)
	}
	// A timeout scope evaluates to Result<T, Timeout>, so the body's
	// expectation is the Ok side, not the whole thing.
	bodyWant := want
	timed := x.Timeout != nil || x.Deadline != nil
	if timed {
		if app, ok := want.(*types.App); ok && app.C == types.Result {
			bodyWant = app.Arg(0)
		} else {
			bodyWant = nil
		}
	}
	body := c.block(x.Body, bodyWant)
	c.pop()
	if timed {
		return types.Apply(types.Result, body, c.named["Timeout"])
	}
	return body
}

func (c *checker) selectExpr(x *ast.SelectExpr, want types.Type) types.Type {
	result := types.Type(types.Unknown)
	for i := range x.Arms {
		arm := &x.Arms[i]
		c.push()
		switch {
		case arm.Else:
		case arm.SendVal != nil:
			ch := c.infer(arm.Ch)
			elem := types.Type(nil)
			if app, ok := ch.(*types.App); ok && app.C == types.Sender {
				elem = app.Elem()
			}
			c.checkExpr(arm.SendVal, elem)
		default:
			ch := c.infer(arm.Ch)
			elem := types.Type(types.Unknown)
			if app, ok := ch.(*types.App); ok && app.C == types.Receiver {
				elem = app.Elem()
			}
			// A recv arm's pattern matches the Option the receive
			// produces, not the element: `None` is end-of-stream.
			c.bind(arm.Pat, types.Opt(elem))
		}
		if arm.Guard != nil {
			c.cond(arm.Guard, "select guard")
		}
		t := c.typeOf(arm.Body, want)
		c.pop()
		result = c.joinArms(result, t, want, arm.Span)
	}
	return result
}
