# Chapter 28: Channels

A **channel** is a typed pipe between tasks. One task sends, another
receives, and the channel handles the synchronisation.

Glide keeps Go's channels — `DESIGN.md` calls them and `select` "Go's
crown jewel" — and fixes the sharp edges. All three of Go's channel
panics are symptoms of one root cause, **"anyone can close anything"**,
and splitting the channel into two half-types dispatches them.

Everything here is ✓. The one wart this chapter used to carry — a sent
`None` reading as end-of-stream — died when `Option` was boxed in M4c.

---

### 1. Basic Usage

#### Creating a channel

```glide
let (tx, rx) = channel()              // unbuffered rendezvous
let (tx, rx) = channel(cap: 64)       // buffered
```

`channel()` returns a **tuple of two halves**: a `Sender` and a
`Receiver`. There is no whole-channel value — the tuple construction
*is* the halves doctrine, so who-sends and who-receives is structural.

The default is **unbuffered**: a send blocks until a receiver takes the
value, and a receive blocks until a sender provides one. That gives
backpressure by construction.

**There are no unbounded channels.** If you want one, build it
visibly.

#### Sending and receiving

```glide
tx.send(v)              // blocks per capacity; panics if closed
tx.close()              // idempotent; only the sender half has it
rx.recv()               // -> Option<T>; None means closed and drained
```

```glide-run
import time

fn main() {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in 1..=3 { tx.send(i) }
            tx.close()
        })
        for v in rx {
            print("{v} ")
        }
        println("")
    }
}
```

```
1 2 3
```

#### `for v in rx`

A receiver satisfies the iteration protocol: `for v in rx` consumes
until the channel is closed and drained. That works because `recv()`
returns `Option<T>` with `None` for end-of-stream — the same shape as
the generator protocol (Chapter 25).

Note the scope. Sending and receiving on an unbuffered channel from the
same task would deadlock; the send happens in a spawned child.

#### Buffered channels

```glide-run
fn main() {
    let (btx, brx) = channel(cap: 3)
    btx.send(1)
    btx.send(2)
    btx.close()
    println("{brx.recv():?}")     // 1
    println("{brx.recv():?}")     // 2
    println("{brx.recv():?}")     // None
}
```

With capacity, sends do not block until the buffer is full — so a
single task can send and receive without a second task, up to the
capacity.

#### Both halves clone; semantics are mpmc

Multiple producers and multiple consumers are the supported case:

```glide
scope s {
    _ = s.spawn(|| worker(jobs_rx, out_tx))     // several workers…
    _ = s.spawn(|| worker(jobs_rx, out_tx))     // …share one receiver
    …
}
```

Worker pools sharing one receiver are bread-and-butter, which is why
mpmc is the default rather than Rust's std mpsc.

#### Only the sender closes

`rx` has no `close` method. The receiver-closes bug is
**unrepresentable**.

`tx.close()` is **idempotent** — closing twice is fine.

#### Sending on a closed channel panics

That is a coordination bug between senders, not an expected outcome
(Chapter 21).

#### A worker pool

```glide-run
import time

fn slow_square(n: Int) -> Int {
    time.sleep(1.ms)
    n * n
}

fn pool_sum(upto: Int) -> Int {
    let (jobs_tx, jobs_rx) = channel()
    let (out_tx, out_rx) = channel(cap: 64)

    scope s {
        _ = s.spawn(|| {
            for j in jobs_rx {
                out_tx.send(slow_square(j))
            }
        })
        _ = s.spawn(|| {
            for j in jobs_rx {
                out_tx.send(slow_square(j))
            }
        })

        for i in 1..=upto {
            jobs_tx.send(i)
        }
        jobs_tx.close()

        let mut total = 0
        for _ in 1..=upto {
            total += out_rx.recv() ?? 0
        }
        total
    }
}

fn main() {
    println(pool_sum(10))     // 385
}
```

