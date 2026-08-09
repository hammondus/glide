package types

// Relations between types. Three of them, deliberately separate,
// because conflating them is how type systems grow holes:
//
//   Identical   — the same type. Strict; Unknown matches only Unknown.
//   Compatible  — the same type, modulo Unknown holes. This is
//                 Identical with the checker's "I don't know yet"
//                 wildcard honoured, and it is never a language rule
//                 the programmer can observe.
//   AssignableTo — a value of one type may be used where another is
//                 wanted. This *is* a language rule: it is exactly
//                 Compatible plus untyped-constant conversion plus the
//                 implicit T -> T? wrap, and nothing else. In
//                 particular there are no implicit numeric
//                 conversions (DESIGN.md: "i32 + i64 is a compile
//                 error").

// Identical reports whether a and b are the same type.
func Identical(a, b Type) bool { return same(a, b, false) }

// Compatible reports whether a and b are the same type, treating
// Unknown as a wildcard. The checker uses this so a partially-typed
// expression (a `None` with no expected type, a call into a construct
// the checker does not model yet) does not manufacture errors.
func Compatible(a, b Type) bool { return same(a, b, true) }

func same(a, b Type, wild bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	if wild && (isUnknownT(a) || isUnknownT(b)) {
		return true
	}
	switch x := a.(type) {
	case *Basic:
		return a == b // singletons
	case *App:
		y, ok := b.(*App)
		if !ok || x.C != y.C || len(x.Args) != len(y.Args) {
			return false
		}
		return allSame(x.Args, y.Args, wild)
	case *Named:
		y, ok := b.(*Named)
		// Nominal: the name decides. Two `distinct Int`s with different
		// names are different types, which is the entire reason the
		// declaration exists.
		if !ok || x.Name != y.Name || len(x.Args) != len(y.Args) {
			return false
		}
		return allSame(x.Args, y.Args, wild)
	case *Tuple:
		y, ok := b.(*Tuple)
		if !ok || len(x.Elems) != len(y.Elems) {
			return false
		}
		return allSame(x.Elems, y.Elems, wild)
	case *Func:
		y, ok := b.(*Func)
		if !ok || len(x.Params) != len(y.Params) || x.Self != y.Self ||
			x.Variadic != y.Variadic {
			return false
		}
		for i := range x.Params {
			if !same(x.Params[i].Type, y.Params[i].Type, wild) {
				return false
			}
		}
		return same(ret(x), ret(y), wild)
	case *Var:
		y, ok := b.(*Var)
		return ok && x.Name == y.Name
	case *Module:
		y, ok := b.(*Module)
		return ok && x.Name == y.Name
	case *Meta:
		y, ok := b.(*Meta)
		return ok && same(x.T, y.T, wild)
	}
	return false
}

func allSame(as, bs []Type, wild bool) bool {
	for i := range as {
		if !same(as[i], bs[i], wild) {
			return false
		}
	}
	return true
}

func isUnknownT(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.IsUnknown()
}

// IsUnknown reports whether t is the checker's "cannot tell" type.
// Callers test this before reporting anything: an error about an
// Unknown is an error about the checker, not about the program.
func IsUnknown(t Type) bool { return t == nil || isUnknownT(t) }

// IsOpaque reports whether t is a type the checker must not reason
// about: Unknown, or a type parameter. A `T` inside a generic body is
// not unknown — it is precisely known and precisely useless until
// bound-checking (M4c) says what `T` can do. Until then every "is this
// an error?" guard treats it like Unknown, which is why `value <
// n.value` inside a `T: Ord` function is accepted in silence rather
// than rejected on evidence the checker does not yet gather.
func IsOpaque(t Type) bool {
	if IsUnknown(t) {
		return true
	}
	_, isVar := t.(*Var)
	return isVar
}

// IsNever reports whether t is the type of a diverging expression.
func IsNever(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.IsNever()
}

func ret(f *Func) Type {
	if f.Ret == nil {
		return Unit
	}
	return f.Ret
}

