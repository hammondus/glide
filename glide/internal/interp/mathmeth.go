package interp

import (
	"fmt"
	"math"

	"glide/internal/source"
)

// The arithmetic that lives on the numbers: abs, min, max, pow.
//
// The dividing line against the `math` module is *width*. These four
// have to work at all nine numeric types, and as methods the receiver
// binds `Self` through machinery that already runs. As free functions
// there is no receiver, so the checker would have to infer from one
// argument and unify an untyped literal against a later one — which
// it does nowhere else, and which the Glide frontend would then have
// to reproduce exactly. That is a permanent frontend tax to spell it
// `math.abs(n)`.
//
// Not "methods forever": when the operator traits (`Add`, `Mul`) make
// a `Numeric` bound expressible, `math.abs<T: Numeric>` types itself
// with no special case and these can move. Everything Float-*only* —
// sqrt, the rounding family, the classification family — is already in
// `math`, along with the constants a method cannot express at all.
//
// Go's own history is the corroboration: `math.Min` was float64-only
// because Go could not write it generically, and Go 1.21's fix was not
// a generic `math.Min` — it made `min`/`max` universe *builtins*. That
// third option is worse here for a different reason: it would reserve
// `min` and `max` program-wide, and both are common variable names.
//
// f32 is computed at f64 precision, as it already is for `+` and `*`:
// the interpreter stores every float in a float64 and rounds to f32
// only at an `f32(x)` conversion.
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
	return nil, false
}

// mathCall is the `math` module: the Float-only operations, and the
// constants. Both are here for the same reason — neither can be a
// method on a number. A constant has no receiver at all, and the
// Float-only functions do not need one, since there is only ever one
// type involved.
func (in *Interp) mathCall(name string, args []Value, at source.Span) Value {
	if _, isConst := mathConstants[name]; isConst {
		panic(rtErr{at, fmt.Sprintf("math.%s is a constant, not a function — write math.%s", name, name)})
	}
	fn, known := mathFuncs[name]
	if !known {
		panic(rtErr{at, fmt.Sprintf("module math has no function %q", name)})
	}
	x, ok := one("math."+name, args, at).(FloatV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("math.%s takes a Float, got %s (conversion is explicit here: math.%s(Float(n)))",
			name, typeName(args[0]), name)})
	}
	return fn(float64(x))
}

// mathFuncs keeps every entry a one-liner so the table reads as the
// module's surface rather than as code.
var mathFuncs = map[string]func(float64) Value{
	// A negative operand gives NaN rather than trapping: that is the
	// IEEE 754 answer, Float already admits NaN, and `is_nan` is right
	// here to ask. Trapping would make sqrt the one float operation
	// that cannot produce a value its own type has.
	"sqrt":  func(x float64) Value { return FloatV(math.Sqrt(x)) },
	"floor": func(x float64) Value { return FloatV(math.Floor(x)) },
	"ceil":  func(x float64) Value { return FloatV(math.Ceil(x)) },
	// Half away from zero (Go's math.Round), not banker's rounding: it
	// is what "round" means to everyone who has not read a numerics
	// paper, and the money case wants Decimal rather than a second
	// rounding mode bolted onto Float.
	"round":       func(x float64) Value { return FloatV(math.Round(x)) },
	"trunc":       func(x float64) Value { return FloatV(math.Trunc(x)) },
	"is_nan":      func(x float64) Value { return BoolV(math.IsNaN(x)) },
	"is_infinite": func(x float64) Value { return BoolV(math.IsInf(x, 0)) },
	"is_finite":   func(x float64) Value { return BoolV(!math.IsNaN(x) && !math.IsInf(x, 0)) },
}

// mathConstants is what a module could not hold until now: values.
// `pi` cannot be a method on a number, so before modules carried
// values it had nowhere in the language to exist.
var mathConstants = map[string]Value{
	"pi":  FloatV(math.Pi),
	"e":   FloatV(math.E),
	"inf": FloatV(math.Inf(1)),
	// A value, not a test. `x == math.nan` is false by IEEE 754 and
	// always will be — asking is still `math.is_nan(x)`.
	"nan": FloatV(math.NaN()),
}

// moduleValue resolves `math.pi` — a module member reached without
// call parens.
func moduleValue(mod, name string) (Value, bool) {
	if mod != "math" {
		return nil, false
	}
	v, ok := mathConstants[name]
	return v, ok
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
