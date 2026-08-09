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

// Exhaustiveness under-approximates: it reports only when a case is
// *definitely* unhandled. These are the shapes where it must stay
// silent, and they matter more than the ones it catches — the first
// version of this analysis rejected examples/links.gld.
func TestExhaustivenessNeverRejectsWorkingCode(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"every variant", "type C = Red | Green\nfn f(c: C) -> Int { match c { Red => 1  Green => 2 } }\nfn main() {}"},
		{"wildcard", "type C = Red | Green\nfn f(c: C) -> Int { match c { Red => 1  _ => 2 } }\nfn main() {}"},
		{"bare binding is a catch-all", "type C = Red | Green\nfn f(c: C) -> Int { match c { seen => 1 } }\nfn main() {}"},
		// The links.gld shape: three Err arms cover Err together, none
		// of them alone. A top-level-only analysis rejects this.
		{"nested arms cover together", "type E = A | B\nfn f(r: Result<Int, E>) -> Int { match r { Ok(n) => n  Err(A) => 0  Err(B) => 1 } }\nfn main() {}"},
		{"guard then total", "type C = Red | Green\nfn f(c: C, h: Bool) -> Int { match c { Red if h => 1  Red => 2  Green => 3 } }\nfn main() {}"},
		{"struct has one case", "type U = struct { n: Int }\nfn f(u: U) -> Int { match u { U{ n } => n } }\nfn main() {}"},
		{"distinct has one case", "type Id = distinct Int\nfn f(i: Id) -> Int { match i { Id(n) => n } }\nfn main() {}"},
		{"multi-arg variant", "type P = Pair(Int, Int) | Nil\nfn f(p: P) -> Int { match p { Pair(a, b) => a + b  Nil => 0 } }\nfn main() {}"},
		{"opaque type parameter", "fn f<T>(v: T) -> Int { match v { _ => 1 } }\nfn main() {}"},
	} {
		if got := frontendErr(t, tc.src); got != "" {
			t.Errorf("%s: should be accepted, got %s", tc.name, got)
		}
	}
}

