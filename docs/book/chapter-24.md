# Chapter 24: Generators

Chapter 23 covered how to *consume* an iterator. This chapter covers
how to *write* one — and it is the feature that Go and Rust both
struggled with for a decade.

The problem: to write an iterator by hand, you must turn your traversal
inside out. A recursive tree walk becomes an explicit stack, a resume
point, and a `next()` method that reconstructs where it was. That is
real code, and it looks nothing like a traversal.

In Glide, **a function containing `yield` is an iterator**:

```glide
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

Three lines. In-order tree traversal — the canonical "iterators are
hard" example — reads as the three lines it conceptually is.

Everything here is ✓.

---

### 1. Basic Usage

#### `yield`

Any function whose body contains `yield` becomes a **generator**:
calling it returns an `Iterator` rather than running the body.

```glide
fn three() -> Iterator<Int> {
    yield 1
    yield 2
    yield 3
}

fn main() {
    println("{three().collect():?}")     // [1, 2, 3]
}
```

`yield` hands the next value to whoever is consuming and **pauses the
function mid-flight**. The next pull resumes it exactly where it
stopped, with all locals intact.

That last sentence is the whole feature. The function's local
variables, its position in a loop, its place in a recursion — all of it
survives the pause.

#### Loops inside generators

```glide
fn evens(xs: List<Int>) -> Iterator<Int> {
    for x in xs {
        if x % 2 == 0 {
            yield x
        }
    }
}
```

The `for` loop's state is part of what gets suspended. Between pulls,
the loop is paused mid-iteration.

#### Infinite generators are ordinary values

```glide
fn fib() -> Iterator<Int> {
    let mut a = 0
    let mut b = 1
    for {
        yield a
        let next = a + b
        a = b
        b = next
    }
}

fn main() {
    println("{fib().take(10).collect():?}")
}
```

```
[0, 1, 1, 2, 3, 5, 8, 13, 21, 34]
```

`fib` never returns. `take(10)` makes it terminate. Because generators
are lazy iterators (Chapter 23), an infinite sequence is a perfectly
good value — you compose it with adapters and bound it at the point of
use.

#### `yield from` delegates

```glide
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

`yield from iter` yields every element of a sub-iterator in turn. Here
the sub-iterator is a recursive call, which is what makes recursive
traversal work.

Written out, an in-order traversal:

```glide
type Node = struct { value: Int, left: Node?, right: Node? }

fn leaf(v: Int) -> Node { Node{ value: v, left: None, right: None } }

fn insert(at: Node?, v: Int) -> Node {
    match at {
        None                   => leaf(v)
        Some(n) if v < n.value => Node{ left: insert(n.left, v), ..n }
        Some(n)                => Node{ right: insert(n.right, v), ..n }
    }
}

fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}

fn main() {
    let mut root = leaf(5)
    for v in [3, 8, 1, 4, 7, 9] {
        root = insert(Some(root), v)
    }
    println("{walk(root).collect():?}")
    println("{walk(root).take(3).collect():?}")
}
```

```
[1, 3, 4, 5, 7, 8, 9]
[1, 3, 4]
```

The second line is the payoff of laziness: `take(3)` visits three nodes
and stops. The traversal is abandoned mid-recursion.

#### Generators are iterators

A generator's result is an ordinary `Iterator`, so everything from
Chapter 23 applies:

```glide
walk(root).filter(|v| v % 2 == 1).map(|v| v * 10).collect()
fib().take(5).sum()
for v in walk(root) { … }
```

There is no second protocol and no adapter that works on one and not
the other.

#### Making a type iterable with a generator

This is the most common use in practice — Chapter 17's `iter()` method:

```glide
type Grid = struct { width: Int, cells: List<Int> }

impl Grid {
    fn iter(self) -> Iterator<Int> {
        for c in self.cells { yield c }
    }

    fn positions(self) -> Iterator<(Int, Int, Int)> {
        for (i, c) in self.cells.iter().enumerate() {
            yield (i / self.width, i % self.width, c)
        }
    }
}
```

