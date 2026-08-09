# Appendix E: Further Reading

Glide is an assembly of borrowed ideas, and `LINEAGE.md` exists
specifically to trace each one to its source. This appendix points at
the primary material — the repository's own documents first, then the
outside sources the design cites.

---

## In this repository

Read in this order:

**`docs/book/`** — this book. Teaches the language from hello-world up.

**`DESIGN.md`** — the design document: every decision and its *why*,
including the deliberate sacrifices and the open questions. It is
unusually honest and is the recommended second read. Roughly 2,000
lines, and the book cites it on nearly every page.

**`LINEAGE.md`** — the companion to `DESIGN.md`: for each significant
decision, who invented the feature, who adopted it, who tried living
without it, and what that evidence says. Written for a reader who knows
Go and nothing else, with dates and named languages rather than vibes.
It deliberately includes the evidence *against* — Rust removing green
threads, for instance.

**`docs/reference/language.md`** and **`docs/reference/stdlib.md`** —
terse lookup references, modelled on Go's spec/stdlib split. Every
feature carries a ✓/○ status marker. The book teaches; these remind.

**`GRAMMAR.md`** — three complete programs written as if Glide exists,
plus the list of decisions those programs forced. This is where the
language was stress-tested by *reading* before anything was
implemented.

**`STDLIB-GOALS.md`** — what the standard library will and will not
contain, and why. The out-list discipline matters as much as the
in-list.

**`glide/DESIGN-DECISIONS.md`** — how the *interpreter* is built, and
which corners are deliberately cut because the real compiler makes them
obsolete. The source for most of this book's "Under the Hood" sections,
and worth reading for the war stories (the GC-truncated generator
stream is a good one).

**`docs/book/01-introduction.md`** — a single-chapter whirlwind tour of
the whole language, written before this book. An hour's read if you
want the shape before the detail.

**`glide/examples/`** — `wordfreq.gld`, `tree.gld`, `pipeline.gld`,
`notes.gld`, `sieve.gld`. All run. `pipeline.gld` is the best single
demonstration of the concurrency surface.

---

## The languages Glide borrows from

Ordered by how much was taken.

### Go

- ***The Go Programming Language*** — Donovan & Kernighan. Still the
  best book about Go, and Glide's runtime model is Go's.
- ***Go at Google: Language Design in the Service of Software
  Engineering*** — Rob Pike. The clearest statement of the philosophy
  Glide inherits: at scale, reading code is the bottleneck.
- **Effective Go** and the **Go Proverbs**. Most of the advice
  transfers.
- **The Go 1.22 loop-variable change**. Read the release notes and the
  design discussion: a semantic break to fix a capture bug, and the
  reason Glide has per-iteration bindings from day one.
- **`iter.Seq` (Go 1.23)**. Internal iteration arriving thirteen years
  late, and the direct motivation for designing iteration in from the
  start.

### Rust

- **The Rust Programming Language** ("the book") — chapters on
  ownership, enums and pattern matching, error handling, and traits.
  Glide takes the type system and declines the ownership half.
- **`thiserror` and `anyhow`**. The eight-year ecosystem search that
  ended in a libraries/applications split — which `DESIGN.md` ships in
  the standard library on day one.
- **"Async Rust Is A Bad Language"** and the various maintainer posts
  on async's difficulty. The evidence behind rejecting `async`/`await`.
- **Niche optimisation**, for why `Option<&T>` is free.

### Zig

- **The Zig language reference** on `comptime`, `errdefer`, `test`
  blocks, and safety modes. Four direct adoptions.
- **Andrew Kelley's talks** on comptime as an alternative to macros.
- Zig's **unused-variable strictness** — adopted in spirit, moved to
  a different boundary, because it became Zig's most-resented
  decision.

### Swift

- **The Swift Programming Language** on optionals, `guard let`,
  protocols, and protocol extensions.
- **Swift Evolution SE-0004**, removing `++` and `--`. A language that
  shipped the operator and paid to remove it.
- Swift's **`any Protocol`** spelling, taken directly.

### The ML family

- **Haskell** — type classes as the ancestor of traits, `Maybe`,
  algebraic data types.
- **Standard ML** and **OCaml** — pattern matching and exhaustiveness,
  and the observation that compilers are the ideal workload for this
  feature set (Chapter 35).

### Others

- **Nim** — `distinct` types.
- **Kotlin** — named arguments and default values, wholesale.
- **CLU (1975)** — iterators as a first-class construct with `yield`.
  Fifty years old and still absent from most mainstream languages.
- **Lua 5.1** — the embedding model, and the evidence that hosts pin an
  embedded interpreter for a decade and stay happy. (Glide does *not*
  take the freeze: the interpreter tracks the language, because the
  shared frontend already guarantees one implementation.)
- **Erlang** — supervision trees, flagged as a likely future scope
  variant.
- **Clojure** — persistent collections with structural sharing.
- **C#** — `??`, and generics that parse without a turbofish.
- **Java** — `java.time`, "the one part of Java stolen without irony",
  and Loom's virtual threads as the fifteen-year vindication of green
  threads.

---

## Structured concurrency

The most important cluster, because the idea is recent and the primary
sources are unusually good.

