package interp

import (
	"fmt"
	"math"
	"strconv"

	"glide/internal/source"
	"glide/internal/types"
)

// Sized-integer arithmetic for i8/i16/i32/u8/u16/u32.
//
// Overflow traps, in every tier, for every integer width. That
// reverses the original "trap in dev, wrap in release" trade
// (DESIGN.md): a program that computes one answer under `glide run`
// and a different one from the compiled binary is drift, and the
// project charter says a difference between the tiers is either
// stated or a bug. The escape hatch is explicit — wrapping_add,
// wrapping_sub, wrapping_mul, wrapping_neg — so modular arithmetic
// is a thing you say rather than a thing that happens to you.

// sizedZero builds the carrier for a Basic, reporting false for the
// integer types that have their own representation (Int, u64) and for
// everything that is not a sized integer.
func sizedZero(b *types.Basic) (SizedV, bool) {
	if b == nil || !b.IsInteger() || b.IsUntyped() || b.Bits() == 64 {
		return SizedV{}, false
	}
	return SizedV{Bits: b.Bits(), Signed: !b.IsUnsigned()}, true
}

func (s SizedV) name() string {
	if s.Signed {
		return fmt.Sprintf("i%d", s.Bits)
	}
	return fmt.Sprintf("u%d", s.Bits)
}

// bounds is the inclusive range of the type. Both ends fit an int64
// comfortably for every width this carrier holds — u32's maximum is
// 4294967295 — which is what makes a single int64 field enough.
func (s SizedV) bounds() (lo, hi int64) {
	if s.Signed {
		return -(int64(1) << (s.Bits - 1)), int64(1)<<(s.Bits-1) - 1
	}
	return 0, int64(1)<<s.Bits - 1
}

func (s SizedV) holds(x int64) bool {
	lo, hi := s.bounds()
	return x >= lo && x <= hi
}

// with range-checks a computed result and traps if it does not fit.
// The message names the escape hatch, because the answer to "my
// checksum keeps trapping" is a different operator, not a different
// build.
func (s SizedV) with(x int64, detail, escape string, at source.Span) SizedV {
	if !s.holds(x) {
		panic(rtErr{at, fmt.Sprintf("%s overflow: %s (use %s for modular arithmetic)", s.name(), detail, escape)})
	}
	return SizedV{Bits: s.Bits, Signed: s.Signed, V: x}
}

// truncate reduces a value to the type's width, two's-complement, the
// way `wrapping_*` and a C cast do. Unsigned is a plain mask; signed
// masks and then sign-extends the top bit back out.
func (s SizedV) truncate(x int64) SizedV {
	mask := int64(1)<<s.Bits - 1
	v := x & mask
	if s.Signed && v > mask>>1 {
		v -= mask + 1
	}
	return SizedV{Bits: s.Bits, Signed: s.Signed, V: v}
}

// sizedBinop is the arithmetic. Operands are at most 32 bits wide, so
// every intermediate is exact in an int64 (signed: 2^31 * 2^31 < 2^63)
// or a uint64 (unsigned: (2^32-1)^2 < 2^64) and the only thing left to
// check is whether the answer fits the declared width.
func sizedBinop(op string, l, r SizedV, at source.Span) Value {
	if l.Bits != r.Bits || l.Signed != r.Signed {
		// Unreachable through the checker — no implicit numeric
		// conversion — so getting here means the checker let a
		// mismatch through. Loud, per DESIGN.md's open question on
		// keeping the dynamic rules as assertions.
		panic(rtErr{at, fmt.Sprintf("operator %s is not defined for %s and %s", op, l.name(), r.name())})
	}
	switch op {
	case "<":
		return BoolV(l.V < r.V)
	case "<=":
		return BoolV(l.V <= r.V)
	case ">":
		return BoolV(l.V > r.V)
	case ">=":
		return BoolV(l.V >= r.V)
	}
	switch op {
	case "+":
		return l.with(l.V+r.V, fmt.Sprintf("%d + %d", l.V, r.V), "wrapping_add", at)
	case "-":
		return l.with(l.V-r.V, fmt.Sprintf("%d - %d", l.V, r.V), "wrapping_sub", at)
	case "*":
		return l.with(l.V*r.V, fmt.Sprintf("%d * %d", l.V, r.V), "wrapping_mul", at)
	case "/":
		if r.V == 0 {
			panic(rtErr{at, "division by zero"})
		}
		// The one signed division that overflows: the minimum divided
		// by -1 has no positive counterpart.
		return l.with(l.V/r.V, fmt.Sprintf("%d / %d", l.V, r.V), "wrapping_neg", at)
	case "%":
		if r.V == 0 {
			panic(rtErr{at, "division by zero"})
		}
		return SizedV{Bits: l.Bits, Signed: l.Signed, V: l.V % r.V}
	}
	panic(rtErr{at, fmt.Sprintf("operator %s is not defined for %s and %s", op, l.name(), r.name())})
}