Two workers share `jobs_rx`. The scope guarantees both are joined
before `pool_sum` returns, so nothing leaks. Note the results channel
is buffered — with an unbuffered one, a worker would block on `send`
while the main task is still feeding jobs.

#### Channel operations are cancellation points

`send`, `recv`, and a blocking `select` all deliver cancellation
(Chapter 27). A task blocked on a channel inside a dying scope unwinds.

---

### 2. Under the Hood

#### The halves are the design

`channel()` returns `(Sender<T>, Receiver<T>)` — two distinct types.
That single decision handles Go's three channel panics:

| Go panic | Glide |
|---|---|
| Receiver closes the channel | **Unrepresentable** — `rx` has no `close` |
| Double close | **Gone** — `tx.close()` is idempotent |
| Send on a closed channel | **Still a panic** — a sender coordination bug |
| Nil channel blocks forever | **Free** — there is no nil |

The tuple destructuring at the creation site means the reader can see
which task got which half.

#### Why close is idempotent

Go panics on a double close, and Go users hand-roll `sync.Once` to
avoid it.

`DESIGN.md`'s reasoning: **there is no deterministic drop in a GC'd
language**, so close must be safe from a `defer` racing another. Making
it idempotent removes the whole category.

The accepted cost: idempotent close hides sloppy double-close where Go
would have panicked.

#### Why send-on-closed is still a panic

Three reasons.

It is a **coordination bug between senders** — someone closed while
someone else was still sending. A `Result`-returning `send` would tax
every correct program with a check, to launder a bug into a value.

**Shutdown flows down the scope tree via cancellation, not up via send
failures.** In Rust, `send` returns `Err` when the receiver is gone,
because that is how a producer learns to stop. Here the producer learns
by being cancelled, so the error return has no customer.

Full static prevention would need **affine senders** — ownership
machinery Glide sacrificed. Recorded cost.

#### Ownership transfer on send

Sending transfers ownership: the sent value is dead to the sender. That
kills the both-sides-mutate race at the root.

**Ratified but dormant in M2** — the checker enforces it; the
tree-walker will not half-enforce it. So today you *can* keep using a
value after sending it, and you should not.

#### A sent `None` is an ordinary element

`recv()` returns `Option<T>`, with `None` meaning closed-and-drained.
That used to mean a *sent* `None` was indistinguishable from the end of
the stream — a payload impersonating the protocol.

Boxing `Option` (Chapter 14) fixed it. Sending `None` down a
`Sender<Int?>` now delivers `Some(None)`: present, holding nothing.
Sending `Option` values through a channel is safe, and needs no
wrapper type.

#### Implementation

The interpreter's channels are Go channels, and `select` rides
`reflect.Select` (Chapter 29). Blocking operations release the
interpreter lock with the task's cancellation channel as an extra case,
which is how a blocked channel operation becomes a cancellation point.

---

### 3. Why This Design?

#### Why channels at all

Go's slogan is "share memory by communicating", and the underlying
claim is that passing ownership of a value through a channel removes
the need for a lock around it. A task that receives a value is its sole
owner; no other task can be looking at it.

Glide keeps the model and makes the ownership part real (○): sending
transfers ownership, enforced by the checker. In Go it is a convention.

Channels are not the *only* answer. `DESIGN.md` also specifies
`Mutex<T>` that **wraps the data it guards** (○) — Rust's best
non-borrow-checker idea, where unguarded access simply does not
compile, unlike Go's `sync.Mutex` sitting beside the data hoping
everyone remembers.

#### Why unbuffered by default

Backpressure by construction.

An unbuffered send blocks until someone receives, so a fast producer is
automatically throttled by a slow consumer. Add capacity when you have
measured that you want decoupling, and the number you write is a
statement about how much lag you will tolerate.

#### Why no unbounded channels

Rust's standard library made unbounded the default for `mpsc`, and it
is the classic slow-consumer memory leak: the producer runs ahead, the
queue grows, and the process dies of memory exhaustion a long way from
the cause.

`DESIGN.md`: want unbounded, build it visibly. That is the pricing
pillar — the dangerous thing gets a longer name.

