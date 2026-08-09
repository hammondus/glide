# Chapter 26: Cancellation, Timeouts, and Deadlines

Go escaped `async`/`await`'s viral function colouring — and then
cancellation forced `ctx context.Context` to be the first parameter of
every serious function. `DESIGN.md` calls that **the same disease,
manual and unchecked**.

Glide's answer: cancellation belongs to the **scope**. It is ambient
within a scope's subtree, it is delivered at blocking operations, and
no signature mentions it.

```glide
scope(timeout: 5.s) {
    http.get(url)?
}
```

That is the whole surface. This chapter explains the machinery behind
it, including a mechanism that is genuinely unusual: cancellation is a
**third unwind**, neither error nor panic, and **user code cannot
catch it**.

Everything here is ✓.

---

### 1. Basic Usage

#### Duration and Instant

Two distinct types, never conflated:

```glide
let d = 1.s + 500.ms
println("{d}")            // 1.5s
println(d > 1.s)          // true

let t = time.now()        // an Instant
let elapsed = time.now() - t     // Instant - Instant -> Duration
```

Constructors are suffix properties on numbers: `.ns`, `.us`, `.ms`,
`.s`, `.mins`, `.h`. They work on `Int` and `Float`:

```glide
250.ms
0.5.s
30.mins
2.h
```

There is no `.days`, deliberately — a "day" is calendar arithmetic, DST
makes 23-hour days, and calendars belong to the future `time` module.
And it is `.mins` rather than `.min` because `a.min(b)` is the obvious
future math method on `Int`.

Arithmetic is minimal and total:

| Operation | Result |
|---|---|
| `Duration ± Duration` | `Duration` |
| `Duration * Int` (either order) | `Duration` |
| `Duration / Int` | `Duration` |
| `Instant - Instant` | `Duration` |
| `Instant ± Duration` | `Instant` |
| comparisons on both | `Bool` |

There is no `Instant + Instant` (meaningless — the operator simply does
not exist) and no `Duration / Duration` (Go's float-division wart; if
you want a ratio, divide nanosecond counts explicitly).

#### `scope(timeout:)`

```glide
import time

fn timed() -> String {
    let r = scope(timeout: 25.ms) s {
        _ = s.spawn(|| time.sleep(10.s))
        time.sleep(10.s)
        "finished"
    }
    match r {
        Ok(m)        => m
        Err(Timeout) => "timed out"
        Err(_)       => "other"
    }
}

fn main() {
    println(timed())
}
```

```
timed out
```

`scope(timeout: d)` evaluates to `Result<T, Timeout>`: `Ok(v)` when the
body completes, `Err(Timeout)` when the clock wins. Both the body *and*
the spawned child were cancelled and joined.

`scope(deadline: t)` takes an `Instant` instead. Go's `context` has
both because real code wants both.

The grammar is `scope [(config)] [handle] { body }` — config first,
then handle. That is Trio's shape (`with move_on_after(5) as scope:`),
and the config parens reuse the named-argument machinery rather than
introducing a new syntax class.

#### Body errors propagate *through* the timeout

Scopes are control-flow transparent:

```glide
fn load() -> Result<Config, Error> {
    scope(timeout: 5.s) {
        let text = fs.read_string(path)?      // this Err propagates out
        parse(text)
    }
}
```

The `?` inside the body exits the *enclosing function*, not just the
scope. So body errors never nest inside the timeout's `Result` — you
never get `Result<Result<T, E>, Timeout>`.

The `Timeout` itself converts at the outer `?` via `E.from(Timeout)` —
the `?`-conversion machinery from Chapter 19 is what makes timeouts
non-viral.

#### `Timeout` matches as a bare pattern

```glide
match r {
    Ok(v)        => …
    Err(Timeout) => …
}
```

#### Reading the effective deadline

```glide
scope(timeout: 5.s) s {
    let d = s.deadline()      // Option<Instant>
    …
}
```

`s.deadline()` returns the nearest enclosing timeout or deadline,
inherited from outer scopes. This is the one place cancellation state
is readable, and it is through an **explicit handle** rather than
hidden.

#### Cancellation points

Cancellation is delivered **only at blocking operations**:

- `t.join()`
- `time.sleep(d)`
- channel `send` and `recv`, and a blocking `select`
- IO: `http.serve`, `http.get`, `http.post`, `db.exec`, `db.query`
- generator handoffs

A pure compute loop is **uncancellable**. That is a recorded cost,
uniform with Trio, Kotlin, and Swift.