- **"Notes on structured concurrency, or: Go statement considered
  harmful"** — Nathaniel J. Smith (2018). The founding document.
  Introduces nurseries and argues that `go`/`spawn` is the modern
  `goto`. If you read one external source from this appendix, read
  this one.
- **Trio's documentation** — the reference implementation, and the best
  writing on cancellation semantics.
- **Kotlin's structured concurrency docs** — including the
  `CancellationException`-in-every-catch-block problem that Glide's
  uncatchable cancellation fixes.
- **JEP 453: Structured Concurrency** (Java) — the same idea arriving
  in the most conservative mainstream ecosystem.
- **"What Color Is Your Function?"** — Bob Nystrom (2015). The
  function-colouring argument against `async`/`await`.

---

## Error handling

- **"Error Handling in Go"** and the `errors` package proposals. The
  half of the answer Go shipped.
- **Rust's `?` operator RFC** and the `From` conversion mechanism. The
  other half.
- **Zig's error return traces** — a genuine novelty, adopted.
- **"Parse, don't validate"** — Alexis King (2019). The clearest
  statement of the pattern Chapters 12, 14, and 15 build to, and the
  most valuable single idea in this book that Glide did not invent.

---

## Types and data modelling

- **"Making Illegal States Unrepresentable"** — Yaron Minsky. The sum
  types argument in its original form.
- **"Designing with Types"** — Scott Wlaschin's series (F#, and
  applicable directly). The best practical writing on sum types for
  people arriving from the C lineage.
- **Tony Hoare's "null references: the billion dollar mistake"** talk
  (2009).
- **"Unsigned: A Guide to Better Code"** and the C++ Core Guidelines
  discussion of unsigned sizes. Background for Chapter 5's signed-index
  decision, including Stroustrup and Sutter calling it a mistake.

---

## Testing

- **"QuickCheck: A Lightweight Tool for Random Testing of Haskell
  Programs"** — Claessen & Hughes (2000). The origin of property
  testing. Thirty years of evidence and never mainstream, for friction
  reasons Glide is trying to delete.
- **Hypothesis** (Python) documentation, particularly on shrinking.
- **Hedgehog**'s integrated shrinking, for the algorithm Glide's greedy
  shrinker is a simpler version of.
- **FoundationDB's deterministic simulation testing** talk. The
  lineage behind the designed seeded-scheduler mode, which `DESIGN.md`
  calls "possibly Glide's most differentiating capability".

---

## Tooling and builds

- **`gofmt` and the "gofmt's style is no one's favourite, yet gofmt is
  everyone's favourite" observation** — Rob Pike. Glide goes further,
  to a pure AST-to-bytes function with no configuration.
- **The Dart formatter rewrite** — the best-regarded width-aware
  formatter shipping, and the model for Glide's harder version.
- **A Prettier Printer** — Philip Wadler. The algorithm behind
  constraint-solving pretty-printers.
- **The `rustfmt.toml` history** — a formatter that reinstated, one
  knob at a time, every argument it existed to end.
- **Reflections on Trusting Trust** — Ken Thompson (1984). Background
  for why the bootstrap chain from a mainstream toolchain with no
  binary seed matters (Chapter 35).

---

## Implementation

- ***Crafting Interpreters*** — Bob Nystrom. Free online, and the best
  practical introduction to writing a tree-walking interpreter and then
  a bytecode VM. The interpreter this book describes is a
  `jlox`-shaped program with a much richer language.
- **C#'s iterator implementation** — twenty years of compiling
  generator state machines, cited in `DESIGN.md` as the proof that the
  transpiler's hardest lowering is solvable.
- **Go's `//line` directives** — the mechanism that lets a transpiled
  language be debugged at source granularity.
- **`modernc.org/sqlite`** — a machine translation of SQLite to pure
  Go, and the interpreter's only third-party dependency.

---

## The counter-arguments

`LINEAGE.md` makes a point of recording evidence *against* decisions,
and it is worth reading the primary sources for the ones Glide
rejected:

- **Rust removing green threads (RFC 230, 2014)** — the strongest
  argument against Glide's concurrency model. Rust's reasoning was that
  green threads impose a runtime cost that a systems language cannot
  make mandatory. Glide accepts the runtime; the argument is still
  worth understanding.
- **The Go generics proposals** and their thirteen-year delay,
  including the compile-time and complexity concerns that Glide's
  monomorphisation choice re-incurs.
- **Zig's unused-variable errors** becoming its most-resented
  decision — the direct source of Glide's move-the-boundary approach.
- **Kotlin's `async` gotcha** — a child's failure killing the scope
  before the planned `await`-and-handle. The reason Glide has one spawn
  primitive rather than two.
- **The `sqlx` compile-time verification model** (Rust) — genuinely
  more powerful than Glide's comptime placeholder checking, at the cost
  of a database in your build. Read it and decide whether you agree
  with the trade.

---

## If you read only three things

1. **`DESIGN.md`** — because it is the actual specification, it is
   honest about costs, and this book is a teaching layer over it.
2. **"Notes on structured concurrency"** — Smith, 2018. It will change
   how you think about background work in every language you use.
3. **"Parse, don't validate"** — King, 2019. Short, and it is the
   pattern that makes the type system pay for itself.
