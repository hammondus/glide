package interp

import (
	"fmt"

	"glide/internal/ast"
	"glide/internal/source"
)

// Option is boxed: every `T?` value is `NoneV` or a `*SomeV`, never a
// bare `T`. Every read goes through readOption, which *asserts* that
// rather than assuming it.
//
// The assertion is the point. Boxing moves the implicit `T -> T?`
// coercion from "free, because the representations are identical" to
// "the checker records each site and the evaluator wraps there", and a
// site the checker fails to record would otherwise silently decide a
// value was `Some` when the program meant `None`. DESIGN.md's standing
// answer for exactly this shape — a rule enforced in two places, where
// the checker is the authority — is that the runtime keeps its copy as
// an assertion, so a checker bug is a loud panic rather than undefined
// behaviour.

// some boxes a value. Every producer of an Option goes through it, so
// grepping `&SomeV{` finds nothing that bypassed the constructor.
func some(v Value) Value { return &SomeV{V: v} }

// readOption destructures a value the checker typed as an Option.
// ok=false means None. A value in neither form is a bug in this
// interpreter or in the checker, never in the Glide program, so it
// says so.
func readOption(v Value, at source.Span) (inner Value, ok bool) {
	switch x := v.(type) {
	case NoneV:
		return nil, false
	case *SomeV:
		return x.V, true
	}
	panic(rtErr{at, fmt.Sprintf(
		"internal: %s reached an Option context unboxed — the checker did not record the T -> T? coercion here",
		typeName(v))})
}

// isNone is readOption where the payload is not wanted.
func isNone(v Value, at source.Span) bool {
	_, ok := readOption(v, at)
	return !ok
}

// wrapIf boxes v when the checker recorded an implicit T -> T?
// coercion at e. This is the whole of the coercion machinery on the
// evaluator's side: one lookup, at every point a value is produced
// from an expression that had an expectation.
func (in *Interp) wrapIf(e ast.Expr, v Value) Value {
	if in.info == nil || !in.info.Wrap[e] {
		return v
	}
	return some(v)
}