Two named traversals, each three lines. Offering more than one way to
walk a structure costs almost nothing.

---

### 2. Under the Hood

#### What a suspended function is

A generator needs to store, between pulls: the values of all its
locals, and the exact point in the body where it paused. That bundle is
a **continuation**, and there are two ways to build one.

**A state machine.** The compiler rewrites the function into a struct
holding the locals plus a state integer, and a `next()` method that
switches on the state, runs to the next `yield`, saves the state, and
returns. This is what C# has done since 2005 and what Glide's compiled
tier will do.

**A separate stack.** Run the body on its own thread of control and
hand values across. That is what the interpreter does.

#### The interpreter's implementation

`glide/DESIGN-DECISIONS.md`: **generators run on a goroutine plus a
channel.** `yield` sends; `next()` receives. Body panics are forwarded
to the consumer. An abandoned iterator's producer is unblocked by a GC
cleanup hook closing a stop channel.

It is described as "the cheapest correct lazy implementation for a
tree-walker", and it is: a suspended goroutine *is* a suspended frame
with its locals intact, which is exactly what a generator needs.

Two consequences you can measure today:

- **One goroutine per generator**, and **one per `yield from`
  delegation level**. A depth-20 tree traversal has 20 live goroutines
  mid-walk. Fine here; irrelevant to the compiled tier.
- **Generator handoffs release the interpreter lock** on both ends, so
  a generator inside a task cannot wedge the interpreter.

There is a good war story recorded in that file, worth reading because
it illustrates how sharp lazy-plus-GC can be. The `Next` closure must
keep its iterator value reachable (`runtime.KeepAlive`): `iterate()`
hands the bare function around, and when only the function was live,
the GC cleanup hook fired mid-loop and **silently truncated the stream
at a GC-chosen point**. The symptom was the tree property test failing
on a *prefix* of the sorted list, nondeterministically, despite a fixed
seed.

#### Why this is easy here and hard in Rust

`DESIGN.md` names the asymmetry directly.

Generators are hard in Rust — a decade unstabilised — because a yielded
reference borrows from suspended stack state, and the borrow checker
must prove that reference is still valid when the generator resumes.
That is a genuinely hard problem, and it is why `Pin` exists.

There are no lifetimes here. In a GC language, generators are almost
boring: values yielded from a suspended frame are just values, kept
alive by the collector like anything else.

Build around the asymmetry: the feature that costs Rust a decade costs
Glide a goroutine.

#### The transpiler's known-hard piece

`DESIGN.md` flags this explicitly on the de-risk list: lowering
generators to Go is "the fiddly one — prototype before depending on the
transpiler". The options are goroutine pairs (what the interpreter
does, correct but heavy) or CPS/state-machine transformation
(correct and fast, and real work).

C# proves it is solvable. It is on the list because it is the piece
most likely to bite.

---

### 3. Why This Design?

#### The problem generators solve

Write an in-order tree traversal as a hand-rolled iterator. You need:

- an explicit stack of nodes,
- a flag or a state variable per stack entry recording whether you have
  descended left, emitted the value, or descended right,
- a `next()` method that resumes from that state.

That is twenty-plus lines of stack manipulation for a traversal that is
three lines recursively, and every line of it is a chance to get the
state machine wrong.

`DESIGN.md`: "Hand-writing `next()` for a tree traversal means manually
maintaining the stack the compiler should build — this is why Go
resisted iterators for a decade and then shipped the awkward callback
form."

The compiler *can* build that state machine. Generators are the syntax
that asks it to.

#### Why `yield` and not a callback

Go's answer (1.23's `iter.Seq`) is internal iteration: you write
`func(yield func(T) bool)` and call `yield` for each element, checking
the return to know when to stop.

