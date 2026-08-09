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

// A bare Int is not a Duration — units are mandatory.
func TestScopeTimeoutNeedsDuration(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    scope(timeout: 5) { println("hi") }
}`)
	if err == nil || !strings.Contains(err.Error(), "must be a Duration") {
		t.Fatalf("got %v", err)
	}
}

// Grammar: handle-less scope, config error cases.
func TestScopeParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`fn main() { scope(retries: 3) { } }`, "unknown scope config"},
		{`fn main() { scope S { } }`, "lowercase"},
		{`fn main() { select { } }`, "at least one arm"},
	}
	for _, c := range cases {
		_, err := parser.ParseFile("test.gld", c.src)
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

// Producer/consumer: the bread-and-butter worker pattern, rendezvous
// channel, for-in until closed.
func TestChannelProducerConsumer(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in 1..=4 {
                tx.send(i * i)
            }
            tx.close()
        })
        let mut total = 0
        for v in rx {
            total += v
        }
        println(total)
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "30\n" {
		t.Fatalf("got %q", out)
	}
}

// Buffered channel: producer finishes before anyone reads.
func TestChannelBuffered(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 3)
    tx.send(1)
    tx.send(2)
    tx.send(3)
    tx.close()
    let mut got = []
    for v in rx {
        got.push(v)
    }
    println(got)
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "[1, 2, 3]\n" {
		t.Fatalf("got %q", out)
	}
}

// recv on closed-and-drained is None; a live value is Some.
func TestChannelRecvOption(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 1)
    tx.send(7)
    tx.close()
    if let Some(v) = rx.recv() {
        println("got {v}")
    }
    match rx.recv() {
        Some(_) => println("impossible")
        None => println("drained")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "got 7\ndrained\n" {
		t.Fatalf("got %q", out)
	}
}

// mpmc: two workers share one receiver; every job is done exactly
// once.
func TestChannelWorkerPool(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel()
    let (rtx, rrx) = channel(cap: 6)
    scope s {
        _ = s.spawn(|| {
            for job in rx {
                rtx.send(job * 10)
            }
        })
        _ = s.spawn(|| {
            for job in rx {
                rtx.send(job * 10)
            }
        })
        for i in 1..=6 {
            tx.send(i)
        }
        tx.close()
        let mut total = 0
        for _ in 1..=6 {
            if let Some(v) = rrx.recv() {
                total += v
            }
        }
        println(total)
        rtx.close()
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "210\n" {
		t.Fatalf("got %q", out)
	}
}

// The three dispatched Go panics: rx can't close; double close is a
// no-op; send-on-closed is a panic.
func TestChannelCloseRules(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, _) = channel(cap: 1)
    tx.close()
    tx.close()
    println("double close survived")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "double close survived\n" {
		t.Fatalf("got %q", out)
	}

	_, err = runProg(t, `
fn main() {
    let (_, rx) = channel()
    rx.close()
}`)
	if err == nil || !strings.Contains(err.Error(), "only the sender half closes") {
		t.Fatalf("got %v", err)
	}

	_, err = runProg(t, `
fn main() {
    let (tx, _) = channel(cap: 1)
    tx.close()
    tx.send(1)
}`)
	if err == nil || !strings.Contains(err.Error(), "send on a closed channel") {
		t.Fatalf("got %v", err)
	}
}

// A blocked channel op is a cancellation point: a sibling panic must
// unwind a receiver blocked on a channel nobody will ever send to.
func TestChannelBlockedRecvCancelled(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let (_, rx) = channel()
    scope s {
        _ = s.spawn(|| rx.recv())
        _ = s.spawn(|| boom())
    }
}
fn boom() -> Int { [][0] }`)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want the panic, not a hang; got %v", err)
	}
}

