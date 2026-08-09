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
  The `Next` closure must keep its `IterV` reachable
  (`runtime.KeepAlive`): `iterate()` hands the bare func around
  (`yield from`, `for … in`), and when only the func was live the
  cleanup fired mid-loop and silently truncated the stream at a
  GC-chosen point — seen as the tree property test failing on a
  *prefix* of the sorted list, nondeterministically despite the fixed
  seed.
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
- **Lexer diagnostics err at the first impossible character, never
  guess-and-recover**: after a format spec's `:`, `{` and `"` can
  never be legal (a spec is just a width), so the lexer errors right
  there with the column and a "missing '}'?" hint. Without this, a
  dropped `}` swallowed the following interpolations as spec text and
  the intended closing quote opened a phantom nested string — the
  reported error ("unterminated string") pointed at the wrong
  construct entirely. Lexer errors carry line:column, and the
  unterminated-string/interpolation errors name the column where the
  construct opened; strings are single-line, so a bare line number
  cannot distinguish which of several quotes on the line is at fault.

- **One interpreter lock (GIL); blocking operations release it.**
  Spawned tasks run on goroutines, but exactly one interprets at a
  time: the evaluator's own structures (env maps, MapV, genCache)
  stay race-free without per-structure locking, and — the real point
  — tasks interleave *exactly at blocking operations*, which is the
  ratified cancellation-point rule. The lock is the semantics, not
  just a guard. Release backends get true parallelism; programs
  can't tell except in throughput. Cost: two compute-bound tasks
  serialize in the interpreter (semantics permit any scheduling).
  Generator handoffs release the lock on both ends, so a generator
  inside a task can't wedge the interpreter; each goroutine's
  cancellation context (`in.cur`) is lock-protected state, saved and
  restored around every release.
- **Cancellation is a Go panic (`cancelUnwind`), not a `sig`.**
  Panics already unwind through every construct uncatchably, and
  `evalBlockDeferred`'s panic path already runs `defer` AND
  `errdefer` on the way out — which is exactly the ratified
  cancellation behavior. A `sig` variant would have needed a "cannot
  be consumed by match/loops/user code" rule bolted onto every
  consumer. Only scope machinery (and the task/generator wrappers)
  recovers it.
- **Scope exit protocol**: body result captured (value, signal, or
  panic) → early exit cancels → drain-join children until the task
  list is stable (children may spawn siblings while we wait) → then
  precedence: body's own bug > child bug > outer cancellation >
  body's signal > first unjoined child `Err` (via the same
  conversion path `?` uses) > body value.

- **`Timeout` is a synthetic variant, not a declared type.** A
  timed-out scope's Err payload is `&VariantV{Type: "Timeout",
  Name: "Timeout"}` — so `Err(Timeout)` matches the bare pattern
  `Timeout`, renders as `Timeout`, and converts through a user's
  `fn from(t: Timeout)` — without any global type declaration the
  program didn't write. The checker era makes `Timeout` a real
  stdlib type; nothing about programs changes.
- **`select` rides `reflect.Select`.** Dynamic arity plus
  uniform-random-among-ready for free — it IS Go's select. Recv
  arms sharing a channel share one case (the delivered value tries
  their patterns in order); send arms never merge (each sends its
  own value). The whole wait happens with the GIL released, with
  the task's cancellation channel as an extra case.

## Deliberately absent (after M3 concurrency)

`distinct` types, static generics (parsed, ignored), trait
*checking* (impl blocks register methods; conformance is asserted,
not verified), `Mutex<T>` (stdlib-era; ownership-transfer culture
first), http/sql/json host shims (the rest of M3), `derive`, method
values as closures (`x.method` unapplied), `or |e|` blocks
(declined — see DESIGN.md), time formatting/parsing/calendars
(the `time` module's own later design).
