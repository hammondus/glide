package interp

import (
	"fmt"

	"glide/internal/ast"
	"glide/internal/source"
)

// The boxing machinery for Error, mirroring option.go's for Option.
//
// `Error` is the erased error type at the *type* level — anything is
// assignable to it, which is what makes `Err("config is empty")` and
// free `?`-propagation work with no `from` to write. It is no longer
// erased at the *value* level: an Error slot holds an *ErrV wrapping
// whatever it was given.
//
// The distinction matters. Type-level erasure is the convenience;
// value-level erasure was an accident of it, and cost the type its
// methods, let a concrete variant be matched straight back out of an
// Error, and made a program-made error print differently from a host
// one. Boxing keeps the convenience and pays none of that: the
// concrete error is still in there, reachable through `find`.

// intoError boxes a value into an Error. **Idempotent** — an *ErrV
// passes through untouched, so a coercion site the checker recorded
// twice, or one where it could not be certain the value was not
// already an Error, is harmless. That is deliberate: the alternative
// is a double-boxed error whose message is the rendering of another
// error, which reads almost right and is very hard to spot.
func intoError(v Value) Value {
	if e, already := v.(*ErrV); already {
		return e
	}
	return &ErrV{Msg: display(v), Held: v}
}

// errIf boxes when the checker recorded a coercion into Error at e.
// The sibling of wrapIf, sharing eval's one chokepoint.
func (in *Interp) errIf(e ast.Expr, v Value) Value {
	if in.info == nil || !in.info.IntoError[e] {
		return v
	}
	return intoError(v)
}

// errorMethod is the Error method set. Small on purpose: a message, a
// link to the next cause, a way to add one, and the typed escape
// hatch. Everything else about an error is the program's business.
func (in *Interp) errorMethod(e *ErrV, name string, args []Value, at source.Span) (Value, bool) {
	switch name {
	case "message":
		nilArgs("message", args, at)
		// This link's own message, not the rendered chain — the chain
		// is what interpolation gives you, and a method that returned
		// it would leave no way to get just this one.
		return StrV(e.Msg), true
	case "cause":
		nilArgs("cause", args, at)
		if e.Cause == nil {
			return NoneV{}, true
		}
		return some(intoError(e.Cause)), true
	case "context":
		msg, ok := one("context", args, at).(StrV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("context takes a String, got %s", typeName(args[0]))})
		}
		return &ErrV{Msg: string(msg), Cause: e}, true
	case "find":
		// The typed escape hatch: walk the cause chain for a concrete
		// error of the named type. Spelled `e.find(ConfigError)` —
		// the type as a *value*, which Glide already has (`Tree.new()`)
		// — rather than DESIGN.md's `find<ConfigError>()`, because
		// `e.find<T>()` cannot be parsed: it reads as a field access
		// followed by `<`. Inventing turbofish for one method costs
		// more than the argument does.
		want, ok := one("find", args, at).(TypeV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("find takes a type, as in e.find(MyError) — got %s", typeName(args[0]))})
		}
		for link := e; link != nil; {
			if link.Held != nil && typeName(link.Held) == string(want) {
				return some(link.Held), true
			}
			next, deeper := link.Cause.(*ErrV)
			if !deeper {
				// A cause that is not itself boxed: the last link, and
				// still worth testing before giving up.
				if link.Cause != nil && typeName(link.Cause) == string(want) {
					return some(link.Cause), true
				}
				break
			}
			link = next
		}
		return NoneV{}, true
	}
	return nil, false
}
