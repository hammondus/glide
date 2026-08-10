# Chapter 26: Structured Concurrency

Glide is green-threaded like Go — cheap tasks, no `async`/`await`, no
function colouring. But where Go's `go` statement launches a goroutine
with **no parent**, every Glide task belongs to a **scope**: the
scope's end waits for its children, a failing child cancels its
siblings, and nothing leaks.

That is the whole idea, and it turns an entire category of production
bugs into shapes you cannot write.

This chapter covers scopes, `spawn`, `join`, and the four rules that
make the model. Cancellation gets Chapter 27; channels get Chapter 28.

Everything here is ✓ — structured concurrency runs in the interpreter
today.

---

### 1. Basic Usage

#### A scope with two tasks

```glide-run
import time

fn work(id: Int, ms: Int) -> Int {
    time.sleep(ms.ms)
    id * 10
}

fn parallel() -> Int {
    scope s {
        let a = s.spawn(|| work(1, 20))
        let b = s.spawn(|| work(2, 10))
        a.join() + b.join()
    }
}

fn main() {
    println(parallel())      // 30
}
```

`scope s { … }` opens a scope with handle `s`. `s.spawn(f)` starts a
task and returns a `Task`. `t.join()` blocks and returns **exactly what
the closure returned**.

A `scope` is an **expression** — it evaluates to its body's tail value,
which is why `parallel()` can return `a.join() + b.join()` directly.

#### The scope always joins

```glide
scope s {
    _ = s.spawn(|| background_work())
    do_something_else()
}
// control does not reach here until the spawned task has finished
```

Scope exit joins every child, on **every** exit path — normal
fall-through, `?`, `return`, `break`, and panic. Early exit cancels the
children first, waits, then continues propagating.

The scope is a super-`defer`: no exit path skips it. Leaks are
unrepresentable.

#### Errors are values that wait

```glide
fn fails() -> Result<Int, String> {
    scope s {
        let a = s.spawn(|| Err("boom"))
        let b = s.spawn(|| Ok(5))
        let x = a.join()?
        let y = b.join()?
        Ok(x + y)
    }
}
```

```
Err("boom")
```

`a.join()` returned `Err("boom")`; the `?` exited the scope body; the
scope cancelled `b` and joined it; the error propagated.

Note that a child's `Err` is **a value, not an event**. It sits in the
handle until joined. That makes partial success trivial — join each,
keep what worked:

```glide
scope s {
    let tasks = urls.iter().map(|u| s.spawn(|| fetch(u))).collect()
    let mut ok: List<String> = []
    for t in tasks {
        match t.join() {
            Ok(page) => ok.push(page)
            Err(e)   => eprintln("skipping: {e}")
        }
    }
    ok
}
```

#### Errors cannot vanish

```glide
fn unjoined() -> Result<Int, String> {
    scope s {
        _ = s.spawn(|| Err("silent failure"))
        Ok(1)
    }
}
```

```
Err("silent failure")
```

The scope body returned `Ok(1)` and never joined the child. At normal
exit, an unjoined child that finished with `Err` **fails the scope**,
as if the body had `?`'d it at the closing brace. First failure wins.

To ignore an error deliberately, discard it explicitly:

```glide
let _ = t.join()
```

#### Panics do not wait

A child's panic is a bug, and bugs do not wait to be observed. It
cancels the siblings **immediately**, and the scope re-panics at exit.

The slogan for the two rules together: **values wait, bugs don't.**

#### The four rules

`DESIGN.md` ratified these on 2026-08-09, and they are the whole model:

1. **Scope exit always joins every child** — normal exit, `?`,
   `return`, `break`, panic. Early exit cancels children first, waits,
   then continues propagating.
2. **A child's `Err` is a value, not an event** — it sits in the handle
   until joined.
3. **Errors can't vanish** — at normal exit, an unjoined child that
   finished with `Err` fails the scope. First failure wins. Ignoring
   takes an explicit `let _ = t.join()`.
4. **A child's panic is a bug** — it cancels siblings immediately and
   the scope re-panics at exit.

#### There is no ambient spawn

