# Chapter 24: Iterators

An **iterator** is a value that produces a sequence of elements on
demand. Not a collection holding them — a thing that yields the next
one when asked. That distinction is the whole chapter: iterators are
**lazy**, and laziness is what makes infinite sequences ordinary values
and pipelines cheap.

Glide uses **external iteration** with a one-method protocol, adapters
as lazy default methods, and a hard separation between `Iterable` and
`Iterator`. Chapter 25 covers how you *write* one.

Everything here is ✓ except the adapters marked ○ (the set grows on
demand) and the trait declarations, which are asserted rather than
checked.

---

### 1. Basic Usage

#### Getting an iterator

```glide
let xs = [1, 2, 3, 4, 5, 6]
let it = xs.iter()
```

`xs.iter()` does **not** walk the list. It produces a value that will
yield elements when something pulls on it.

Iterators come from three places: `.iter()` on a `List`, `Map`, or
`Range`; a generator function (Chapter 25); or any type with an
`iter()` method — which is what makes a user type participate.

#### Adapters compose, lazily

```glide-run
fn main() {
    let nums = [1, 2, 3, 4, 5, 6]

    let evens = nums.iter().filter(|n| n % 2 == 0).collect()
    println("{evens:?}")

    let doubled = nums.iter().map(|n| n * 2).collect()
    println("{doubled:?}")

    let first_two_big = nums.iter().map(|n| n * 10).take(2).collect()
    println("{first_two_big:?}")
}
```

```
[2, 4, 6]
[2, 4, 6, 8, 10, 12]
[10, 20]
```

The third line is the important one. `.map(|n| n * 10).take(2)` calls
the closure **twice**, not six times. Nothing runs until something
consumes, and `take(2)` stops pulling after two.

#### The adapter set

Lazy — they build a new iterator and run nothing:

| Adapter | Produces |
|---|---|
| `map(f)` | each element transformed |
| `filter(pred)` | elements where `pred` is true |
| `take(n)` | the first `n`, then stops the source |
| `enumerate()` | `(Int, T)` pairs, indexed from 0 |
| `zip(other)` | `(T, U)` pairs; stops at the shorter side |

Consumers — they drain the iterator and produce a value:

| Consumer | Produces |
|---|---|
| `collect()` | a `List<T>` |
| `count()` | an `Int` |
| `sum()` | folds `+` from the first element |

```glide-run
fn main() {
    println([1, 2, 3].iter().sum())                     // 6
    println((0..5).iter().filter(|n| n % 2 == 1).count()) // 2

    let z = [1, 2, 3].iter().zip(["a", "b", "c"]).collect()
    println("{z:?}")

    let en = ["a", "b"].iter().enumerate().collect()
    println("{en:?}")
}
```

```
6
2
[(1, "a"), (2, "b"), (3, "c")]
[(0, "a"), (1, "b")]
```

Note that `zip` takes **any iterable** — a list, a range, another
iterator — not just an iterator. And `sum()` folds `+` from the first
element, so `Int`, `Float`, and `String` all work; an empty sum is
`Int` 0.

Further adapters (`skip`, `take_while`, `fold`, `flat_map`, `any`,
`all`, `min`, `max`) are ○ and arrive on demand — the dogfood rule.

#### Chains read one per line

```glide
let top = entries
    .iter()
    .filter(|e| e.1 > 1)
    .map(|e| e.0)
    .take(20)
    .collect()
```

A line beginning with `.` continues the previous statement (Chapter 3).
This rule exists specifically because of adapter chains.

#### `for` consumes an iterator

```glide
for x in xs { … }              // xs is iterable
for x in xs.iter().take(3) { … }   // an iterator is iterable too
```

`for … in` accepts an iterator directly, or anything with an `iter()`
method.

#### `Iterable` is not `Iterator`

This distinction matters and catches people:

```glide
let xs = [1, 2, 3]
println(xs.iter().count())     // 3
println(xs.iter().count())     // 3 — a fresh iterator each time
```

A `List` is **iterable**: you can ask it for an iterator, many times.
An `Iterator` is a cursor: consuming it exhausts it.

```glide
let it = xs.iter()
println(it.count())            // 3
println(it.count())            // 0 — already drained
```

