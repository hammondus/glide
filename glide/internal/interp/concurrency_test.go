package interp

import (
	"strings"
	"testing"

	"glide/internal/parser"
)

// Rule: spawn returns a Task; join returns exactly what the closure
// returned.
func TestScopeSpawnJoin(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    scope s {
        let a = s.spawn(|| 1 + 2)
        let b = s.spawn(|| "hi")
        println("{a.join()} {b.join()}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "3 hi\n" {
		t.Fatalf("got %q", out)
	}
}

// The scope is an expression: its body's tail is its value.
func TestScopeIsExpression(t *testing.T) {
	out, err := runProg(t, `
fn sum() -> Int {
    scope s {
        let a = s.spawn(|| 20)
        let b = s.spawn(|| 22)
        a.join() + b.join()
    }
}
fn main() {
    println(sum())
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n" {
		t.Fatalf("got %q", out)
	}
}

// Rule 1: scope exit waits for children even when nobody joins.
func TestScopeWaitsForChildren(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let mut log = []
    scope s {
        _ = s.spawn(|| log.push("child"))
    }
    log.push("after")
    println(log)
}`)
	// The child mutates through the captured binding; the scope must
	// have joined it before "after" is pushed.
	if err != nil {
		t.Fatal(err)
	}
	if out != `["child", "after"]`+"\n" {
		t.Fatalf("got %q", out)
	}
}

// Rule 2: a child's Err is a value in the handle — joined and
// handled, it does not fail the scope.
func TestScopeJoinedErrIsAValue(t *testing.T) {
	out, err := runProg(t, `
fn work(n: Int) -> Result<Int, String> {
    if n > 2 { Ok(n * 10) } else { Err("too small") }
}
fn main() {
    scope s {
        let a = s.spawn(|| work(5))
        let b = s.spawn(|| work(1))
        match a.join() {
            Ok(v) => println("a: {v}")
            Err(e) => println("a failed: {e}")
        }
        match b.join() {
            Ok(v) => println("b: {v}")
            Err(e) => println("b failed: {e}")
        }
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a: 50\nb failed: too small\n" {
		t.Fatalf("got %q", out)
	}
}

// Rule 3: an unjoined Err fails the scope at normal exit, as if the
// body ?'d it at the closing brace.
func TestScopeUnjoinedErrFailsScope(t *testing.T) {
	out, err := runProg(t, `
fn work() -> Result<Int, String> { Err("dropped on the floor") }
fn run() -> Result<Int, String> {
    scope s {
        _ = s.spawn(|| work())
        println("body done")
        Ok(0)
    }
}
fn main() {
    match run() {
        Ok(_) => println("ok")
        Err(e) => println("scope failed: {e}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "body done\nscope failed: dropped on the floor\n" {
		t.Fatalf("got %q", out)
	}
}

// Rule 3's escape hatch: explicitly discarding via join counts as
// observed.
func TestScopeDiscardedJoinIsObserved(t *testing.T) {
	out, err := runProg(t, `
fn work() -> Result<Int, String> { Err("meh") }
fn main() {
    scope s {
        let t = s.spawn(|| work())
        _ = t.join()
    }
    println("survived")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "survived\n" {
		t.Fatalf("got %q", out)
	}
}

// ?-conversion applies to a scope-propagated error exactly as it
// would at an explicit ? site.
func TestScopeErrConversion(t *testing.T) {
	out, err := runProg(t, `
type ApiError = Db(String) | Timeout
impl ApiError {
    fn from(e: String) -> ApiError { .Db(e) }
}
fn work() -> Result<Int, String> { Err("boom") }
fn run() -> Result<Int, ApiError> {
    scope s {
        _ = s.spawn(|| work())
        Ok(1)
    }
}
fn main() {
    match run() {
        Ok(_) => println("ok")
        Err(e) => println("converted: {e:?}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "converted: Db(\"boom\")\n" {
		t.Fatalf("got %q", out)
	}
}

// Early exit from the body (a propagating ?) cancels children: a
// child consuming an endless generator is blocked at the handoff (a
// cancellation point) and must be unwound, or the scope exit would
// hang forever.
func TestScopeEarlyExitCancelsBlockedChild(t *testing.T) {
	out, err := runProg(t, `
fn ones() -> Iterator<Int> {
    for {
        yield 1
    }
}
fn fail() -> Result<Int, String> { Err("early") }
fn run() -> Result<Int, String> {
    scope s {
        _ = s.spawn(|| ones().count())
        let n = fail()?
        Ok(n)
    }
}
fn main() {
    match run() {
        Ok(_) => println("ok")
        Err(e) => println("failed: {e}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "failed: early\n" {
		t.Fatalf("got %q", out)
	}
}

// Rule 4: a child's panic cancels siblings and re-panics at scope
// exit — main dies with the child's error, not a hang.
func TestScopeChildPanicPropagates(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    scope s {
        let a = s.spawn(|| [1, 2][9])
        let b = s.spawn(|| 7)
        println(a.join() + b.join())
    }
    println("unreachable")
}`)
	if err == nil {
		t.Fatal("want the child's panic to surface as main's error")
	}
	if !strings.Contains(err.Error(), "index 9 out of range") &&
		!strings.Contains(err.Error(), "out of range") {
		t.Fatalf("got %v", err)
	}
}

// Cancellation runs defers and errdefers in the cancelled child
// (ratified: a cancelled transfer still rolls back).
func TestCancellationRunsDefers(t *testing.T) {
	out, err := runProg(t, `
fn ones() -> Iterator<Int> {
    for {
        yield 1
    }
}
fn main() {
    scope s {
        _ = s.spawn(|| {
            defer { println("defer ran") }
            errdefer { println("errdefer ran") }
            _ = ones().count()
        })
        _ = s.spawn(|| boom())
    }
    println("unreachable")
}
fn boom() -> Int { [][0] }`)
	// boom's panic cancels the sibling blocked in the generator
	// handoff; its defer AND errdefer must run before the panic
	// resurfaces as main's death.
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want the panic to surface, got %v", err)
	}
	if !strings.Contains(out, "defer ran") || !strings.Contains(out, "errdefer ran") {
		t.Fatalf("cancelled child's defers must run; got %q", out)
	}
	if strings.Contains(out, "unreachable") {
		t.Fatalf("main must die at the scope: %q", out)
	}
}

// No ambient spawn: the handle is the only door.
func TestSpawnNeedsScopeHandle(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let t = 3
    _ = t.spawn(|| 1)
}`)
	if err == nil || !strings.Contains(err.Error(), "no method") {
		t.Fatalf("got %v", err)
	}
}

// Spawning after the scope has ended is an error, not a leak.
func TestSpawnAfterScopeEnds(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let mut escaped = []
    scope s {
        escaped.push(s)
    }
    let leaked = escaped[0]
    _ = leaked.spawn(|| 1)
}`)
	if err == nil || !strings.Contains(err.Error(), "already ended") {
		t.Fatalf("got %v", err)
	}
}

// Timeout config parses but is honest about not existing yet.
func TestScopeTimeoutNotYet(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    scope(timeout: 5) { println("hi") }
}`)
	if err == nil || !strings.Contains(err.Error(), "time types") {
		t.Fatalf("got %v", err)
	}
}

// Grammar: handle-less scope, config error cases.
func TestScopeParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`fn main() { scope(retries: 3) { } }`, "unknown scope config"},
		{`fn main() { scope S { } }`, "lowercase"},
		{`fn main() { select { } }`, "not yet implemented"},
	}
	for _, c := range cases {
		_, err := parser.ParseFile(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("src %q: got %v, want %q", c.src, err, c.want)
		}
	}
}

// A worker pipeline: tasks spawned in a loop, joined in order.
func TestScopeFanOut(t *testing.T) {
	out, err := runProg(t, `
fn square(n: Int) -> Int { n * n }
fn main() {
    scope s {
        let mut tasks = []
        for i in 1..=5 {
            tasks.push(s.spawn(|| square(i)))
        }
        let mut total = 0
        for t in tasks {
            total += t.join()
        }
        println(total)
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "55\n" {
		t.Fatalf("got %q", out)
	}
}

// Generators keep working under the runtime lock, including inside
// spawned tasks.
func TestGeneratorInsideTask(t *testing.T) {
	out, err := runProg(t, `
fn nums(n: Int) -> Iterator<Int> {
    for i in 1..=n {
        yield i * i
    }
}
fn main() {
    scope s {
        let t = s.spawn(|| nums(4).sum())
        println(t.join())
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "30\n" {
		t.Fatalf("got %q", out)
	}
}
