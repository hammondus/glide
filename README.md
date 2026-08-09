<p align="center">
  <img src="assets/logo/glide-glider.svg" alt="Glide" width="140">
</p>

# The Glide Programming Language

Do we need ANOTHER programming language? I would definitely say NO.
This started as an experiment. I asked the question: what would you get if you
looked back at programming language history, took the time-tested features that
have proven themselves, and put them together in one language?

Nothing here is really new — and that's deliberate. The aim is the best
features from a whole range of languages, minus their worst ones. Glide is a
clean slate, so no baggage has to be kept. [`LINEAGE.md`](LINEAGE.md) traces
every borrowed idea back to its source. Inspiration has come from:

- **Go** — kept: the whole runtime model (green threads, channels, GC,
  `defer`), sub-second builds, one static binary, one canonical formatter,
  errors as values. Rejected: `nil`, zero values, implicit interfaces,
  `init()`, runtime reflection.
- **Rust** — kept: `Result` + `?`, sum types with exhaustive `match`,
  `let`/`mut` immutability, `|x|` closures, `Mutex<T>` that owns its data.
  Rejected: the borrow checker, lifetimes, macros, async/await, the
  turbofish.
- **Zig** — kept: comptime instead of macros, `errdefer`, `test` blocks in
  the language itself, overflow traps in dev builds. Rejected: manual
  memory management, comptime-as-generics, errors on every unused variable.
- **Swift** — kept: `T?` optionals, `if let` and `let … else`, declared
  trait conformance, leading-dot enum shorthand, block-scoped `defer`.
  Rejected: trailing closures, `$0`, the two-name parameter split.
- **Haskell & the ML family** — kept: the data model — sum types, pattern
  matching, no null, type classes (as traits), typed holes. Rejected:
  whole-program type inference, user-invented operators.
- **Kotlin** — kept: named arguments and default values, wholesale.
  Rejected: trailing closures and `it`.
- **JavaScript** — (only joking)

Smaller debts, all recorded in [`LINEAGE.md`](LINEAGE.md): C# (`??`, and
generics that parse without a turbofish), Java (`java.time`, the one part
stolen without irony), Erlang (supervision policies), Lua (the embedding
model), CLU (generators), Python's Trio library (nurseries for structured
concurrency).

The result:

Glide is a compiled, statically typed language in the Go tradition —
garbage collected, green-threaded, one binary, boring on purpose — with
the type system of the ML family: sum types, pattern matching, no null.
Effortless motion, no visible struggle, real speed. Files end in `.gld`.

Today it is a tree-walking interpreter written in Go, but the goal is
self-hosting: the compiler will be written in Glide, with a Glide→Go
transpiler as the first native backend. Until then the interpreter is the
dev tier — and it makes type-checked scripting nearly free:
`glide run tool.gld`, or a `#!/usr/bin/env glide run` shebang. The
interpreter will also be embeddable as a Go library, so Go programs can
use Glide as their scripting language (see the embedding section of
[`DESIGN.md`](DESIGN.md)).

**The ubiquitous hello world**
```glide
fn main() {
    greet("world")
}

fn greet(name: String) {
    println("Hello, {name}!")
}
```

## Status

**Early and moving fast.** A tree-walking interpreter (milestones M1–M3)
runs the whole ratified surface today: bindings, functions, closures,
lists/maps/tuples, structs and methods, sum types with `match`, `Result`
+ `?`, generators, `distinct` types, structured concurrency (scope,
spawn, channels, `select`), and built-in testing with property-based
tests — plus `http`, `sql` and `json`.

**Programs are type-checked before they run** (M4b), in every tier,
with no way to opt out. The checker reports only what it is certain
of — anything it does not yet model passes in silence — so what is
still enforced dynamically is a shrinking list, not a policy: generic
bounds, trait conformance and match exhaustiveness are **M4c, the work
in progress**.

The interpreter is not scaffolding. The plan is two tiers over one
shared frontend — `glide run` as a statically-checked scripting
language, and a compiler for standalone binaries — so the two can
differ in speed but never in meaning. The compiler comes after M4.

One user, no compatibility promise — breaking changes are free until
further notice, deliberately.

## Try it

Requires a Go toolchain to build the interpreter:

```bash
cd glide          # the interpreter lives in the glide/ subdirectory
make build                                      # → bin/glide
bin/glide run examples/wordfreq.gld testdata/sample.txt
bin/glide run yourfile.gld                      # any .gld file
bin/glide test yourfile.gld                     # runs its test blocks
```

## Reading order

- [`DESIGN.md`](DESIGN.md) — the design document: every decision and its
  *why*, including the deliberate sacrifices.
- [`LINEAGE.md`](LINEAGE.md) — the history behind each decision: who
  invented it, who adopted it, who tried living without it.
- [`docs/reference/language.md`](docs/reference/language.md) and
  [`docs/reference/stdlib.md`](docs/reference/stdlib.md) — terse lookup
  references, Go's spec/stdlib split. ✓ marks what runs today, ○ what is
  designed but not yet implemented.
- [`docs/book/`](docs/book/) — the book: teaches the language from
  hello-world up, assuming Go instincts and nothing else.
- [`GRAMMAR.md`](GRAMMAR.md) — the grammar, including the open fights.
- [`STDLIB-GOALS.md`](STDLIB-GOALS.md) — what the standard library will
  and won't contain, and why.

## License

[MIT](LICENSE)