#### Why mpmc rather than mpsc

Worker pools sharing one receiver are bread-and-butter. The evidence
cited: **Rust's ecosystem abandoned std's mpsc for crossbeam's mpmc.**
When the standard library's restriction is routinely worked around by a
third-party crate, the restriction was wrong.

#### Why split halves

Because "anyone can close anything" is the root cause of Go's channel
panics, and types are the cheapest fix.

There is a second benefit: the halves make **direction** structural. In
Go you can express direction in a parameter type
(`func worker(jobs <-chan int)`), and it is optional and easy to omit.
Here there is no whole-channel value to pass, so a function taking a
`Receiver<Job>` cannot send, ever.

#### Why `for v in rx` works

`recv()` returns `Option<T>` with `None` for end-of-stream — the same
shape as `Iterator.next()`. So a `Receiver` satisfies the iteration
protocol and `for v in rx` follows for free.

`DESIGN.md` is careful that this does not contradict "channels are not
the iterator protocol" (Chapter 24): the ban is on *implementing* an
iterator with a thread and a channel. **Consuming an existing stream is
the legitimate direction.**

#### Why no `ctx.Done()` arm is needed

In Go, the most common `select` arm is `case <-ctx.Done():`. Here a
blocked channel operation is a cancellation point, so the scope cancels
it directly. Chapter 29 makes this concrete.

---

### 4. Competing Approaches

**Go.** `make(chan T)` and `make(chan T, n)`, a single channel value,
`close(ch)`, `v, ok := <-ch`, directional channel types in signatures.
The direct model. Its three panics are the three things the split
halves fix, and its `ok` tuple is the shape `Option` replaces.

**Rust.** `std::sync::mpsc` (single consumer, unbounded by default —
both mistakes), and `crossbeam-channel` (mpmc, bounded) which the
ecosystem uses instead. Rust's `send` returns `Result` because the
receiver may be gone; Glide's does not, because shutdown flows through
cancellation instead.

**Kotlin.** `Channel<T>` with configurable capacity including
`UNLIMITED` and `CONFLATED`, plus `Flow` as a separate reactive
abstraction. Two concurrency-communication mechanisms in one language.

**Erlang.** Per-process mailboxes with selective receive — the deepest
version of the idea, and a different model: the mailbox belongs to the
process rather than being a separate object.

**Clojure.** `core.async`, an explicit port of CSP to a language
without green threads, implemented with a macro that CPS-transforms the
body. Instructive as an example of what you must build when the runtime
does not provide it.

**Python (Trio / asyncio).** Memory channels with capacity, and `async
with` for closing. Trio's send/receive halves are the closest analogue
to Glide's split, and are also where the idea of closing one end
cleanly is best worked out.

**Java.** `BlockingQueue` — the same primitive without language
integration, so there is no `select` and no `for … in`.

---

### 5. Common Mistakes

**Deadlocking on an unbuffered channel in one task.**

```glide
// Bad — the send blocks forever; nobody is receiving
let (tx, rx) = channel()
tx.send(1)
println("{rx.recv():?}")

// Good — a second task receives
scope s {
    _ = s.spawn(|| tx.send(1))
    println("{rx.recv():?}")
}

// Also good — capacity means the send does not block
let (tx, rx) = channel(cap: 1)
tx.send(1)
println("{rx.recv():?}")
```

**Forgetting to close, so `for v in rx` never ends.** A `for` over a
receiver runs until the channel is closed. If the producer finishes
without closing, the consumer blocks forever — or, in a scope, until
the scope is cancelled.

```glide
// Good
_ = s.spawn(|| {
    for i in 1..=3 { tx.send(i) }
    tx.close()                        // essential
})
```

**Abandoning a producer by falling off the end of the scope.** The
sharpest edge in the chapter, covered in full in the Examples section:

```glide
// Bad — normal exit JOINS without cancelling; the blocked producer
// never finishes and the program deadlocks
scope s {
    _ = s.spawn(|| { for i in items { tx.send(i) }
                     tx.close() })
    let v = rx.recv() ?? 0
    v
}

// Good — early exit cancels first, then joins
scope s {
    _ = s.spawn(|| { for i in items { tx.send(i) }
                     tx.close() })
    let v = rx.recv() ?? 0
    return v
}
```

Rule 1: *early exit cancels children first*. Normal exit does not.

**Closing from the receiving side.** There is no `rx.close()`. If you
want the consumer to signal "stop", that is a second channel, or —
usually better — an enclosing scope that cancels.

**Sending after close.** Panics. If several tasks send, one of them
closing while others are still sending is a coordination bug; the usual
fix is that the *coordinator* closes after joining all senders.

**Blocking the results channel in a pool.**

```glide
// Bad — with an unbuffered out channel, workers block while
// the main task is still feeding jobs, and jobs_tx.send blocks
// because the workers are blocked. Deadlock.
let (out_tx, out_rx) = channel()

// Good
let (out_tx, out_rx) = channel(cap: 64)
```

This is the most common real deadlock in the worker-pool shape.

**Using a value after sending it.** Ownership transfer is ratified and
dormant. The checker will reject it; write as though it already does.

**Reaching for a channel where a `Task` would do.**

```glide
// Bad — a channel to return one value
let (tx, rx) = channel()
_ = s.spawn(|| tx.send(compute()))
let result = rx.recv() ?? 0

// Good
let t = s.spawn(|| compute())
let result = t.join()
```

`join()` returns exactly what the closure returned. Channels are for
*streams*, not for single results.

---

### 6. Performance Considerations

**An unbuffered send is a rendezvous** — the sender blocks until a
receiver arrives, so each element costs a full synchronisation. That is
the price of backpressure.

**A buffered send costs an enqueue** when there is room. Capacity trades
memory for fewer synchronisations, and the right number is usually
small — enough to smooth jitter, not enough to hide a throughput
mismatch.

**Channel operations are far more expensive than function calls** —
that is exactly why `DESIGN.md` bans channels as the iteration protocol
(Chapter 24). A channel per element in a hot loop is orders of
magnitude slower than an iterator.

**Both halves cloning has no cost** — they are handles to the same
underlying queue.

**In the interpreter**, channels are Go channels and each blocking
operation releases and reacquires the interpreter lock. That makes
channel ops the scheduling points, which is the intended semantics —
but it also means a channel-heavy program is doing a lot of lock
traffic.

**Cancellation adds a case to every blocking wait.** In the interpreter
that is one extra `reflect.Select` case; negligible relative to the
block itself.

---

### 7. Best Practices

**Let the scope own the channel's lifetime.**

```glide
// Good — producer and consumer are both bounded by the scope
scope s {
    let (tx, rx) = channel()
    _ = s.spawn(|| produce(tx))
    consume(rx)
}
```

Nothing outlives the scope, so a stalled producer cannot leak.

**Close in the task that owns the sending side, and close exactly
once.**

```glide
// Good — the producer closes when it is done producing
_ = s.spawn(|| {
    for item in source { tx.send(item) }
    tx.close()
})
```

With several producers, join them first and let the coordinator close:

```glide
scope s {
    let ts = ranges.iter().map(|r| s.spawn(|| produce(r, tx))).collect()
    for t in ts { let _ = t.join() }
    tx.close()                        // after every producer has finished
}
```

**Buffer the results channel in a fan-out.** The pool deadlock above is
the reason.

**Prefer `Task` + `join` for single values, channels for streams.**

**Choose capacity deliberately, and write down why.**

```glide
// Good — the number means something
let (out_tx, out_rx) = channel(cap: 64)    // ~2x worker count; smooths jitter
```

**Do not build an unbounded channel by accident.** A very large
capacity is an unbounded channel with a slower failure mode. If the
producer can outrun the consumer indefinitely, the answer is
backpressure (small buffer) or dropping, not a bigger number.

