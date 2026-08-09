// Package check is Glide's type checker: one implementation, shared by
// both tiers.
//
// It is bidirectional and local (Pierce & Turner, 1997-2000), not
// Hindley-Milner: Glide requires signatures on every function, so
// there is nothing to infer across a call boundary and no need for
// unification variables that escape a body. `infer` computes a type
// from an expression; `checkExpr` pushes an expected type inward. Two
// ratified features need the inward direction and cannot be built
// without it — `.Shorthand` variant resolution and the implicit
// T -> T? wrap — which is the argument for bidirectional rather than
// plain bottom-up.
//
// The checker reports a diagnostic only when it is *certain*. Anything
// it does not model yet evaluates to types.Unknown, which is
// compatible with everything, so a partially-built checker can be
// mandatory in both tiers without ever rejecting a working program.
// See LINEAGE.md for the lineage of both choices.
package check

import (
	"fmt"

	"glide/internal/ast"
	"glide/internal/program"
	"glide/internal/types"
)

// The type parameters used in builtin signatures. They are bound by
// substitution against the receiver's type arguments, or by matching
// an argument against the declared parameter type — the same
// machinery user generics use, so there is one inference path and not
// two.
var (
	tvT = &types.Var{Name: "T"}
	tvU = &types.Var{Name: "U"}
	tvK = &types.Var{Name: "K"}
	tvV = &types.Var{Name: "V"}
	// tvSelf stands for the receiver in a signature shared across a
	// family of types — every integer width gets the same methods, and
	// `u8.wrapping_add` must take and return a u8, not an Int.
	tvSelf = &types.Var{Name: "Self"}
)

func p(name string, t types.Type) types.Param { return types.Param{Name: name, Type: t} }
func pd(name string, t types.Type) types.Param {
	return types.Param{Name: name, Type: t, HasDefault: true}
}

// meth builds a method signature taking `self`.
func meth(ret types.Type, params ...types.Param) *types.Func {
	return &types.Func{Self: ast.Self, Params: params, Ret: ret}
}

// methMut builds a method needing `mut self` — the checker uses Self
// to enforce statically what the evaluator enforces at the call
// (`List.push` on an immutable binding).
func methMut(ret types.Type, params ...types.Param) *types.Func {
	return &types.Func{Self: ast.MutSelf, Params: params, Ret: ret}
}

func free(ret types.Type, params ...types.Param) *types.Func {
	return &types.Func{Params: params, Ret: ret}
}

// Shorthands for the types that appear constantly below.
var (
	tErr      = types.Apply(types.Error)
	tDuration = types.Apply(types.Duration)
	tInstant  = types.Apply(types.Instant)
	tRouter   = types.Apply(types.Router)
	tRequest  = types.Apply(types.Request)
	tResponse = types.Apply(types.Response)
	tDb       = types.Apply(types.Db)
	tOutput   = types.Apply(types.Output)
	tStrList  = types.Apply(types.List, types.String)
	// A row from sql.query: column name to a value whose type the
	// database decides. Typed rows arrive with `derive Row`.
	tRow = types.Apply(types.Map, types.String, types.Unknown)
)

func result(ok types.Type) *types.App { return types.Apply(types.Result, ok, tErr) }

// stringMethods, and the other tables below, mirror
// docs/reference/stdlib.md row for row. When a method lands in the
// interpreter it lands here in the same commit; a method the checker
// does not know is reported as unknown, so the tables cannot silently
// fall behind.
var stringMethods = map[string]*types.Func{
	"len":              meth(types.Int),
	"trim":             meth(types.String),
	"trim_prefix":      meth(types.String, p("prefix", types.String)),
	"trim_suffix":      meth(types.String, p("suffix", types.String)),
	"contains":         meth(types.Bool, p("sub", types.String)),
	"starts_with":      meth(types.Bool, p("prefix", types.String)),
	"ends_with":        meth(types.Bool, p("suffix", types.String)),
	"split":            meth(tStrList, p("sep", types.String)),
	"split_whitespace": meth(tStrList),
	"lines":            meth(tStrList),
	"replace":          meth(types.String, p("old", types.String), p("new", types.String)),
	"to_upper":         meth(types.String),
	"to_lower":         meth(types.String),
	"repeat":           meth(types.String, p("k", types.Int)),
	"parse_int":        meth(types.Opt(types.Int)),
	"runes":            meth(types.Apply(types.Iterator, types.Rune)),
	"bytes":            meth(types.Apply(types.Iterator, types.Int)),
	"cmp":              meth(types.Int, p("other", types.String)),
}