That is a real solution and it has two costs. It is *internal*
iteration, so `zip` and lockstep consumption become hard (Chapter 23).
And the early-exit protocol is manual — you must check `yield`'s return
value and thread the "stop" signal back out through your recursion,
which for a recursive traversal is exactly the bookkeeping generators
were meant to remove.

With external iteration plus generators, abandoning a traversal is
"stop calling `next()`", and the producer simply never resumes.

#### Why `yield from` rather than a loop

You could write:

```glide
for v in walk(l) { yield v }
```

and `yield from walk(l)` is sugar for it. The sugar earns its place
because delegation is the *common* case in recursive generators, and
because a compiled implementation can flatten `yield from` chains
rather than nesting a state machine per level — which matters when the
recursion is deep.

Python has exactly this (`yield from`, PEP 380) and for exactly this
reason.

#### Why generators are sugar, not a second protocol

Restated from Chapter 23 because it is the decision that keeps the
ecosystem coherent: a generator returns an ordinary `Iterator`. Every
adapter works on it. A consumer cannot tell whether a sequence came
from a list, an adapter chain, or a hand-written traversal.

If generators produced a distinct type, half the library would work on
one and half on the other, and `for` would need to handle both.

#### Why not channels for this

A green thread plus a channel *is* a generator, and `DESIGN.md`
explicitly bans it as the user-facing pattern:

> Channels are not the iterator protocol. A green thread + channel per
> loop means synchronisation per element, and early exit leaks a
> thread. Iteration is control flow, not communication.

Two costs. **Synchronisation per element** — every value crosses a
channel, which is orders of magnitude more expensive than a function
call. And **early exit leaks a thread**: abandon the loop and the
producer is blocked forever on a send nobody will receive.

The interpreter uses a goroutine and a channel *internally*, and it
solves the leak with a GC cleanup hook — machinery a user writing the
pattern by hand would not have. That is the difference between an
implementation detail of the dev tier and a pattern you are told to
write.

---

### 4. Competing Approaches

**Python.** `yield` and `yield from` — the direct source of Glide's
spelling and semantics. Python generators also support `send()`
(two-way communication), which Glide does not adopt: it turns a
generator into a coroutine and the use cases belong to structured
concurrency.

**C#.** `yield return` and `yield break`, compiled to state machines,
since 2005. The existence proof that this compiles well, cited in
`DESIGN.md` as the reason the backend obligation is
"known-hard-but-solved".

**JavaScript.** `function*` and `yield`, `yield*` for delegation. Same
model, and the foundation `async`/`await` was built on.

**Go.** Nothing for thirteen years, then `iter.Seq` in 1.23 — internal
iteration via a `yield` callback returning `bool`. Workable, awkward
for `zip` and early exit, and arriving after the ecosystem had built
its own conventions. The cautionary tale that motivates designing
iteration in from the start.

**Rust.** Generators unstable for a decade because of the borrow-
checker interaction; the ecosystem uses `impl Iterator` with
hand-written state or the `genawaiter`/`async-stream` crates. Rust's
`async` blocks are generators wearing a different name, which is why
the two features share machinery and both need `Pin`.

**Java.** No generators. `Iterator` implemented by hand, or a thread
plus a `BlockingQueue` (with the leak problem), or Streams built from
`Spliterator`. The absence is felt.

**Kotlin.** `sequence { yield(x) }` — coroutine-based, so genuinely
suspendable, and one of the nicer mainstream implementations.

**CLU.** The origin, 1975. Iterators as a first-class language
construct with `yield`. `LINEAGE.md` credits it, and it is worth noting
that the feature is fifty years old and still absent from most
mainstream languages.

---

### 5. Common Mistakes

**Expecting the body to run when you call it.**

```glide
fn setup() -> Iterator<Int> {
    println("starting")       // does NOT print at call time
    yield 1
}

fn main() {
    let it = setup()          // nothing printed
    println("created")
    let _ = it.collect()      // now "starting" prints
}
```

```
created
starting
```