`spawn` exists only on a scope handle. You cannot start a task without
naming its parent — including in `main`. That is the property that
makes leaks unrepresentable.

A bare `scope { }` with no handle parses and is merely pointless: you
need the handle to spawn.

#### `errgroup` semantics fall out

There is no fail-fast *mode* to configure. Cancel-on-first-error is
composition:

```glide
scope s {
    let a = s.spawn(|| fetch(u1))
    let b = s.spawn(|| fetch(u2))
    let x = a.join()?          // if this fails…
    let y = b.join()?          // …rule 1 cancels b on the way out
    Ok(combine(x, y))
}
```

No `supervisorScope`, no policy objects, no `errgroup.WithContext`.

---

### 2. Under the Hood

#### The interpreter's model

Spawned tasks run on real goroutines, but **exactly one interprets at a
time** — there is a single interpreter lock, released around blocking
operations.

`glide/DESIGN-DECISIONS.md` is emphatic that this is not a shortcut to
apologise for. Tasks interleave *exactly at blocking operations*, which
is precisely the ratified cancellation-point rule. **The lock is the
semantics, not just a guard.**

The cost: two compute-bound tasks serialise. Semantics permit any
scheduling, so no program can detect this except through throughput,
and release backends add true parallelism without changing observable
behaviour.

Each goroutine's cancellation context is lock-protected state, saved
and restored around every release. Generator handoffs release the lock
on both ends, so a generator inside a task cannot wedge the
interpreter.

#### The scope exit protocol

Recorded precisely, because the ordering matters:

1. Capture the body's result — value, signal, or panic.
2. If exiting early, cancel the children.
3. **Drain-join** children until the task list is stable — children may
   spawn siblings while you wait.
4. Apply precedence:

> body's own bug > child bug > outer cancellation > body's signal >
> first unjoined child `Err` > body value

That precedence list answers every "what if both happen at once"
question. A panic in the body beats a child's panic; a child's panic
beats being cancelled from outside; a `return` in the body beats an
unjoined child's error.

#### What a `Task` is

A handle holding the eventual result. `join()` blocks (a cancellation
point) and returns whatever the closure returned — a value, an `Ok`, an
`Err`, whatever. There is no unwrapping and no special error channel; a
`Result`-returning closure yields its `Result` and you `?` it like any
call.

#### The `mut`-capture rule ✓

One compile-time restriction, enforced as of M4c:

> **Closures crossing task boundaries must not capture `mut`
> bindings.**

```glide
fn main() {
    let mut total = 0
    scope s {
        let t = s.spawn(|| { total = total + 1 })
        let _ = t.join()
    }
    println(total)
}
```

```
app.gld:4:30: a spawned closure cannot capture the mutable binding "total" — the parent may still be writing it. Freeze it first (`let total_now = total`) or send it over a channel
 4 |         let t = s.spawn(|| { total = total + 1 })
   |                              ^^^^^
```

This is the data-race archetype and it is statically visible: mut-ness
is known and `spawn` is a known boundary. Freeze (`let snapshot =
counter`) or clone to cross.

`DESIGN.md`: cheap rule, whole race class turned into a compile error;
the race detector backstops what escapes via reference-typed immutable
captures.

---

### 3. Why This Design?

#### Why green threads and not `async`/`await`

`async`/`await` makes concurrency a **viral type-system property of
every function** — Bob Nystrom's "What Color Is Your Function?".

The evidence `DESIGN.md` cites is brutal and specific:

- **Python maintains two parallel ecosystems.** `requests` and
  `aiohttp`, `psycopg2` and `asyncpg`, sync and async everything. A
  library must pick, and a program must not mix.
- **Async Rust is described by its own maintainers as a second, harder
  language.** `Pin`, `Send + 'static`, executor fragmentation, and
  functions that cannot be called from the wrong context.
- **Java built reactive frameworks for fifteen years, shipped virtual
  threads in Java 21, and its ecosystem is migrating back to plain
  blocking style.** That is a fifteen-year controlled experiment ending
  in a reversal.