// AssignableTo reports whether a value of type v may be used where dst
// is wanted.
func AssignableTo(v, dst Type) bool {
	if Compatible(v, dst) {
		return true
	}
	if IsNever(v) {
		return true // control never reaches the use
	}
	// Error is the *erased* error type: any value propagates into it.
	// This is what the runtime does (an Err holds whatever it was
	// given) and it is Rust's `Box<dyn Error>` bargain — without it,
	// `fn run() -> Result<T, Error>` would need a hand-written `from`
	// for every error type any callee might raise, which is exactly
	// the ceremony `?` exists to delete. A named error type still
	// converts through `E.from` as designed; Error is the one target
	// that needs no conversion.
	if e, ok := dst.(*App); ok && e.C == Error {
		return true
	}
	if vb, ok := v.(*Basic); ok && vb.IsUntyped() {
		return untypedFits(vb, dst)
	}
	// Implicit T -> T?. One level only: it is a convenience for the
	// overwhelmingly common case, not a subtyping rule, and letting it
	// chain would make Option<Option<T>> unwritable all over again.
	if o, ok := dst.(*App); ok && o.C == Option {
		return AssignableTo(v, o.Elem())
	}
	return false
}

// untypedFits decides where an untyped constant may land. Range is
// checked separately, at the literal, because that is where the
// diagnostic belongs ("300 overflows u8" points at the 300).
func untypedFits(v *Basic, dst Type) bool {
	d, ok := dst.(*Basic)
	if !ok {
		// An untyped constant may still wrap into an Option.
		if o, isOpt := dst.(*App); isOpt && o.C == Option {
			return untypedFits(v, o.Elem())
		}
		return false
	}
	switch {
	case v == UntypedInt:
		// An integer literal is at home in any numeric type: `1.0` and
		// `1` denote the same real number and Glide has no reason to
		// make the programmer spell the difference. It is *not* at home
		// in Rune — DESIGN.md keeps Rune out of the integer lattice on
		// purpose.
		return d.IsNumeric()
	case v == UntypedFloat:
		return d.IsFloat()
	}
	return false
}

// Default is the type an untyped constant takes when nothing forces
// it: `let n = 1` is an Int. Non-constant types are returned unchanged.
func Default(t Type) Type {
	switch t {
	case UntypedInt:
		return Int
	case UntypedFloat:
		return Float
	}
	return t
}

// Numeric reports whether t is a type an explicit conversion can name
// as its target or take as its source: the integer widths, the floats,
// and Rune.
//
// Rune is in the set even though it is deliberately *not* an integer
// alias (DESIGN.md). Excluding it would leave no way to compute a code
// point at all, and the reason Rune is its own type is that it must
// never pass for a count *implicitly* — an explicit `Int(c)` says
// exactly what it means, which is the opposite problem.
func Numeric(t Type) bool {
	b, ok := t.(*Basic)
	return ok && !b.IsUntyped() && (b.IsNumeric() || b.IsRune())
}

// ConvertibleTo reports whether `dst(v)` is defined, where v has type
// src. Conversion is deliberately much narrower than a C cast: it
// covers the numeric lattice and Rune, and nothing else.
//
// String is excluded on purpose. Go's `string(65) == "A"` is the wart
// its own vet tool warns about, and Glide already spells that
// `"{n}"`. Bool was never in it: `Int(true)` is C's legacy, not a
// conversion anyone means.
func ConvertibleTo(src, dst Type) bool {
	if IsOpaque(src) || IsOpaque(dst) {
		return true
	}
	if !Numeric(dst) {
		return false
	}
	if b, ok := src.(*Basic); ok && b.IsUntyped() {
		src = Default(b)
	}
	return Numeric(src)
}

// ValidCodePoint reports whether a magnitude/sign pair is a Unicode
// scalar value — in range and not a surrogate half. It lives here so
// the checker rejects `Rune(-1)` and the evaluator rejects `Rune(n)`
// by the same rule rather than two that are meant to agree.
func ValidCodePoint(mag uint64, neg bool) bool {
	if neg && mag != 0 {
		return false
	}
	return mag <= 0x10FFFF && (mag < 0xD800 || mag > 0xDFFF)
}

