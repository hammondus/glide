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
   - **M4b — the checker core (done, with two gaps named below).**
     `internal/types` (the semantic type universe) and `internal/check`
     (bidirectional local checking with expected-type propagation),
     mandatory in both tiers, plus the conformance corpus. Static
     versions of the call/field/method/operator/mutation rules,
     `.Shorthand` resolved in the expected type, `Timeout` as a real
     type, integer literal range checking, and `?` conversion resolved
     from real types — retiring `resultErrType`'s string slicing.
     Deferred out of M4b: **boxing `Option`** and **sized numerics in
     the runtime**.
   - **M4c — generics, traits, and the numeric floor.** Landed in
     order: sized numerics with a runtime representation and
     trap-everywhere overflow; explicit numeric conversion (`u8(n)`);
     then bound checking and trait conformance, with `Self` as a real
     type and a two-trait universe. Still to come: static match
     exhaustiveness, boxed `Option`, generator element types, the
     spawn-captures-mut ban, and operator traits (which is what
     `a < b` on a `T: Ord` waits for). The interpreter runs generics
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

- **The checker reports only when it is certain.** Everything it
  cannot yet model has type `Unknown`, which is compatible with
  everything in both directions and never produces a diagnostic.
  A type parameter is treated the same way (`types.IsOpaque`): inside
  `fn compare<T: Ord>(a: T, b: T)`, `a < b` is accepted in silence
  because bound checking is M4c and rejecting it would be a guess.

  This is the load-bearing decision of M4b, and it is what makes a
  *mandatory* checker safe to land half-finished. An
  under-approximation of errors gets better every commit and never
  breaks a working program; an over-approximation is a language that
  rejects code that runs. TypeScript's internal `any` and go/types'
  `Invalid` are the same device. `TestUnknownNeverReports` and
  `TestExamplesCheckClean` are the guards.

- **The checker is mandatory in both tiers; there is no `--no-check`.**
  `glide run`, `glide test` and the eventual compiler all check first.
  `glide check` exists as a go-vet-shaped convenience — report and
  stop — never as a way to skip. An interpreter that can run unchecked
  Glide makes unchecked Glide the real scripting dialect, and the
  annotations rot exactly the way they did in M1–M3.

- **Types attach to AST nodes, not to a lowered IR** (`check.Info`,
  `go/types.Info`-shaped: `Types`, `Shorthand`, `Funcs`). A second tree
  would double the node definitions and double the eventual port to
  Glide, and Glide's first backend emits Go — whose own compiler does
  the optimising, so there is nothing an intermediate representation
  would buy. The evaluator keeps walking the AST unchanged.

- **The host surface has one source of truth: the checker's tables.**
  `check.Host()` derives the reserved names and importable modules from
  the same tables that give them types, and `interp.hostKnows()`
  delegates to it. `TestHostSurfaceMatchesRuntime` asserts the typed
  set and the implemented set are equal, so a builtin cannot exist in
  one tier's head and not the other's.

- **`Error` is the erased error type: anything propagates into it.**
  `fn run() -> Result<T, Error>` accepts an `Err` of any type, with no
  `from`. Named error types still convert through `E.from` as designed;
  `Error` is the one target that needs no conversion. This is Rust's
  `Box<dyn Error>` bargain, and it is what the runtime already did.
  Without it, every application-level function would need a
  hand-written `from` for every error type any callee might raise —
  precisely the ceremony `?` exists to delete.

- **Inference is one function of unification, bounded by a single
  call.** `unify` binds a signature's type parameters from the
  arguments actually passed, and that is the whole solver: no
  cross-function unification, no occurs check, no constraint store.
  Mandatory signatures are what buys this (LINEAGE.md). A type
  parameter the call site could not bind is *erased* to `Unknown`
  rather than leaked — `Tree.new()` outside `impl Tree<T>` is "a Tree
  of something", not a free variable that then fails to equal
  anything. Call-site inference proper (`Tree.new()` learning `Int`
  from a later `t.insert(1)`) is M4c.

