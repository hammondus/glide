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
