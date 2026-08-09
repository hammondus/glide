# Interpreter implementation decisions

Language decisions live in `../DESIGN.md`. This file records how the
*interpreter* is built, and which corners are deliberately cut.

Note the change of footing as of M4: the interpreter is no longer
"bootstrap step 1, retires later". It is a shipping tier — a
statically-checked scripting language — sharing one frontend with the
compiler. "The real compiler makes it obsolete" is no longer available
as a justification for cutting a corner; a corner is either a stated
tier difference (speed, and a binary that runs with nothing installed)
or it is a bug.

## Milestones

1. **M1 (done): run `wordfreq`** (GRAMMAR.md program 1) — the whole
   expression language, zero user-defined types.
2. **M2 (done): run program 3 (Tree)** — `type` (structs + simple
   sum types), `match` with guards, `impl` (inherent + trait),
   `mut self` with call-path checks, `if let`, generators
   (`yield` / `yield from`), `test` blocks with property-based
   generation and shrinking, `expect` with both-sides reporting.
3. **M3 (done): run program 2 (HTTP + SQL)** — named-field variants,
   dot-shorthand (`.NotFound(id)`), `distinct` types, named
   arguments, `defer`, structured concurrency (scope/spawn/join,
   cancellation, channels, select, time), http/sql/json host shims.
   `examples/notes.gld` is program 2 adapted to the ratified
   language (no or-blocks, no derive — the dynamic shims stand in
   for `derive Json`/`Row` until the comptime era). One deviation
   recorded: `derive` itself is comptime-era work, not M3.
4. **M4 (in progress): the checker.** Annotations stop being
   documentation. Three landings:
   - **M4a — representation (done).** No checking yet: a real
     `ast.TypeExpr` replacing the annotation strings, generic parameter
     lists parsed instead of discarded, byte-offset spans on every token
     and AST node with caret-rendered diagnostics, and `load()` factored
     into `internal/program` — the declaration table the checker and the
     evaluator share.
   - **M4b — the checker core.** Bidirectional local checking,
     expected-type propagation, boxed `Option`, sized numerics in the
     runtime, static versions of the rules currently enforced
     dynamically, and `?` conversion resolved from real types.
   - **M4c — generics and traits.** Declaration-site bound checking,
     trait structural conformance. The interpreter runs generics
     type-erased; monomorphisation is the compiled tier's problem.

## Decisions

- **Statically checked, and the checker is written here in Go.**
  *Reverses the original M1 decision*, which was: "type annotations are
  parsed, kept as strings, and ignored; the checker arrives with the
  Glide-written frontend; writing a static checker in Go now would be
  thrown-away work." Both halves of that turned out to be wrong.

  The "thrown-away" half depended on the interpreter retiring at
  self-hosting. It does not — it ships (`../DESIGN.md`, Implementation
  path). Work that lands in a shipping tier is not thrown away, which
  removes the entire basis of the deferral.

  The sequencing half inverted its own dependency. The bootstrap
  justified a Glide-written frontend on the grounds that "compilers are
  the best-case workload for this feature set — ASTs are sum types, a
  checker is exhaustive matching". True, and none of it was *available*:
  exhaustiveness was dynamic, generics never reached the AST,
  `Option<Option<T>>` was unrepresentable, trait conformance was
  asserted rather than verified. That plan wrote the largest and most
  type-dense Glide program that will ever exist in the one tier that
  checks none of it.

  What actually survives into Glide is not the Go code — it is the
  design (type representation, bidirectional rules, expected-type
  propagation, diagnostic wording) and the conformance corpus. Those are
  the expensive part, they are cheaper to get right in a checked
  language against an existing test suite, and having them turns the
  Glide frontend into a transcription instead of a rediscovery.

  Accepted cost: the Go frontend gets written once and ported once.
- **The conformance corpus is a first-class artifact**, not Go table
  tests: `testdata/conformance/**.gld`, each program carrying its
  expected verdict and, for rejections, the exact diagnostic. Every
  implementation must pass it unchanged — this Go checker, the Glide
  port, and both backends. It is the device that makes "no drift between
  tiers" checkable rather than merely intended, and it is why it gets
  written alongside the first checker commit rather than afterwards.
- **Types attach as side tables, not a second tree** (`go/types.Info`
  shape): the checker annotates AST nodes rather than lowering to its
  own IR. A separate typed tree would double the node definitions and
  the eventual port cost, and buy nothing — the first backend emits Go,
  so Go's compiler does the optimising an IR would exist to enable. It
  also means the evaluator keeps walking the AST, so M4a and M4b need no
  rewrite of `interp`. Revisit only at step 5 (own codegen), where a
  real IR earns its place.
