package check

import (
	"sort"
	"strings"

	"glide/internal/ast"
	"glide/internal/types"
)

// Match exhaustiveness.
//
// The rule is the checker's standing one: report only when certain.
// Everything this file cannot analyse counts as *covered*, so a match
// is rejected only when a case is definitely unhandled, and no working
// program can be rejected by a gap in the analysis.
//
// Coverage recurses, because top-level-only would reject real code:
//
//	match r {
//	    Ok(resp)              => …
//	    Err(BadInput{ msg })  => …
//	    Err(NotFound{ code }) => …
//	    Err(Store{ cause })   => …
//	}
//
// Those three Err arms together cover Err, and each on its own covers
// none of it — examples/links.gld is exactly this shape, and a
// top-level-only first attempt rejected it. So a case is covered when
// some arm matches it irrefutably, *or* when the sub-patterns
// collected under it are themselves exhaustive over its payload type.
//
// Guards never cover: `Red if hot => …` may not fire, so a match whose
// only Red arm is guarded is not exhaustive — which is what a reader
// expects and what the evaluator already enforces.
//
// Recursion is capped. A self-referential type (`Node?` inside `Node`)
// would otherwise descend forever, and past a few levels the analysis
// stops paying for itself; at the cap everything counts as covered.
const maxDepth = 4

// armPat is one pattern reaching a position, carrying whether the arm
// it came from was guarded.
type armPat struct {
	pat     ast.Pattern
	guarded bool
}

// checkExhaustive reports a match that definitely misses a case, and
// an arm that definitely cannot run.
func (c *checker) checkExhaustive(x *ast.Match, scrut types.Type) {
	c.checkReachable(x)
	pats := make([]armPat, 0, len(x.Arms))
	for i := range x.Arms {
		for _, p := range x.Arms[i].Pats {
			pats = append(pats, armPat{pat: p, guarded: x.Arms[i].Guard != nil})
		}
	}
	missing, decided := c.uncovered(scrut, pats, 0)
	if !decided || len(missing) == 0 {
		return
	}
	if len(missing) == 1 && missing[0] == "_" {
		c.errf(x.Span, "match is not exhaustive: %s has too many values to enumerate, so it needs a `_` arm",
			types.Default(scrut))
		return
	}
	c.errf(x.Span, "match is not exhaustive: %s not handled", list(missing))
}

// uncovered returns the cases of t the patterns leave unhandled.
// decided is false when the analysis cannot judge the position at all,
// in which case the caller says nothing.
func (c *checker) uncovered(t types.Type, pats []armPat, depth int) (missing []string, decided bool) {
	if depth >= maxDepth {
		return nil, false
	}
	// An unguarded irrefutable pattern covers the whole position.
	for _, ap := range pats {
		if !ap.guarded && irrefutable(ap.pat) {
			return nil, true
		}
	}
	names, finite, known := c.casesOf(t)
	if !known {
		return nil, false
	}
	if !finite {
		return []string{"_"}, true // too many values, and no catch-all above
	}
	// Group the sub-patterns arriving under each case.
	full := map[string]bool{}
	subs := map[string][]armPat{}
	for _, ap := range pats {
		if ap.guarded {
			continue
		}
		name, args, analysable := destructure(ap.pat)
		if !analysable {
			return nil, false // an unmodelled pattern: judge nothing here
		}
		switch {
		case len(args) == 0:
			full[name] = true
		case len(args) != 1:
			// A multi-argument variant would need a product analysis.
			// Counting it as fully covering is the safe direction.
			full[name] = true
		default:
			subs[name] = append(subs[name], armPat{pat: args[0]})
		}
	}
	for _, n := range names {
		if full[n] {
			continue
		}
		under, ok := subs[n]
		if !ok {
			missing = append(missing, n)
			continue
		}
		payload, hasPayload := c.payloadOf(t, n)
		if !hasPayload {
			continue // cannot see inside: assume this case is covered
		}
		inner, ok := c.uncovered(payload, under, depth+1)
		if !ok {
			continue
		}
		if len(inner) > 0 {
			missing = append(missing, n+"("+list(inner)+")")
		}
	}
	return missing, true
}