Calling a generator creates an iterator. The body runs when something
pulls.

**Putting side effects in a generator.** Because of the above, a
generator whose body has effects has them at unpredictable times — or
never, if nobody consumes it. Generators should produce values.

**Forgetting `yield from` and yielding the iterator itself.**

```glide
// Bad — yields one element, which happens to be an iterator
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left { yield walk(l) }
    yield n.value
}

// Good
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left { yield from walk(l) }
    yield n.value
}
```

**Using a generator where a list would do.** If the sequence is small,
already materialised, and consumed once, `collect()`ing it into a list
is simpler and, in the interpreter, cheaper (no goroutine).

```glide
// Overkill
fn keys(m: Map<String, Int>) -> Iterator<String> {
    for (k, _) in m { yield k }
}

// Fine
fn keys(m: Map<String, Int>) -> List<String> {
    m.entries().iter().map(|e| e.0).collect()
}
```

Use a generator when the sequence is large, infinite, expensive to
produce, or when the caller may not want all of it.

**Deep `yield from` recursion in the interpreter.** One goroutine per
level. A depth-1000 recursion is 1000 goroutines. Correct, and not
free.

**Trying to `?` out of a generator.** A generator's early exits are
`return` (ending the sequence) and yielding. Propagating an error out
of a generator means the iterator's element type has to carry it —
`Iterator<Result<T, E>>` — which is workable and awkward. For fallible
production, a loop that collects and returns `Result<List<T>, E>` is
usually clearer.

**Expecting a generator to be restartable.** It is an iterator: consume
it once. Call the generator function again for a fresh one.

```glide
// Bad
let it = walk(root)
let a = it.collect()
let b = it.collect()      // empty

// Good
let a = walk(root).collect()
let b = walk(root).collect()
```

**Mutating the structure you are walking.** The generator is suspended
mid-traversal holding references. Mutating underneath it is undefined
in the practical sense — do not.

---

### 6. Performance Considerations

**In the interpreter: one goroutine and one channel per generator**,
plus one per `yield from` delegation level. Every `yield` is a channel
send and every `next()` a receive — orders of magnitude more expensive
than a function call.

This is the dev tier's cost and it is why generator-heavy code should
not be benchmarked here.

**In the compiled tier** (○): a state machine. A `yield` becomes "store
the state, return the value"; a `next()` becomes "switch on the state,
run to the next yield". Roughly the cost of a function call plus a
switch, and the whole thing inlines into the consuming loop when the
generator is known statically. This is C#'s cost model.

**Laziness is the win.** `walk(root).take(3)` visits three nodes of a
million-node tree. No amount of implementation efficiency in an eager
version competes with not doing the work.

**`yield from` nesting has a cost at both tiers.** In the interpreter
it is a goroutine per level; in a naive state-machine lowering it is a
`next()` call per level per element, so a depth-*d* tree costs O(d) per
element yielded — the classic "yield from is O(depth)" problem. A good
lowering flattens the chain; Python does not, which is why deep
`yield from` recursion is slow there too.

**A generator keeps its captured state alive.** A generator over a
large structure holds that structure reachable until the iterator is
dropped. If you `take(3)` and abandon it, the rest stays alive until
collection.

**Abandonment is handled.** The interpreter's GC cleanup hook closes a
stop channel so an abandoned producer does not block forever. A
hand-written thread-plus-channel generator would leak — which is
exactly why the pattern is banned as a user-facing idiom.

---

### 7. Best Practices

**Write the traversal, not the state machine.** That is the entire
point:

```glide
// Good — reads as what it is
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

If you find yourself building an explicit stack inside a generator, ask
whether recursion plus `yield from` would say it.

**Name your traversals.** A structure often has more than one useful
order, and generators make each one three lines:

```glide
impl Tree {
    fn in_order(self) -> Iterator<Int> { … }
    fn pre_order(self) -> Iterator<Int> { … }
    fn leaves(self) -> Iterator<Int> { … }
}
```

Reserve the name `iter()` for the default traversal, since that is what
`for … in` calls.

**Use a generator for the sequence, a loop for the effects.**

```glide
// Good
for line in file_lines(path) {
    process(line)?
}
```

The generator produces; the loop consumes and can propagate errors.

**Keep generators pure.** No IO, no mutation of shared state, no
printing. The body runs at unpredictable times and may never finish, so
effects inside are hard to reason about.

**Bound infinite generators at the point of use, visibly.**

```glide
// Good — the bound is right there
let sample = sensor_readings().take(100).collect()

// Dangerous — nothing stops this
for reading in sensor_readings() { … }
```

An unbounded `for` over an infinite generator is an infinite loop, and
it should look like one.

**Prefer returning a `List` for small, eagerly-known sequences.**
Generators earn their keep when the sequence is large, infinite,
expensive, or partially consumed. For "the three keys of this config",
a list is simpler.

**Do not build a generator out of a thread and a channel by hand.**
The language provides the construct; the hand-rolled version
synchronises per element and leaks on early exit.

---

### 8. Examples

**The canonical example, complete:**

```glide
type Node = struct { value: Int, left: Node?, right: Node? }

fn leaf(v: Int) -> Node { Node{ value: v, left: None, right: None } }

fn insert(at: Node?, v: Int) -> Node {
    match at {
        None                   => leaf(v)
        Some(n) if v < n.value => Node{ left: insert(n.left, v), ..n }
        Some(n)                => Node{ right: insert(n.right, v), ..n }
    }
}

fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}

fn main() {
    let mut root = leaf(5)
    for v in [3, 8, 1, 4, 7, 9] {
        root = insert(Some(root), v)
    }

    println("{walk(root).collect():?}")
    println("{walk(root).take(3).collect():?}")

    let odds = walk(root).filter(|v| v % 2 == 1).collect()
    println("{odds:?}")

    println(walk(root).sum())
}
```

```
[1, 3, 4, 5, 7, 8, 9]
[1, 3, 4]
[1, 3, 5, 7, 9]
21
```

Three lines of generator, and the tree composes with every adapter.
Note `take(3)`: three nodes visited, the recursion abandoned.

Compare what the hand-written version needs — an explicit stack, a
per-entry phase marker, and a `next()` that resumes the state machine.
The `insert` function above uses struct update so untouched subtrees
are shared, which is the other half of why this code is short.

**An infinite sequence:**

```glide
fn fib() -> Iterator<Int> {
    let mut a = 0
    let mut b = 1
    for {
        yield a
        let next = a + b
        a = b
        b = next
    }
}

fn primes() -> Iterator<Int> {
    let mut found = []
    let mut n = 2
    for {
        let mut is_prime = true
        for p in found {
            if p * p > n { break }
            if n % p == 0 {
                is_prime = false
                break
            }
        }
        if is_prime {
            found.push(n)
            yield n
        }
        n += 1
    }
}

fn main() {
    println("{fib().take(10).collect():?}")
    println("{primes().take(10).collect():?}")

    let big_fibs = fib().filter(|n| n > 100).take(3).collect()
    println("{big_fibs:?}")
}
```

```
[0, 1, 1, 2, 3, 5, 8, 13, 21, 34]
[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]
[144, 233, 377]
```

The third one is worth pausing on: `filter` over an infinite sequence,
bounded by `take(3)`. The generator produced exactly as many Fibonacci
numbers as were needed to find three above 100, and then stopped
mid-loop.

**Two traversals of one structure:**

```glide
type Doc = struct {
    title: String
    sections: List<String>
}

impl Doc {
    // The default traversal — what `for x in doc` uses.
    fn iter(self) -> Iterator<String> {
        yield self.title
        for s in self.sections { yield s }
    }

    // A different order, named.
    fn body_only(self) -> Iterator<String> {
        for s in self.sections { yield s }
    }