- **The declaration table is one implementation, in `internal/program`.**
  "Is `Red` a variant, and of what type?" and "does `Tree` have a method
  `insert`?" have to get the same answer in both tiers, and the only
  reliable way to guarantee that is one piece of code rather than two
  that are meant to agree. `program.Load` indexes imports, fns, types,
  variants, traits, methods and consts, and enforces the rules that need
  no type information: nothing declared twice, no shadowing a builtin,
  no impl for an unknown type, no unknown import.

  What the host provides is passed in as `program.Known` (builtin names,
  importable modules) rather than reached for, so the checker is handed
  the same set the evaluator uses — a program that redeclares `println`
  must fail identically in both tiers or they have already drifted.

  Const *evaluation* deliberately stays in the interpreter: it produces
  values, not declarations. The table records that a const exists and
  where; the evaluator decides what it is worth.

  Unlike the parser, this pass reports **every** collision and returns a
  usable table alongside the error, because that is what the checker and
  an editor both need.
- **Positions are byte offsets; line and column are rendering.** Every
  token and every AST node carries a `source.Span` (half-open byte
  range); `source.File` turns an offset into line:col and a caret
  snippet when a diagnostic is actually printed. M1–M3 carried a bare
  `Line int` on about half the nodes and *nothing* on the rest — Param,
  FieldDecl, Closure, every literal and every pattern — which put a
  hard floor under diagnostic quality: "line 42" cannot point at one
  field of three on that line. Columns count runes, not bytes, so a
  multi-byte character earlier in the line doesn't shift the caret; the
  caret's run-up is copied from the source so tabs stay aligned.
- **Runtime errors carry spans too, not line numbers.** `rtErr` holds a
  `source.Span` and renders through the same `source.File` as a parse
  error, so both look identical and a checker diagnostic can never
  disagree with a runtime one about where something is. This is a
  two-tier consequence, not a cosmetic one: `glide run` is a shipping
  tier, and its errors are the ones users see.
- **Interpolation segments are lexed in file coordinates.** `LexAt`
  takes the segment's byte offset, so a diagnostic inside `"{x + 1}"`
  points into the string rather than at the string. The alternative —
  lexing the snippet standalone and rebasing line numbers — is what
  M1–M3 did, and it could never produce a column at all.
- **The parser stays fail-fast; the *checker* is what needs to
  continue.** `source.Bag` exists and collects, but the parser reports
  one error and stops. Parser error recovery is a large, famously
  low-yield feature: resynchronising a recursive-descent parser after a
  syntax error reliably produces one true diagnostic followed by
  several phantoms, and the phantoms train you to ignore the list. The
  requirement that motivated a collector — DESIGN.md's typed holes,
  where checking must continue past an error — belongs to the checker,
  which is new code in M4b and gets built multi-error from the start.
  Revisit only if editing real Glide shows one-error-per-parse actually
  costs something.
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

- **First (and only) third-party dependency: `modernc.org/sqlite`.**
  Chosen over mattn/go-sqlite3 because it is pure Go: no CGO, so
  cross-compilation stays `GOOS=… go build` and no C toolchain ever
  enters the build. Cost accepted: a large transitive module tree (a
  machine-translated SQLite). The alternative of a fake in-memory
  store would dogfood nothing real. Everything else remains stdlib.
- **json/sql shims do dynamically what derive will do statically.**
  `json.encode` walks values structurally; `db.query` returns rows
  as column→value Maps; `:name` placeholders are verified at call
  time (missing AND unused names are errors). None of this survives
  into the compiled tier — `derive Json`/`derive Row` generate the
  typed versions and the comptime check moves the placeholder
  verification to compile time. The shim exists to prove the
  *surfaces*, not the mechanism.
- **distinct in M2**: `DistinctV{Type, V}` — construction checks the
  base type name dynamically; operators simply don't match in binop
  (no code needed — falling through IS the semantics); `.value()`
  is the built-in unwrap; codecs (json encode, sql bind) unwrap
  transparently, because a codec's conversion is the explicit kind.
  Not yet a map key (needs hashable boxing; add when a program
  wants it).
- **Route patterns and JSON literals are raw strings** — discovered
  the hour the http shim landed: `"/notes/{id}"` interpolates `id`.
  `` `/notes/{id}` `` is the idiom, and it is exactly what raw
  strings were ratified for. Documented in stdlib.md.

## Scheduled for M4

Static generics (M4a parses type parameters and bounds into the AST;
nothing checks them yet), trait *checking* (impl
blocks register methods; conformance is asserted, not verified), boxed
`Option`, sized numerics, `.Shorthand` resolved in the expected type
rather than a global namespace, `Timeout` as a real type, static match
exhaustiveness, receiver-mut on builtin methods, the spawn-captures-mut
ban, integer literal range checking. Each has its own entry above or in
`../docs/reference/language.md`; they are listed together here because
they are one body of work, not a scattering of small ones.

## Deliberately absent (after M4)

`Mutex<T>` (stdlib-era; ownership-transfer culture first), `derive`
(comptime era — the json/sql shims stand in), typed json decode / typed
query rows (wait for derive), method values as closures (`x.method`
unapplied), `or |e|` blocks (declined — see DESIGN.md), time
formatting/parsing/calendars (the `time` module's own later design),
error-to-status middleware for http (the one default mapping — Err →
500 — until middleware is designed).