#### `defer` and `errdefer` both run

A cancelled task unwinds, running its `defer` blocks (it must release
its locks) **and** its `errdefer` blocks (the operation did not
complete, so compensations apply). Chapter 21's table:

| Exit path | `defer` | `errdefer` |
|---|---|---|
| Cancellation | ✓ | ✓ |

#### You cannot catch it

There is no `Cancelled` error type, no way to observe cancellation, and
no way to suppress it. This is the closure property, and section 3
explains why it matters.

#### Only scopes cancel

There is no `t.cancel()`. Cancellation comes from the enclosing scope
on exactly three events:

1. Early exit from the scope body (a `?`, `return`, or `break`).
2. A sibling's panic.
3. A timeout or deadline expiring.

---

### 2. Under the Hood

#### The third unwind

Glide has three ways a computation stops early, and cancellation is the
odd one:

| Unwind | Cause | Catchable? | Is it a value? |
|---|---|---|---|
| Error | `?` propagating an `Err` | Yes | Yes |
| Panic | A bug | No | No |
| **Cancellation** | The scope dying | **No** | **No** |

`glide/DESIGN-DECISIONS.md` records the implementation choice and its
reasoning:

> **Cancellation is a Go panic (`cancelUnwind`), not a `sig`.** Panics
> already unwind through every construct uncatchably, and
> `evalBlockDeferred`'s panic path already runs `defer` AND `errdefer`
> on the way out — which is exactly the ratified cancellation
> behavior. A `sig` variant would have needed a "cannot be consumed by
> match/loops/user code" rule bolted onto every consumer.

Only the scope machinery (and the task and generator wrappers) recovers
it.

That is a nice piece of design economy: the cleanup semantics
cancellation needs were already implemented for panics, so the
mechanism came for free.

#### How a blocking operation becomes a cancellation point

Every host-level blocking call obtains a context from `hostCtx()`,
which is wired to the current task's cancellation channel and to the
nearest enclosing deadline. When the scope cancels, the context is
cancelled, the blocking operation returns, and the interpreter raises
`cancelUnwind`.

The interpreter's single lock is released while blocked — which is what
makes the interleaving happen exactly at cancellation points, as
Chapter 25 described.

#### `Timeout` is a synthetic variant

An interesting corner. A timed-out scope's `Err` payload is a variant
value with type and name `Timeout`, synthesised by the interpreter — so
`Err(Timeout)` matches the bare pattern `Timeout`, renders as
`Timeout`, and converts through a user's `fn from(t: Timeout)`, all
**without any global type declaration the program did not write**.

The checker era makes `Timeout` a real stdlib type, and nothing about
programs changes.

#### Duration and Instant are host types

The interpreter wraps Go's `time.Duration` and `time.Time`, which means
the dual wall/monotonic semantics come free: elapsed-time arithmetic
automatically uses the monotonic reading, so measuring a timeout across
an NTP step does not produce a negative duration.

`==` on `Instant` compares as Go's `Equal` (wall + monotonic aware),
not as a struct comparison. `Duration` renders as Go does — `1.5s`,
`2m30s`.

#### The deferred sub-question

`DESIGN.md` leaves one thing open: what happens when a `defer` block
performs a blocking operation *during* cancellation? Leaning toward
Trio's answer — cleanup gets a brief grace period, then blocking
operations fail loudly — and undecided.

---

### 3. Why This Design?

#### Why `context.Context` is function colouring by hand

This is the central argument, and it is worth spelling out.

Go avoided `async`/`await` and therefore avoided a viral type-system
property. Then cancellation had to come from somewhere, and Go's answer
was a value threaded through every call:

```go
func GetUser(ctx context.Context, id int) (*User, error)
func loadPrefs(ctx context.Context, u *User) (*Prefs, error)
func query(ctx context.Context, q string) (*Rows, error)
```

`DESIGN.md` names it: **the same disease, manual and unchecked.**

- It is **viral**: any function that might call a cancellable operation
  needs a `ctx`, so `ctx` reaches everywhere.
- It is **unchecked**: nothing stops you passing
  `context.Background()`, or forgetting to check `ctx.Done()`, or
  storing the ctx in a struct (which the documentation forbids and
  people do anyway).
- It is **two features in one**: cancellation *and* request-scoped
  values, and `ctx.Value` is an untyped stringly grab bag where auth,
  transactions, and loggers travel invisibly.

The Glide answer takes the parameter away entirely: cancellation is a
property of the *scope you are running in*, and blocking operations
consult it. No signature changes.

