package interp

import (
	"errors"
	"os"
	"strings"
	"testing"

	"glide/internal/parser"
)

// runProg parses and runs src, returning stdout and the Run error.
func runProg(t *testing.T, src string, args ...string) (string, error) {
	t.Helper()
	f, err := parser.ParseFile(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var out, errOut strings.Builder
	in := New()
	in.Stdout = &out
	in.Stderr = &errOut
	in.Args = append([]string{"prog"}, args...)
	err = in.Run(f)
	return out.String(), err
}

func TestWordfreqGolden(t *testing.T) {
	src, err := os.ReadFile("../../examples/wordfreq.gl")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(string(src))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	in := New()
	in.Stdout = &out
	in.Stderr = &strings.Builder{}
	in.Args = []string{"wordfreq", "../../testdata/sample.txt"}
	if err := in.Run(f); err != nil {
		t.Fatal(err)
	}
	want := "     3  the\n     2  quick\n     1  lazy\n     1  dog\n"
	if out.String() != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestWordfreqUsageExit(t *testing.T) {
	src, _ := os.ReadFile("../../examples/wordfreq.gl")
	f, _ := parser.ParseFile(string(src))
	in := New()
	in.Stdout = &strings.Builder{}
	in.Stderr = &strings.Builder{}
	in.Args = []string{"wordfreq", "a", "b"} // too many: exact-arity pattern rejects
	err := in.Run(f)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("want exit 2, got %v", err)
	}
}

func TestTryPropagatesWithContext(t *testing.T) {
	src, _ := os.ReadFile("../../examples/wordfreq.gl")
	f, _ := parser.ParseFile(string(src))
	in := New()
	in.Stdout = &strings.Builder{}
	in.Stderr = &strings.Builder{}
	in.Args = []string{"wordfreq", "no-such-file.txt"}
	err := in.Run(f)
	if err == nil || !strings.HasPrefix(err.Error(), "reading input: ") {
		t.Fatalf("want context-wrapped error, got %v", err)
	}
}

func TestPrintFamilyNewlines(t *testing.T) {
	f, err := parser.ParseFile(`
fn main() {
    print("a")
    print("b")
    println("c")
    eprint("x")
    eprintln("y")
}`)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	in := New()
	in.Stdout = &out
	in.Stderr = &errOut
	in.Args = []string{"prog"}
	if err := in.Run(f); err != nil {
		t.Fatal(err)
	}
	if out.String() != "abc\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), "abc\n")
	}
	if errOut.String() != "xy\n" {
		t.Fatalf("stderr = %q, want %q", errOut.String(), "xy\n")
	}
}

func TestBlockExpression(t *testing.T) {
	// let-bound: the block's tail is its value, its locals die at }.
	out, err := runProg(t, `
fn main() {
    let size = {
        let num = 60
        if num > 50 { "big" } else { "small" }
    }
    println(size)
}`)
	if err != nil || out != "big\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	// Bare statement block: Go's scoping idiom.
	out, err = runProg(t, `
fn main() {
    {
        let hidden = "in"
        println(hidden)
    }
    println("out")
}`)
	if err != nil || out != "in\nout\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	// The block is not a function boundary: shadowing an enclosing
	// name inside it stays banned, and its locals really do die.
	_, err = runProg(t, "fn main() {\n let x = 1\n {\n  let x = 2\n }\n}")
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("want shadow error, got %v", err)
	}
	_, err = runProg(t, "fn main() {\n {\n  let inner = 1\n }\n println(inner)\n}")
	if err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("block locals must not escape, got %v", err)
	}
}

func TestBuiltinsReserved(t *testing.T) {
	for _, src := range []string{
		"fn main() {\n let println = 5\n}",
		"fn main() {\n let f = |print| { print }\n f(1)\n}",
		"fn eprint(s: String) {}\nfn main() {}",
	} {
		_, err := runProg(t, src)
		if err == nil || !strings.Contains(err.Error(), "is a builtin") {
			t.Fatalf("want builtin-reservation error for %q, got %v", src, err)
		}
	}
	// Imports are NOT reserved: a local named after a module is legal
	// as long as the module isn't used through the shadow (the checker
	// era enforces the two-live-meanings conflict; M1 cannot see it).
	out, err := runProg(t, "import os\nfn main() {\n let os = \"fine\"\n println(os)\n}")
	if err != nil || out != "fine\n" {
		t.Fatalf("local named after import should run, got out=%q err=%v", out, err)
	}
}

func TestErrorLinesPointAtSource(t *testing.T) {
	// A bad format spec blames the string's line...
	_, err := runProg(t, "fn main() {\n let n = 5\n println(\"{n: 6}\")\n}")
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("format spec error should blame line 3, got %v", err)
	}
	// ...and so does a runtime error inside an interpolated expression,
	// whose tokens were lexed from a standalone snippet.
	_, err = runProg(t, "fn main() {\n let t = (1, 2)\n println(\"{t.5}\")\n}")
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("interpolated expr error should blame line 3, got %v", err)
	}
}

func TestNullishDefault(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let mut m: Map<String, Int> = [:]
    let a = "a"
    let b = "b"
    m[a] = 2
    println("{m[a] ?? 0} {m[b] ?? 0}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "2 0\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestMutEnforced(t *testing.T) {
	_, err := runProg(t, "fn main() {\n let x = 1\n x = 2\n}")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("want immutability error, got %v", err)
	}
	_, err = runProg(t, "fn main() {\n let m: Map<String, Int> = [:]\n m[\"k\"] = 1\n}")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("mutation through immutable map binding should fail, got %v", err)
	}
}

