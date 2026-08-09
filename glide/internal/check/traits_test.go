package check_test

import (
	"strings"
	"testing"

	"glide/internal/check"
	"glide/internal/parser"
	"glide/internal/program"
)

// frontendErr runs the whole frontend the way both tiers do and
// returns every diagnostic as one string. Unlike checkSrc, a
// declaration-table failure is a *result* here rather than a fatal:
// to a programmer "unknown trait" and "does not satisfy" are the same
// event, and only the stage differs.
func frontendErr(t *testing.T, src string) string {
	t.Helper()
	f, err := parser.ParseFile("test.gld", src)
	if err != nil {
		return err.Error()
	}
	tab, err := program.Load(f, check.Host())
	if err != nil {
		return err.Error()
	}
	if _, err := check.File(f, tab); err != nil {
		return err.Error()
	}
	return ""
}

// Conformance is declared; satisfaction is structural (DESIGN.md).
// The declaration is mandatory, so nothing conforms by accident — but
// an empty impl body is correct when the shape already matches, which
// is the half Rust makes you write forwarding methods for.
func TestConformanceIsDeclaredAndStructural(t *testing.T) {
	const trait = "trait Greet {\n    fn hello(self) -> String\n}\n"

	// Structural: the inherent hello satisfies it, empty body and all.
	got := frontendErr(t, trait+`
type Foo = struct { n: Int }
impl Foo { fn hello(self) -> String { "hi" } }
impl Greet for Foo { }
fn main() { println(Foo{ n: 1 }.hello()) }`)
	if got != "" {
		t.Errorf("an inherent method should satisfy the trait: %s", got)
	}

	// Declared: having the method without declaring conformance does
	// not make the type conform.
	got = frontendErr(t, trait+`
type Foo = struct { n: Int }
impl Foo { fn hello(self) -> String { "hi" } }
fn greet<T: Greet>(x: T) -> String { x.hello() }
fn main() { println(greet(Foo{ n: 1 })) }`)
	if !strings.Contains(got, "Foo does not implement Greet") {
		t.Errorf("accidental conformance must not count: %s", got)
	}
}

