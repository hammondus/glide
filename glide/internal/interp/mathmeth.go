package interp

import (
	"fmt"
	"math"

	"glide/internal/source"
)

// The arithmetic method set: abs, min, max, pow on every numeric type,
// plus the float-only rounding and classification family.
//
// **Methods, not a `math` module.** Glide already hangs `cmp` and the
// `wrapping_*` family off the numeric types; a module would split the
// numeric surface across two places and put an import in front of
// `n.abs()`. Go's `math.Abs` is float-only precisely because Go could
// not hang a method on `int`, and the result is that Go still has no
// integer abs — a warning, not a model. Rust, Swift and Kotlin all put
// these on the number.
//
// No constants (`pi`, `e`, infinity) yet: they have nowhere to live —
// a module here holds functions, not values — and nothing has needed
// one. They arrive with whatever program first wants them.
//
// f32 is computed at f64 precision, as it already is for `+` and `*`:
// the interpreter stores every float in a float64 and rounds to f32
// only at an `f32(x)` conversion. Singling out `sqrt` for a different
// rule would be arbitrary while the operators behave this way.
func (in *Interp) mathMethod(recv Value, name string, args []Value, at source.Span) (Value, bool) {
	switch name {
	case "abs":
		nilArgs("abs", args, at)
		return absValue(recv, at)
	case "min", "max":
		o := one(name, args, at)
		if typeName(o) != typeName(recv) {
			panic(rtErr{at, fmt.Sprintf("%s.%s takes another %s, got %s", typeName(recv), name, typeName(recv), typeName(o))})
		}
		// Ordered by the same comparison `<` and `sorted()` use, so
		// the three can never disagree. For Float that is the *total*
		// order, where NaN sorts after every number: min(nan, 1.0) is
		// 1.0 and max(nan, 1.0) is nan. Rust's f64::min agrees about
		// the first, Go's math.Max about the second; picking one
		// coherent order beats matching either piecemeal.
		c := builtinCmp(recv, o, at)
		if (name == "min") == (c <= 0) {
			return recv, true
		}
		return o, true
	case "pow":
		return in.powValue(recv, args, at)
	}
	f, isFloat := recv.(FloatV)
	if !isFloat {
		return nil, false
	}
	switch name {
	case "sqrt":
		nilArgs("sqrt", args, at)
		// A negative operand gives NaN rather than trapping: that is
		// the IEEE 754 answer, Float already admits NaN, and `is_nan`
		// is right here to ask. Trapping would make sqrt the one float
		// operation that cannot produce a NaN its own type has.
		return FloatV(math.Sqrt(float64(f))), true
	case "floor":
		nilArgs("floor", args, at)
		return FloatV(math.Floor(float64(f))), true
	case "ceil":
		nilArgs("ceil", args, at)
		return FloatV(math.Ceil(float64(f))), true
	case "round":
		nilArgs("round", args, at)
		// Half away from zero (Go's math.Round), not banker's
		// rounding: it is what "round" means to everyone who has not
		// read a numerics paper, and the money case wants Decimal
		// rather than a second rounding mode on Float.
		return FloatV(math.Round(float64(f))), true
	case "trunc":
		nilArgs("trunc", args, at)
		return FloatV(math.Trunc(float64(f))), true
	case "is_nan":
		nilArgs("is_nan", args, at)
		return BoolV(math.IsNaN(float64(f))), true
	case "is_infinite":
		nilArgs("is_infinite", args, at)
		return BoolV(math.IsInf(float64(f), 0)), true
	case "is_finite":
		nilArgs("is_finite", args, at)
		return BoolV(!math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)), true
	}
	return nil, false
}

// absValue traps at a signed type's minimum, which has no positive
// counterpart — the same rule as `-x`, and for the same reason.
// Unsigned types have no `abs` at all: it would be the identity, and
// offering it invites the reader to believe a sign was handled.
func absValue(v Value, at source.Span) (Value, bool) {
	switch x := v.(type) {
	case IntV:
		if x == math.MinInt64 {
			panic(rtErr{at, "Int overflow: abs of the minimum Int has no positive value (use wrapping_neg for modular arithmetic)"})
		}
		if x < 0 {
			return -x, true
		}
		return x, true
	case SizedV:
		if !x.Signed {
			return nil, false
		}
		if x.V < 0 {
			return x.with(-x.V, fmt.Sprintf("abs of %d", x.V), "wrapping_neg", at), true
		}
		return x, true
	case FloatV:
		return FloatV(math.Abs(float64(x))), true
	}
	return nil, false // u64: unsigned, so abs is the identity
}

// powValue is integer and float exponentiation. There is no `**`
// operator (DESIGN.md keeps the operator set closed), so this is the
// only way to raise a number to a power.
//
// The integer form multiplies in a loop through the ordinary checked
// `*`, so it traps at exactly the step that overflows and the message
// names the operands — an exponentiation-by-squaring version would
// report a mid-computation product the caller never wrote.
func (in *Interp) powValue(recv Value, args []Value, at source.Span) (Value, bool) {
	if f, isFloat := recv.(FloatV); isFloat {
		e, ok := one("pow", args, at).(FloatV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("Float.pow takes a Float exponent, got %s", typeName(args[0]))})
		}
		return FloatV(math.Pow(float64(f), float64(e))), true
	}
	switch recv.(type) {
	case IntV, UintV, SizedV:
	default:
		return nil, false
	}
	// The exponent is an Int at every receiver width: it counts
	// multiplications, it is not a value of the receiver's type. A u8
	// raised to the 200th is an overflow, not an unrepresentable
	// exponent.
	e, ok := one("pow", args, at).(IntV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s.pow takes an Int exponent, got %s", typeName(recv), typeName(args[0]))})
	}
	if e < 0 {
		panic(rtErr{at, fmt.Sprintf("pow: exponent %d is negative (an integer raised to a negative power is not an integer; convert to Float first)", e)})
	}
	acc := onesOf(recv)
	for range int64(e) {
		acc = in.binop("*", acc, recv, at)
	}
	return acc, true
}

// onesOf is the multiplicative identity at the receiver's own width,
// so `x.pow(0)` is a 1 of the right type rather than an Int.
func onesOf(v Value) Value {
	switch x := v.(type) {
	case UintV:
		return UintV(1)
	case SizedV:
		return SizedV{Bits: x.Bits, Signed: x.Signed, V: 1}
	}
	return IntV(1)
}