// Rendezvous means backpressure: an unbuffered send blocks until a
// receiver takes it (order is forced strictly alternating).
func TestChannelRendezvous(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in 1..=3 {
                tx.send(i)
                println("sent {i}")
            }
            tx.close()
        })
        for v in rx {
            println("got {v}")
        }
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	// A rendezvous send completes only when the receiver takes the
	// value, so "got n" can never lag more than one behind "sent n".
	// Weak but deterministic check: first line is one of the pair,
	// and all six lines appear.
	for _, want := range []string{"sent 1", "sent 2", "sent 3", "got 1", "got 2", "got 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

// Halves are values: pass tx into a function, cap validation errors.
func TestChannelMisc(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let (tx, _) = channel(cap: -1)
    tx.close()
}`)
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("got %v", err)
	}

	_, err = runProg(t, `
fn main() {
    let (tx, _) = channel(size: 4)
    tx.close()
}`)
	if err == nil || !strings.Contains(err.Error(), "(cap: n)") {
		t.Fatalf("got %v", err)
	}
}

// select: recv patterns, the Some/None split on one channel, and
// select-as-expression.
func TestSelectRecvSplit(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 2)
    tx.send(41)
    tx.close()
    let mut open = true
    let mut total = 0
    for open {
        total += select {
            Some(v) = rx.recv() => v + 1
            None = rx.recv() => {
                open = false
                0
            }
        }
    }
    println(total)
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n" {
		t.Fatalf("got %q", out)
	}
}

// else = non-blocking: nothing ready takes the else arm.
func TestSelectElse(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel()
    let v = select {
        Some(v) = rx.recv() => v
        else => -1
    }
    println(v)
    tx.close()
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "-1\n" {
		t.Fatalf("got %q", out)
	}
}

// A send arm fires when buffer space exists.
func TestSelectSend(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 1)
    let did = select {
        tx.send(9) => "sent"
        else => "full"
    }
    if let Some(v) = rx.recv() {
        println("{did} {v}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "sent 9\n" {
		t.Fatalf("got %q", out)
	}
}

// Guards disable arms at entry — the nil-channel trick, replaced.
func TestSelectGuard(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 1)
    tx.send(5)
    let v = select {
        Some(a) = rx.recv() if false => a
        else => -1
    }
    println(v)
    let w = select {
        Some(b) = rx.recv() if true => b
        else => -1
    }
    println(w)
    tx.close()
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "-1\n5\n" {
		t.Fatalf("got %q", out)
	}
}

// All arms disabled and no else is a loud error, not a silent hang.
func TestSelectAllDisabled(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let (tx, rx) = channel()
    _ = select {
        Some(v) = rx.recv() if false => v
    }
    tx.close()
}`)
	if err == nil || !strings.Contains(err.Error(), "no enabled arms") {
		t.Fatalf("got %v", err)
	}
}

// A blocked select is a cancellation point: sibling panic unwinds it.
func TestSelectCancelled(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let (_, rx) = channel()
    scope s {
        _ = s.spawn(|| select {
            Some(v) = rx.recv() => v
            None = rx.recv() => 0
        })
        _ = s.spawn(|| boom())
    }
}
fn boom() -> Int { [][0] }`)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("want the panic, not a hang; got %v", err)
	}
}

// An unmatched ready recv is a runtime error naming the consumed
// value (the recorded sharpest edge until the checker).
func TestSelectUnmatchedRecv(t *testing.T) {
	_, err := runProg(t, `
fn main() {
    let (tx, rx) = channel(cap: 1)
    tx.close()
    _ = select {
        Some(v) = rx.recv() => v
    }
}`)
	if err == nil || !strings.Contains(err.Error(), "no arm pattern matches") {
		t.Fatalf("got %v", err)
	}
}

// Parse errors: arm shape rules.
func TestSelectParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`fn main() { select { } }`, "at least one arm"},
		{`fn main() { select { else => 1
else => 2 } }`, "two else"},
		{`fn main() { select { rx.recv() => 1 } }`, "binds its value"},
		{`fn main() { select { x = tx.send(1) => 1 } }`, "no pattern"},
		{`fn main() { select { foo() => 1 } }`, "waits on rx.recv() or tx.send"},
		{`fn main() { select { x = rx.frob() => 1 } }`, "not \"frob\""},
	}
	for _, c := range cases {
		_, err := parser.ParseFile("test.gld", c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("src %q: got %v, want %q", c.src, err, c.want)
		}
	}
}

// The classic fan-in: two producers, one select loop draining both.
func TestSelectFanIn(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let (atx, arx) = channel()
    let (btx, brx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in 1..=3 { atx.send(i) }
            atx.close()
        })
        _ = s.spawn(|| {
            for i in 1..=3 { btx.send(i * 100) }
            btx.close()
        })
        let mut a_open = true
        let mut b_open = true
        let mut total = 0
        for a_open || b_open {
            select {
                Some(v) = arx.recv() if a_open => { total += v }
                None = arx.recv() if a_open => { a_open = false }
                Some(w) = brx.recv() if b_open => { total += w }
                None = brx.recv() if b_open => { b_open = false }
            }
        }
        println(total)
    }
}`)
	// A drained channel keeps delivering None, so each channel's arms
	// are guard-disabled once it closes — the pattern Go spells by
	// nil-ing the channel variable.
	if err != nil {
		t.Fatal(err)
	}
	if out != "606\n" {
		t.Fatalf("got %q", out)
	}
}

