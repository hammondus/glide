package check

import (
	"fmt"
	"sort"

	"glide/internal/ast"
	"glide/internal/source"
	"glide/internal/types"
)

// Trait conformance and generic bound checking.
//
// The rule is DESIGN.md's, and it is Swift's: **conformance is
// declared, satisfaction is structural**. `impl Ord for Blob {}` with
// an empty body is correct when `Blob` already has an inherent `cmp`
// of the right shape — no forwarding boilerplate for methods that
// already match — but the declaration itself is mandatory, so
// "who implements Ord" stays a text search and nothing conforms by
// accident.
//
// Builtins are the one exception, and they have to be: `Int` cannot
// write `impl Ord for Int`. They satisfy a trait structurally, out of
// the same method tables that give them types, which is why
// `max<T: Ord>(1, 2)` works without the prelude declaring anything
// per-type.

// checkConformance verifies every declared `impl Trait for Type`.
// Driven off the impl blocks rather than the TypeTraits index because
// a generic trait's arguments live on the header — `impl Iterable<Int>
// for Bag` is the only place `Int` is written.
func (c *checker) checkConformance() {
	for _, im := range c.file.Impls {
		if im.Trait == "" {
			continue
		}
		tr := c.tab.Traits[im.Trait]
		if tr == nil {
			continue // unknown trait: already reported by program.Load
		}
		self := c.selfType(im.Target)
		// The impl header's `<…>` are resolved in the *target's* scope,
		// so `impl Iterable<T> for Tree<T>` means Tree's own T.
		var args []types.Type
		c.withSelf(self, func() {
			c.withParams(c.paramsOf(im.Target), func() {
				for _, a := range im.TraitArgs {
					args = append(args, c.resolve(a))
				}
			})
		})
		bind := traitBinding(tr, args, self)
		for _, miss := range c.missing(self, im.Trait, bind) {
			c.errf(im.Span, "%s does not satisfy %s: %s", im.Target, im.Trait, miss)
		}
	}
}

// traitBinding maps a trait's `Self` and its own type parameters to
// what this impl says they are. A parameter the header did not supply
// stays Unknown rather than being invented, so a missing argument
// under-reports instead of reporting the wrong thing.
func traitBinding(tr *ast.TraitDecl, args []types.Type, self types.Type) map[string]types.Type {
	m := map[string]types.Type{"Self": self}
	for i, tp := range tr.TypeParams {
		if i < len(args) {
			m[tp.Name] = args[i]
		} else {
			m[tp.Name] = types.Unknown
		}
	}
	return m
}