func TestUnsatisfiedConformanceIsReported(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"missing method",
			"trait G { fn hello(self) -> String }\ntype F = struct { n: Int }\nimpl G for F { }\nfn main() {}",
			"F does not satisfy G: missing hello() -> String",
		},
		{
			"wrong return",
			"trait G { fn hello(self) -> String }\ntype F = struct { n: Int }\nimpl G for F { fn hello(self) -> Int { 1 } }\nfn main() {}",
			"hello returns Int, but the trait declares String",
		},
		{
			"wrong arity",
			"trait G { fn hello(self) -> String }\ntype F = struct { n: Int }\nimpl G for F { fn hello(self, x: Int) -> String { \"h\" } }\nfn main() {}",
			"takes 1 argument(s), but the trait declares 0",
		},
		{
			"wrong argument type",
			"trait G { fn hello(self, x: Int) -> String }\ntype F = struct { n: Int }\nimpl G for F { fn hello(self, x: String) -> String { x } }\nfn main() {}",
			"argument 1 is String, but the trait declares Int",
		},
		{
			"wrong receiver kind",
			"trait G { fn hello(mut self) -> String }\ntype F = struct { n: Int }\nimpl G for F { fn hello(self) -> String { \"h\" } }\nfn main() {}",
			"takes self, but the trait declares mut self",
		},
		{
			"generic trait, wrong argument",
			"type B = struct { xs: List<Int> }\nimpl Iterable<String> for B { fn iter(self) -> Iterator<Int> { self.xs.iter() } }\nfn main() {}",
			"iter returns Iterator<Int>, but the trait declares Iterator<String>",
		},
	} {
		if got := frontendErr(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

// The universe traits name machinery that already runs, so the
// builtins satisfy them structurally — `Int` cannot write
// `impl Ord for Int`, and should not have to.
func TestBuiltinsSatisfyUniverseTraits(t *testing.T) {
	src := `
fn biggest<T: Ord>(a: T, b: T) -> T { if a.cmp(b) > 0 { a } else { b } }
fn count<T, C: Iterable<T>>(c: C) -> Int { c.iter().count() }
fn main() {
    println(biggest(3, 7))
    println(biggest("a", "b"))
    println(count([1, 2, 3]))
}`
	if got := frontendErr(t, src); got != "" {
		t.Errorf("builtins should satisfy Ord/Iterable structurally: %s", got)
	}
}

// The load-bearing asymmetry: a bound is the complete method set, and
// an unbounded parameter is a hole nothing may be said about.
func TestBoundsAreTheCompleteMethodSet(t *testing.T) {
	got := frontendErr(t, `
fn f<T: Ord>(x: T, y: T) -> Int { x.frobnicate() }
fn main() { println(f(1, 2)) }`)
	if !strings.Contains(got, `T has no method "frobnicate"`) {
		t.Errorf("a bounded T should reject an undeclared method: %q", got)
	}
	if !strings.Contains(got, "bounded by Ord") {
		t.Errorf("the diagnostic should name the bound: %q", got)
	}

	// Unbounded stays silent: the checker genuinely knows nothing, and
	// guessing here would reject working programs.
	if got := frontendErr(t, `
fn g<T>(x: T) { x.whatever() }
fn main() { }`); got != "" {
		t.Errorf("an unbounded T must stay opaque: %s", got)
	}
}

// One type parameter can satisfy another's bound without any concrete
// type being known, which is what makes generic code compose.
func TestTypeParameterSatisfiesBound(t *testing.T) {
	if got := frontendErr(t, `
fn inner<U: Ord>(a: U, b: U) -> Int { a.cmp(b) }
fn outer<T: Ord>(a: T, b: T) -> Int { inner(a, b) }
fn main() { println(outer(1, 2)) }`); got != "" {
		t.Errorf("T: Ord should satisfy U: Ord: %s", got)
	}
	if got := frontendErr(t, `
fn inner<U: Ord>(a: U, b: U) -> Int { a.cmp(b) }
fn outer<T>(a: T, b: T) -> Int { inner(a, b) }
fn main() { println(outer(1, 2)) }`); !strings.Contains(got, "does not implement Ord") {
		t.Errorf("an unbounded T must not satisfy U: Ord: %q", got)
	}
}

// `Self` had no meaning as a type name before M4c, which made the
// canonical trait shape a compile error.
func TestSelfResolves(t *testing.T) {
	if got := frontendErr(t, `
type B = struct { n: Int }
impl B { fn cmp(self, other: Self) -> Int { self.n - other.n } }
impl Ord for B { }
fn main() { println(B{ n: 2 }.cmp(B{ n: 1 })) }`); got != "" {
		t.Errorf("Self should resolve inside an impl: %s", got)
	}
	// A default body is checked against Self: ThisTrait — exactly the
	// generality it has.
	if got := frontendErr(t, `
trait Named {
    fn name(self) -> String
    fn greet(self) -> String { "hi " + self.name() }
}
type P = struct { s: String }
impl Named for P { fn name(self) -> String { self.s } }
fn main() { println(P{ s: "a" }.greet()) }`); got != "" {
		t.Errorf("a default body should see the trait's own methods: %s", got)
	}
	// And nothing beyond them.
	if got := frontendErr(t, `
trait Named {
    fn name(self) -> String
    fn greet(self) -> String { self.nope() }
}
fn main() { }`); !strings.Contains(got, `has no method "nope"`) {
		t.Errorf("a default body should be held to the trait's own surface: %q", got)
	}
}

func TestUniverseTraitsAreReserved(t *testing.T) {
	for _, name := range []string{"Ord", "Iterable"} {
		src := "trait " + name + " { fn zzz(self) -> Int }\nfn main() {}"
		if got := frontendErr(t, src); !strings.Contains(got, "builtin trait and cannot be redeclared") {
			t.Errorf("%s should be reserved: %q", name, got)
		}
	}
	// An impl for a trait nobody declared is a typo, not a feature.
	if got := frontendErr(t, "type F = struct { n: Int }\nimpl Nope for F { }\nfn main() {}"); !strings.Contains(got, `unknown trait "Nope"`) {
		t.Errorf("want unknown trait, got %q", got)
	}
}
