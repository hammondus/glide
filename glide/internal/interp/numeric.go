package interp

import (
	"fmt"
	"math"

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
func intMethod(recv Value, name string, args []Value, at source.Span) (Value, bool) {
	switch name {
	case "cmp":
		o := one("cmp", args, at)
		if typeName(o) != typeName(recv) {
			panic(rtErr{at, fmt.Sprintf("%s.cmp compares against another %s, got %s", typeName(recv), typeName(recv), typeName(o))})
		}
		switch {
		case naturalLess(recv, o, at):
			return IntV(-1), true
		case naturalLess(o, recv, at):
			return IntV(1), true
		}
		return IntV(0), true
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
	return nil, false
}

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