// wrappingBinop is the explicit modular counterpart, reached only
// through the wrapping_* methods. It is defined for every integer
// width, which is why it takes Values rather than a SizedV pair. A nil
// return means the operands were not a matching integer pair; the
// caller turns that into a positioned error.
func wrappingBinop(op string, l, r Value) Value {
	switch x := l.(type) {
	case IntV:
		y, ok := r.(IntV)
		if !ok {
			return nil
		}
		switch op {
		case "+":
			return IntV(uint64(x) + uint64(y))
		case "-":
			return IntV(uint64(x) - uint64(y))
		case "*":
			return IntV(uint64(x) * uint64(y))
		}
	case UintV:
		y, ok := r.(UintV)
		if !ok {
			return nil
		}
		switch op {
		case "+":
			return x + y
		case "-":
			return x - y
		case "*":
			return x * y
		}
	case SizedV:
		y, ok := r.(SizedV)
		if !ok || x.Bits != y.Bits || x.Signed != y.Signed {
			return nil
		}
		switch op {
		case "+":
			return x.truncate(x.V + y.V)
		case "-":
			return x.truncate(x.V - y.V)
		case "*":
			return x.truncate(x.V * y.V)
		}
	}
	return nil
}

// numericValue reads any integer/float/Rune value as an exact integer
// magnitude plus a sign, or as a float. Conversions all funnel through
// this so there is one place that knows how each carrier stores its
// value, rather than an N×N table of pairs.
func numericValue(v Value) (mag uint64, neg bool, f float64, isFloat, ok bool) {
	switch x := v.(type) {
	case IntV:
		if x < 0 {
			// Negated as unsigned, because i64's minimum has no
			// positive counterpart to subtract from.
			return -uint64(x), true, 0, false, true
		}
		return uint64(x), false, 0, false, true
	case UintV:
		return uint64(x), false, 0, false, true
	case SizedV:
		if x.V < 0 {
			return uint64(-x.V), true, 0, false, true
		}
		return uint64(x.V), false, 0, false, true
	case RuneV:
		return uint64(x), false, 0, false, true
	case FloatV:
		return 0, false, float64(x), true, true
	}
	return 0, false, 0, false, false
}

// convert is `dst(v)`, the explicit numeric conversion.
//
// Out of range traps rather than truncating, which is the one place
// this deliberately parts company with Go: `uint8(300)` is 44 there,
// silently. A language whose `+` traps at the declared width cannot
// then hand you 44 for the same overflow spelled as a conversion.
// `n.wrapping_u8()` is the truncating form, named.
//
// Float to integer truncates toward zero — dropping a fraction is not
// overflow, and `/` already truncates the same way.
func convert(dst *types.Basic, v Value, at source.Span) Value {
	mag, neg, f, isFloat, ok := numericValue(v)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("cannot convert %s to %s (conversion is defined between numbers and Rune only)", typeName(v), dst)})
	}
	if dst.IsFloat() {
		if isFloat {
			if dst == types.F32 {
				return FloatV(float32(f))
			}
			return FloatV(f)
		}
		g := float64(mag)
		if neg {
			g = -g
		}
		if dst == types.F32 {
			return FloatV(float32(g))
		}
		return FloatV(g)
	}
	// Integer or Rune target. A float source truncates toward zero
	// first, then has to survive the same range check as any integer.
	if isFloat {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			panic(rtErr{at, fmt.Sprintf("cannot convert %s to %s", render(v, false), dst)})
		}
		t := math.Trunc(f)
		if t < -9.223372036854776e18 || t >= 9.223372036854776e18 {
			panic(rtErr{at, fmt.Sprintf("%s overflow: converting %s", dst, render(v, false))})
		}
		n := int64(t)
		neg = n < 0
		if neg {
			mag = uint64(-n)
		} else {
			mag = uint64(n)
		}
	}
	if dst.IsRune() {
		// Stricter than Go, and deliberately: Rune is its own type
		// precisely so an invalid code point cannot masquerade as one.
		if !types.ValidCodePoint(mag, neg) {
			panic(rtErr{at, fmt.Sprintf("%s is not a Unicode code point", signedMag(mag, neg))})
		}
		return RuneV(rune(mag))
	}
	if !types.FitsIn(mag, neg, dst) {
		panic(rtErr{at, fmt.Sprintf("%s overflow: %s does not fit (use wrapping_%s to truncate)",
			dst, signedMag(mag, neg), dst)})
	}
	return buildInt(dst, mag, neg)
}

// buildInt makes the value for an in-range magnitude at the target
// width, choosing the carrier the width uses.
func buildInt(dst *types.Basic, mag uint64, neg bool) Value {
	if dst == types.U64 {
		return UintV(mag)
	}
	n := int64(mag)
	if neg {
		n = -int64(mag)
	}
	if s, sized := sizedZero(dst); sized {
		s.V = n
		return s
	}
	return IntV(n)
}