#### Making a user type iterable

Provide an `iter()` method:

```glide-run
type Bag = struct { items: List<Int> }

impl Bag {
    fn iter(self) -> Iterator<Int> {
        for x in self.items { yield x }
    }
}

fn main() {
    let b = Bag{ items: [1, 2, 3] }
    for x in b { print("{x} ") }
    println("")

    let doubled = b.iter().map(|x| x * 2).collect()
    println("{doubled:?}")
}
```

```
1 2 3
[2, 4, 6]
```

One method, and `Bag` works with `for` and with every adapter. The body
uses `yield` — that is a generator, and Chapter 25 explains it.

#### Iterating a Map

```glide
for (k, v) in m { … }          // insertion order
m.iter()                        // an Iterator<(K, V)>
```

---

### 2. Under the Hood

#### The protocol is one method

```glide
trait Iterator<T> {
    fn next(mut self) -> T?
}
```

That is the whole thing. `for x in xs` desugars to:

```glide
let it = xs.iter()
for {
    let Some(x) = it.next() else { break }
    body
}
```

One method with an `Option` return means:

- **No invalid states.** There is no "have I called `hasNext`?"
  question.
- **No desynchronisation.** Java's `hasNext()`/`next()` pair can get
  out of step; a single method cannot.
- **No `(value, ok)` tuple.** Go's older iteration idioms and channel
  receives use this shape; `Option` is the same information with a type
  that composes.

#### External versus internal iteration

This is the design axis, and it is worth understanding because it
explains why `zip` is easy here and hard in Go.

**External iteration** (Glide, Rust, Java, Python): the *consumer*
drives. It calls `next()` when it wants an element. The iterator holds
its own position.

**Internal iteration** (Go 1.23's `iter.Seq`, Ruby's `each`, callback
APIs): the *producer* drives. You hand it a callback and it calls you
for each element.

External iteration makes `zip` trivial — pull one from each side, pair
them, stop when either is exhausted. With internal iteration, zipping
two callback-driven sequences requires either coroutines or running one
side to completion into a buffer. Go's `iter.Seq` addresses this with a
`yield`-returning-bool protocol that is workable and awkward, and it is
why `DESIGN.md` calls Go's function-iterators "late and awkward".

External iteration also makes early exit natural: stop calling `next()`.
Internal iteration needs the callback to signal "stop", which is where
Go's `func(yield func(T) bool)` shape comes from.

#### Adapters are lazy default methods

`map`, `filter`, and `take` are default methods on the `Iterator` trait
(Chapter 17). Each returns a *new* iterator that wraps the source:

```
xs.iter()                    →  ListIter over [1..6]
    .map(f)                  →  MapIter { source: ListIter, f }
    .take(2)                 →  TakeIter { source: MapIter, n: 2 }
    .collect()               →  pulls twice, stops
```

`collect()` calls `next()` on `TakeIter`, which calls `next()` on
`MapIter`, which calls `next()` on `ListIter`, applies `f`, and returns.
Two pulls, two calls to `f`.

Nothing was allocated for intermediate lists. That is the entire
performance argument for laziness.

#### Generators are the same protocol

A function containing `yield` returns an `Iterator` when called. It is
sugar for the trait, **not a second protocol** — which is why
generators compose with every adapter.

In the interpreter, a generator runs on a goroutine with a channel:
`yield` sends, `next()` receives. `glide/DESIGN-DECISIONS.md` calls it
"the cheapest correct lazy implementation for a tree-walker", and
records that this costs one goroutine per delegation level for
`yield from` recursion.

The compiled tier lowers generators to state machines, which is the
fiddly part of the transpiler and is flagged in `DESIGN.md` as
something to prototype early.

#### Why user types are iterable through `iter()`

`for x in v` looks for an `iter()` method on `v`. That is the
`Iterable` half of the protocol, and it is why one method is enough to
join the ecosystem.

---

### 3. Why This Design?

#### Why laziness

Three things fall out of it, and none is available otherwise.

**Infinite sequences become ordinary values.**