- **Integer literals are magnitudes, and the type they land in
  decides what they become.** `lexer.Token.Num` is a `uint64` holding
  a literal's magnitude; a leading `-` is a separate token, so the
  lexer never sees a sign. That is the only representation in which
  both ends of the range are expressible — i64's minimum is 2^63,
  which no int64 holds, and u64's maximum is 2^64-1, which no int64
  holds either. `check.Info` records the type each untyped constant
  settled into, and the evaluator builds the value from it.

  This closed three silent-wrong-answer bugs that predate M4:
  1. `strconv.ParseInt`'s error was **discarded**, and ParseInt clamps.
     Every literal above 2^63-1 silently became 9223372036854775807 and
     the program ran — including `let a: u64 = 18446744073709551615`,
     which the checker already accepted as a type.
  2. `-9223372036854775808` printed `-9223372036854775807`. i64's
     minimum was unwritable, because the magnitude was clamped before
     the negation could reach it. `-<literal>` is now folded as one
     constant rather than evaluated as a negation of a value.
  3. `let f: Float = 5` built an *integer*, so `f / 2` did integer
     division and answered `2`. An untyped constant now settles into
     the type it landed in, including through a binary operator: in
     `f / 2` the 2 adopts Float, and in `big - 1` the 1 adopts u64.

  `UintV uint64` exists solely for u64 — the one integer type whose
  range an i64 cannot hold. A u64 never mixes with an Int: DESIGN.md
  forbids implicit numeric conversion, the checker enforces it, and
  `binop` has no case for the pair.

  Remaining gap: magnitudes are uint64, so a constant *expression* that
  exceeds 64 bits part-way (`1 << 100`, which DESIGN.md says is fine in
  constant math) still cannot be evaluated. That wants real
  arbitrary-precision constants and arrives with comptime. The range
  *check* is now exact for every type the language has.

- **The six narrow widths carry their own size on the value, not on
  the checker's annotation.** `SizedV{Bits, Signed, V int64}` is one
  carrier for i8/i16/i32/u8/u16/u32.

  The tempting cheaper design was to leave them in an `IntV` and read
  the width from `check.Info` at the operator node — the seam already
  exists, and it is what fixed the Float-division bug. It is wrong,
  and the counter-example is one line: inside
  `fn double<T>(v: T) -> T { v + v }` the interpreter runs generics
  type-erased, so by the time `+` executes there is no static type to
  consult. A `u8` passed in would silently not trap. The width has to
  survive erasure, which means it has to be on the value.

  One carrier rather than six Go types (`U8V`, `I8V`, …) because six
  would mean six more cases in `typeName`, `render`, `eq`, `hashable`,
  `binop`, `naturalLess`, the json encoder and the sql binder — the
  same switch written six times. `Int` and `u64` keep their own
  unboxed types: `Int` is the default and hot, `u64` is the one width
  an int64 cannot hold, and the narrow six are exactly the ones with
  something to remember. `V` is kept sign- or zero-extended so it is
  always the true magnitude, which makes `%d` correct for both signs
  and makes the carrier comparable — so map keys and `==` need no
  extra cases at all.

  Arithmetic is exact before the range check: operands are at most 32
  bits, so signed products fit an int64 (2^31 · 2^31 < 2^63) and
  unsigned products fit a uint64 ((2^32-1)^2 < 2^64). Nothing has to
  be done in two halves.

- **Overflow traps in every tier, and `wrapping_*` is the escape.**
  This reverses `../DESIGN.md`'s original "trap in dev, wrap in
  release" — see that file for the language-level argument. What it
  means here: the interpreter is no longer "the dev tier that traps",
  it is simply the tier that computes the same answer as the other
  one. The overflow message names the operator that would have
  succeeded (`use wrapping_add for modular arithmetic`) rather than
  the build mode that would have wrapped, because the second was
  advice to change how you build in order to get a wrong answer
  quietly.

  `wrapping_add`, `wrapping_sub`, `wrapping_mul` and `wrapping_neg`
  live in one table on each side — `check.intMethods` with `Self`
  bound to the receiver, and `interp.intMethod` — so a width cannot
  acquire a method in one tier and not the other. That also fixed a
  smaller gap: `cmp` was `Int`-only, so a `u64` could not answer it.