#### Why cancellation is uncatchable

Trio and Kotlin model cancellation as an exception that **convention
forbids swallowing**. `DESIGN.md`: making it uncatchable turns the
convention into a guarantee.

The failure mode with a catchable cancellation is specific and common:
a `catch (Exception e)` or `except:` somewhere in the stack swallows
the cancellation, the task keeps running, and the scope that was trying
to shut down hangs waiting for it. Kotlin's documentation devotes real
space to *not* catching `CancellationException`, which is the tell.

Uncatchable removes the possibility.

#### The closure property

This is the subtle one and it is elegant:

> **User code never observes a cancelled task.**

Cancellation implies the scope is going down. If the scope is going
down, no live code remains to `join()` a cancelled child. So there is
nothing to observe, no `Cancelled` value to return, and — crucially —
**no `Cancelled` error type leaking into any signature.**

Kotlin leaks `CancellationException` into every `catch` block in its
ecosystem. Glide's type system never mentions cancellation at all.

#### Why only scopes cancel

There is no `t.cancel()`, and Kotlin's `job.cancel()` is the thing
being declined.

`DESIGN.md`: `job.cancel()` is a structured-concurrency escape hatch —
arbitrary code killing a task it does not own. Once any code can cancel
any task, the lexical reasoning that structured concurrency exists to
provide is gone.

The legitimate use case — "cancel the losers when one wins" — is a
*race* construct, and it is `select`-shaped rather than
cancel-shaped (Chapter 28).

#### Why cancellation points are blocking operations only

Because the alternative is worse.

`DESIGN.md` records that loop back-edge checks in the interpreter were
**considered and rejected**: dev-tier programs would be more cancellable
than release-tier, and **semantics that differ by tier are poison.**

That is a strong principle worth noting — the tiered-backend design
allows overflow, backtraces, hygiene, and debug info to differ, and it
explicitly does not allow *semantics* to differ.

The recorded cost: a pure compute loop cannot be cancelled. If you
write `for { compute() }` with no blocking call, a timeout will not
stop it. Trio, Kotlin, and Swift all have the same property, and the
mitigation is the same: put a blocking operation (even a zero
`sleep`) in a long compute loop if it must be interruptible.

#### Why `errdefer` fires on cancellation

Because the operation did not complete, so compensations apply.
`DESIGN.md` is explicit: skipping them would make timeouts corrupt
state that errors do not.

If a timeout leaves a half-written file that an error would have
cleaned up, the timeout is a data-corruption mechanism. Firing
`errdefer` makes the two paths consistent.

#### Why `Duration` and `Instant` are distinct types

What Go and C++ `chrono` got right. A duration and a point in time are
different concepts, and conflating them permits nonsense:
`now() + now()`, `sleep(timestamp)`, comparing a timeout to a clock
reading.

`DESIGN.md` goes further in the designed `time` module: four types —
`Time` (an instant), `Date` (calendar date, no zone), `TimeOfDay`
(09:00), and `ZonedTime` (civil time plus IANA zone) — because Go's
single `time.Time` moonlighting as all four manufactures bugs, and
`java.time` is the gold standard.

The sharpest of those: **future events are civil time plus a zone,
never instants.** "9am Sydney next March" is not a fixed point until it
happens — DST legislation moves the instant, not the 9am. Storing
future appointments as UTC is one of the subtlest widespread datetime
bugs in production.

#### The monotonic hybrid

Kept from Go: `now()` carries both wall and monotonic readings, and
elapsed-time arithmetic automatically uses monotonic. That kills the
measured-a-timeout-across-an-NTP-step bug **in the default path**.

`DESIGN.md`'s framing is worth keeping: Rust makes you choose
correctly (`Instant` versus `SystemTime`); Go makes the correct thing
happen when you do not think. For a bug this subtle, the second is
better.

---

### 4. Competing Approaches

**Go.** `context.Context` as an explicit first parameter, with
`WithCancel`, `WithTimeout`, `WithDeadline`, and `WithValue`.
Cancellation is cooperative — you must `select` on `ctx.Done()` — and
unchecked. Go's own standard library duplicated its entire database API
(`Query`/`QueryContext`, `Exec`/`ExecContext`) to retrofit it, which
`DESIGN.md` cites as the cost.

**Python (Trio).** `move_on_after(5)` and `fail_after(5)` as scope
config — the direct shape Glide copies. Cancellation is a
`Cancelled` exception that convention says not to catch, and Trio's
documentation works hard on that convention.

