# Appendix C: Glide for Rust, Swift, and Python Programmers

Appendix B covers Go, which is Glide's closest relative. This one
covers the other three backgrounds most likely to meet it.

---

## For Rust programmers

You will be immediately fluent. `fn`, `let`/`let mut`, `|x|` closures,
tail expressions, `Result` and `?`, sum types with exhaustive `match`,
`impl` blocks, traits with default methods, the orphan rule, `//`
comments, `snake_case`/`PascalCase`. All of it is Rust's.

**The one-sentence summary: it is Rust with a garbage collector, and
roughly half of Rust's conceptual weight was borrow-checker overhead.**

### What is gone, and what went with it

| Gone | And therefore also gone |
|---|---|
| The borrow checker | Lifetimes, `'a` annotations, `'static` bounds |
| Ownership | `&`/`&mut` at call sites, `move`, `Cow`, `Rc`/`Arc`/`RefCell` |
| `Drop` | (replaced by `defer`/`errdefer`) |
| `Fn`/`FnMut`/`FnOnce` | **One function type**, `fn(A) -> B` |
| `Pin`, `Send + 'static` | `async`/`await` entirely |
| Macros | `println!`, `vec!`, `matches!`, proc macros, `derive` as a macro |
| The turbofish | `::<>` never appears |
| Match ergonomics | `ref`, `ref mut`, default binding modes |

That last row is worth dwelling on. Rust's match-ergonomics saga
exists because patterns must decide whether they move or borrow. Here,
patterns bind values, the GC holds them, and `let (mut a, b) = pair`
just works. `DESIGN.md` calls it "the biggest pattern dividend of no
borrow checker".

### What is different, and why

**Generators are easy.** A function containing `yield` is an iterator,
and `yield from` delegates. Rust's generators were unstable for a
decade because a yielded reference borrows from suspended stack state —
which is why `Pin` exists. With a GC, that problem does not exist.

**No `async`.** Green threads with structured concurrency instead. No
function colouring, no executor fragmentation, no cancellation-safety
documentation. The cost `DESIGN.md` concedes: stackful tasks are
kilobytes where futures are bytes, so async wins at extreme task
counts.

**Combinators are mostly absent.** No `and_then`, `map_err`,
`ok_or_else`, `unwrap_or_default`. Three constructs plus `match`:

```rust
// Rust
config.get("port").and_then(|s| s.parse().ok()).unwrap_or(8080)
```

```glide
// Glide
let port = (config["port"] ?? "8080").parse_int() ?? 8080
```

**`?` does not work on `Option`.** `?` propagates a `Result`'s `Err`
and carries error-type conversion; overloading it onto `Option` would
give one glyph two behaviours resolved by operand type. Use `??`,
`if let`, or `let … else`.