**Pass halves, not whole channels, through APIs.**

```glide
// Good — the signature says which direction this function moves data
fn worker(jobs: Receiver<Job>, results: Sender<Report>)
```

There is no whole-channel value, so this is the only thing you *can*
write — which is the point.

---

### 8. Examples

**The basic pattern, end to end:**

```glide-run
fn main() {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in 1..=3 { tx.send(i) }
            tx.close()
        })
        for v in rx {
            print("{v} ")
        }
        println("")
    }
}
```

```
1 2 3
```

**Buffered, in one task:**

```glide-run
fn main() {
    let (btx, brx) = channel(cap: 3)
    btx.send(1)
    btx.send(2)
    btx.close()
    println("{brx.recv():?}")
    println("{brx.recv():?}")
    println("{brx.recv():?}")
}
```

```
1
2
None
```

The third `recv()` returns `None` — closed and drained. That is the
signal `for v in rx` uses to stop.

**A worker pool, from the repository's own `pipeline.gld`:**

```glide-run
import time

fn slow_square(n: Int) -> Int {
    time.sleep(1.ms)
    n * n
}

fn pool_sum(upto: Int) -> Int {
    let (jobs_tx, jobs_rx) = channel()
    let (out_tx, out_rx) = channel(cap: 64)

    scope s {
        _ = s.spawn(|| {
            for j in jobs_rx {
                out_tx.send(slow_square(j))
            }
        })
        _ = s.spawn(|| {
            for j in jobs_rx {
                out_tx.send(slow_square(j))
            }
        })

        for i in 1..=upto {
            jobs_tx.send(i)
        }
        jobs_tx.close()

        let mut total = 0
        for _ in 1..=upto {
            total += out_rx.recv() ?? 0
        }
        total
    }
}

fn main() {
    println(pool_sum(10))
}
```

```
385
```

Points worth noting:

- **Both workers share `jobs_rx`** — that is mpmc, and it is why the
  restriction to a single consumer would be wrong.
- **`jobs_tx.close()` after the feed loop** is what lets the workers'
  `for j in jobs_rx` terminate.
- **`out_tx` is buffered** so workers do not block while the main task
  is still feeding.
- **The scope joins both workers** before `pool_sum` returns. There is
  no `WaitGroup`, and there is no way to forget.

**Go's three panics, side by side:**

```go
// Go — all three compile
ch := make(chan int)
close(ch)
close(ch)          // panic: close of closed channel
ch <- 1            // panic: send on closed channel

var nilCh chan int
<-nilCh            // blocks forever, silently
```

```glide
// Glide
let (tx, rx) = channel()
tx.close()
tx.close()          // fine — idempotent
tx.send(1)          // panics — a sender coordination bug
rx.close()          // does not exist
// there is no nil channel
```

Three of the four are gone at the type or semantic level. The fourth is
kept as a panic deliberately.

**Bad versus good: the leaked producer**

```go
// Go — StartFeed returns, the goroutine blocks forever on send
func StartFeed(items []int) <-chan int {
    ch := make(chan int)
    go func() {
        for _, i := range items {
            ch <- i          // blocks forever if the consumer stops early
        }
        close(ch)
    }()
    return ch
}
```

If the caller breaks out of the range loop early, the producer goroutine
is blocked on a send nobody will receive, forever. This is the single
most common Go channel leak.

```glide
// Glide — and this version DEADLOCKS. Read on.
fn consume_some(items: List<Int>, n: Int) -> Int {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in items { tx.send(i) }
            tx.close()
        })
        let mut total = 0
        for _ in 0..n {
            total += rx.recv() ?? 0
        }
        total          // normal exit: JOIN, do not cancel
    }
}
```

```
fatal error: all goroutines are asleep - deadlock!
```

This is worth dwelling on, because it is rule 1 from Chapter 26 biting
exactly as specified:

> Scope exit always joins every child. **Early exit cancels them
> first**, waits, then continues propagating.

