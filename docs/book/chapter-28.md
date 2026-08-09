# Chapter 28: `select`

`select` waits on several channel operations at once and takes the
first one that is ready. It is the construct that makes channels
composable, and `DESIGN.md` calls it, with channels, "Go's crown
jewel".

Glide's version is **match's clothes, Go's engine, `ctx.Done` designed
away**. The syntax is `match`'s; the semantics — evaluate operands
once, choose uniformly at random among ready arms, block if none — are
Go's verbatim; and the single most common arm in real Go `select`
statements has no Glide equivalent, because the scope cancels a blocked
select directly.

It also gains something Go lacks and CSP had in 1983: **per-arm
guards**.

Everything here is ✓.

---

### 1. Basic Usage

#### The shape

```glide
select {
    pat = rx.recv() => expr
    tx.send(v)      => expr
    else            => expr
}
```

Arms are **line-separated**, like `match`. `select` is an
**expression** — it yields the taken arm's value.

Three arm kinds:

| Arm | Meaning |
|---|---|
| `pat = rx.recv() => e` | receive; `pat` matches the `Option<T>` |
| `tx.send(v) => e` | send |
| `else => e` | non-blocking default |

#### Waiting on two channels

```glide
import time

fn main() {
    let (atx, arx) = channel()
    let (ctx2, crx) = channel()

    scope s {
        _ = s.spawn(|| {
            time.sleep(5.ms)
            atx.send("a")
        })
        _ = s.spawn(|| {
            time.sleep(20.ms)
            ctx2.send("c")
        })

        for i in 0..2 {
            let got = select {
                Some(v) = arx.recv() => "from a: {v}"
                Some(v) = crx.recv() => "from c: {v}"
            }
            println(got)
        }
    }
}
```

```
from a: a
from c: c
```

The first `select` blocks until either channel is ready — `a` arrives
at 5ms. The second blocks again and takes `c` at 20ms.

#### The receive pattern matches the Option

`rx.recv()` returns `Option<T>`, and the arm's pattern matches *that*.
So `Some(v)` and `None` are both available, and the same channel may
appear in several arms:

```glide
select {
    Some(v) = rx.recv() => handle(v)
    None    = rx.recv() => "channel closed"
}
```

A ready operation's arms are tried **in order**; if none matches, it is
a runtime error.

#### `else` makes it non-blocking

```glide
let r = select {
    Some(v) = erx.recv() => "got {v}"
    else                 => "nothing ready"
}
println(r)          // nothing ready
```

With an `else` arm, `select` never blocks: if nothing is ready
immediately, `else` runs.

#### Timeouts are an ordinary receive

```glide
import time

let r = select {
    _ = time.after(10.ms).recv() => "timeout arm"
}
println(r)          // timeout arm
```

`time.after(d)` returns a `Receiver<()>` that fires after `d`, so a
timeout arm is just another receive case. There is no special syntax.

#### Arm guards

An optional `if cond` per arm, **evaluated once at entry**. A false
guard *removes* the arm:

```glide
select {
    Some(v) = arx.recv() if a_open => { total += v }
    None    = arx.recv() if a_open => { a_open = false }
    Some(v) = brx.recv() if b_open => { total += v }
    None    = brx.recv() if b_open => { b_open = false }
}
```

This is the fan-in pattern: drain two producers, disabling each
channel's arms as it closes. In Go you would set the channel variable
to `nil` to disable a case — a trick that works because receiving from
a nil channel blocks forever. Guards say it honestly.

#### There is no `ctx.Done()` arm

The most common arm in real Go `select` statements does not exist here,
because **a blocking select is a cancellation point**. When the
enclosing scope dies, the blocked select unwinds. Chapter 26 covers the
mechanism.

#### Zero arms is a parse error

`select {}` — Go's spelling for "block forever" — does not parse.

---

### 2. Under the Hood

#### `select` rides `reflect.Select`

`glide/DESIGN-DECISIONS.md`: the interpreter implements `select` with
Go's `reflect.Select`, which gives dynamic arity and
uniform-random-among-ready for free. **It literally is Go's select.**

Details recorded there:

- **Receive arms sharing a channel share one case**, and the delivered
  value tries their patterns in order.
- **Send arms never merge** — each sends its own value.
- The whole wait happens with the interpreter lock released, with the
  task's cancellation channel as an extra case. That is how a blocked
  select becomes a cancellation point.