func TestExhaustivenessCatchesMissingCases(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"missing variant", "type C = Red | Green | Blue\nfn f(c: C) -> Int { match c { Red => 1  Green => 2 } }\nfn main() {}",
			"not exhaustive: Blue not handled"},
		{"two missing, listed", "type C = Red | Green | Blue\nfn f(c: C) -> Int { match c { Red => 1 } }\nfn main() {}",
			"Blue and Green not handled"},
		// A guard may not fire, so the only Red arm being guarded
		// leaves Red unhandled.
		{"guarded covers nothing", "type C = Red | Green\nfn f(c: C, h: Bool) -> Int { match c { Red if h => 1  Green => 2 } }\nfn main() {}",
			"Red not handled"},
		{"Option", "fn f(o: Int?) -> Int { match o { Some(n) => n } }\nfn main() {}", "None not handled"},
		{"Result", "fn f(r: Result<Int, Error>) -> Int { match r { Ok(n) => n } }\nfn main() {}", "Err not handled"},
		{"Bool", "fn f(b: Bool) -> Int { match b { true => 1 } }\nfn main() {}", "false not handled"},
		// Recursion is what names the case *inside* the constructor.
		{"nested", "type E = A | B\nfn f(r: Result<Int, E>) -> Int { match r { Ok(n) => n  Err(A) => 0 } }\nfn main() {}",
			"Err(B) not handled"},
		{"infinite type needs _", "fn f(s: String) -> Int { match s { \"GET\" => 1 } }\nfn main() {}",
			"needs a `_` arm"},
	} {
		if got := frontendErr(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestUnreachableArms(t *testing.T) {
	for _, name := range []string{
		"type C = Red | Green\nfn f(c: C) -> Int { match c { _ => 1  Red => 2 } }\nfn main() {}",
		"type C = Red | Green\nfn f(c: C) -> Int { match c { Red => 1  Red => 2  Green => 3 } }\nfn main() {}",
	} {
		if got := frontendErr(t, name); !strings.Contains(got, "this arm cannot run") {
			t.Errorf("want an unreachable-arm diagnostic, got %q", got)
		}
	}
	// A guard makes the arm below it reachable, not dead.
	if got := frontendErr(t, "type C = Red | Green\nfn f(c: C, h: Bool) -> Int { match c { Red if h => 1  Red => 2  Green => 3 } }\nfn main() {}"); got != "" {
		t.Errorf("an unguarded arm after a guarded one is reachable: %s", got)
	}
}

// One mistake, one diagnostic. A typo'd constructor already reports
// what is wrong; adding "and by the way it is not exhaustive" makes
// the second error vanish when the first is fixed.
func TestExhaustivenessDoesNotCascade(t *testing.T) {
	got := frontendErr(t, "type C = Red | Green\nfn main() {\n    match C.Red {\n        Rd => println(\"typo\")\n    }\n}")
	if !strings.Contains(got, "not a constructor") {
		t.Fatalf("want the constructor error: %q", got)
	}
	if strings.Contains(got, "not exhaustive") {
		t.Errorf("exhaustiveness should not pile on: %q", got)
	}
}

// Closure parameter annotations. The book always said they existed;
// no grammar did, so `|x: Int| …` was a parse error. They are rarely
// needed — a closure in a typed slot learns its parameters from the
// slot — but a closure nothing constrains had no way to be checked,
// which is the hole they close.
func TestClosureParameterAnnotations(t *testing.T) {
	// The motivating case: unconstrained, and now checked.
	if got := frontendErr(t, "fn main() {\n    let f = |x: Int| x + 1\n    println(f(\"no\"))\n}"); !strings.Contains(got, "expected Int, found String") {
		t.Errorf("an annotated closure should check its call sites: %q", got)
	}
	if got := frontendErr(t, "fn main() {\n    let f = |x: Int| x.nope()\n    println(f(1))\n}"); !strings.Contains(got, `Int has no method "nope"`) {
		t.Errorf("an annotated closure should check its body: %q", got)
	}
	// Unannotated in a typed slot: unchanged, and still checked.
	if got := frontendErr(t, "fn main() {\n    let mut xs = [3, 1, 2]\n    xs.sort_by(|a, b| a.cmp(b))\n}"); got != "" {
		t.Errorf("the common unannotated case must keep working: %s", got)
	}
	// Annotated and agreeing with the slot.
	if got := frontendErr(t, "fn main() {\n    let mut xs = [3, 1, 2]\n    xs.sort_by(|a: Int, b: Int| a.cmp(b))\n}"); got != "" {
		t.Errorf("an agreeing annotation should be accepted: %s", got)
	}
	// Partially annotated.
	if got := frontendErr(t, "fn main() {\n    let g = |a: Int, b| a + b\n    println(g(1, 2))\n}"); got != "" {
		t.Errorf("a partial annotation should be accepted: %s", got)
	}
}

// A wrong annotation is one diagnostic, at the annotation — not that
// plus a signature mismatch at the call plus a cascade through the
// body.
func TestClosureAnnotationConflictReportsOnce(t *testing.T) {
	got := frontendErr(t, "fn main() {\n    let mut xs = [3, 1, 2]\n    xs.sort_by(|a: String, b| a.cmp(b))\n}")
	if !strings.Contains(got, "annotated String") {
		t.Fatalf("want the annotation conflict: %q", got)
	}
	if strings.Contains(got, "expected fn(") {
		t.Errorf("the signature mismatch should not also fire: %q", got)
	}
}

// Every closure had a zero span, so any diagnostic pointing at one
// printed with no position at all.
func TestClosureDiagnosticsHaveAPosition(t *testing.T) {
	got := frontendErr(t, "fn main() {\n    let mut xs = [3, 1, 2]\n    xs.sort_by(|a| 1)\n}")
	if !strings.Contains(got, "test.gld:3:") {
		t.Errorf("a closure diagnostic needs a position: %q", got)
	}
}

// A function type can be written as of M4c: `fn(A, B) -> C`. The type
// existed inside the checker since M4b — a closure passed to sort_by
// is checked against the parameter's signature — but parseType had no
// case for it, so the reference's ✓ was a claim the parser refused.
func TestWrittenFunctionTypes(t *testing.T) {
	const decl = "fn apply(f: fn(Int) -> Int, x: Int) -> Int { f(x) }\n"
	for _, tc := range []struct{ name, src string }{
		{"closure argument", decl + "fn main() { println(apply(|x| x + 1, 41)) }"},
		{"named function argument", decl + "fn d(n: Int) -> Int { n * 2 }\nfn main() { println(apply(d, 21)) }"},
		{"no arrow means unit", "fn each(xs: List<Int>, s: fn(Int)) { for x in xs { s(x) } }\nfn main() { each([1], |v| println(v)) }"},
		{"nested", "fn hof(g: fn(fn(Int) -> Int) -> Int) -> Int { g(|x| x) }\nfn main() { println(hof(|f| f(1))) }"},
		{"inside a List", "fn d(n: Int) -> Int { n }\nfn pick(fs: List<fn(Int) -> Int>, x: Int) -> Int { fs[0](x) }\nfn main() { println(pick([d], 1)) }"},
		{"returns an Option", "fn m(f: fn(Int) -> Int?) -> Int { 1 }\nfn main() { println(m(|x| Some(x))) }"},
	} {
		if got := frontendErr(t, tc.src); got != "" {
			t.Errorf("%s: %s", tc.name, got)
		}
	}
	for _, tc := range []struct{ name, src, want string }{
		{"wrong return", decl + `fn main() { println(apply(|x| "no", 3)) }`, "this closure must return Int, got String"},
		{"wrong arity", decl + "fn main() { println(apply(|a, b| 1, 3)) }", "expected fn(Int) -> Int"},
		{"not a function", decl + "fn main() { println(apply(5, 3)) }", "expected fn(Int) -> Int, found Int"},
		{"wrong named function", decl + "fn s(v: String) -> Int { 1 }\nfn main() { println(apply(s, 3)) }", "expected fn(Int) -> Int"},
		// The declared type is what gives the closure body its types.
		{"body checked from the type", decl + "fn main() { println(apply(|x| x.nope(), 3)) }", `Int has no method "nope"`},
	} {
		if got := frontendErr(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

// One mistake, the most specific diagnostic available. A wrong closure
// body is reported at the body rather than as a signature mismatch at
// the call — and not as both.
func TestClosureReturnMismatchReportsOnce(t *testing.T) {
	got := frontendErr(t, "fn apply(f: fn(Int) -> Int, x: Int) -> Int { f(x) }\nfn main() { println(apply(|x| \"no\", 3)) }")
	if !strings.Contains(got, "this closure must return Int") {
		t.Fatalf("want the body diagnostic: %q", got)
	}
	if strings.Contains(got, "expected fn(") {
		t.Errorf("the signature mismatch should not also fire: %q", got)
	}
}

// A generator's declared Iterator<T> now types its yields. `yield "s"`
// in an Iterator<Int> passed through M4b and every M4c landing before
// this one.
func TestGeneratorElementTypes(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"wrong yield", "fn g() -> Iterator<Int> { yield 1\n yield \"no\" }\nfn main() {}", "expected Int, found String"},
		{"wrong delegation", "fn a() -> Iterator<String> { yield \"x\" }\nfn b() -> Iterator<Int> { yield from a() }\nfn main() {}",
			"expected Iterator<Int>, found Iterator<String>"},
		{"not an iterator at all", "fn g() -> Int { yield 1 }\nfn main() {}", "yields, so it returns Iterator<T>"},
	} {
		if got := frontendErr(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
	for _, tc := range []struct{ name, src string }{
		{"matching yields", "fn g() -> Iterator<Int> { yield 1\n yield 2 }\nfn main() {}"},
		{"delegation of the same type", "fn a() -> Iterator<Int> { yield 1 }\nfn b() -> Iterator<Int> { yield from a()\n yield 2 }\nfn main() {}"},
		// The T -> T? coercion applies inside a yield like anywhere else.
		{"coercion in a yield", "fn g() -> Iterator<Int?> { yield 1\n yield None }\nfn main() {}"},
	} {
		if got := frontendErr(t, tc.src); got != "" {
			t.Errorf("%s: %s", tc.name, got)
		}
	}
}

// DESIGN.md: a closure crossing a task boundary must not capture a
// `mut` binding — the parent going on to write it is the data-race
// archetype, and it is statically visible because mut-ness is known
// and spawn is a known boundary.
func TestSpawnCannotCaptureMut(t *testing.T) {
	got := frontendErr(t, "fn main() {\n    let mut total = 0\n    scope s {\n        s.spawn(|| { total = total + 1 })\n    }\n    println(total)\n}")
	if !strings.Contains(got, `cannot capture the mutable binding "total"`) {
		t.Fatalf("want the capture diagnostic: %q", got)
	}
	// The rule is about mut-ness, which is what makes it cheap: an
	// immutable capture crosses freely.
	if got := frontendErr(t, "fn main() {\n    let base = 10\n    scope s {\n        let t = s.spawn(|| base * 2)\n        println(t.join())\n    }\n}"); got != "" {
		t.Errorf("an immutable capture must cross freely: %s", got)
	}
	// The freeze idiom: mut for setup, frozen before it crosses.
	if got := frontendErr(t, "fn main() {\n    let mut b = []\n    b.push(1)\n    let frozen = b\n    scope s {\n        let t = s.spawn(|| frozen.len())\n        println(t.join())\n    }\n}"); got != "" {
		t.Errorf("the freeze idiom must work: %s", got)
	}
	// A closure that is not spawned may capture whatever it likes.
	if got := frontendErr(t, "fn main() {\n    let mut n = 0\n    let bump = || { n = n + 1 }\n    bump()\n}"); got != "" {
		t.Errorf("the rule is about task boundaries, not closures: %s", got)
	}
}

// A call whose type parameter nothing determines used to erase to
// Unknown in silence, so `Box.new()` then `add(1)` then `add("s")`
// both passed. Rust answers the same call with "type annotations
// needed", and so does this — inferring T from a *later* statement
// needs a constraint store this checker deliberately lacks.
func TestUndeterminedTypeParameter(t *testing.T) {
	const decl = "type Box<T> = struct { items: List<T> }\n" +
		"impl Box<T> {\n" +
		"    fn new() -> Box<T> { Box{ items: [] } }\n" +
		"    fn of(v: T) -> Box<T> { Box{ items: [v] } }\n" +
		"    fn add(mut self, v: T) { self.items.push(v) }\n" +
		"}\n"
	if got := frontendErr(t, decl+"fn main() {\n    let mut c = Box.new()\n    c.add(1)\n}"); !strings.Contains(got, "cannot tell what T is in Box<T>") {
		t.Errorf("want the annotation diagnostic: %q", got)
	}
	// The binding's annotation determines it — and then the element
	// type is actually enforced, which erasure had been hiding.
	if got := frontendErr(t, decl+"fn main() {\n    let mut c: Box<Int> = Box.new()\n    c.add(1)\n}"); got != "" {
		t.Errorf("an annotated binding should be accepted: %s", got)
	}
	if got := frontendErr(t, decl+"fn main() {\n    let mut c: Box<Int> = Box.new()\n    c.add(\"mixed\")\n}"); !strings.Contains(got, "expected Int, found String") {
		t.Errorf("the annotated element type must be enforced: %q", got)
	}
	// An argument determines it just as well.
	if got := frontendErr(t, decl+"fn main() {\n    let c = Box.of(1)\n    println(c)\n}"); got != "" {
		t.Errorf("an argument-determined parameter needs no annotation: %s", got)
	}
}