```glide
fn counter(from: Int) -> Iterator<Int> {
    let mut n = from
    for {
        yield n
        n += 1
    }
}

let first_five = counter(10).take(5).collect()   // [10, 11, 12, 13, 14]
```

`counter` never terminates. `take(5)` makes it terminate. With eager
evaluation this program does not exist.

**Pipelines cost what you consume, not what you have.**
`xs.iter().map(expensive).take(3)` calls `expensive` three times, even
if `xs` has a million elements.

**No intermediate collections.** An eager `map` over a million-element
list allocates a million-element list, and a subsequent `filter`
allocates another. Lazy adapters allocate nothing until `collect()`.

The accepted cost, recorded honestly in `DESIGN.md`: **laziness
surprises.** `xs.map(f)` does nothing until consumed, so a `map` used
for its side effects silently does not happen. That is a real footgun,
and the mitigation is a cultural one — use a `for` loop for effects.

#### Why external iteration

Covered above mechanically; the design argument is `zip`.

`DESIGN.md`: "External iteration is what makes `zip` trivial — the
thing Go's callback-style `iter.Seq` makes miserable." Any operation
that consumes two sequences in lockstep — zip, merge, diff, interleave
— is natural with external iteration and requires coroutines or
buffering with internal iteration.

Early exit is the second argument, and it is the one people hit daily.

#### Why `Iterable` is separate from `Iterator`

Because a `List` can be iterated many times and an iterator cannot be
iterated at all — it *is* the iteration.

Collapsing the two bites every language that tries. In Python, a `list`
is iterable and `iter(list)` is an iterator, and the distinction is
learned by hitting it: a generator expression consumed twice gives you
elements then nothing. Java's `Iterable`/`Iterator` split is the same
one Glide makes and is one of the parts of Java's collections framework
that is straightforwardly right.

#### Why generators are sugar, not a second protocol

If generators produced something other than an `Iterator`, the
ecosystem would fragment: adapters would work on one and not the other,
and `for` would need to handle both.

Making `yield` produce an ordinary `Iterator` means a generator
composes with everything, and a consumer cannot tell whether the
sequence came from a list, an adapter chain, or a hand-written
traversal.

#### Why channels are not the iteration protocol

`DESIGN.md` is explicit, and this is worth stating because the
implementation happens to use channels:

> Channels are not the iterator protocol. A green thread + channel per
> loop means synchronisation per element, and early exit leaks a
> thread. Iteration is control flow, not communication.

The ban is on *implementing* an iterator with a thread and a channel as
a user-facing pattern. Consuming an existing stream is the legitimate
direction, which is why `for v in rx` over a channel receiver works
(Chapter 28).

That the tree-walking interpreter implements generators with a
goroutine and a channel is an implementation detail of the dev tier,
and it is exactly the thing the compiled tier replaces with a state
machine.

#### The accepted costs

`DESIGN.md` lists three, and they are worth knowing:

1. **Adapter-chain type errors are ugly.**
   `Map<Filter<Take<ListIter<Int>>>>` in an error message is not
   friendly. This is Rust's experience too.
2. **The release backend must compile generator state machines well.**
   Known-hard-but-solved — C# has done it for twenty years.
3. **Laziness surprises**, as above.

---

### 4. Competing Approaches

**Go.** Iteration was `for … range` over built-in types only for
thirteen years; user types needed a `Next()` method by convention or a
channel-based generator (which leaks a goroutine on early exit). Go
1.23 added `iter.Seq` — internal iteration via
`func(yield func(T) bool)` — which works and is awkward, and standard
library adapters arrived even later. `DESIGN.md` cites this as the
example of designing iteration in from the start rather than bolting it
on.

**Rust.** `Iterator` with one required method (`next`) and a large set
of provided adapters — the direct model. Rust's adapter surface is much
larger than Glide's current one, and Glide's will grow toward it on
demand. Rust's generators took a decade to stabilise because yielded
references borrow from suspended stack state; with a GC that problem
does not exist, which `DESIGN.md` calls the asymmetry to build around.