**Kotlin.** Structured cancellation with `withTimeout`, cooperative
`isActive` checks, and `CancellationException`. Also `job.cancel()`,
which Glide declines. The `CancellationException`-in-every-catch-block
problem is the specific thing uncatchability fixes.

**Swift.** `Task.isCancelled` and `Task.checkCancellation()` —
cooperative and explicit, so you must poll. `withTaskGroup` gives
structure. Swift's approach is more visible and more manual than
Glide's ambient delivery.

**Java (Loom).** `StructuredTaskScope` with timeouts; interruption
remains the underlying mechanism, and `InterruptedException` is a
checked exception that everyone mishandles — the twenty-five-year
version of the catch-the-cancellation problem.

**Rust (async).** Dropping a future cancels it, which is elegant and
has a sharp edge: cancellation can happen at *any* await point, so
"cancellation safety" is a property every async function must document
and most do not. `tokio::select!` is where this bites hardest.

**C#.** `CancellationToken` passed explicitly — Go's model in a
language with exceptions. Same virality, same unchecked-ness.

---

### 5. Common Mistakes

**Expecting a compute loop to be cancellable.**

```glide
// Bad — no blocking operation, so a timeout cannot stop this
scope(timeout: 1.s) {
    for { fibonacci(1_000_000) }
}

// Good — a blocking operation gives cancellation a delivery point
scope(timeout: 1.s) {
    for {
        chunk_of_work()
        time.sleep(0.ms)      // a cancellation point
    }
}
```

The second form is not elegant, and it is the honest mitigation for the
recorded cost.

**Trying to catch cancellation.** There is nothing to catch. If you
need to know that a deadline was hit, that is the scope's `Result`:

```glide
match scope(timeout: 5.s) { work() } {
    Ok(v)        => v
    Err(Timeout) => fallback()
}
```

**Looking for `t.cancel()`.** It does not exist. Restructure so the
scope's lifetime is the cancellation boundary, or use `select`
(Chapter 28) for a race.

**Expecting body errors to nest inside the timeout Result.** They do
not — scopes are control-flow transparent, so a `?` in the body exits
the enclosing function.

**Passing a timeout as a parameter.**

```glide
// Bad — this is ctx-threading reinvented
fn fetch_all(urls: List<String>, timeout: Duration) -> …

// Good — the caller sets the scope
scope(timeout: 5.s) {
    fetch_all(urls)
}
```

The whole point is that the timeout is ambient. Reintroducing it as a
parameter is the disease coming back.

**Using bare integers for durations.**

```glide
// Bad — 60 what?
sleep(60)

// Good
time.sleep(1.mins)
```

Duration literals are the reason `sleep(1.mins)` cannot be confused
with `sleep(60)` of unknown unit. There is no implicit conversion.

**Reaching for `.days`.** It does not exist. A day is calendar
arithmetic. Use `24.h` if you genuinely mean 24 hours, and note that on
a DST boundary that is *not* the same as "tomorrow at the same time".

**Comparing Instants with struct equality intuitions.** `==` on
`Instant` behaves like Go's `Equal`, which is wall-clock aware. Two
Instants representing the same moment compare equal even if their
monotonic readings differ.

---

### 6. Performance Considerations

**Cancellation costs nothing on the happy path.** There is no polling
loop and no periodic check. A blocking operation registers with the
task's cancellation channel; if nothing cancels, nothing happens.

**Delivery is at the next blocking operation**, so the latency between
"the scope cancelled" and "the task stopped" is the duration of the
current non-blocking stretch. For IO-bound code that is microseconds;
for a long compute stretch it is the length of the compute.

**`hostCtx()` allocates a context per blocking call** in the
interpreter, plus a goroutine when the task has a cancellation channel.
That is a real per-call cost at this tier and not at the compiled one.

**Unwinding runs every `defer` and `errdefer` frame.** That is required
for correctness and happens once, on the way out.

**`Duration` and `Instant` are value types** — 64-bit integers plus, in
`Instant`'s case, the monotonic reading. Arithmetic is integer
arithmetic. No allocation.

**Scope exit with a timeout costs one timer.**

---

### 7. Best Practices

**Put the timeout at the boundary where the operation has a deadline.**

```glide
// Good — the request has a budget; everything inside inherits it
fn handle(req: Request) -> Result<Response, ApiError> {
    scope(timeout: 2.s) {
        let user = load_user(req.user_id)?
        let prefs = load_prefs(user)?
        Ok(render(user, prefs))
    }
}
```