- **A primitive type name is a value, so calling it is a conversion.**
  `u8(n)` resolves through the same `*types.Meta` callee that already
  built `distinct` types (`NoteId(7)`), so no new call form was
  invented — a type in expression position was already callable, and
  this widens *which* types.

  Three consequences worth stating:

  1. **Primitive names are now reserved.** `fn u8()` would otherwise
     win the name lookup and the conversion would silently vanish.
     This also closed a pre-existing hole: `type Int = struct { … }`
     was accepted and then unreachable, because `resolve.go` tries the
     primitives before program declarations. `program.Load` now checks
     the reserved set for types and consts, not only functions.
  2. **Locals still shadow**, matching Go's universe block. `let u8 =
     5` then `u8(3)` fails with "Int is not callable" — loud, local,
     and not a silent reinterpretation.
  3. **Bool and String resolve too**, even though they are not
     convertible, so `String(65)` reports what is actually wrong with
     it rather than "undefined name \"String\"".

  The conversion itself *infers* its argument rather than checking it
  against the target — the whole point is that the source type
  differs — and pushes only the range check inward, so `u8(300)` is a
  compile error while `u8(n)` for an out-of-range `n` traps at
  runtime.

- **`TestHostSurfaceMatchesRuntime` grew a second half rather than a
  loosening.** The reserved set now holds two kinds of name with two
  different implementations: callable builtins in the evaluator's
  `builtins` map, and predeclared type names resolved by `evalIdent`.
  Collapsing them into one membership assertion would let a name be
  reserved by the checker and undefined at runtime, which is the exact
  drift the test exists to catch, so the primitives are asserted end
  to end instead — every numeric primitive is *run* as a conversion,
  and `Bool`/`String` are asserted not to be.

- **The universe traits are written in Glide, not built in Go.**
  `internal/check/prelude.go` holds ~6 lines of Glide source, parsed
  once. They *are* Glide declarations, so spelling them as Glide is
  the auditable form: the whole definition is in one place, there is
  no second half hidden in a Go table, and the parser is exercised by
  the same source every program is. They reach `program.Load` through
  `Known.Traits`, like every other thing the host provides, so both
  tiers index one set.

  Admission rule, and it is strict: **a universe trait names machinery
  that already runs.** `Ord` qualifies — `Int` and `String` both have
  `cmp` in the method tables. `Iterable` qualifies — the evaluator's
  `iterate` already says "anything with an `iter()` method". `Hash`
  and `Display` do not, and stay out; a trait whose required method the
  evaluator cannot execute is `TestHostSurfaceMatchesRuntime`'s drift
  in a different costume.

- **Conformance is checked off the impl blocks, not the TypeTraits
  index.** A generic trait's arguments live on the header — `impl
  Iterable<Int> for Bag` is the only place `Int` is written — so
  driving the pass off `Table.TypeTraits` (which records only trait
  names) cannot bind them. Caught by the corpus: `tree.gld`'s `impl
  Iterable<T> for Tree<T>` passed with the naive version *by
  coincidence*, because its `T` and the trait's `T` happen to share a
  name.

- **`unify` binds a type parameter to another type parameter.** It
  previously skipped any actual that was `IsOpaque`, which covers both
  Unknown and a `*types.Var`. Unknown must indeed not bind — it would
  poison the parameter for every later use — but a type *parameter*
  must: `outer<T: Ord>` calling `inner<U: Ord>(a)` binds `U := T`,
  which is how generic code composes and how the bound on `U` gets
  something to check against. Before the fix, passing an unbounded `T`
  where an `Ord` was required went unnoticed.

- **The method-lookup guard tests `IsUnknown`, not `IsOpaque`.** This
  is the whole of "a bound is the complete method set", in one line.
  `methodOf` returns `modelled=false` for an unbounded parameter and
  `modelled=true` for a bounded one, so `modelled` already carries the
  distinction; keeping `IsOpaque` in the guard would silence exactly
  the receivers bounds exist to make checkable. Operators still use
  `IsOpaque`, which is why `a < b` on a `T: Ord` stays quiet — that
  needs operator traits, deliberately deferred.

- **A trait's default body is checked against `Self: ThisTrait`.**
  Exactly the generality a default has: it may call the trait's own
  methods and nothing else. Skipped entirely before M4c, for want of
  a type variable to check it against.

- **A required (body-less) trait method is no longer inherited.**
  `declareFns` used to copy *every* trait method into the implementing
  type's method set, defaults and requirements alike, which is what let
  `impl Greet for Foo {}` resolve a `hello` that was never written —
  the call type-checked and died at runtime. Only bodied methods are
  inherited now; a requirement is a demand on the type, not a method
  it acquires.

- **A tuple literal's expected element types win, as a list
  literal's already did.** `checkExpr` pushed the expectation into
  each slot and then rebuilt the tuple's type from
  `types.Default(got)`, so `(250, -300)` at `(u8, i16)` reported
  "expected (u8, i16), found (Int, Int)" — every element had just
  satisfied the expectation individually. Pre-existing since M4b and
  not specific to widths: `let t: (Int, Float) = (1, 2)` failed the
  same way.

- **The checker closed two holes the dynamic tier had papered over.**
  Both are language changes, both make an existing annotation mean
  what `DESIGN.md` already said it meant, and both changed a test that
  asserted the old behaviour:
  1. **Propagating an unconvertible error is now an error.** M1–M3 let
     `?` push a `String` error straight into a `Result<Int, ApiError>`
     when `ApiError` had no `from` — so the declared error type was
     decoration. Now it is a compile error; declare the `from`, or
     declare `Result<_, Error>`, which accepts anything by design.
  2. **Comparing sibling `distinct` types is now an error.** `NoteId(1)
     == UserId(1)` answered `false` in M1–M3, because `==` was
     structural and the two values simply never matched. That made the
     exact mistake the wrapper exists to prevent produce a plausible
     answer. `distinct` means "a loud error on mixing", and silence
     that evaluates to `false` is the opposite of loud.

  A third case is a *capability* loss rather than a bug fix, and is
  recorded as such: a heterogeneous literal like `[1, true, None]` has
  no element type and no longer compiles. It only ever worked because
  the runtime was dynamic. Mixed-shape JSON comes back with typed
  encoding (`derive Json`); until then `json.encode` takes one shape
  at a time. `modules_test.go` was rewritten accordingly.

- **The conformance corpus is a first-class artifact**
  (`testdata/conformance/`), not a Go table test that happens to live
  in files. Each `.gld` file is a whole program; a `// error: <text>`
  comment on a line asserts exactly one diagnostic there containing
  that text, and any unexpected diagnostic fails. Every implementation
  of the frontend — this Go checker, the Glide port, both backends —
  must pass it unchanged. It exists from M4b's first commit rather
  than being written afterwards, because its whole job is to be older
  than the thing it constrains. Accept-cases matter as much as
  reject-cases: most checker bugs are false positives.