Falling off the end of the body is a **normal** exit, so the scope
*joins* the producer without cancelling it — and the producer is
blocked forever on a send nobody will receive. The scope waits, and the
program deadlocks.

The fix is to exit early, which triggers the cancel:

```glide-run
// Good — `return` is an early exit, so the scope cancels first
fn consume_some(items: List<Int>, n: Int) -> Int {
    let (tx, rx) = channel()
    scope s {
        _ = s.spawn(|| {
            for i in items { tx.send(i) }
            tx.close()
        })
        let mut total = 0
        for _ in 0..n {
            total += rx.recv() ?? 0
        }
        return total       // early exit: cancel the producer, then join
    }
}

fn main() {
    println(consume_some([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 3))
    println("returned without hanging")
}
```

```
6
returned without hanging
```

The producer was blocked on `send` — a cancellation point — so it
unwound, ran its defers, and was joined.

Compare this with the Go version above. Go's leak is **silent**: the
goroutine sits blocked forever, the program continues, and you find it
in a heap dump weeks later. Glide's failure is **immediate and loud**:
the program deadlocks at the scope boundary, on the first run, with a
stack trace pointing at the join.

That is the trade structured concurrency makes. It does not
automatically do the right thing with an abandoned producer; it refuses
to let you *not notice*. And the one-character fix — `return` instead
of a tail expression — is the difference between "wait for the child"
and "the child is no longer wanted", which is a distinction the code
should be making explicitly anyway.

---

### 9. Summary & Exercises

**Summary**

- `let (tx, rx) = channel()` gives an **unbuffered rendezvous**;
  `channel(cap: n)` buffers. **There are no unbounded channels** — Rust
  std's unbounded default is the classic slow-consumer leak.
- **Two half-types, no whole-channel value.** Direction is structural,
  and the tuple construction is the doctrine.
- `tx.send(v)` blocks per capacity and **panics on a closed channel** —
  a sender coordination bug, not an expected outcome. `tx.close()` is
  **idempotent**. `rx` has **no close**.
- `rx.recv()` returns `Option<T>`, with `None` meaning
  closed-and-drained. That shape is why **`for v in rx`** works.
- **Both halves clone; semantics are mpmc.** Worker pools sharing one
  receiver are the motivating case, and Rust's ecosystem abandoning std
  mpsc for crossbeam is the evidence.
- Go's three channel panics: receiver-closes is **unrepresentable**,
  double-close is **gone**, send-on-closed is **kept deliberately**,
  and nil-channel-blocks is free because there is no nil.
- **Send transfers ownership** — ratified, and dormant in M2 pending
  the checker.
- Channel operations are **cancellation points**, so a blocked task in
  a dying scope unwinds.
- Channels are for **streams**; a single result is a `Task` and
  `join()`.
- A sent `None` is an ordinary element, not end-of-stream: `Option` is
  boxed as of M4c, so a payload cannot impersonate the channel's own
  closed-and-drained signal. Sending Options is fine.
- ○: `Mutex<T>` that wraps the data it guards — the other half of the
  data-sharing story.

**Exercises**

1. **Find the leaked producer.** In a Go codebase, find a function that
   returns a `<-chan T` fed by a goroutine. Determine what happens if
   the consumer stops reading early. Then write the Glide version and
   note that the scope makes the question answerable by reading the
   code.

2. **Build the pool three ways.** Implement a worker pool that squares
   numbers, with (a) an unbuffered results channel, (b) a buffered one,
   and (c) no results channel at all — spawn one task per job and
   `join` them. Find the deadlock in (a), and then decide when (c) is
   better than (b). (Hint: it depends on whether the job count is
   bounded and known.)

3. **Argue send-on-closed.** `DESIGN.md` keeps send-on-closed as a
   panic rather than returning a `Result`. Write the strongest case for
   the `Result` version, including what every correct call site would
   then look like. Then check your case against the argument that
   shutdown flows *down* the scope tree rather than *up* via send
   failures — that is the load-bearing claim, and if you can break it,
   the decision changes.
