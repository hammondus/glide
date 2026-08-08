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
	src, err := os.ReadFile("../../examples/wordfreq.gld")
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
	src, _ := os.ReadFile("../../examples/wordfreq.gld")
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
	src, _ := os.ReadFile("../../examples/wordfreq.gld")
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

func TestBuiltinMutMethodsEnforced(t *testing.T) {
	_, err := runProg(t, "fn main() {\n let xs = [1]\n xs.push(2)\n}")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("push through immutable binding should fail, got %v", err)
	}
	_, err = runProg(t, "fn main() {\n let xs = [2, 1]\n xs.sort_by(|a, b| a.cmp(b))\n}")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("sort_by through immutable binding should fail, got %v", err)
	}
	// Transitive: the path's root binding is what must be mut.
	_, err = runProg(t, "fn main() {\n let xs = [[1]]\n xs[0].push(2)\n}")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("push through immutable root should fail, got %v", err)
	}
	// A temporary is not a path — no mut path exists.
	_, err = runProg(t, "fn main() {\n [1, 2].push(3)\n}")
	if err == nil || !strings.Contains(err.Error(), "temporary") {
		t.Fatalf("push on temporary should fail, got %v", err)
	}
	out, err := runProg(t, "fn main() {\n let mut xs = [1]\n xs.push(2)\n println(xs.len())\n}")
	if err != nil || out != "2\n" {
		t.Fatalf("push through mut binding: out %q, err %v", out, err)
	}
	// Read-only methods stay callable through immutable bindings.
	out, err = runProg(t, "fn main() {\n let xs = [3, 1]\n println(xs.sorted()[0])\n println(xs.len())\n}")
	if err != nil || out != "1\n2\n" {
		t.Fatalf("read-only methods on let binding: out %q, err %v", out, err)
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
	src, err := os.ReadFile("../../examples/tree.gld")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(string(src))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if failed := RunTests(f, &out); failed != 0 {
		t.Fatalf("tree.gld tests failed:\n%s", out.String())
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

func TestBreakContinue(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    // break in an infinite loop
    let mut i = 0
    for {
        i += 1
        if i >= 3 { break }
    }
    println("{i}")

    // continue in a for-in: sum the odd values only
    let mut total = 0
    for n in [1, 2, 3, 4, 5] {
        if n % 2 == 0 { continue }
        total += n
    }
    println("{total}")

    // continue in a conditional loop
    let mut k = 0
    let mut odds = 0
    for k < 10 {
        k += 1
        if k % 2 == 0 { continue }
        odds += 1
    }
    println("{odds}")

    // break exits the inner loop only
    let mut hits = 0
    for _ in [1, 2, 3] {
        for j in [1, 2, 3] {
            if j == 2 { break }
            hits += 1
        }
    }
    println("{hits}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "3\n9\n5\n3\n" {
		t.Fatalf("output:\n%q", out)
	}
}

func TestBreakInGeneratorLoop(t *testing.T) {
	// break belongs to the loop inside the generator body; the
	// generator finishes normally when its loop ends.
	out, err := runProg(t, `
fn firsts() -> Iterator<Int> {
    for n in [1, 2, 3, 4] {
        if n > 2 { break }
        yield n
    }
}
fn main() {
    for x in firsts() {
        println(x)
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n2\n" {
		t.Fatalf("output:\n%q", out)
	}
}

func TestCompoundAssignFullSet(t *testing.T) {
	out, err := runProg(t, `
type Point = struct { x: Int }
fn main() {
    let mut a = 7
    a *= 6
    a /= 2
    a %= 4
    println("{a}")

    let mut xs = [10, 20]
    xs[1] *= 3
    xs[0] /= 5
    xs[0] %= 4
    println("{xs[0]} {xs[1]}")

    let mut m = ["n": 9]
    m["n"] %= 5
    println("{m["n"]}")

    let mut p = Point{ x: 8 }
    p.x /= 2
    println("{p.x}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	// 7*6=42, 42/2=21, 21%4=1
	if out != "1\n2 60\n4\n4\n" {
		t.Fatalf("output:\n%q", out)
	}
}

func TestCompoundDivideByZero(t *testing.T) {
	_, err := runProg(t, "fn main() {\n let mut a = 1\n a /= 0\n}")
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("want division by zero, got: %v", err)
	}
}

func TestListRepeat(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let xs = [1, 2].repeat(3)
    println("{xs.len()} {xs[0]} {xs[5]}")
    println([9].repeat(0).len())
    // Read-only: callable through an immutable binding.
    let ys = [7]
    println(ys.repeat(2).len())
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "6 1 2\n0\n2\n" {
		t.Fatalf("output:\n%q", out)
	}

	// Shallow by design: repeating a reference value shares it, the
	// same trade every dynamic-era fill makes (Python's [[0]]*3).
	// The aliasing-safe spelling is the adapter form (0..n).map(...).
	out, err = runProg(t, `
fn main() {
    let mut grid = [[0]].repeat(2)
    grid[0].push(1)
    println("{grid[0].len()} {grid[1].len()}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "2 2\n" {
		t.Fatalf("shallow sharing: output %q", out)
	}

	_, err = runProg(t, "fn main() {\n _ = [1].repeat(0 - 1)\n}")
	if err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("negative count should fail, got %v", err)
	}
}

func TestMatchLiteralRangeString(t *testing.T) {
	out, err := runProg(t, `
fn describe(code: Int) -> String {
    match code {
        200        => "ok"
        301, 302   => "redirect"
        400..500   => "client error"
        n if n < 0 => "nonsense"
        _          => "other"
    }
}
fn main() {
    println(describe(200))
    println(describe(302))
    println(describe(404))
    println(describe(499))
    println(describe(500))
    println(describe(0 - 7))

    println(match "PUT" {
        "GET"          => "read"
        "PUT", "POST"  => "write"
        _              => "?"
    })
    println(match true {
        true  => "yes"
        false => "no"
    })
    // negative literals and ranges
    println(match 0 - 3 {
        -5..-1 => "small negative"
        -1     => "minus one"
        _      => "other"
    })
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "ok\nredirect\nclient error\nclient error\nother\nnonsense\nwrite\nyes\nsmall negative\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

func TestMatchMultiPatternCtors(t *testing.T) {
	out, err := runProg(t, `
type Color = Red | Green | Blue
fn main() {
    let c = Green
    println(match c {
        Red, Green => "warm"
        Blue       => "cool"
    })
}`)
	if err != nil || out != "warm\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestMatchTypeMismatchFallsThrough(t *testing.T) {
	// Dynamically typed until the checker era: a literal of the wrong
	// type simply doesn't match, and the fall-through panic reports it.
	_, err := runProg(t, "fn main() {\n _ = match \"GET\" {\n  1 => \"?\"\n }\n}")
	if err == nil || !strings.Contains(err.Error(), "no match arm matched") {
		t.Fatalf("want fall-through panic, got %v", err)
	}
}

func TestSubjectlessMatch(t *testing.T) {
	out, err := runProg(t, `
fn grade(score: Int) -> String {
    match {
        score >= 90 => "A"
        score >= 80 => "B"
        score >= 0  => "pass"
        _           => "invalid"
    }
}
fn main() {
    println(grade(95))
    println(grade(85))
    println(grade(3))
    println(grade(0 - 1))
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "A\nB\npass\ninvalid\n" {
		t.Fatalf("output:\n%q", out)
	}

	// A non-Bool arm is an error at that arm.
	_, err = runProg(t, "fn main() {\n _ = match {\n  5 => 1\n }\n}")
	if err == nil || !strings.Contains(err.Error(), "must be Bool") {
		t.Fatalf("want Bool-arm error, got %v", err)
	}

	// No true arm and no `_`: the fall-through panic names the fix.
	_, err = runProg(t, "fn main() {\n _ = match {\n  false => 1\n }\n}")
	if err == nil || !strings.Contains(err.Error(), "no match arm was true") {
		t.Fatalf("want fall-through panic, got %v", err)
	}
}

func TestYieldInsideMatchArmDetected(t *testing.T) {
	// Generator detection must look inside match arms (both forms).
	out, err := runProg(t, `
fn evens(xs: List<Int>) -> Iterator<Int> {
    for x in xs {
        match x % 2 {
            0 => { yield x }
            _ => ()
        }
    }
}
fn main() {
    for x in evens([1, 2, 3, 4]) {
        println(x)
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "2\n4\n" {
		t.Fatalf("output:\n%q", out)
	}
}

func TestIteratorAdapters(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    // map/filter/collect chain
    let doubled_evens = [1, 2, 3, 4, 5].iter()
        .filter(|n| n % 2 == 0)
        .map(|n| n * 10)
        .collect()
    println("{doubled_evens[0]} {doubled_evens[1]} {doubled_evens.len()}")

    // enumerate yields (index, value)
    for (i, name) in ["a", "b"].iter().enumerate() {
        println("{i}:{name}")
    }

    // zip stops at the shorter side; takes a bare iterable
    for (x, y) in [1, 2, 3].iter().zip(["one", "two"]) {
        println("{x}={y}")
    }

    // consumers
    println([1, 2, 3].iter().sum())
    println([1.5, 2.5].iter().sum())
    println((0..100).iter().filter(|n| n % 7 == 0).count())

    // the aliasing-safe fill: fresh inner list per slot
    let mut grid = (0..2).iter().map(|_| []).collect()
    grid[0].push(1)
    println("{grid[0].len()} {grid[1].len()}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "20 40 2\n0:a\n1:b\n1=one\n2=two\n6\n4\n15\n1 0\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

func TestAdaptersAreLazy(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let it = [1, 2, 3].iter().map(|n| {
        println("mapping {n}")
        n * 2
    })
    println("nothing yet")
    println(it.take(2).collect().len())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "nothing yet\nmapping 1\nmapping 2\n2\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

func TestFilterPredicateMustBeBool(t *testing.T) {
	_, err := runProg(t, "fn main() {\n _ = [1].iter().filter(|n| n).collect()\n}")
	if err == nil || !strings.Contains(err.Error(), "must return Bool") {
		t.Fatalf("want Bool-predicate error, got %v", err)
	}
}