Neither `load_user` nor `load_prefs` mentions a timeout. Their database
queries are cancellation points, so both are bounded by the scope.

**Do not thread timeouts as parameters.** If you find a `timeout:
Duration` parameter spreading through your call graph, you have
rebuilt `ctx`.

**Use `deadline:` for a shared budget, `timeout:` for a fresh one.**

```glide
// A per-attempt budget
for attempt in 1..=3 {
    match scope(timeout: 1.s) { try_once() } {
        Ok(v)        => return Ok(v)
        Err(Timeout) => continue
    }
}

// A total budget across attempts
let end = time.now() + 3.s
for attempt in 1..=3 {
    match scope(deadline: end) { try_once() } {
        Ok(v)        => return Ok(v)
        Err(Timeout) => break        // the whole budget is gone
    }
}
```

**Write durations with units, always.** `500.ms`, `30.s`, `2.h`. Never
a bare integer that a reader has to interpret.

**Give long compute loops a cancellation point** if they need to be
interruptible. This is the honest mitigation, and it should be a
conscious decision rather than a surprise.

**Use `s.deadline()` to adapt, not to poll.**

```glide
// Good — size the batch to the remaining budget
scope(timeout: 5.s) s {
    if let d = s.deadline() {
        let remaining = d - time.now()
        let batch = if remaining > 3.s { 1000 } else { 100 }
        process(batch)
    }
}
```

**Never write cancellation-detection logic.** There is nothing to
detect. If you want to know that a deadline was hit, read the scope's
`Result`.

---

### 8. Examples

**The timeout, complete:**

```glide
import time

fn with_deadline() -> String {
    let r = scope(timeout: 25.ms) s {
        _ = s.spawn(|| time.sleep(10.s))
        time.sleep(10.s)
        "finished"
    }
    match r {
        Ok(msg)      => msg
        Err(Timeout) => "timed out (children cancelled and joined)"
        Err(_)       => "other"
    }
}

fn main() {
    println(with_deadline())
}
```

```
timed out (children cancelled and joined)
```

Both the body's `sleep` and the child's `sleep` were cancelled at their
next cancellation point (immediately, since they were blocked), the
child was joined, and the scope produced `Err(Timeout)`. No shutdown
code, no `done` channel, no polling.

**Duration arithmetic:**

```glide
import time

fn main() {
    let d = 1.s + 500.ms
    println("{d}")                  // 1.5s
    println(d > 1.s)                // true
    println("{d * 3}")              // 4.5s
    println("{2.mins + 30.s}")      // 2m30s

    let start = time.now()
    time.sleep(20.ms)
    let elapsed = time.now() - start
    println("elapsed at least 20ms: {elapsed >= 20.ms}")
}
```

```
1.5s
true
4.5s
2m30s
elapsed at least 20ms: true
```

`Duration` renders as Go does. Note `time.now() - time.now()` is an
`Instant - Instant`, producing a `Duration` — and using the monotonic
reading, so an NTP step during the sleep cannot make it negative.

**Side by side with Go — the shape that motivates the whole design:**

```go
// Go: ctx in every signature, a select in every loop
func Sweeper(ctx context.Context, db *sql.DB) error {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if _, err := db.ExecContext(ctx, "delete from stale"); err != nil {
                return err
            }
        }
    }
}

func Run() error {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    g, ctx := errgroup.WithContext(ctx)
    g.Go(func() error { return Sweeper(ctx, db) })
    g.Go(func() error { return server.ListenAndServe() })
    return g.Wait()
}
```

```glide
// Glide: no ctx, no select, no errgroup, no cancel
fn sweeper(db: Db) {
    for {
        time.sleep(1.mins)
        _ = db.exec("delete from stale") ?? 0
    }
}

fn run(db: Db, r: Router) -> Result<(), Error> {
    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

Count what disappeared: the `ctx` parameter on `sweeper`, the `select`
with its `ctx.Done()` arm, the ticker (a plain `sleep` is a
cancellation point), the `errgroup`, the `WithCancel`, and the
`defer cancel()`.

`sweeper` has no way to be told to stop, and it stops correctly.
`time.sleep` is a cancellation point, so when the scope dies — because
`serve` returned, or because an enclosing timeout fired, or because
something panicked — the sleeping sweeper unwinds, runs its defers, and
is joined.

That is the payoff `DESIGN.md` is claiming when it says the
ctx-replacement covers HTTP for free.

**Cancellation reaching an HTTP client:**

```glide
import http
import time