**Nested shadowing is banned.** Sequential redeclaration in the same
scope is allowed and idiomatic (Rust's quiet win, kept). Shadowing a
*live enclosing* name is a compile error — which catches `match x { x
=> … }`, idiomatic in Rust and illegal here.

**Comptime, not proc macros.** `derive(Json)` is an ordinary
compile-time function walking the type's structure, not a token-stream
transformer in a separate crate. No second compiler, and the tooling
sees through it.

**Angle brackets, no turbofish.** The `f<T>(x)` ambiguity is solved
with C#/TypeScript's tentative-parse rule. `DESIGN.md`: "no turbofish,
ever — Rust's `::<>` is the wart this paragraph exists to avoid."

**`distinct` instead of the newtype pattern.** Same semantics — zero
cost, no inherited operators — with a keyword instead of a tuple
struct, and `.value()` instead of `.0`.

### The one thing that is genuinely worse

**`mut` is a path property, not an object guarantee.** Collections are
references; two bindings can alias one object; `let` means "no mutation
through this name", not "frozen".

```glide
let mut a = [1, 2, 3]
let b = a
a.push(4)
println("{b:?}")      // [1, 2, 3, 4]
```

`DESIGN.md` records this as a deliberate sacrifice. The designed
mitigation is stdlib persistent collections (`PList`/`PMap`, ○) — a
module, not a default.

You will notice this within an hour, and the honest framing is: you
traded compile-time aliasing guarantees for deleting half the
language's conceptual weight.

---

## For Swift programmers

Also very familiar. `T?` optionals, `if let`, `guard let` (spelled
`let … else`), `??`, leading-dot enum shorthand, declared conformance
with structural satisfaction, block-scoped `defer`, `mutating func`
(spelled `mut self`), `any Protocol` for the dispatch choice.

`DESIGN.md` takes more from Swift than from any language except Go and
Rust, and mostly on ergonomics.

### What is different

| Swift | Glide |
|---|---|
| Trailing closures | **Declined** — closures are arguments and sit in the parens |
| `$0` | **Declined** — naming the parameter is documentation |
| External/internal parameter labels | **Declined** — one name; parameter names are API |
| Classes alongside structs | **Structs only**; no inheritance |
| ARC, capture lists (`[weak self]`) | Tracing GC; no retain cycles to break |
| `\(expr)` interpolation | `{expr}`, always on |
| Grapheme-cluster string indexing | No indexing at all; `bytes()`/`runes()` |
| Protocol extensions | Trait default methods (same thing) |
| `throws`/`try` | `Result` and `?` |
| `async`/`await`, `TaskGroup` | Green threads, `scope` |
| `where` clauses | Inline bounds only, in v0 |

The trailing-closure refusal is the one you will miss. `DESIGN.md`'s
reason: it is "the gateway to builder DSLs and parser pain".

Swift's `guard let` is `let … else` with the same divergence
requirement, and the no-`Some`-wrapper ergonomic (`if let user =
find(id)`) is taken directly.

---

## For Python programmers

The biggest adjustment set, because Glide is statically typed,
compiled, brace-delimited, and refuses truthiness.

### The five that hurt

**1. No truthiness.** `if xs` does not compile. Conditions take `Bool`.
Every legitimate use has an explicit substitute:

| You mean | Write |
|---|---|
| present? | `if let x = maybe { … }` |
| empty? | `if !xs.is_empty() { … }` |
| nonzero? | `if n != 0 { … }` |
| default | `x ?? fallback` |

`DESIGN.md`'s specific objection to Python's version: even the
principled form conflates **absent** with **empty**, which is exactly
the distinction `Option` exists to make.

**2. Types are mandatory at boundaries.** Signatures are always
explicit. Inference works inside bodies only.

**3. No exceptions.** Errors are `Result` values in the return type.
There is no `try`/`except`, and there is no `recover` for panics.

**4. Mandatory initialisation and no null.** Every struct field is
supplied; absence is `T?` in the type.

**5. Braces and static structure.** No significant whitespace; a
directory is a module rather than a file; imports execute nothing.

### What transfers directly

More than you would expect:

- **Generators.** `yield` and `yield from` are Python's, semantics
  included. `DESIGN.md` takes the spelling from PEP 380.
- **`match`/`case` → `match`.** Structural patterns, guards. And
  Glide avoids Python's notorious capture-versus-constant footgun (a
  bare lowercase name in a `case` always binds, so `case RED:` matches
  everything) — because capitalised patterns test and lowercase bind.
- **Insertion-ordered dicts.** Same choice, same reason.
- **f-strings → interpolation**, except always-on. The
  forgotten-`f`-prefix bug is why.
- **Keyword arguments and defaults.** Same feature, with one
  improvement: defaults evaluate **per call**, so the shared mutable
  default (`def f(xs=[])`) cannot happen.
- **List/dict comprehension instincts → iterator adapters.**
  `[f(x) for x in xs if p(x)]` becomes
  `xs.iter().filter(p).map(f).collect()`.
- **`with` → `defer`**, flat instead of nested one level per resource.

### Habits to drop

- `if x:` for anything but a `Bool`.
- Returning `None` for errors — use `Result` when the failure carries
  information.
- Dictionaries as records — maps at the boundary, structs inside.
- Monkey-patching, `**kwargs` soup, dynamic attribute access. None has
  an equivalent, and there is no runtime reflection.
- Module-level code that runs on import. Imports are inert.

---

## For everyone: the three things that surprise people most

**1. Absence is a type, and that changes function signatures more than
it changes function bodies.** `fn owner() -> User` is a *promise*, and
you stop defensively checking. The value is as much in the `T`
signatures as the `T?` ones.

**2. Exhaustiveness turns a refactor into a work list.** Add a variant
and the compiler hands you every place that needs updating. It is the
single most valuable property in day-to-day use, and every `_ =>` arm
spends a piece of it.

**3. Structured concurrency deletes an entire category of code.** No
shutdown handler, no stop channel, no `ctx`, no `WaitGroup`, no
`errgroup`. A background task's lifetime is a lexical region, and when
that region ends the task is cancelled and joined. Chapter 27's Go
comparison is the clearest demonstration in the book.