// intMethods are shared by every integer width, with Self bound to the
// receiver. The wrapping_* family is the explicit escape from
// trap-on-overflow: `+` traps in every tier, and code that wants
// modular arithmetic — hashes, checksums, PRNGs, wrapping counters —
// says so at the call site (DESIGN.md).
var intMethods = func() map[string]*types.Func {
	m := map[string]*types.Func{
		"cmp":          meth(types.Int, p("other", tvSelf)),
		"wrapping_add": meth(tvSelf, p("other", tvSelf)),
		"wrapping_sub": meth(tvSelf, p("other", tvSelf)),
		"wrapping_mul": meth(tvSelf, p("other", tvSelf)),
		"wrapping_neg": meth(tvSelf),
		"min":          meth(tvSelf, p("other", tvSelf)),
		"max":          meth(tvSelf, p("other", tvSelf)),
		// The exponent counts multiplications, so it is an Int at
		// every receiver width rather than a value of the receiver's
		// type: a u8 raised to the 200th is an overflow, not an
		// unrepresentable exponent.
		"pow": meth(tvSelf, p("exp", types.Int)),
	}
	// The truncating conversions: `n.wrapping_u8()` is the counterpart
	// to `u8(n)`, which traps. Generated from types.Primitives so the
	// method set cannot fall out of step with the type list. `i64` is
	// spelled that way rather than `Int` because the method names the
	// width, and `wrapping_Int` reads like an operation on a value.
	for name, b := range types.Primitives {
		if b.IsInteger() && name != "Int" {
			m["wrapping_"+name] = meth(b)
		}
	}
	return m
}()

// ordMethods is what a type needs to satisfy Ord, and nothing more.
// Rune gets exactly this: the wrapping_* family is integer-only, and
// Rune is deliberately not an integer. Float gets these *plus*
// floatMethods.
var ordMethods = map[string]*types.Func{
	"cmp": meth(types.Int, p("other", tvSelf)),
}

// floatMethods is Ord plus the arithmetic surface Float alone has.
// `sqrt` and the rounding family are Float-only because their integer
// forms would each need a decision the language has not made — does
// `Int.sqrt()` truncate or return a Float? — and explicit conversion
// already spells the answer: `Float(n).sqrt()`.
var floatMethods = func() map[string]*types.Func {
	m := map[string]*types.Func{
		"abs":         meth(tvSelf),
		"min":         meth(tvSelf, p("other", tvSelf)),
		"max":         meth(tvSelf, p("other", tvSelf)),
		"pow":         meth(tvSelf, p("exp", tvSelf)),
		"sqrt":        meth(tvSelf),
		"floor":       meth(tvSelf),
		"ceil":        meth(tvSelf),
		"round":       meth(tvSelf),
		"trunc":       meth(tvSelf),
		"is_nan":      meth(types.Bool),
		"is_infinite": meth(types.Bool),
		"is_finite":   meth(types.Bool),
	}
	for name, sig := range ordMethods {
		m[name] = sig
	}
	return m
}()

// Duration constructors are suffix *properties* on a number, not
// calls: `250.ms`, `0.5.s`. They are looked up as fields, which is
// why they are a set of names rather than signatures.
var durationSuffixes = map[string]bool{
	"ns": true, "us": true, "ms": true, "s": true, "mins": true, "h": true,
}