    // A derived sequence.
    fn word_counts(self) -> Iterator<(String, Int)> {
        for s in self.sections {
            yield (s, s.split_whitespace().len())
        }
    }
}

fn main() {
    let d = Doc{
        title: "Report",
        sections: ["intro para here", "body of the text", "end"],
    }

    for line in d {
        println("- {line}")
    }

    println("{d.body_only().count()} sections")

    for (s, n) in d.word_counts() {
        println("{n} words: {s}")
    }
}
```

```
- Report
- intro para here
- body of the text
- end
3 sections
3 words: intro para here
4 words: body of the text
1 words: end
```

Three traversals, nine lines. Offering multiple views of a structure is
cheap enough that you do it by default.

**Bad versus good: the eager traversal**

```glide
// Bad — builds the whole list, even when the caller wants three
fn walk_eager(n: Node) -> List<Int> {
    let mut out = []
    if let l = n.left {
        for v in walk_eager(l) { out.push(v) }
    }
    out.push(n.value)
    if let r = n.right {
        for v in walk_eager(r) { out.push(v) }
    }
    out
}
```

For a million-node tree, `walk_eager(root)` allocates a million-element
list — plus intermediate lists at every level of the recursion, which
makes it O(n log n) allocations rather than O(n). And a caller wanting
the smallest three elements pays for all million.

```glide
// Good
fn walk(n: Node) -> Iterator<Int> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

Shorter, allocates nothing, and `walk(root).take(3)` visits three
nodes.

---

### 9. Summary & Exercises

**Summary**

- **A function containing `yield` is a generator**: calling it returns
  an `Iterator` and the body does not run until something pulls.
- `yield` hands over a value and **pauses the function mid-flight**;
  the next pull resumes it with all locals and loop positions intact.
- **`yield from iter`** delegates to a sub-iterator — which is what
  makes recursive traversal work, and is why an in-order tree walk is
  three lines instead of a twenty-line state machine.
- Generators are **sugar for the `Iterator` protocol**, not a second
  protocol, so they compose with every adapter and with `for … in`.
- **Infinite generators are ordinary values.** Bound them with `take`
  at the point of use.
- The most common use is an `iter()` method, which makes a user type
  `for`-able and adapter-able (Chapter 17).
- Under the hood: the interpreter runs each generator on a **goroutine
  plus a channel** (one per `yield from` level), with a GC cleanup hook
  to unblock abandoned producers. The compiled tier lowers to a **state
  machine**, as C# has done since 2005 — flagged in `DESIGN.md` as the
  fiddly piece of the transpiler.
- Generators are cheap here and hard in Rust because **there are no
  lifetimes**: a yielded reference borrowing from suspended stack state
  is the problem `Pin` exists for, and a GC does not have it.
- **Channels are not the iteration protocol.** Hand-rolling a generator
  from a thread and a channel synchronises per element and leaks the
  thread on early exit.
- Keep generators pure and side-effect free; their bodies run at
  unpredictable times and may never finish.

**Exercises**

1. **Write the state machine by hand.** Implement in-order tree
   traversal as a struct with a `next()` method, using an explicit
   stack. Count the lines and the number of distinct states you had to
   track. Then delete it and write the three-line generator. Keep both
   files; the comparison is the argument for the feature.

2. **Build an infinite generator with a real use.** Write a generator
   producing exponential backoff delays (100ms, 200ms, 400ms, …, capped
   at 30s, forever). Then use it in a retry loop bounded by `take(5)`.
   Note that the cap and the bound are stated in two different places
   and decide whether that is a feature or a smell.

3. **Find the leak.** Write a generator that opens a resource before
   its first `yield` and closes it after its last. Then consume only
   the first element and abandon it. What happens to the resource?
   Compare your answer with the interpreter's GC-cleanup-hook mechanism
   and with what a `defer` inside the generator body would do. This is
   the sharpest edge in the chapter, and it is why generators should
   produce values rather than manage resources.