The technical point: `async`/`await` exists as a compiler transform to
stackless state machines, and it is *essential* when you cannot have a
runtime (Rust, embedded) or cannot have threads (JavaScript). Neither
constraint applies to Glide. Green threads are what the
GC-and-runtime purchase buys.

The accepted cost, stated plainly: stackful tasks cost kilobytes where
Rust futures cost bytes. At extreme task counts — millions of
connections on tiny hardware — async wins on memory. That workload is
conceded.

#### Why structure, and what `go` actually costs

Go's `go f()` launches a goroutine with no parent, and three problems
follow:

**It outlives its spawner.** A function that starts a goroutine and
returns has leaked it, unless something else arranges otherwise. Every
mature Go codebase has a goroutine-leak story and a `pprof` screenshot.

**Its panic kills the program.** Not the goroutine — the process. Which
is why `recover` is mandatory in every handler.

**Its errors vanish.** A goroutine returning an error has nowhere to
put it. Which is why every mature Go codebase reinvents `errgroup`.

Trio's insight — **nurseries** — is that a task's lifetime should be
bounded by a lexical region. Since Trio, the idea has been adopted by
Kotlin (`coroutineScope`), Swift (`TaskGroup`), and Java (Loom's
`StructuredTaskScope`). It is not experimental.

The consequence in Glide: **leaks are unrepresentable**, because
`spawn` exists only on a scope handle and scope exit always joins.

#### Why one spawn primitive, not Kotlin's two

Kotlin has `launch` (fire-and-forget) and `async` (returns a
`Deferred`). Glide has one.

`DESIGN.md`'s reasoning: **Kotlin needed two because exceptions are
invisible control flow.** With `launch`, a failure propagates
immediately to the scope; with `async`, it is stored in the `Deferred`
and surfaces at `await`. And Kotlin's `async` carries the library's
most-complained-about gotcha — a child's failure kills the scope
*before* the planned `await`-and-handle, so the handler never runs.

Glide's `Result`s are visible values, so one primitive covers both
roles. A `Task` holding an `Err` is just a task holding a value. Rules
2 and 3 make it safe: the error waits to be joined, and if it is never
joined it still cannot vanish.

#### Why "values wait, bugs don't"

The asymmetry is the interesting part of the design.

An `Err` is an expected outcome. The scope body may want to join other
children first, may want partial success, may want to try a fallback.
Cancelling everything the instant one child returns an `Err` would make
partial success impossible.

A panic is a bug. The program is in an unknown state, and there is no
sensible reason to keep siblings running. So it cancels immediately.

Both are recoverable-at-the-scope, and neither can be silently lost.

#### The accepted costs

`DESIGN.md` records three, honestly:

**No early cancellation on `Err`.** Child A runs for 60 seconds; child
B fails at 1 second. `a.join()?` waits out the full 60 before the error
propagates. The outcome is identical and the waste is bounded. Joining
in completion order is `select` territory (Chapter 29).

**First error wins; the rest are dropped.** An M2 simplification —
revisit if real code shows the dropped errors mattered.

**No ambient spawn.** `spawn` exists only on a scope handle, `main`
included. That is the point, not a limitation, but it does mean a
utility function cannot start background work without being handed a
scope.

---

### 4. Competing Approaches

**Go.** `go f()`, unstructured, plus `sync.WaitGroup`, `errgroup`, and
`context.Context` threaded manually. Every one of those three libraries
is reconstructing a piece of what a scope gives you: waiting,
error collection, and cancellation. Glide's scope is all three,
built in, and unavoidable.

**Python (Trio).** The origin of nurseries. `async with
trio.open_nursery() as n: n.start_soon(f)`. Trio's `MultiError` handles
several children failing at once, which Glide simplifies to
first-wins.

**Kotlin.** `coroutineScope`, `launch`/`async`, `Job.cancel()`,
`supervisorScope`. The closest full-featured relative. Glide declines
the two-primitive split (above), `Job.cancel()` (Chapter 27), and
`supervisorScope` (errgroup semantics fall out of composition).

**Swift.** `withTaskGroup`, structured concurrency with `async let`.
Also `async`/`await`, so it pays the colouring cost that Glide avoids.