- **128-bit integers are deferred past M4** (resolving DESIGN.md's open
  question). `big.Int` would put an allocation and a lie in the Value
  representation — a `u128` that does not wrap is not a machine
  integer — and a hi/lo pair means hand-writing division, modulo,
  shifts and formatting for a type no program currently uses. Deferring
  does not foreclose: adding `i128`/`u128` later is a new Value case
  and a new `types.Basic`, not a change to any existing one. The one
  thing that had to be got right now was not baking "every integer is
  an int64" into the *type* representation, and `types.Basic` carries
  an explicit width for exactly that reason.

## Remaining in M4c

Sized numerics, explicit conversion, bound checking and trait
conformance are **done** (see the decisions above). Still open, in the
order they are worth doing:

1. **Operator traits.** The half of `Ord` that bound checking
   deliberately did not deliver: `a < b` on a `T: Ord` still passes in
   silence, because operators consult `IsOpaque` where methods now
   consult `modelled`. This is why `examples/tree.gld` — the flagship
   generic in the repo, whose `insert_node<T: Ord>` compares with
   `<` — is checked everywhere except the line that motivates its
   bound. It was split out because it carries four unsettled language
   decisions of its own: does `Ord` drive all of `< <= > >=`; does
   `==` go through `Eq` or stay structural (it is structural today);
   do `Add`/`Mul` exist and does `+` on a user type dispatch through
   them; is `Ord` derived for structs or hand-written. Those belong in
   their own commit where they are visible.
2. **Boxed `Option`.** A key present in a `Map<K, V?>` holding `None`
   is indistinguishable from an absent key — real data loss, and the
   only remaining *wrong answer* rather than a missing diagnostic. The
   most invasive change left: runtime representation as well as
   checker.
3. The cheaper leftovers: static match exhaustiveness (the runtime
   catches it, and its message still says "exhaustiveness checking
   arrives with the compiler" — now the checker's job), the
   spawn-captures-mut ban, generator element types (a `yield`ing
   function is exempt from the tail-value check, so `yield "s"` in an
   `Iterator<Int>` passes), and call-site inference for *nullary*
   associated functions (`Box.new()` erases its parameter, so a later
   `add(1)` then `add("s")` passes; `Box.new(1)` already infers).

**Two lexer gaps found while testing the conversions**, both small
and both out of scope for the commit that found them: there are no
hex integer literals (`0xFF`), even though `{n:hex}` *formatting*
exists, and no float exponent notation (`1.0e30`). Neither blocks
anything today — `Rune(128512)` works where `Rune(0x1F600)` does
not — but the first is conspicuous the moment anyone writes
byte-level code, which is exactly what the sized types are for.

## Deliberately absent (after M4)

`Mutex<T>` (stdlib-era; ownership-transfer culture first), `derive`
(comptime era — the json/sql shims stand in), typed json decode / typed
query rows (wait for derive), method values as closures (`x.method`
unapplied), `or |e|` blocks (declined — see DESIGN.md), time
formatting/parsing/calendars (the `time` module's own later design),
error-to-status middleware for http (the one default mapping — Err →
500 — until middleware is designed).
