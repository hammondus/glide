package interp

import (
	"testing"

	"glide/internal/check"
)

// The checker's tables and the evaluator's implementations describe
// the same host surface. They are separate bodies of code — one says
// what a name means, the other says what it does — and the only thing
// keeping them in step is this test. A builtin implemented but not
// typed would be rejected by the checker in a program that runs; a
// builtin typed but not implemented would be accepted by the checker
// and crash. Both are drift, and drift between the two tiers is the
// one bug class the whole M4 design exists to make impossible.
func TestHostSurfaceMatchesRuntime(t *testing.T) {
	host := check.Host()

	for name := range builtins {
		if !host.Builtins[name] {
			t.Errorf("builtin %q is implemented but the checker does not know it", name)
		}
	}
	for name := range host.Builtins {
		if _, ok := builtins[name]; !ok {
			t.Errorf("builtin %q is typed but not implemented", name)
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