func TestSequentialRedeclareOK(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let x = 10
    let x = x + 1
    println("{x}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "11\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestNestedShadowBanned(t *testing.T) {
	_, err := runProg(t, "fn main() {\n let x = 1\n for i in 0..2 { let x = 2 }\n}")
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("want shadow error, got %v", err)
	}
}

func TestClosureMayReuseOuterNames(t *testing.T) {
	// A closure is a new function; the shadow ban is function-local.
	out, err := runProg(t, `
fn main() {
    let x = 1
    let f = || {
        let x = 2
        x
    }
    println("{f()} {x}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "2 1\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestTailExpressionIsValue(t *testing.T) {
	out, err := runProg(t, `
fn double(n: Int) -> Int {
    n * 2
}
fn main() {
    println("{double(21)}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestTailValueRuleEnforced(t *testing.T) {
	_, err := runProg(t, "fn f() { 5 }\nfn main() {\n f()\n}")
	if err == nil || !strings.Contains(err.Error(), "declares no return value") {
		t.Fatalf("want tail-value error, got %v", err)
	}
}

func TestLetElseMustDiverge(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let [x] = [1, 2] else {
        println("nope")
    }
}`)
	if err == nil || !strings.Contains(err.Error(), "diverge") {
		t.Fatalf("want divergence error, got %v", err)
	}
}

func TestListRestPattern(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let [first, ..rest] = [1, 2, 3] else { return }
    println("{first} {rest.len()}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1 2\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestClosureCapture(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let mut total = 0
    let add = |n| { total += n }
    add(2)
    add(40)
    println("{total}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestClosureCapturesBindingNotName(t *testing.T) {
	// Sequential redeclaration creates a new binding; a closure that
	// captured the old one keeps it.
	out, err := runProg(t, `
fn main() {
    let name = "Cocko"
    let who = || name
    let name = "Cccc"
    println("{who()} {name}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Cocko Cccc\n" {
		t.Fatalf("out = %q (closure must keep the binding it captured)", out)
	}
}

func TestIfIsExpression(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let ok = true
    let status = if ok { "active" } else { "disabled" }
    println("{status}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "active\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestFreshLoopBindings(t *testing.T) {
	// Closures capture per-iteration bindings, not one shared variable.
	out, err := runProg(t, `
fn main() {
    let mut fns = []
    for i in 0..3 {
        fns.push(|| i)
    }
    for f in fns.iter() {
        println("{f()}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "0\n1\n2\n" {
		t.Fatalf("out = %q", out)
	}
}

// --- M2 ---

func TestTreeProgramProperty(t *testing.T) {
	src, err := os.ReadFile("../../examples/tree.gl")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(string(src))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if failed := RunTests(f, &out); failed != 0 {
		t.Fatalf("tree.gl tests failed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "100 cases") {
		t.Fatalf("expected property cases, got:\n%s", out.String())
	}
}

func TestMatchGuardsAndVariants(t *testing.T) {
	out, err := runProg(t, `
type Shape = Circle(Int) | Square(Int) | Dot

fn area(s: Shape) -> Int {
    match s {
        Circle(r) if r > 10 => 999
        Circle(r) => r * 3
        Square(w) => w * w
        Dot => 0
    }
}
fn main() {
    println("{area(Circle(4))} {area(Circle(11))} {area(Square(5))} {area(Dot)}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "12 999 25 0\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestStructUpdateIsACopy(t *testing.T) {
	out, err := runProg(t, `
type P = struct {
    x: Int
    y: Int
}
fn main() {
    let a = P{ x: 1, y: 2 }
    let b = P{ x: 10, ..a }
    println("{a.x} {b.x} {b.y}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1 10 2\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestMandatoryInit(t *testing.T) {
	_, err := runProg(t, "type P = struct {\n x: Int\n y: Int\n}\nfn main() {\n let p = P{ x: 1 }\n println(\"{p.x}\")\n}")
	if err == nil || !strings.Contains(err.Error(), "missing field") {
		t.Fatalf("want missing-field error, got %v", err)
	}
}

func TestMutSelfNeedsMutPath(t *testing.T) {
	src := `
type Counter = struct {
    n: Int
}
impl Counter {
    fn new() -> Counter { Counter{ n: 0 } }
    fn bump(mut self) { self.n += 1 }
}
fn main() {
    let c = Counter.new()
    c.bump()
}`
	_, err := runProg(t, src)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("mut self through let binding should fail, got %v", err)
	}
	out, err := runProg(t, strings.Replace(src, "let c", "let mut c", 1)+"\n")
	if err != nil {
		t.Fatal(err)
	}
	_ = out
}

func TestGeneratorIsLazy(t *testing.T) {
	// An infinite generator only computes what take() demands.
	out, err := runProg(t, `
fn naturals() -> Iterator<Int> {
    let mut n = 0
    for {
        yield n
        n += 1
    }
}
fn main() {
    for x in naturals().take(5) {
        println(x)
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "0\n1\n2\n3\n4\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestIfLetElse(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let m: Map<String, Int> = [:]
    let k = "missing"
    if let v = m[k] {
        println("{v}")
    } else {
        println("absent")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "absent\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestShrinkFindsMinimalCase(t *testing.T) {
	f, err := parser.ParseFile(`
test "short lists" (xs: List<Int>) {
    expect(xs.len() <= 4)
}`)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if failed := RunTests(f, &out); failed != 1 {
		t.Fatalf("want 1 failure, got %d:\n%s", failed, out.String())
	}
	if !strings.Contains(out.String(), "[0, 0, 0, 0, 0]") {
		t.Fatalf("want minimal counterexample [0, 0, 0, 0, 0], got:\n%s", out.String())
	}
}