#### Evaluation order

Go's engine verbatim:

1. **Operands evaluate once, at entry, in order.** `rx` in
   `Some(v) = rx.recv()` is evaluated once; the channel expression is
   not re-evaluated per poll.
2. **Guards evaluate once, at entry.** A false guard removes the arm
   for this execution of the select.
3. Among the arms whose operations are **ready**, one is chosen
   **uniformly at random**. This is anti-starvation: a
   perpetually-ready channel cannot monopolise a select that always
   checks it first.
4. If none is ready and there is an `else`, take `else`. If none is
   ready and there is no `else`, **block**.
5. If every arm is disabled by a guard and there is no `else`, it is a
   **runtime error**.

#### The sharpest edge

Recorded honestly in `DESIGN.md`: **an unmatched ready receive has
already consumed the value when it errors.**

```glide
select {
    Some(v) = rx.recv() => use(v)
    // no None arm
}
```

If the channel closes, `recv()` yields `None`, no arm's pattern
matches, and the select raises a runtime error — but the receive
already happened. The value (here, the end-of-stream signal) is gone.

This is the same bargain dynamic `match` already makes, and it becomes
static in the checker era. Until then: cover both `Some` and `None`, or
know that you will not see a close.

---

### 3. Why This Design?

#### Why match's syntax

One glyph, one meaning. `=>` separates a pattern from a body in
`match`, so it does the same here. Arms are line-separated in `match`,
so they are here.

The deeper alignment: a select arm genuinely *is* a pattern match — the
pattern is over the `Option<T>` a receive produces. Go's
`case v, ok := <-ch:` is the comma-ok form doing the same job with less
structure, and it cannot express "the `None` case goes over there".

#### Why Go's engine verbatim

Because the semantics are right and have been battle-tested for fifteen
years.

**Uniform random choice among ready arms** is the non-obvious one, and
it is anti-starvation: with a deterministic order, a channel that is
always ready would starve every arm below it. Go got this right, and
the accepted cost is that **there is no priority select** — you cannot
say "prefer the shutdown channel". The workaround carries over: a
nested select with `else` to poll the high-priority channel first.

**Operands evaluated once at entry** avoids re-evaluating a channel
expression on every poll, which matters when the expression has a cost
or a side effect.

#### Why guards, which Go lacks

Go's way to disable a select case is to set the channel variable to
`nil`, exploiting the fact that receiving from a nil channel blocks
forever:

```go
for aOpen || bOpen {
    select {
    case v, ok := <-a:
        if !ok { a = nil; aOpen = false; continue }   // disable this case
        total += v
    case v, ok := <-b:
        if !ok { b = nil; bOpen = false; continue }
        total += v
    }
}
```

That works and it is a *trick* — it relies on nil-channel semantics
most people have to look up, and Glide has no nil anyway.

`DESIGN.md` notes the historical point: **Occam's ALT had guarded
alternatives in 1983.** The CSP lineage had this feature and Go dropped
it. Glide puts it back:

```glide
Some(v) = arx.recv() if a_open => { total += v }
```

Same behaviour, stated rather than encoded.

#### Why no `ctx.Done()` arm

This is the payoff of the cancellation design, and it is the single
biggest difference from Go in practice.

Look at almost any real Go `select` in a long-running loop:

```go
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case v := <-work:
        process(v)
    }
}
```

Half the arms are cancellation plumbing. In Glide a blocking select is
a cancellation point, so the scope cancels it directly:

```glide
for {
    let v = select {
        Some(v) = work_rx.recv() => v
        None    = work_rx.recv() => break
    }
    process(v)
}
```

No `Done` arm exists, and none is needed. `DESIGN.md` calls this "the
payoff of the cancellation design", and Chapter 26 is where the
mechanism lives.

#### Why no timeout arm syntax

Go has no special timeout syntax either — `case <-time.After(d):` is an
ordinary receive. Glide keeps that: `time.after(d)` returns a
`Receiver<()>`.

And most real timeout cases are better served by
`scope(timeout: d) { … }`, which bounds the whole subtree rather than
one wait.

#### Why no select over task joins

You might want "whichever of these tasks finishes first". `DESIGN.md`
declines it for now, with a workaround: a selectable task sends its
result into a channel. Revisit on evidence.

