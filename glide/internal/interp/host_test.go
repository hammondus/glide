package interp

import (
	"testing"

	"glide/internal/check"
	"glide/internal/types"
)

// The checker's tables and the evaluator's implementations describe
// the same host surface. They are separate bodies of code — one says
// what a name means, the other says what it does — and the only thing
// keeping them in step is this test. A builtin implemented but not
// typed would be rejected by the checker in a program that runs; a
// builtin typed but not implemented would be accepted by the checker
// and crash. Both are drift, and drift between the two tiers is the
// one bug class the whole M4 design exists to make impossible.
// The reserved set holds two kinds of name with two different
// implementations: callable builtins (`println`), which live in the
// evaluator's `builtins` map, and predeclared type names (`u8`), which
// `evalIdent` resolves to a TypeV so that calling one is a conversion.
// Each half is checked against its own implementation — collapsing
// them into one membership test would let a name be reserved by the
// checker and undefined at runtime.
func TestHostSurfaceMatchesRuntime(t *testing.T) {
	host := check.Host()

	for name := range builtins {
		if !host.Builtins[name] {
			t.Errorf("builtin %q is implemented but the checker does not know it", name)
		}
	}
	for name := range host.Builtins {
		if _, ok := builtins[name]; ok {
			continue
		}
		if _, isPrimitive := types.Primitives[name]; isPrimitive {
			continue // checked properly below
		}
		t.Errorf("builtin %q is typed but not implemented", name)
	}
	for name := range types.Primitives {
		if !host.Builtins[name] {
			t.Errorf("primitive %q is not reserved, so a program could redeclare it", name)
		}
	}
	for name := range knownModules {
		if !host.Modules[name] {
			t.Errorf("module %q is importable but the checker does not know it", name)
		}
	}
	for name := range host.Modules {
		if !knownModules[name] {
			t.Errorf("module %q is typed but not importable", name)
		}
	}
}

// Every primitive the checker will treat as a conversion callee has to
// be one at runtime too. Asserted end to end rather than by map
// membership, because "the name resolves" and "calling it converts"
// are different claims and only the second one matters.
func TestEveryNumericPrimitiveConverts(t *testing.T) {
	for name, b := range types.Primitives {
		if !types.Numeric(b) {
			continue // Bool and String are deliberately not convertible
		}
		src := "fn main() {\n    println(" + name + "(65))\n}"
		out, err := runProg(t, src)
		if err != nil {
			t.Errorf("%s(65): %v", name, err)
			continue
		}
		want := "65\n"
		if b.IsRune() {
			want = "A\n"
		}
		if out != want {
			t.Errorf("%s(65) = %q, want %q", name, out, want)
		}
	}
	// The two primitives that must NOT be callable.
	for _, name := range []string{"Bool", "String"} {
		if _, err := runProg(t, "fn main() {\n    println("+name+"(65))\n}"); err == nil {
			t.Errorf("%s(65) should not be a conversion", name)
		}
	}
}