**Java (Loom).** Virtual threads plus `StructuredTaskScope`. The most
significant vindication of the model — a conservative, enormous
ecosystem concluded after fifteen years of reactive programming that
plain blocking code on cheap threads is better.

**Erlang.** Processes plus supervision trees. Architecturally similar
at a different granularity: an Erlang supervisor restarts a dead child
rather than propagating the failure. `DESIGN.md` flags Erlang-style
supervision policies (`supervise(restart: .on_failure, backoff: …)`) as
a likely future scope variant, and "likely the first-used adopt" for
always-on services.

**Rust (async).** `tokio::spawn` is unstructured (a detached task);
`JoinSet` and the `tokio-scoped`/`async-scoped` crates approximate
scopes. Rust's difficulty here is downstream of the borrow checker: a
scoped task borrowing from the parent's stack is exactly what
lifetimes must reason about.

---

### 5. Common Mistakes

**Expecting `spawn` outside a scope to work.** There is no ambient
spawn. If a function needs to start background work, it takes a scope
handle:

```glide
// Good
fn start_sweeper(s: Scope, db: Db) {
    _ = s.spawn(|| sweeper(db))
}
```

**Forgetting that scope exit blocks.**

```glide
// This does not return until the task finishes
scope s {
    _ = s.spawn(|| slow_thing())
}
println("after")      // runs after slow_thing completes
```

That is the guarantee, not a surprise — but if you wanted the function
to return immediately, you wanted a *longer-lived* scope, not a
fire-and-forget task.

**Assuming an unjoined error is ignored.** It is not — rule 3. If you
genuinely want to ignore it, `let _ = t.join()`.

**Capturing a `mut` binding in a spawned closure.**

```glide
// A compile error: the data-race archetype
let mut count = 0
s.spawn(|| { count += 1 })

// Good
let snapshot = count
s.spawn(|| use(snapshot))
```

Adopt the freeze idiom now, before the rule is enforced.

**Expecting `a.join()?` to cancel `b` immediately.** It does — but only
when the `?` fires, which is after `a` completes. If `b` fails first
and `a` runs for a minute, you wait the minute. Joining in completion
order is `select`'s job.

**Using a scope for a single task.** A scope with one child that you
immediately join is a function call with extra steps:

```glide
// Pointless
let x = scope s { s.spawn(|| work()).join() }

// Just call it
let x = work()
```

Scopes earn their keep with concurrency, or with a long-lived child
whose lifetime should be bounded.

**Benchmarking compute parallelism in the interpreter.** One
interpreter lock means two compute-bound tasks serialise. IO-bound
concurrency works properly.

---

### 6. Performance Considerations

**Tasks are green threads.** In the compiled tier they are goroutines
— a few kilobytes of stack that grows on demand, multiplexed onto OS
threads by a work-stealing scheduler. Spawning thousands is ordinary.

**Stackful is the cost.** Kilobytes per task where Rust futures are
bytes. At a million concurrent connections on small hardware, async
wins. `DESIGN.md` concedes that workload explicitly.

**Scope exit costs a join per child**, which is a synchronisation per
task, not per operation.

**In the interpreter**, one lock means no compute parallelism.
IO-bound work interleaves correctly because the lock is released around
blocking operations. Do not measure CPU-bound speedup here.

**`join()` is a cancellation point** and blocks. So is `sleep`, so are
channel operations and IO.

**The drain-join loop handles children spawning siblings**, so scope
exit is O(total children) rather than O(children at exit time).

---

### 7. Best Practices

**Let the scope define the lifetime.** The idiom is that a scope's
extent *is* the answer to "how long should this run?":

```glide
scope s {
    _ = s.spawn(|| sweeper(db))              // dies with the server
    _ = s.spawn(|| metrics_reporter())       // dies with the server
    http.serve(":8080", router)              // the long-running thing
}
```

When `serve` returns — for any reason, including an error — the sweeper
and reporter are cancelled and joined. **No shutdown code exists
anywhere.** That is the design working.

**Join deliberately, or discard deliberately.**

```glide
// Good — the error matters
let x = t.join()?

// Good — the error genuinely does not matter, and it says so
let _ = t.join()

// Bad — relying on rule 3 to notice
_ = s.spawn(|| might_fail())
```