The reason it is not free: `join()` is a different kind of wait from a
channel operation, and unifying them means either making `Task` a
channel (which leaks the implementation) or growing the select engine a
second case kind.

#### Why zero arms is a parse error

`select {}` in Go means "block forever", which is a deadlock spelled as
a statement. It is used to keep `main` alive, which in Glide is what a
scope does.

---

### 4. Competing Approaches

**Go.** `select` with `case` clauses, uniform random choice, `default`
for non-blocking, and the nil-channel disable trick. The direct model
and the direct engine. Glide changes the syntax to match `match`, adds
guards, and removes the need for `ctx.Done()` arms.

**Occam (1983).** `ALT` with guarded alternatives — the CSP ancestor,
and the source of the observation that Go dropped a feature its lineage
already had.

**Rust.** `tokio::select!` — a macro, with per-branch `if` guards
(same idea as Glide's) and a documented cancellation-safety hazard:
when one branch completes, the others are *dropped* mid-future, which
can lose data if a future was partway through consuming something.
Glide's arms do not have that problem because a blocked receive that
loses the race simply never happened.

**Kotlin.** `select { onReceive { } onSend { } }` — a DSL with a
similar shape, marked experimental for years.

**Erlang.** Selective `receive` with pattern matching over the mailbox
— arguably the most elegant version, and a different model: the
mailbox belongs to the process and messages can be left for later.

**C / POSIX.** `select(2)`, `poll(2)`, `epoll`, `kqueue` — the same
idea at the file-descriptor level, and where the name comes from.

**Java.** `Selector` for NIO channels; nothing at the language level
for queues. `BlockingQueue` has no multi-queue wait, so you build one
out of a shared queue and tagging.

---

### 5. Common Mistakes

**Forgetting the `None` arm and losing the close.**

```glide
// Bad — when the channel closes, this errors AND the None is consumed
select {
    Some(v) = rx.recv() => use(v)
}

// Good
select {
    Some(v) = rx.recv() => use(v)
    None    = rx.recv() => break
}
```

This is the sharpest edge in the chapter.

**Expecting priority order.** Arms are chosen **uniformly at random**
among the ready ones. Writing the important channel first does nothing.

```glide
// Bad — this does not prefer the urgent channel
select {
    Some(v) = urgent_rx.recv() => handle(v)
    Some(v) = normal_rx.recv() => handle(v)
}

// The workaround, carried over from Go: poll first, then block
let got = select {
    Some(v) = urgent_rx.recv() => Some(v)
    else                       => None
}
let v = got ?? select {
    Some(v) = urgent_rx.recv() => v
    Some(v) = normal_rx.recv() => v
}
```

**Writing a `ctx.Done()` arm out of habit.** There is nothing to write.
The scope cancels a blocked select.

**Expecting guards to be re-evaluated.** They are evaluated **once at
entry**. A guard that becomes true while the select is blocked does not
enable its arm; the arm was already removed for this execution.

```glide
// The loop re-enters the select, which re-evaluates the guards
for a_open || b_open {
    select {
        Some(v) = arx.recv() if a_open => { total += v }
        None    = arx.recv() if a_open => { a_open = false }
        …
    }
}
```

**Disabling every arm with no `else`.** Runtime error. If all your
guards can be false simultaneously, either add an `else` or make the
enclosing loop's condition prevent it — which is what
`for a_open || b_open` does above.

**Using `select` for a single channel.** A plain `rx.recv()` is
clearer and cheaper.

**Writing `select {}`.** Parse error. To keep a task alive, use a
scope with a long-running child.

**Putting side effects in the operand expressions.** They evaluate once
at entry, in order, whether or not that arm is taken. A `select` whose
operand is `make_channel_and_register()` does the registration
regardless.

---

### 6. Performance Considerations

**A blocking select registers with every channel** and waits on all of
them. Cost scales with arm count, and for the small arm counts real
code uses this is negligible.

**Uniform random choice costs a random number** per ready-set. Go pays
this too; it is the price of anti-starvation.

**`else` makes it a poll**, so a `select` with `else` in a tight loop is
a busy-wait. That is almost never what you want:

```glide
// Bad — spins the CPU
for {
    let v = select {
        Some(v) = rx.recv() => Some(v)
        else                => None
    }
    if let x = v { handle(x) }
}

// Good — block
for v in rx {
    handle(v)
}
```

**In the interpreter**, `reflect.Select` builds a case slice per
execution — one allocation proportional to arm count — and the wait
happens with the interpreter lock released. Cancellation adds one case.

**Guards cost their own evaluation, once per entry.** Keep them cheap;
a guard calling an expensive predicate runs on every loop iteration
that re-enters the select.

**`time.after(d)` allocates a timer and a channel per call.** In a loop
that re-enters a select with a timeout arm, that is a timer per
iteration — the same cost Go's `time.After` has, and the same reason Go
programmers reach for a reusable `time.Timer` in hot loops.

---

### 7. Best Practices

**Always cover `None` when the channel can close.** Either handle it or
`break`; do not let a close become a runtime error that has already
eaten the signal.

**Use guards to disable arms as sources finish.** This is the canonical
fan-in shape:

```glide
let mut a_open = true
let mut b_open = true
for a_open || b_open {
    select {
        Some(v) = arx.recv() if a_open => { total += v }
        None    = arx.recv() if a_open => { a_open = false }
        Some(v) = brx.recv() if b_open => { total += v }
        None    = brx.recv() if b_open => { b_open = false }
    }
}
```

The loop condition and the guards agree, so the "all arms disabled"
error cannot occur.

**Prefer `scope(timeout:)` to a timeout arm.** A timeout arm bounds one
wait; a scope bounds the whole subtree, and it is what you almost
always mean:

```glide
// Usually better
scope(timeout: 5.s) {
    for v in rx { handle(v) }
}

// Rather than
select {
    Some(v) = rx.recv()          => handle(v)
    _       = time.after(5.s).recv() => give_up()
}
```

**Do not use `select` where `for v in rx` says it.** A single-channel
consume loop needs no select.

**Keep arm bodies short.** An arm body is a single expression; use a
block for several statements, and call a named function when it grows.

**Do not reach for `else` unless you genuinely mean non-blocking.** A
`select` with `else` in a loop is a busy-wait. The legitimate uses are
"try to drain without waiting" and the priority workaround.

**Let the scope handle shutdown.** No `Done` arm, no shutdown channel,
no `stop` flag. If a select-driven loop should stop when its scope
dies, it already does.

---

### 8. Examples

**Two producers, taken as they arrive:**

```glide
import time

fn main() {
    let (atx, arx) = channel()
    let (ctx2, crx) = channel()

    scope s {
        _ = s.spawn(|| {
            time.sleep(5.ms)
            atx.send("a")
        })
        _ = s.spawn(|| {
            time.sleep(20.ms)
            ctx2.send("c")
        })

        for i in 0..2 {
            let got = select {
                Some(v) = arx.recv() => "from a: {v}"
                Some(v) = crx.recv() => "from c: {v}"
            }
            println(got)
        }
    }
}
```

```
from a: a
from c: c
```

**Non-blocking poll:**

```glide
fn main() {
    let (etx, erx) = channel(cap: 1)

    let r = select {
        Some(v) = erx.recv() => "got {v}"
        else                 => "nothing ready"
    }
    println(r)

    etx.send(42)

    let r2 = select {
        Some(v) = erx.recv() => "got {v}"
        else                 => "nothing ready"
    }
    println(r2)
}
```

```
nothing ready
got 42
```

**A timeout arm:**

```glide
import time

fn main() {
    let (tx, rx) = channel()

    scope s {
        _ = s.spawn(|| {
            time.sleep(50.ms)
            tx.send("slow")
        })

        let r = select {
            Some(v) = rx.recv()          => "received {v}"
            _ = time.after(10.ms).recv() => "gave up waiting"
        }
        println(r)
        return
    }
}
```

```
gave up waiting
```

Note the `return` — the producer is still blocked on `send`, and a
normal scope exit would join it forever (Chapter 27's sharpest edge).
Early exit cancels it.

**Fan-in with guards, from the repository's own `pipeline.gld`:**

```glide
fn fan_in() -> Int {
    let (atx, arx) = channel()
    let (btx, brx) = channel()

    scope s {
        _ = s.spawn(|| {
            for i in 1..=5 { atx.send(i) }
            atx.close()
        })
        _ = s.spawn(|| {
            for i in 1..=5 { btx.send(i * 10) }
            btx.close()
        })

        let mut a_open = true
        let mut b_open = true
        let mut total = 0

        for a_open || b_open {
            select {
                Some(v) = arx.recv() if a_open => { total += v }
                None    = arx.recv() if a_open => { a_open = false }
                Some(v) = brx.recv() if b_open => { total += v }
                None    = brx.recv() if b_open => { b_open = false }
            }
        }
        total
    }
}

fn main() {
    println(fan_in())
}
```

```
165
```

(1+2+3+4+5 = 15, plus 10+20+30+40+50 = 150.)

This is the chapter in one function. Four arms over two channels, with
each channel appearing twice — once for `Some`, once for `None`. The
guards disable both of a channel's arms once it has closed, so the
select never re-reads a drained channel. The loop condition mirrors the
guards, so "all arms disabled" cannot happen. And the whole thing
terminates cleanly with no shutdown protocol.

Compare the Go version, which needs `a = nil` and `b = nil` to disable
its cases, and needs a reader who knows why receiving from a nil
channel blocks forever.

**Bad versus good: the busy-wait**

```glide
// Bad — `else` makes this non-blocking, so the loop spins
fn drain(rx: Receiver<Int>) -> Int {
    let mut total = 0
    for {
        let v = select {
            Some(v) = rx.recv() => Some(v)
            None    = rx.recv() => None
            else                => Some(0)      // spins
        }
        …
    }
}

// Good — no else, so the select blocks until something happens
fn drain(rx: Receiver<Int>) -> Int {
    let mut total = 0
    for v in rx {
        total += v
    }
    total
}
```

And note that the good version does not use `select` at all — a
single-channel consume loop is `for v in rx`. `select` earns its place
when there is more than one thing to wait on.

---

### 9. Summary & Exercises

**Summary**

- `select` waits on several channel operations and takes the first
  ready one. It is an **expression** yielding the taken arm's value,
  with **line-separated arms** like `match`.
- Three arm kinds: `pat = rx.recv() => e`, `tx.send(v) => e`, and
  `else => e` (non-blocking).
- The receive pattern matches the **`Option<T>`**, so the same channel
  may appear in several arms — `Some` and `None` split. A ready
  operation's arms are tried in order.
- **Go's engine verbatim:** operands evaluate once at entry in order;
  choice among ready arms is **uniformly random** (anti-starvation);
  no ready arm and no `else` means **block**; a blocking select is a
  **cancellation point**.
- **Per-arm guards** (`if cond`, evaluated once at entry) replace Go's
  nil-channel disable trick. Occam's ALT had guarded alternatives in
  1983; Go dropped them.
- **There is no `ctx.Done()` arm** — the most common arm in real Go
  selects has no equivalent, because the scope cancels a blocked
  select. This is the payoff of the cancellation design.
- Timeouts are an ordinary receive: `time.after(d)` returns a
  `Receiver<()>`. Most real cases want `scope(timeout:)` instead.
- Deliberately absent: priority select (random choice is
  anti-starvation; the nested-select-with-`else` workaround carries
  over), select over task joins (a selectable task sends into a
  channel; revisit on evidence), and `select {}` (a parse error).
- **The sharpest edge:** an unmatched ready receive has already
  consumed the value when it errors. Cover both `Some` and `None`.
  Static in the checker era.
- Implementation: `reflect.Select`, so it literally is Go's select,
  with the task's cancellation channel as an extra case.

**Exercises**

1. **Count the `Done` arms.** In a Go codebase with meaningful
   concurrency, count the `select` statements and count how many have a
   `case <-ctx.Done():` arm. Then count the total arms. The fraction of
   arms that are pure cancellation plumbing is what the scope design
   deletes.

2. **Build fan-in without guards.** Write the two-producer fan-in using
   only what Go has: no guards, so you must disable a channel some
   other way. Since there is no nil channel in Glide, you will need a
   different structure — probably a single merged channel and a counter
   of live producers. Compare the result with the guarded version and
   decide which you would rather maintain.

3. **Break the random choice.** Write a select over two channels where
   one is always ready and one is rarely ready, and confirm that the
   rare one is eventually taken. Then reason about what would happen
   with deterministic top-to-bottom ordering. This is why Go
   randomises, and it is worth seeing the starvation you would
   otherwise get.