// signedMag renders a magnitude/sign pair, which exists for the one
// value that has no int64 to be formatted from: i64's minimum.
func signedMag(mag uint64, neg bool) string {
	if neg && mag != 0 {
		return "-" + strconv.FormatUint(mag, 10)
	}
	return strconv.FormatUint(mag, 10)
}

// wrappingConvert is `n.wrapping_u8()`: the truncating counterpart to
// `u8(n)`, two's-complement, never trapping. It is the operation the
// byte-and-hash work sized integers exist for actually needs, and it
// only exists between integer types — truncating a float is not a
// wrap, and widening never loses anything.
func wrappingConvert(dst *types.Basic, v Value) (Value, bool) {
	mag, neg, _, isFloat, ok := numericValue(v)
	if !ok || isFloat {
		return nil, false
	}
	bits := mag
	if neg {
		bits = -mag
	}
	if dst == types.U64 {
		return UintV(bits), true
	}
	if s, sized := sizedZero(dst); sized {
		return s.truncate(int64(bits)), true
	}
	return IntV(int64(bits)), true
}

var wrappingOps = map[string]string{
	"wrapping_add": "+",
	"wrapping_sub": "-",
	"wrapping_mul": "*",
}

// intMethod serves every integer width from one place, because the
// checker's intMethods table serves every width from one place —
// keeping the two in step is the point of having a single table on
// each side. It reports handled=false for a name it does not know, so
// the caller still produces the ordinary "no method" error.
//
// Float and Rune arrive here too, for `cmp` alone: they satisfy Ord
// and nothing else, matching check.ordMethods. The wrapping_* family
// rejects them below, because wrappingConvert and wrappingBinop have
// no case for a float and Rune is deliberately not an integer.
func intMethod(recv Value, name string, args []Value, at source.Span) (Value, bool) {
	switch name {
	case "cmp":
		o := one("cmp", args, at)
		if typeName(o) != typeName(recv) {
			panic(rtErr{at, fmt.Sprintf("%s.cmp compares against another %s, got %s", typeName(recv), typeName(recv), typeName(o))})
		}
		return IntV(builtinCmp(recv, o, at)), true
	case "wrapping_add", "wrapping_sub", "wrapping_mul":
		o := one(name, args, at)
		out := wrappingBinop(wrappingOps[name], recv, o)
		if out == nil {
			panic(rtErr{at, fmt.Sprintf("%s.%s takes another %s, got %s", typeName(recv), name, typeName(recv), typeName(o))})
		}
		return out, true
	case "wrapping_neg":
		if len(args) != 0 {
			panic(rtErr{at, "wrapping_neg takes no arguments"})
		}
		return wrappingNeg(recv), true
	}
	// wrapping_u8, wrapping_i32, … — the truncating conversions.
	if dst, ok := wrappingTargets[name]; ok {
		if len(args) != 0 {
			panic(rtErr{at, fmt.Sprintf("%s takes no arguments", name)})
		}
		out, converted := wrappingConvert(dst, recv)
		if !converted {
			panic(rtErr{at, fmt.Sprintf("%s has no method %q", typeName(recv), name)})
		}
		return out, true
	}
	return nil, false
}

// wrappingTargets is the truncating-conversion method set, keyed by
// method name. Built from types.Primitives so it cannot fall out of
// step with the type list, and integer-only: truncating a float is
// not a wrap, it is `Int(f)`.
var wrappingTargets = func() map[string]*types.Basic {
	m := map[string]*types.Basic{}
	for name, b := range types.Primitives {
		if b.IsInteger() && name != "Int" {
			m["wrapping_"+name] = b
		}
	}
	return m
}()

// wrappingNeg is unary minus without the trap: the modular negation of
// a type's minimum is itself.
func wrappingNeg(v Value) Value {
	switch x := v.(type) {
	case IntV:
		return IntV(-uint64(x))
	case UintV:
		return -x
	case SizedV:
		return x.truncate(-x.V)
	}
	return nil
}

// negateChecked is unary `-`, trapping. Unsigned types have no
// negation at all beyond zero, which is a range error rather than a
// missing operator — `-x` on a u8 is a sensible thing to write and a
// wrong thing to mean.
func negateChecked(v Value, at source.Span) (Value, bool) {
	switch x := v.(type) {
	case IntV:
		if x == math.MinInt64 {
			panic(rtErr{at, "Int overflow: negating the minimum Int (use wrapping_neg for modular arithmetic)"})
		}
		return -x, true
	case UintV:
		if x != 0 {
			panic(rtErr{at, fmt.Sprintf("u64 overflow: negating %d (u64 has no negative values; use wrapping_neg for modular arithmetic)", uint64(x))})
		}
		return x, true
	case SizedV:
		return x.with(-x.V, fmt.Sprintf("negating %d", x.V), "wrapping_neg", at), true
	case FloatV:
		return -x, true
	}
	return nil, false
}