var ctorMethods = map[types.Ctor]map[string]*types.Func{
	types.List: {
		"len":      meth(types.Int),
		"push":     methMut(types.Unit, p("v", tvT)),
		"sorted":   meth(types.Apply(types.List, tvT)),
		"sort_by":  methMut(types.Unit, p("cmp", free(types.Int, p("a", tvT), p("b", tvT)))),
		"repeat":   meth(types.Apply(types.List, tvT), p("k", types.Int)),
		"join":     meth(types.String, p("sep", types.String)),
		"iter":     meth(types.Apply(types.Iterator, tvT)),
		"contains": meth(types.Bool, p("v", tvT)),
		"index_of": meth(types.Opt(types.Int), p("v", tvT)),
		"first":    meth(types.Opt(tvT)),
		"last":     meth(types.Opt(tvT)),
		"pop":      methMut(types.Opt(tvT)),
		"insert":   methMut(types.Unit, p("i", types.Int), p("v", tvT)),
		"remove":   methMut(tvT, p("i", types.Int)),
		"extend":   methMut(types.Unit, p("other", types.Apply(types.List, tvT))),
		"reversed": meth(types.Apply(types.List, tvT)),
		"slice":    meth(types.Apply(types.List, tvT), p("lo", types.Int), p("hi", types.Int)),
	},
	types.Map: {
		"len":          meth(types.Int),
		"entries":      meth(types.Apply(types.List, &types.Tuple{Elems: []types.Type{tvK, tvV}})),
		"keys":         meth(types.Apply(types.List, tvK)),
		"values":       meth(types.Apply(types.List, tvV)),
		"contains_key": meth(types.Bool, p("k", tvK)),
		"remove":       methMut(types.Opt(tvV), p("k", tvK)),
	},
	types.Output: {
		"status": meth(types.Int),
		"stdout": meth(types.String),
		"stderr": meth(types.String),
		"ok":     meth(types.Bool),
	},
	types.Iterator: {
		"take":      meth(types.Apply(types.Iterator, tvT), p("n", types.Int)),
		"map":       meth(types.Apply(types.Iterator, tvU), p("f", free(tvU, p("v", tvT)))),
		"filter":    meth(types.Apply(types.Iterator, tvT), p("pred", free(types.Bool, p("v", tvT)))),
		"enumerate": meth(types.Apply(types.Iterator, &types.Tuple{Elems: []types.Type{types.Int, tvT}})),
		// zip takes any iterable, which the checker models as Unknown
		// rather than inventing an Iterable trait the language does not
		// have yet. The element type of the result follows.
		"zip":     meth(types.Apply(types.Iterator, &types.Tuple{Elems: []types.Type{tvT, types.Unknown}}), p("other", types.Unknown)),
		"collect": meth(types.Apply(types.List, tvT)),
		"count":   meth(types.Int),
		"sum":     meth(tvT),
	},
	types.Result: {
		// context re-wraps the error, so the error type becomes Error
		// regardless of what it was.
		"context": meth(types.Apply(types.Result, tvT, tErr), p("msg", types.String)),
	},
	types.Sender: {
		"send":  meth(types.Unit, p("v", tvT)),
		"close": meth(types.Unit),
	},
	types.Receiver: {
		"recv": meth(types.Opt(tvT)),
	},
	types.Task: {
		"join": meth(tvT),
	},
	types.Scope: {
		"spawn":    meth(types.Apply(types.Task, tvU), p("f", free(tvU))),
		"deadline": meth(types.Opt(tInstant)),
	},
	types.Router: {
		"get":    methMut(types.Unit, p("pat", types.String), p("h", free(types.Unknown, p("req", tRequest)))),
		"post":   methMut(types.Unit, p("pat", types.String), p("h", free(types.Unknown, p("req", tRequest)))),
		"put":    methMut(types.Unit, p("pat", types.String), p("h", free(types.Unknown, p("req", tRequest)))),
		"delete": methMut(types.Unit, p("pat", types.String), p("h", free(types.Unknown, p("req", tRequest)))),
	},
	types.Request: {
		"path_param": meth(types.Opt(types.String), p("name", types.String)),
		"body":       meth(types.String),
		"method":     meth(types.String),
		"path":       meth(types.String),
	},
	types.Response: {
		"status": meth(types.Int),
		"body":   meth(types.String),
	},
	types.Db: {
		"exec":      meth(result(types.Int), p("q", types.String), pd("params", tRow)),
		"query":     meth(result(types.Apply(types.List, tRow)), p("q", types.String), pd("params", tRow)),
		"query_one": meth(result(types.Opt(tRow)), p("q", types.String), pd("params", tRow)),
		"close":     meth(result(types.Unit)),
	},
}