The third form works (rule 3 catches it), and it is worse because the
reader cannot tell whether you thought about it.

**Freeze before spawning.**

```glide
let snapshot = config
let id = request.id
s.spawn(|| handle(snapshot, id))
```

**Prefer partial success where it is meaningful.**

```glide
// Good — fetch everything, report what failed, use what worked
scope s {
    let tasks = urls.iter().map(|u| s.spawn(|| fetch(u))).collect()
    let mut pages: List<String> = []
    for t in tasks {
        match t.join() {
            Ok(p)  => pages.push(p)
            Err(e) => eprintln("fetch failed: {e}")
        }
    }
    pages
}
```

Rule 2 is what makes this trivial: an `Err` waits in the handle rather
than killing the scope.

**Prefer fail-fast where it is meaningful.**

```glide
// Good — if any leg fails, the request fails
scope s {
    let user = s.spawn(|| load_user(id))
    let prefs = s.spawn(|| load_prefs(id))
    Ok(Page{ user: user.join()?, prefs: prefs.join()? })
}
```

Same primitive, different composition. That is why there is no mode to
configure.

**Keep spawned closures small.** A `spawn` whose closure is fifteen
lines is fifteen lines you cannot test independently. Spawn a call to a
named function.

```glide
// Good
_ = s.spawn(|| sweeper(db))
```

**Do not nest scopes without a reason.** Each nesting level is a
synchronisation barrier. Nest when the lifetimes genuinely differ.

---

### 8. Examples

**Parallel work with a joined result:**

```glide-run
import time

fn work(id: Int, ms: Int) -> Int {
    time.sleep(ms.ms)
    id * 10
}

fn parallel() -> Int {
    scope s {
        let a = s.spawn(|| work(1, 20))
        let b = s.spawn(|| work(2, 10))
        a.join() + b.join()
    }
}

fn main() {
    println(parallel())
}
```

```
30
```

Two tasks, 20ms and 10ms, and the scope takes about 20ms rather than
30. (In the interpreter, `sleep` releases the lock, so this genuinely
overlaps.)

**All four rules, demonstrated:**

```glide-run
import time

// Rule 1 + 2: an Err is a value; join it and decide.
fn partial() -> List<String> {
    scope s {
        let a = s.spawn(|| Ok("alpha"))
        let b = s.spawn(|| Err("beta failed"))
        let c = s.spawn(|| Ok("gamma"))

        let mut good: List<String> = []
        for t in [a, b, c] {
            match t.join() {
                Ok(v)  => good.push(v)
                Err(e) => eprintln("  skipped: {e}")
            }
        }
        good
    }
}

// Rule 1: `?` exits the body; the scope cancels and joins the rest.
fn fail_fast() -> Result<Int, String> {
    scope s {
        let a = s.spawn(|| Err("boom"))
        let b = s.spawn(|| Ok(5))
        let x = a.join()?
        let y = b.join()?
        Ok(x + y)
    }
}

// Rule 3: an unjoined Err still fails the scope.
fn cannot_vanish() -> Result<Int, String> {
    scope s {
        _ = s.spawn(|| Err("silent failure"))
        Ok(1)
    }
}

fn main() {
    println("{partial():?}")
    println("{fail_fast():?}")
    println("{cannot_vanish():?}")
}
```

```
  skipped: beta failed
["alpha", "gamma"]
Err("boom")
Err("silent failure")
```

Three different behaviours from one primitive, chosen by how the body
composes.

**The server pattern — the shape most real programs have:**

```glide
import http
import sql
import time

fn sweeper(db: Db) {
    for {
        time.sleep(10.mins)
        _ = db.exec("delete from sessions where expires < :now",
                    ["now": time.now()]) ?? 0
    }
}

fn run() -> Result<(), Error> {
    let db = sql.open("sqlite:app.db")?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/health`, |req| http.text("ok"))

    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