// missing returns one description per unsatisfied requirement, empty
// when the type conforms. Descriptions rather than a bool because
// "does not satisfy Ord" without saying which method is useless.
func (c *checker) missing(self types.Type, traitName string, bind map[string]types.Type) []string {
	sigs := c.traits[traitName]
	if sigs == nil {
		return nil // unknown trait: already reported by program.Load
	}
	names := make([]string, 0, len(sigs))
	for name := range sigs {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []string
	for _, name := range names {
		want := substFunc(sigs[name], bind)
		got, modelled := c.methodOf(self, name)
		if !modelled {
			return nil // a receiver the checker does not model: stay silent
		}
		if got == nil {
			out = append(out, fmt.Sprintf("missing %s%s", name, sigString(want)))
			continue
		}
		if why := mismatch(got, want); why != "" {
			out = append(out, fmt.Sprintf("%s %s", name, why))
		}
	}
	return out
}

// substFunc applies a binding to a signature, keeping the *Func type.
func substFunc(f *types.Func, bind map[string]types.Type) *types.Func {
	out, _ := types.Subst(f, bind).(*types.Func)
	if out == nil {
		return f
	}
	return out
}

// mismatch compares an implementation against a requirement, returning
// "" when it satisfies. Deliberately not types.Identical on the whole
// Func: the receiver kind (`self` vs `mut self`) is part of the
// contract, and a parameter's *name* is not.
func mismatch(got, want *types.Func) string {
	if got.Self != want.Self {
		return fmt.Sprintf("takes %s, but the trait declares %s", selfWord(got.Self), selfWord(want.Self))
	}
	if len(got.Params) != len(want.Params) {
		return fmt.Sprintf("takes %d argument(s), but the trait declares %d",
			len(got.Params), len(want.Params))
	}
	for i := range got.Params {
		if !types.Compatible(got.Params[i].Type, want.Params[i].Type) {
			return fmt.Sprintf("argument %d is %s, but the trait declares %s",
				i+1, got.Params[i].Type, want.Params[i].Type)
		}
	}
	if !types.Compatible(retOf(got), retOf(want)) {
		return fmt.Sprintf("returns %s, but the trait declares %s", retOf(got), retOf(want))
	}
	return ""
}

func retOf(f *types.Func) types.Type {
	if f.Ret == nil {
		return types.Unit
	}
	return f.Ret
}

func sigString(f *types.Func) string {
	if f == nil {
		return ""
	}
	s := "("
	for i, p := range f.Params {
		if i > 0 {
			s += ", "
		}
		s += p.Type.String()
	}
	return s + ") -> " + retOf(f).String()
}

// conformsTo reports whether t satisfies the named trait. Used at call
// sites, where a bound has to be checked against the type actually
// passed.
func (c *checker) conformsTo(t types.Type, traitName string) bool {
	if types.IsUnknown(t) {
		return true // nothing known: report nothing
	}
	if v, ok := t.(*types.Var); ok {
		// One type parameter satisfying another's bound: `fn f<T: Ord>`
		// calling `g<U: Ord>(x)` with x: T. No transitivity to chase —
		// traits have no supertraits in this design.
		for _, b := range v.Bounds {
			if b == traitName {
				return true
			}
		}
		return false
	}
	if n, ok := t.(*types.Named); ok {
		for _, declared := range c.tab.TypeTraits[n.Name] {
			if declared == traitName {
				return true
			}
		}
		return false
	}
	// A builtin: no `impl` is writable for it, so satisfaction is
	// structural against the same method tables that type it. A
	// generic trait's parameters are unbound here — nothing wrote
	// them — so they compare as Unknown, which is a wildcard.
	tr := c.tab.Traits[traitName]
	if tr == nil {
		return true
	}
	return len(c.missing(t, traitName, traitBinding(tr, nil, t))) == 0
}

// checkBounds verifies the bindings a call site chose against the
// declaration's bounds. This is the half a caller sees: "your T does
// not implement Ord", at the call.
func (c *checker) checkBounds(sig *types.Func, bind map[string]types.Type, at source.Span) {
	for _, tp := range sig.TypeParams {
		arg, bound := bind[tp.Name]
		if !bound || arg == nil || types.IsUnknown(arg) {
			continue
		}
		for _, trName := range tp.Bounds {
			if c.traits[trName] == nil {
				continue // unknown trait: already reported at the declaration
			}
			if !c.conformsTo(arg, trName) {
				c.errf(at, "%s does not implement %s, required by %s",
					arg, trName, tp.Name)
			}
		}
	}
}

// boundMethod finds a method on a type parameter, through its bounds.
// This is what makes a generic body checkable: on a `T: Ord`, `cmp`
// resolves and everything else is an error.
//
// A parameter with *no* bounds returns modelled=false, and the caller
// stays silent — an unbounded `T` genuinely tells us nothing, so
// reporting anything about it would be a guess. That asymmetry is the
// whole design: bounds are what turn a type parameter from a hole into
// a surface.
func (c *checker) boundMethod(v *types.Var, name string) (sig *types.Func, modelled bool) {
	if len(v.Bounds) == 0 {
		return nil, false
	}
	for _, trName := range v.Bounds {
		sigs := c.traits[trName]
		if sigs == nil {
			return nil, false // unknown trait: do not compound the error
		}
		if f, ok := sigs[name]; ok {
			out, _ := types.Subst(f, map[string]types.Type{"Self": v}).(*types.Func)
			if out == nil {
				out = f
			}
			return out, true
		}
	}
	return nil, true
}

// boundsOf renders a type parameter's bounds for a diagnostic.
func boundsOf(v *types.Var) string {
	if len(v.Bounds) == 0 {
		return ""
	}
	s := v.Bounds[0]
	for _, b := range v.Bounds[1:] {
		s += " + " + b
	}
	return s
}

func selfWord(k ast.SelfMode) string {
	switch k {
	case ast.NoSelf:
		return "no self"
	case ast.Self:
		return "self"
	}
	return "mut self"
}