**Python.** Iterables and iterators separated correctly, generators
with `yield` and `yield from` (the direct source of Glide's spelling),
and lazy `itertools`. Python's model is the closest ergonomic
relative. The difference is that Python's laziness is opt-in per
function (`map` is lazy, list comprehensions are eager) whereas Glide's
adapters are uniformly lazy.

**Java.** `Iterable`/`Iterator` with `hasNext()`/`next()` — the
two-method protocol that can desynchronise — plus Streams (lazy,
adapter-based, single-use) as a parallel system. Two iteration
mechanisms in one language, which is what happens when you retrofit.

**C#.** `IEnumerable`/`IEnumerator`, LINQ as the lazy adapter set, and
`yield return` generators compiled to state machines — since 2005.
`DESIGN.md` cites C# as the proof that compiling generator state
machines well is a solved problem.

**Haskell.** Lazy by default, everywhere, so iteration is just list
processing. Maximally elegant and the source of the "laziness
surprises" critique in its strongest form — space leaks from unevaluated
thunks are a real Haskell problem. Glide's laziness is confined to
iterators, deliberately.

---

### 5. Common Mistakes

**Using an adapter for side effects.**

```glide
// Bad — nothing happens; map is lazy and nothing consumes it
notes.iter().map(|n| publish(n))

// Bad — even with a consumer, this is a loop wearing a costume
notes.iter().map(|n| publish(n)).count()

// Good
for n in notes {
    publish(n)?
}
```

The `?` in the good version is decisive: error propagation works in a
loop and does not work inside a closure, because `?` returns from the
*closure* (Chapter 8).

**Consuming an iterator twice.**

```glide
// Bad
let it = xs.iter()
let n = it.count()
let doubled = it.map(|x| x * 2).collect()    // empty — it is drained

// Good
let n = xs.iter().count()
let doubled = xs.iter().map(|x| x * 2).collect()
```

**Collecting in the middle of a chain.**

```glide
// Bad — materialises a million-element list for no reason
let result = xs.iter().map(f).collect().iter().filter(g).collect()

// Good
let result = xs.iter().map(f).filter(g).collect()
```

**Indexing instead of iterating.**

```glide
// Bad
for i in 0..xs.len() { process(xs[i]) }

// Good
for x in xs { process(x) }

// Good, when the index is genuinely needed
for (i, x) in xs.iter().enumerate() { process(i, x) }
```

**Expecting `zip` to fail on different lengths.** It stops at the
shorter side, silently. If mismatched lengths are a bug in your data,
check the lengths first.

**Trying to `?` inside an adapter.**

```glide
// Bad — the ? returns from the closure
let contents = paths.iter().map(|p| fs.read_string(p)?).collect()

// Good
let mut contents = []
for p in paths {
    contents.push(fs.read_string(p)?)
}
```

This is the single biggest reason loops remain the right tool for
fallible work.

**Reaching for an adapter that does not exist yet.** The set is
deliberately small and grows when a program needs one. Write the loop.

**Assuming laziness is free in the interpreter.** Generators cost a
goroutine each today, and `yield from` recursion costs one per level.
Fine for correctness; not a benchmark target.

---

### 6. Performance Considerations

**Adapters allocate nothing.** A chain of five adapters is five small
wrapper values, and no intermediate lists. Compare an eager `map` over
a million elements, which allocates a million-element list per stage.

**`take(n)` bounds the work.** `xs.iter().map(expensive).take(3)` calls
`expensive` three times regardless of `xs.len()`.

**Each element passes through one `next()` call per adapter.** A
five-adapter chain is five calls per element. In the compiled tier
these inline into a single loop — this is Rust's "zero-cost
abstraction" claim and it holds when the closures are known at the call
site. In the interpreter they do not inline, so a long chain over a
large list is measurably slower than a hand-written loop.

**`collect()` allocates once**, growing geometrically. `count()` and
`sum()` allocate nothing.

**Generators cost a goroutine and a channel each** in the interpreter.
`yield from` recursion costs one goroutine per delegation level, so a
depth-20 tree traversal has 20 live goroutines mid-walk. This is a
tree-walker artifact; the compiled tier uses state machines.

**A `for` loop over a `List` is the cheapest thing.** In the compiled
tier, `for x in xs` over a list degenerates to an index-and-bounds-check
loop. If you are not transforming, do not build a pipeline.

**Adapter-chain error messages are ugly**, which is a compile-time
ergonomics cost rather than a runtime one, and it is recorded as an
accepted cost.

---

### 7. Best Practices

**Use adapters for transformations, loops for effects.**

```glide
// Good — a transformation
let titles = notes
    .iter()
    .filter(|n| n.published)
    .map(|n| n.title)
    .collect()

// Good — an effect
for note in notes {
    publish(note)?
}
```

The rule of thumb: if the pipeline ends in `collect()`, `count()`, or
`sum()`, adapters are right. If it ends in a side effect or needs `?`,
write the loop.

**Put `take` and `filter` early.** Adapters run per element in chain
order, so filtering before mapping means the map runs on fewer
elements:

```glide
// Better
xs.iter().filter(cheap_test).map(expensive).collect()

// Worse
xs.iter().map(expensive).filter(cheap_test).collect()
```

**Write chains one adapter per line.**

```glide
// Good
let result = entries
    .iter()
    .filter(|e| e.1 > threshold)
    .map(|e| e.0)
    .take(20)
    .collect()
```

The leading-dot continuation rule exists for this, and it is what the
formatter will produce.

**Give a user type an `iter()` method rather than exposing its
backing list.**

```glide
// Bad — leaks the representation, and the caller can mutate it
impl Bag {
    pub fn items(self) -> List<Int> { self.items }
}

// Good
impl Bag {
    pub fn iter(self) -> Iterator<Int> {
        for x in self.items { yield x }
    }
}
```

The good version makes `Bag` `for`-able and adapter-able, keeps the
representation private, and hands out no mutable reference.

**Do not collect just to get a length or a sum.**

```glide
// Bad
let n = xs.iter().filter(p).collect().len()

// Good
let n = xs.iter().filter(p).count()
```

**Consume an iterator once, and name it if you need it twice.** If you
find yourself wanting to iterate the same iterator twice, you wanted
the *iterable* — call `.iter()` again.

**Prefer `enumerate()` to a manual counter.**

```glide
// Bad
let mut i = 0
for x in xs {
    use(i, x)
    i += 1
}

// Good
for (i, x) in xs.iter().enumerate() {
    use(i, x)
}
```

---

### 8. Examples

**Laziness, demonstrated:**

```glide-run
fn expensive(n: Int) -> Int {
    println("  computing {n}")
    n * 10
}

fn main() {
    let xs = [1, 2, 3, 4, 5, 6]

    println("building the chain:")
    let chain = xs.iter().map(|n| expensive(n)).take(2)
    println("  (nothing has happened yet)")

    println("consuming:")
    let result = chain.collect()
    println("{result:?}")
}
```

```
building the chain:
  (nothing has happened yet)
consuming:
  computing 1
  computing 2
[10, 20]
```

Two calls, not six. The chain existed as a value for two lines and did
nothing.

**A word-frequency pipeline, adapters and loop each where they belong:**

```glide-run
fn main() {
    let text = `
        the quick brown fox
        jumps over the lazy dog
        the dog sleeps
    `

    // A loop, because we are accumulating into a map (an effect).
    let mut counts: Map<String, Int> = [:]
    for word in text.split_whitespace() {
        let word = word.to_lower()
        counts[word] = (counts[word] ?? 0) + 1
    }

    // A pipeline, because we are transforming.
    let mut entries = counts.entries()
    entries.sort_by(|a, b| b.1.cmp(a.1))

    let report = entries
        .iter()
        .take(3)
        .map(|e| "{e.1} {e.0}")
        .collect()

    for line in report {
        println(line)
    }
}
```

```
3 the
2 dog
1 quick
```

**Making a type first-class:**

```glide-run
type Grid = struct {
    width: Int
    cells: List<Int>
}

impl Grid {
    fn new(width: Int, cells: List<Int>) -> Grid {
        Grid{ width: width, cells: cells }
    }

    // Iterate cells.
    fn iter(self) -> Iterator<Int> {
        for c in self.cells { yield c }
    }

    // Iterate (row, col, value) triples — a second, named iterator.
    fn positions(self) -> Iterator<(Int, Int, Int)> {
        for (i, c) in self.cells.iter().enumerate() {
            yield (i / self.width, i % self.width, c)
        }
    }
}

fn main() {
    let g = Grid.new(3, [1, 2, 3, 4, 5, 6])

    println(g.iter().sum())

    for (row, col, v) in g.positions() {
        if v % 2 == 0 {
            println("even {v} at ({row}, {col})")
        }
    }

    let big = g.iter().filter(|v| v > 3).collect()
    println("{big:?}")
}
```

```
21
even 2 at (0, 1)
even 4 at (1, 0)
even 6 at (1, 2)
[4, 5, 6]
```

`iter()` makes `Grid` work with `for` and every adapter; `positions()`
is a second iterator with a name, which is how you offer more than one
traversal.

**Bad versus good: the pipeline that should be a loop**

```glide
// Bad — three problems in three lines
fn save_all(notes: List<Note>) -> Int {
    let mut saved = 0
    notes.iter().map(|n| {
        save(n)
        saved += 1
    }).count()
    saved
}
```

The `map` is being used for its side effect; `count()` is there only to
force the chain to run; `saved` is a `mut` binding mutated from inside
a closure (which the task-boundary rule will eventually forbid); and
`save` returning a `Result` cannot be propagated with `?`.

```glide
// Good
fn save_all(notes: List<Note>) -> Result<Int, Error> {
    let mut saved = 0
    for n in notes {
        save(n)?
        saved += 1
    }
    Ok(saved)
}
```

Shorter, propagates errors, no closure, no forcing consumer.

---

### 9. Summary & Exercises

**Summary**

- An **iterator** yields elements on demand. `xs.iter()` does not walk
  the list; it produces a cursor.
- The protocol is **one method**: `fn next(mut self) -> T?`. `None`
  means exhausted. No `hasNext` to desynchronise, no `(value, ok)`
  tuple, no invalid states.
- **External iteration** — the consumer drives — is what makes `zip`
  and early exit natural. Go's internal `iter.Seq` makes both awkward.
- **Adapters are lazy**: `map`, `filter`, `take`, `enumerate`, `zip`
  build new iterators and run nothing. **Consumers** — `collect`,
  `count`, `sum` — drain them. `xs.iter().map(f).take(2)` calls `f`
  twice.
- Laziness buys infinite sequences as ordinary values, cost
  proportional to what you consume, and no intermediate collections.
  Its accepted cost is that a `map` used for effects silently does
  nothing.
- **`Iterable` is separate from `Iterator`.** A `List` can be iterated
  many times; an iterator is consumed once.
- A user type with an **`iter()` method** works in `for … in` and with
  every adapter.
- **Generators are sugar for the same protocol**, not a second one, so
  they compose with everything (Chapter 25).
- **Channels are not the iteration protocol** — implementing an
  iterator with a thread and a channel means synchronisation per
  element and a leaked thread on early exit. Consuming an existing
  stream (`for v in rx`) is the legitimate direction.
- Use adapters for transformations and loops for effects. `?` does not
  work inside a closure, which settles most cases.
- ○: `skip`, `take_while`, `fold`, `flat_map`, `any`, `all`, `min`,
  `max` and friends, arriving on demand.

**Exercises**

1. **Prove the laziness.** Write a `map` whose closure prints, chain it
   with `take(2)` over a ten-element list, and count the prints. Then
   move the `take` before the `map` and count again. Then remove the
   `collect()` entirely and count once more. The three numbers are the
   whole model.

2. **Implement `zip` twice.** Write `zip` for two external iterators
   (pull one from each, stop at the shorter). Then try to write it for
   two Go-style internal iterators — `func(yield func(T) bool)` — using
   only ordinary function calls. You will find you cannot without a
   coroutine or a buffer, and that is the argument for external
   iteration in one exercise.

3. **Find the pipeline that should be a loop.** In a codebase using
   LINQ, Streams, or Rust iterators, find a chain whose final consumer
   exists only to force evaluation (`.count()` discarded, `.forEach`,
   `.collect::<Vec<_>>()` then ignored). Rewrite it as a loop and
   compare. Then find one going the other way — a hand-written loop
   building a list that a `filter`/`map`/`collect` would say better.