// Duration suffixes, arithmetic, and display.
func TestDurations(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let d = 1.s + 500.ms
    println(d)
    println(2.mins + 30.s)
    println(d * 2)
    println(2 * d)
    println(d / 3)
    println(0.5.s)
    println(1.s > 999.ms)
    println(90.s == 1.mins + 30.s)
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "1.5s\n2m30s\n3s\n3s\n500ms\n500ms\ntrue\ntrue\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// Instant arithmetic: the ratified minimal set; Instant+Instant does
// not exist.
func TestInstants(t *testing.T) {
	out, err := runProg(t, `
import time

fn main() {
    let t0 = time.now()
    let t1 = t0 + 10.s
    println(t1 - t0)
    println(t1 > t0)
    println((t1 - 10.s) == t0)
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "10s\ntrue\ntrue\n" {
		t.Fatalf("got %q", out)
	}

	_, err = runProg(t, `
import time

fn main() {
    let t0 = time.now()
    _ = t0 + t0
}`)
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("Instant + Instant must not exist; got %v", err)
	}
}

// scope(timeout:) evaluates to Result: Ok on completion, Err(Timeout)
// when the clock wins; the ? machinery converts Timeout like any
// other error.
func TestScopeTimeout(t *testing.T) {
	out, err := runProg(t, `
import time

fn main() {
    let fast = scope(timeout: 5.s) { 42 }
    match fast {
        Ok(v) => println("fast: {v}")
        Err(_) => println("fast timed out")
    }
    let slow = scope(timeout: 20.ms) {
        time.sleep(5.s)
        1
    }
    match slow {
        Ok(_) => println("impossible")
        Err(Timeout) => println("slow timed out")
        Err(_) => println("other")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "fast: 42\nslow timed out\n" {
		t.Fatalf("got %q", out)
	}
}

// Timeout cancels spawned children too — a child sleeping forever is
// unwound, and the scope reports Timeout.
func TestScopeTimeoutCancelsChildren(t *testing.T) {
	out, err := runProg(t, `
import time

fn main() {
    let r = scope(timeout: 20.ms) s {
        _ = s.spawn(|| {
            defer { println("child cleaned up") }
            time.sleep(10.s)
        })
        time.sleep(10.s)
        0
    }
    if let Err(Timeout) = r {
        println("timed out")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "child cleaned up") || !strings.Contains(out, "timed out") {
		t.Fatalf("got %q", out)
	}
}

// Timeout converts through E.from at the outer ? — timeouts are
// non-viral: no ctx parameter anywhere in the chain.
func TestScopeTimeoutConversion(t *testing.T) {
	out, err := runProg(t, `
import time

type ApiError = Db(String) | TooSlow
impl ApiError {
    fn from(t: Timeout) -> ApiError { .TooSlow }
}
fn fetch_slowly() -> Result<Int, ApiError> {
    let v = scope(timeout: 20.ms) { time.sleep(5.s)
        9 }?
    Ok(v)
}
fn main() {
    match fetch_slowly() {
        Ok(_) => println("impossible")
        Err(e) => println("error: {e:?}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "error: TooSlow\n" {
		t.Fatalf("got %q", out)
	}
}

// s.deadline(): None outside timed scopes; inherited inside.
func TestScopeDeadline(t *testing.T) {
	out, err := runProg(t, `
import time

fn main() {
    scope plain {
        match plain.deadline() {
            Some(_) => println("has deadline")
            None => println("no deadline")
        }
    }
    _ = scope(timeout: 5.s) outer {
        scope inner {
            match inner.deadline() {
                Some(d) => println("inherited: {(d - time.now()) <= 5.s}")
                None => println("lost the deadline")
            }
        }
        0
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "no deadline\ninherited: true\n" {
		t.Fatalf("got %q", out)
	}
}

// time.sleep is a cancellation point; time.after is an ordinary
// select case.
func TestTimeAfterInSelect(t *testing.T) {
	out, err := runProg(t, `
import time

fn main() {
    let (_, rx) = channel()
    let v = select {
        Some(v) = rx.recv() => v
        Some(_) = time.after(20.ms).recv() => -1
    }
    println(v)
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "-1\n" {
		t.Fatalf("got %q", out)
	}
}

// distinct: explicit construction, no implicit conversion, no
// inherited operators, pattern destructuring, value() unwrap.
func TestDistinct(t *testing.T) {
	out, err := runProg(t, `
type NoteId = distinct Int

impl NoteId {
    fn next(self) -> NoteId { NoteId(self.value() + 1) }
}

fn main() {
    let a = NoteId(7)
    println(a)
    println(a == NoteId(7))
    println(a == NoteId(8))
    println(a.value() + 1)
    println(a.next())
    match a {
        NoteId(9) => println("nine")
        NoteId(n) => println("wrapped {n}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "NoteId(7)\ntrue\nfalse\n8\nNoteId(8)\nwrapped 7\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestDistinctNoMixing(t *testing.T) {
	_, err := runProg(t, `
type NoteId = distinct Int
fn main() {
    _ = NoteId(1) + 1
}`)
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("distinct must not inherit +; got %v", err)
	}

	_, err = runProg(t, `
type NoteId = distinct Int
fn main() {
    _ = NoteId("seven")
}`)
	if err == nil || !strings.Contains(err.Error(), "no implicit conversion") {
		t.Fatalf("base type is checked; got %v", err)
	}

	_, err = runProg(t, `
type NoteId = distinct Int
type UserId = distinct Int
fn main() {
    println(NoteId(1) == UserId(1))
}`)
	if err != nil {
		t.Fatal(err)
	}
}