// casesOf enumerates a type's top-level cases. finite is false for a
// type with too many values to list (Int, String, a struct); known is
// false when the checker cannot see the type at all.
func (c *checker) casesOf(t types.Type) (names []string, finite, known bool) {
	if types.IsOpaque(t) {
		return nil, false, false
	}
	// Not through Base: a distinct type's cases are its own, not its
	// base type's. `match id { NoteId(n) => … }` is total.
	switch x := t.(type) {
	case *types.Named:
		vs := x.AllVariants()
		if len(vs) == 0 {
			// A struct or a distinct type has exactly one case: itself.
			// `User{ name, role, age }` and `NoteId(n)` are total, and
			// treating them as un-enumerable demanded a `_` arm that
			// nothing could ever reach.
			return []string{x.Name}, true, true
		}
		for _, v := range vs {
			names = append(names, v.Name)
		}
		return names, true, true
	case *types.App:
		switch x.C {
		case types.Option:
			return []string{"Some", "None"}, true, true
		case types.Result:
			return []string{"Ok", "Err"}, true, true
		}
		return nil, false, true
	case *types.Basic:
		if x == types.Bool {
			return []string{"true", "false"}, true, true
		}
		return nil, false, true
	}
	return nil, false, false
}

// payloadOf is the type carried by one case of t: Option's Some,
// Result's Ok and Err, and a sum type's single-payload variants.
func (c *checker) payloadOf(t types.Type, name string) (types.Type, bool) {
	switch x := types.Base(t).(type) {
	case *types.App:
		switch {
		case x.C == types.Option && name == "Some":
			return x.Elem(), true
		case x.C == types.Result && name == "Ok":
			return x.Arg(0), true
		case x.C == types.Result && name == "Err":
			return x.Arg(1), true
		}
	case *types.Named:
		if v, ok := x.Variant(name); ok && len(v.Fields) == 1 {
			return v.Fields[0].Type, true
		}
	}
	return nil, false
}

// destructure reads a pattern as "this case, with these sub-patterns".
// analysable is false for a shape this file does not model, which the
// caller turns into silence rather than a guess.
func destructure(p ast.Pattern) (name string, args []ast.Pattern, analysable bool) {
	switch x := p.(type) {
	case *ast.BoolPat:
		if x.V {
			return "true", nil, true
		}
		return "false", nil, true
	case *ast.CtorPat:
		if allIrrefutable(x.Args) {
			return x.Name, nil, true
		}
		return x.Name, x.Args, true
	case *ast.StructPat:
		// All-binding fields make the pattern total for its type:
		// `User{ name, role, age }` always matches a User. A refutable
		// field would need the product analysis multi-argument
		// variants would, so it is left unmodelled — which silences
		// this position in both directions rather than claiming
		// coverage it does not have, or denying coverage it does.
		// `..` says nothing about whether the *listed* fields are
		// refutable: `User{ role: "admin", .. }` has Rest set and is
		// not total, which is what made every arm below it look dead.
		if allFieldsIrrefutable(x.Fields) {
			return x.Type, nil, true
		}
		return "", nil, false
	}
	return "", nil, false
}

// irrefutable reports whether a pattern always matches. Conservative:
// an unmodelled shape is treated as able to fail, so it can only make
// a match look *less* exhaustive, never more.
func irrefutable(p ast.Pattern) bool {
	switch x := p.(type) {
	case *ast.WildPat, *ast.IdentPat:
		return true
	case *ast.TuplePat:
		return allIrrefutable(x.Elems)
	}
	return false
}

func allFieldsIrrefutable(fs []ast.FieldPat) bool {
	for _, f := range fs {
		if !irrefutable(f.Pat) {
			return false
		}
	}
	return true
}

func allIrrefutable(ps []ast.Pattern) bool {
	for _, p := range ps {
		if !irrefutable(p) {
			return false
		}
	}
	return true
}

// checkReachable reports an arm that cannot run because an earlier
// unguarded arm already matches everything it does. Only the two clear
// cases: a catch-all above it, or the same bare constructor above it.
func (c *checker) checkReachable(x *ast.Match) {
	seen := map[string]bool{}
	catchAll := false
	for i := range x.Arms {
		arm := &x.Arms[i]
		dead := catchAll
		if !dead && len(arm.Pats) > 0 {
			dead = true
			for _, p := range arm.Pats {
				name, args, analysable := destructure(p)
				if !analysable || len(args) > 0 || !seen[name] {
					dead = false
					break
				}
			}
		}
		if dead {
			c.errf(arm.Span, "this arm cannot run: everything it matches is handled above")
		}
		if arm.Guard != nil {
			continue
		}
		for _, p := range arm.Pats {
			if irrefutable(p) {
				catchAll = true
				continue
			}
			if name, args, analysable := destructure(p); analysable && len(args) == 0 {
				seen[name] = true
			}
		}
	}
}

// list renders names for a diagnostic: "Blue", "Blue and Green",
// "Blue, Green and Red".
func list(names []string) string {
	sort.Strings(names)
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