`sweeper` is an infinite loop. It has no shutdown flag, no `done`
channel, no `context.Context` parameter, and no way to be told to stop.
When `serve` returns — because of an error, because the scope was
cancelled by an enclosing timeout, or because the whole thing is
unwinding — the sweeper is cancelled at its next blocking operation
(`time.sleep`), its `defer`s run, and the scope joins it.

Count the lines of shutdown code: zero.

The Go equivalent needs a `context.Context` threaded into `sweeper`, a
`select` on `ctx.Done()` inside its loop, an `errgroup` to collect the
server's error, and a `defer cancel()`. Chapter 27 does that comparison
properly.

**Bad versus good: the leaked goroutine**

```go
// Go — this compiles, runs, and leaks
func StartMonitor(db *sql.DB) {
    go func() {
        for {
            time.Sleep(time.Minute)
            checkHealth(db)
        }
    }()
}
```

`StartMonitor` returns immediately. The goroutine runs until the
process dies. If `StartMonitor` is called per request, you have a leak
that grows linearly with traffic, and it is invisible until a heap
dump.

```glide
// Glide — unwritable. `spawn` needs a scope, so the caller must
// decide how long the monitor should live.
fn start_monitor(s: Scope, db: Db) {
    _ = s.spawn(|| monitor(db))
}

fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    scope s {
        start_monitor(s, db)
        serve_forever()
    }
}
```

The lifetime is now visible at the call site, and it is bounded by a
lexical region a reader can see.

---

### 9. Summary & Exercises

**Summary**

- Glide is **green-threaded** — cheap tasks, no `async`/`await`, no
  function colouring. Async exists as a compiler transform for
  languages that cannot have a runtime or threads; Glide has both.
- Every task belongs to a **scope**. `scope s { … }` is an expression;
  `s.spawn(f)` returns a `Task`; `t.join()` blocks and returns exactly
  what the closure returned.
- **There is no ambient spawn.** `spawn` exists only on a scope handle,
  `main` included, which is what makes leaks unrepresentable.
- The four rules: **(1)** scope exit always joins every child on every
  path, cancelling first on early exit; **(2)** a child's `Err` is a
  value that waits in the handle; **(3)** an unjoined child's `Err`
  fails the scope at normal exit, first-wins, ignorable only with an
  explicit `let _ = t.join()`; **(4)** a child's panic cancels siblings
  immediately and re-panics at exit.
- Slogan: **values wait, bugs don't.**
- There is **no fail-fast mode**. `errgroup` semantics fall out of
  composition — `a.join()?` exits the body and rule 1 does the rest.
- One spawn primitive, not Kotlin's `launch`/`async` split: Kotlin
  needed two because exceptions are invisible; Glide's `Result`s are
  visible values.
- Accepted costs: no early cancellation on `Err` (a slow sibling is
  waited out); first error wins and the rest are dropped; stackful
  tasks cost kilobytes where async futures cost bytes.
- Interpreter: one lock, released around blocking operations, so tasks
  interleave exactly at cancellation points. No compute parallelism at
  this tier.
- **A spawned closure may not capture a `mut` binding** ✓ — the
  data-race archetype, turned into a compile error that names the
  binding and the two fixes. Immutable captures cross freely.
- ○: ownership transfer on channel send, and `Mutex<T>`.

**Exercises**

1. **Find the leaked goroutine.** In a Go codebase, find a function
   that starts a goroutine and returns without providing any way to
   stop it. Trace how the goroutine eventually terminates — if the
   answer is "when the process exits", you have found the shape Glide
   makes unwritable. Then write down what the scope would have to be
   and where it would live.

2. **Implement partial success and fail-fast with the same
   primitive.** Write one function that fetches five URLs and returns
   all successes, and another that fetches five URLs and fails if any
   fails. Note that the difference is entirely in how the body joins,
   not in any configuration. Then write a third that fails only if
   *more than two* fail.

3. **Cost the concession.** `DESIGN.md` concedes that async wins at
   extreme task counts. Estimate the crossover: at what number of
   concurrent idle connections does a few-kilobyte stack per task
   become a problem on a machine you actually deploy to? Then decide
   whether your real workloads live on either side of that line — for
   most backends the answer is "nowhere near", which is why the
   concession was affordable.