// modules is the host surface reachable through an import.
var modules = map[string]map[string]*types.Func{
	"os": {
		"args":    free(tStrList),
		"exit":    free(types.Never, p("code", types.Int)),
		"env":     free(types.Opt(types.String), p("name", types.String)),
		"set_env": free(result(types.Unit), p("name", types.String), p("value", types.String)),
		"cwd":     free(result(types.String)),
		"chdir":   free(result(types.Unit), p("path", types.String)),
	},
	"fs": {
		"read_string":   free(result(types.String), p("path", types.String)),
		"write_string":  free(result(types.Unit), p("path", types.String), p("contents", types.String)),
		"append_string": free(result(types.Unit), p("path", types.String), p("contents", types.String)),
		"exists":        free(types.Bool, p("path", types.String)),
		"is_dir":        free(types.Bool, p("path", types.String)),
		"remove":        free(result(types.Unit), p("path", types.String)),
		"remove_all":    free(result(types.Unit), p("path", types.String)),
		"mkdir_all":     free(result(types.Unit), p("path", types.String)),
		"rename":        free(result(types.Unit), p("from", types.String), p("to", types.String)),
		"list_dir":      free(result(tStrList), p("path", types.String)),
		// A List, not a variadic: the language has no variadics, and
		// inventing one for a path helper would be the tail wagging the
		// dog. Moves to `path` when that module lands (STDLIB-GOALS.md).
		"join": free(types.String, p("segments", tStrList)),
	},
	"process": {
		// The argument list is optional: `process.run("date")` should
		// not have to write an empty one.
		"run": free(result(tOutput), p("cmd", types.String), pd("args", tStrList)),
	},
	"json": {
		"encode": free(types.String, p("v", types.Unknown)),
		// A dynamic decode: the value's shape is the document's
		// business until `derive Json` gives it a type.
		"decode": free(result(types.Unknown), p("s", types.String)),
	},
	"http": {
		"router":      free(tRouter),
		"serve":       free(result(types.Unit), p("addr", types.String), p("r", tRouter)),
		"get":         free(result(tResponse), p("url", types.String)),
		"post":        free(result(tResponse), p("url", types.String), p("body", types.String)),
		"text":        free(tResponse, p("s", types.String)),
		"json":        free(tResponse, p("v", types.Unknown)),
		"created":     free(tResponse),
		"bad_request": free(tResponse, p("msg", types.String)),
		"not_found":   free(tResponse),
	},
	"sql": {
		"open": free(result(tDb), p("dsn", types.String)),
	},
	"time": {
		"now":   free(tInstant),
		"sleep": free(types.Unit, p("d", tDuration)),
		"after": free(types.Apply(types.Receiver, types.Unit), p("d", tDuration)),
	},
}

// freeBuiltins are the names callable with no import. Ok, Err, Some
// and channel are absent on purpose: their result depends on the
// expected type, so the checker handles them directly rather than
// pretending they have ordinary signatures.
var freeBuiltins = map[string]*types.Func{
	"print":    free(types.Unit, p("v", types.Unknown)),
	"println":  free(types.Unit, p("v", types.Unknown)),
	"eprint":   free(types.Unit, p("v", types.Unknown)),
	"eprintln": free(types.Unit, p("v", types.Unknown)),
	"expect":   free(types.Unit, p("cond", types.Bool)),
}

// methodHints replace "X has no method Y" where the absence is a
// deliberate design decision rather than an oversight. A checker that
// only ever says "no such method" makes the reader go and find out
// why; these are the cases where the language already knows.
var methodHints = func() map[string]string {
	m := map[string]string{
		"Receiver.close": "only the sender half closes a channel",
		"Receiver.send":  "only the sender half sends; this is the receiving end",
		"Sender.recv":    "only the receiver half receives; this is the sending end",
	}
	// The float-only arithmetic, reached from an integer. `5.sqrt()`
	// is the obvious thing to type, and "Int has no method sqrt" is
	// true but unhelpful: the answer is a conversion, and this
	// language does not perform one behind your back.
	widths := []string{"Int", "i8", "i16", "i32", "u8", "u16", "u32", "u64"}
	for _, name := range []string{"sqrt", "floor", "ceil", "round", "trunc"} {
		for _, recv := range widths {
			m[recv+"."+name] = fmt.Sprintf(
				"%s is defined on Float — write Float(n).%s()", name, name)
		}
	}
	for _, name := range []string{"is_nan", "is_infinite", "is_finite"} {
		for _, recv := range widths {
			m[recv+"."+name] = fmt.Sprintf(
				"%s asks a Float question; an integer is always finite and never NaN", name)
		}
	}
	// `abs` on an unsigned type is the one absence that is a design
	// decision rather than a gap: it would be the identity, and
	// writing it reads like a sign was handled.
	for _, recv := range []string{"u8", "u16", "u32", "u64"} {
		m[recv+".abs"] = recv + " is unsigned, so abs would be the identity — there is no sign to remove"
	}
	return m
}()

// typeCtorName is the constructor name of a built-in type, used to key
// methodHints without the type arguments getting in the way.
func typeCtorName(t types.Type) string {
	if a, ok := t.(*types.App); ok {
		return a.C.String()
	}
	return t.String()
}

