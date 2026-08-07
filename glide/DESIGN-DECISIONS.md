# Interpreter implementation decisions

Language decisions live in `../DESIGN.md`. This file records how the
*interpreter* (bootstrap step 1) is built, and which corners are
deliberately cut because the real compiler makes them obsolete.

## Milestones

1. **M1 (done): run `wordfreq`** (GRAMMAR.md program 1) — the whole
   expression language, zero user-defined types.
2. **M2 (done): run program 3 (Tree)** — `type` (structs + simple
   sum types), `match` with guards, `impl` (inherent + trait),
   `mut self` with call-path checks, `if let`, generators
   (`yield` / `yield from`), `test` blocks with property-based
   generation and shrinking, `expect` with both-sides reporting.
3. **M3: run program 2 (HTTP + SQL)** — named-field variants,
   dot-shorthand (`.NotFound(id)`), `distinct` types, named
   arguments, `defer`, structured concurrency, http/sql/json host
   shims, `derive` doing real work.

## Decisions

- **Dynamically checked.** Type annotations are parsed, kept as
  strings, and ignored. The interpreter proves *semantics*; the
  checker arrives with the Glide-written frontend (bootstrap step 2).
  Writing a static checker in Go now would be thrown-away work.
  Rules the compiler will enforce statically are enforced dynamically
  instead so programs still can't cheat: mut, nested-shadow ban,
  let-else divergence, the tail-value rule.
- **Go panics for runtime errors, explicit signals for Glide control
  flow.** `return` and `?` thread a `sig` value up the evaluator
  (they are semantics); runtime errors panic and are recovered once
  in `Run` (they are diagnostics). `os.exit` panics with a sentinel
  that becomes `ExitError`, so tests intercept exits instead of the
  test process dying.
- **The parser is the grammar.** Recursive descent + small Pratt
  core. EBNF gets extracted from the working parser later, not
  written ahead of it.
- **Maps are insertion-ordered** (keys slice + Go map). Makes
  programs and the golden test deterministic. Provisional language
  semantics — ratify or revisit when Map lands properly.
- **`sort_by` is stable**, comparator returns an Int in cmp order
  (<0, 0, >0). Whether Glide grows an `Ordering` type is an open
  stdlib question; Int is the M1 shim.
- **Collections are Go pointers** (`*ListV`, `*MapV`): reference
  semantics under GC, matching the recorded "mut is a path property"
  sacrifice. Mutability is checked at the *root binding* of an
  assignment path (`counts[k] = v` requires `mut counts`).
- **Receiver-mut on builtin methods is not enforced** (`xs.push`
  works through a `let` binding). The checker's job; noted so nobody
  mistakes it for a semantic decision.
- **Closures capture a flattened binding snapshot at creation**
  (`Env.capture`): capture-by-reference to binding *cells*, resolved
  when the closure is made. Sharing the cells keeps mutation through
  captured `mut` variables visible; snapshotting keeps a later
  same-scope redeclare from retargeting the closure. The first
  implementation shared the live env map (call-time name lookup) and
  got redeclare-straddling closures wrong.
- **Option is unboxed**: a `T?` is "the value, or None". The sketch
  assigns a `Node` straight to a `Node?` field, so the language has
  implicit `T -> T?` coercion; without static types the interpreter
  can't see the wrap points, and unboxing makes coercion a no-op.
  `Some` is the identity function; `Some(p)` patterns match any
  non-None value. Cost: `Option<Option<T>>` is unrepresentable in
  this tier — the checker era must box.
- **Generators run on a goroutine + channel.** Cheapest correct lazy
  implementation for a tree-walker: `yield` sends, `Next` receives;
  body panics are forwarded to the consumer; an abandoned iterator's
  producer is unblocked by a GC cleanup hook closing a stop channel.
  This proves generator *semantics* only — the transpiler needs CPS
  or a state machine, and DESIGN.md records that lowering as the
  thing to prototype early. `yield from` recursion costs a goroutine
  per delegation level; fine here, irrelevant to the compiled tier.
- **Property tests**: fixed seed (reproducible), 100 cases, case 0
  is always the type's simplest value (empty list, 0, "") because
  empty-case bugs dominate. Shrinking is greedy: structurally
  smaller first (shorter lists), then simpler values (ints toward
  0), rerunning the test body each step.
- **Test/bench are contextual keywords**: `test` only starts a
  declaration at top level when followed by a string, so `let test =`
  stays legal. Benches parse and are skipped (`glide bench` later).
- **Known lexer limitation**: nested tuple access `x.0.1` lexes
  `0.1` as a float. Rust special-cases this; we will when it matters.
  Same bucket: string literals inside interpolation are unsupported.

## Deliberately absent (after M2)

Named-field variants and dot-shorthand, `distinct` types, static
generics (parsed, ignored), trait *checking* (impl blocks register
methods; conformance is asserted, not verified), concurrency,
`defer`/`errdefer`, `or |e|` blocks, named arguments, parameter
defaults, `break`/`continue`, method values as closures (`x.method`
unapplied), `else if let`, struct patterns in `match`.