fn fetch_with_budget(url: String) -> String {
    let r = scope(timeout: 50.ms) {
        match http.get(url) {
            Ok(resp) => "{resp.status()}"
            Err(e)   => "error: {e}"
        }
    }
    match r {
        Ok(s)        => s
        Err(Timeout) => "gave up after 50ms"
        Err(_)       => "other"
    }
}
```

`http.get` is a cancellation point, so the scope's deadline aborts the
in-flight request. Nothing in `fetch_with_budget`'s signature mentions
a timeout, and `http.get` takes no `ctx`.

**Bad versus good: the timeout parameter**

```glide
// Bad — ctx-threading, rebuilt by hand
fn load_page(id: Int, timeout: Duration) -> Result<Page, Error> {
    let user = load_user(id, timeout)?
    let prefs = load_prefs(user, timeout)?      // …and now the budget doubles
    Ok(render(user, prefs))
}
```

Two problems. The parameter is viral — `load_user` and `load_prefs`
both need it, and so does everything they call. And the semantics are
wrong: passing the same `timeout` to two sequential calls gives a total
budget of twice the timeout.

```glide
// Good
fn load_page(id: Int) -> Result<Page, Error> {
    let user = load_user(id)?
    let prefs = load_prefs(user)?
    Ok(render(user, prefs))
}

// The caller sets the budget, once, for the whole subtree
scope(timeout: 2.s) {
    load_page(id)?
}
```

One budget, applied to the whole subtree, and no signature mentions
it.

---

### 9. Summary & Exercises

**Summary**

- **Cancellation belongs to the scope.** No `ctx` parameter, no
  signature changes. `scope(timeout: 5.s) { … }` bounds everything in
  the subtree.
- `scope(timeout: d)` and `scope(deadline: t)` evaluate to
  `Result<T, Timeout>`. Body errors propagate *through* the scope, so
  they never nest inside the timeout's Result. `Timeout` converts at
  the outer `?` via `E.from(Timeout)`.
- **Cancellation is a third unwind** — neither error nor panic. It is
  **uncatchable**, which turns Trio's and Kotlin's "do not swallow
  this" convention into a guarantee.
- **`defer` and `errdefer` both run** during cancellation: locks must
  be released, and compensations apply because the operation did not
  complete.
- **Closure property:** user code never observes a cancelled task, so
  no `Cancelled` type leaks into any signature. Kotlin leaks
  `CancellationException` into every catch block; Glide's type system
  never mentions cancellation.
- **Only scopes cancel.** There is no `t.cancel()` — arbitrary code
  killing a task it does not own is the escape hatch that destroys the
  lexical reasoning.
- **Cancellation points are blocking operations**: `join`, `sleep`,
  channel ops, `select`, IO, generator handoffs. Pure compute loops are
  uncancellable — a recorded cost, uniform with Trio/Kotlin/Swift. Loop
  back-edge checks were rejected because semantics that differ by tier
  are poison.
- **`Duration` and `Instant` are distinct types.** Constructors are
  suffix properties (`250.ms`, `0.5.s`, `2.h`); no `.days`, because a
  day is calendar arithmetic. No `Instant + Instant`, no
  `Duration / Duration`.
- The **monotonic hybrid** is kept from Go: elapsed-time arithmetic
  uses the monotonic reading automatically, so an NTP step cannot
  corrupt a measurement.
- `s.deadline()` reads the nearest inherited deadline — the one place
  cancellation state is readable, through an explicit handle.

**Exercises**

1. **Count the `ctx`s.** In a Go service, count how many function
   signatures take a `context.Context`. Then count how many of those
   functions actually *use* it for anything other than passing it
   along. The ratio is the tax the parameter charges, and it is what
   ambient cancellation deletes.

2. **Find the uncancellable loop.** Write a Glide program with a
   compute-only loop inside a `scope(timeout:)` and confirm the timeout
   does not stop it. Then add a cancellation point and confirm it does.
   Decide where you would put such a point in a real CPU-bound job —
   per batch, per row, per second? — and note that this is a decision
   Go, Rust, Trio, Kotlin, and Swift all make you take.

3. **Design the datetime types.** `DESIGN.md` specifies four —
   `Time`, `Date`, `TimeOfDay`, `ZonedTime` — with the rule that future
   events are civil time plus zone, never instants. Take a scheduling
   feature you have built and write down which type each stored field
   should be. Then find the field that was stored as UTC and should not
   have been; almost every calendar application has one.