// Host is the surface the host provides: what a program may import,
// and what it may not redeclare. Both tiers are handed this same
// value, so a name that collides in one collides in the other — the
// tables above are the single source of truth for both.
//
// `expect` is deliberately absent from the reserved names: it is a
// special form recognised at the call site rather than a binding, and
// it has never been reserved. Reserving it is a one-line change if the
// wart ever bites.
func Host() program.Known {
	bs := make(map[string]bool, len(freeBuiltins)+len(expectedTypeBuiltins))
	for name := range freeBuiltins {
		if name != "expect" {
			bs[name] = true
		}
	}
	for name := range expectedTypeBuiltins {
		bs[name] = true
	}
	// Every spelling of a primitive, including `i64` alongside `Int`.
	// These were shadowable until M4c, which was harmless while a type
	// name meant nothing in expression position — `type Int = struct {
	// … }` was simply unreachable, since resolution tries primitives
	// first. Now that `u8(n)` is a conversion, a program-level `fn
	// u8()` would silently win and the conversion would vanish, so the
	// names are reserved. Locals still shadow, as they do in Go.
	for name := range types.Primitives {
		bs[name] = true
	}
	ms := make(map[string]bool, len(modules))
	for name := range modules {
		ms[name] = true
	}
	return program.Known{Builtins: bs, Modules: ms, Traits: preludeTraits}
}

// builtinMethod finds a method on a built-in receiver, with the
// receiver's type arguments already substituted in. It returns nil
// when the receiver has no methods the checker models, which the
// caller must distinguish from "this receiver has methods but not this
// one" — the second is an error, the first is silence.
func builtinMethod(recv types.Type, name string) (fn *types.Func, modelled bool) {
	switch r := recv.(type) {
	case *types.Basic:
		// Unknown and Never are the two the checker must stay quiet
		// about: one means "not yet known", the other "unreachable".
		if r.IsUnknown() || r.IsNever() {
			return nil, false
		}
		switch {
		case r == types.String:
			return stringMethods[name], true
		case r.IsInteger():
			// Every width, not just Int: a u8 that cannot answer
			// `cmp` or `wrapping_add` is a type you cannot compute
			// with. Self binds to the receiver — defaulted first, so
			// an untyped literal's methods are Int's.
			if name == "abs" && r.IsUnsigned() {
				// Withheld rather than made the identity: an `abs` on
				// an unsigned type reads like a sign was handled when
				// there was never a sign to handle.
				return nil, true
			}
			if name == "abs" {
				return selfBound(meth(tvSelf), r), true
			}
			return selfBound(intMethods[name], r), true
		case r.IsFloat():
			return selfBound(floatMethods[name], r), true
		case r.IsRune():
			return selfBound(ordMethods[name], r), true
		}
		// Bool and () have no methods, and that is a *known* empty
		// set rather than an unmodelled one — the tables above are the
		// whole truth about a primitive. Reporting `false` here was
		// what let Bool satisfy any trait vacuously: conformance saw
		// "not modelled" and stayed silent, so `T: Ord` accepted a
		// Bool and the call died at runtime.
		return nil, true
	case *types.App:
		tab, ok := ctorMethods[r.C]
		if !ok {
			return nil, false
		}
		sig := tab[name]
		if sig == nil {
			return nil, true
		}
		return instantiate(sig, r), true
	}
	return nil, false
}

// selfBound substitutes a primitive receiver for `Self`, defaulting
// first so an untyped literal's methods are its default type's.
func selfBound(sig *types.Func, recv *types.Basic) *types.Func {
	if sig == nil {
		return nil
	}
	f, _ := types.Subst(sig, map[string]types.Type{"Self": types.Default(recv)}).(*types.Func)
	return f
}

// instantiate binds a builtin signature's T/U/K/V from the receiver's
// type arguments. Positional: the first argument is T, the second V
// for a Map (whose first is K), the second E for a Result. Ctors with
// one argument bind T; the two-argument ones are named explicitly so
// the mapping is visible rather than implied.
func instantiate(sig *types.Func, recv *types.App) *types.Func {
	var m map[string]types.Type
	switch recv.C {
	case types.Map:
		m = map[string]types.Type{"K": recv.Arg(0), "V": recv.Arg(1)}
	case types.Result:
		m = map[string]types.Type{"T": recv.Arg(0), "E": recv.Arg(1)}
	default:
		if len(recv.Args) > 0 {
			m = map[string]types.Type{"T": recv.Arg(0)}
		}
	}
	f, _ := types.Subst(sig, m).(*types.Func)
	if f == nil {
		return sig
	}
	return f
}