// FitsIn reports whether an integer constant fits in b. The constant
// arrives as a magnitude plus a sign, because that is the only
// representation in which both ends of the range are expressible:
// i64's minimum is 2^63, which no int64 holds, and u64's maximum is
// 2^64-1, which no int64 holds either. Returns true for anything that
// is not a sized integer, so callers can ask unconditionally.
//
// Remaining gap: magnitudes are uint64, so a constant *expression*
// that exceeds 64 bits mid-way (`1 << 100`, which DESIGN.md says is
// fine in constant math) still cannot be evaluated. That needs real
// arbitrary-precision constants and arrives with comptime; the range
// check itself is now exact for every type the language has.
func FitsIn(mag uint64, neg bool, b *Basic) bool {
	if !b.IsInteger() || b.IsUntyped() {
		return true
	}
	if b.IsUnsigned() {
		if neg && mag != 0 {
			return false
		}
		if b.bits == 64 {
			return true
		}
		return mag < uint64(1)<<b.bits
	}
	// Signed: the negative side reaches one further than the positive.
	lim := uint64(1) << (b.bits - 1)
	if neg {
		return mag <= lim
	}
	return mag < lim
}

// Join is the type of a construct with two result arms — the branches
// of an `if`, the arms of a `match`, the elements of a list literal.
// It returns nil when the arms genuinely disagree, and the caller
// reports that; it never invents a common supertype, because Glide has
// none (no top type, by decision).
func Join(a, b Type) Type {
	switch {
	case a == nil || b == nil:
		return nil
	case IsUnknown(a), IsNever(a):
		return b
	case IsUnknown(b), IsNever(b):
		return a
	}
	if Compatible(a, b) {
		return merge(a, b)
	}
	// Untyped constants adopt the other arm's type where they can.
	if ab, ok := a.(*Basic); ok && ab.IsUntyped() && untypedFits(ab, b) {
		return b
	}
	if bb, ok := b.(*Basic); ok && bb.IsUntyped() && untypedFits(bb, a) {
		return a
	}
	// `if c { x } else { None }` — one arm optional, the other not.
	if o, ok := a.(*App); ok && o.C == Option && AssignableTo(b, a) {
		return a
	}
	if o, ok := b.(*App); ok && o.C == Option && AssignableTo(a, b) {
		return b
	}
	return nil
}

// merge fills each Unknown hole in a from the corresponding position
// in b. `if c { [] } else { [1] }` should be List<Int>, not
// List<?> — whichever arm knows more wins, position by position.
func merge(a, b Type) Type {
	if IsUnknown(a) {
		return b
	}
	if IsUnknown(b) {
		return a
	}
	switch x := a.(type) {
	case *App:
		y, ok := b.(*App)
		if !ok || x.C != y.C || len(x.Args) != len(y.Args) {
			return a
		}
		return &App{C: x.C, Args: mergeAll(x.Args, y.Args)}
	case *Named:
		y, ok := b.(*Named)
		if !ok || len(x.Args) != len(y.Args) || len(x.Args) == 0 {
			return a
		}
		out := *x
		out.Args = mergeAll(x.Args, y.Args)
		return &out
	case *Tuple:
		y, ok := b.(*Tuple)
		if !ok || len(x.Elems) != len(y.Elems) {
			return a
		}
		return &Tuple{Elems: mergeAll(x.Elems, y.Elems)}
	}
	return a
}

func mergeAll(as, bs []Type) []Type {
	out := make([]Type, len(as))
	for i := range as {
		out[i] = merge(as[i], bs[i])
	}
	return out
}

// Unwrap strips one layer of Option. Reports false if t is not
// optional, so `x?` and `if let` can say so.
func Unwrap(t Type) (Type, bool) {
	if o, ok := t.(*App); ok && o.C == Option {
		return o.Elem(), true
	}
	if IsUnknown(t) {
		return Unknown, true
	}
	return t, false
}

// Base strips a distinct wrapper, and keeps stripping: `type A =
// distinct B` where B is itself distinct. Non-distinct types are
// returned unchanged. Note this is *not* an implicit conversion —
// callers use it only where the language explicitly looks through the
// wrapper (a distinct's own methods, its literal construction).
func Base(t Type) Type {
	for {
		n, ok := t.(*Named)
		if !ok || n.decl().Base == nil {
			return t
		}
		t = Subst(n.decl().Base, n.binding())
	}
}
