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
- **Option is boxed** (M4c). *Reverses the M1 decision*, which was:
  a `T?` is "the value, or None", `Some` is the identity, and
  `Some(p)` matches any non-None value. That was forced at the time —
  the language has implicit `T -> T?` coercion, and without static
  types the interpreter could not see the wrap points, so unboxing
  made the coercion a no-op.

  The checker removed the constraint, and the cost of keeping it was
  three *silent wrong answers*, not three missing diagnostics:

  1. A key present in a `Map<K, V?>` holding `None` was
     indistinguishable from an absent key. Data loss.
  2. A `None` **sent** over a channel read as end-of-stream, ending
     the loop early — recorded as a wart in stdlib.md for two
     milestones.
  3. `Option<Option<T>>` was unwritable, so any generic over `T?`
     silently collapsed a level.

  What boxing costs is that the coercion is no longer free. The
  checker records each site in `Info.Wrap` and the evaluator wraps
  there — see the two entries below for the chokepoint and the
  assertion that makes a missed site loud rather than silent.

  Not taken: removing the implicit coercion, which would have made
  the representation canonical by construction with no sites to
  record at all. `DESIGN.md` names that coercion as one of the two
  features justifying bidirectional checking, and `let x: Int? =
  Some(5)` everywhere is a real ergonomic loss to pay for
  implementation convenience.

- **The coercion has one chokepoint on each side.** In the checker,
  `checkExpr` is the only place `AssignableTo` decides a `T` may
  become a `T?`, so `noteWrap` records there. In the evaluator, `eval`
  wraps `evalRaw`'s result once, so every expression is considered.
  The alternative — wrapping at each of the dozen syntactic sites
  where the coercion is legal (let, argument, return, struct field,
  list element, map value, tuple slot, …) — is a list that can be
  incomplete, and an incomplete list is a silent wrong value.

  One gap had to be closed explicitly: `checkExpr` returns early when
  `hasVar(want)`, without asserting. The coercion is still recorded
  there, because whether the target is an Option is knowable without
  knowing what it is an Option *of*. Missing that left `left:
  insert_node(…)` at a `Node<T>?` field unwrapped, and tree.gld
  caught it.

- **Every Option consumer asserts canonical form.** `readOption`
  panics if a value reaching an Option context is neither `NoneV` nor
  `*SomeV`. That is what makes the coercion machinery safe to trust: a
  site the checker fails to record fails loudly in the test suite
  instead of silently deciding a value was `Some` when the program
  meant `None`. It is also DESIGN.md's standing answer for a rule
  enforced in two places — the runtime keeps its copy as an
  assertion, so a checker bug is a panic rather than undefined
  behaviour.
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

- **Corpus coverage is measured against the checker's own source.**
  `coverage_test.go` reads every `c.errf` format string out of
  `internal/check/*.go` and every `bag.Add` out of
  `internal/program/program.go`, runs every corpus program, and
  asserts each diagnostic is actually produced by one. Adding a
  diagnostic without a case fails the test.

  Reading the source rather than keeping a list is the whole point: a
  hand-maintained inventory of diagnostics is a second thing to keep
  in step with the first, and it would rot the way stale reference
  docs do. The corpus went from 44/80 checker diagnostics covered to
  91/91 across both stages, and from 31 files to 37.

  Two diagnostics are `exempt` with reasons — genuinely unreachable
  from Glide source, reached only if the checker itself is wrong.
  Writing a case for a third is how the checker's dead
  `yield`-with-no-value branch was found: the parser requires an
  expression after `yield`, so `st.E == nil` could not arise. Deleted.

  The parser's own 46 diagnostics are *not* measured, and the reason
  is recorded rather than hidden: a parse error aborts at the first
  one, so each needs its own file, and 46 one-line files would cost
  more to read than they buy.

- **The spawn-captures-mut rule is enforced at the call, not in the
  closure.** `dotCall` arms a `spawned` map before checking a
  `s.spawn(…)` argument, and `ident` records any name whose lookup
  crossed a function boundary to reach a `mut` binding. Doing it the
  other way — flagging inside `closure()` — would need the closure to
  know why it was being checked, which it has no business knowing.

  `lookup` split into `lookupCrossing`, which reports whether the
  binding was found past a `fnBound` scope. That distinction was
  already in the loop; it just was not returned.

  The rule cost 2 of 43 existing spawn sites and both were worth it:
  `examples/notes.gld` froze a router that was `mut` only for setup,
  and `TestScopeWaitsForChildren` had a child pushing to a captured
  `mut` list — a genuine race that passed only because the interpreter
  holds one lock. It now sends over a channel, which is the doctrinal
  answer and tests the same rule.

- **A generator's yields are checked against its declared element
  type.** `c.yields` holds the T of the enclosing `Iterator<T>`, set
  in `fnBody` where the generator exemption from the tail-value rule
  already lived. `yield e` checks e against T; `yield from e` checks
  against `Iterator<T>`, because delegation takes an iterator rather
  than an element. A `yield`ing function declaring anything but an
  Iterator is an error — `yield` produces an iterator and saying
  otherwise is not a shape the language has.

- **The expected type seeds a call's type-parameter binding.**
  `checkArgs` bound parameters from arguments alone, so `Box.new()`
  had nothing to bind from. `let c: Box<Int> = Box.new()` *appeared*
  to work — erase turned T into Unknown, and the annotation won on the
  wildcard — which is why the element type went unchecked whenever no
  annotation was there to win. Threading `want` through `dotCall` into
  `checkArgsWanting` makes the binding real, and makes "nothing
  determines T here" a diagnosable condition rather than the default.

  `requireBound` walks the *return type* for free variables rather
  than iterating `sig.TypeParams`: an associated function takes its
  parameters from the impl header (`impl Box<T> { fn new() -> Box<T> }`),
  so its own list is empty and the T belongs to the Named. That is the
  same condition `erase` keys on, caught one step earlier.

- **`ast.TypeFunc` reuses `Elems` for parameters.** A written function
  type needed somewhere to put them, and `Elems` already meant "a list
  of types with no names" for tuples. A third slice would have been a
  field that is nil in every case but one. `Ret` is its own field and
  nil means unit, matching an `fn` declaration with no arrow.

- **A wrong closure body reports at the body, not at the call.** The
  closure checker already had the better message — "this closure must
  return Int, got String", pointing inside — but then returned the
  *actual* signature, so the caller's own assertion fired too and
  produced "expected fn(Int) -> Int, found fn(x: Int) -> String" as
  well. It now returns the expectation once it has reported, the same
  rule `checkExpr` uses. Two cascades of this exact shape were fixed
  in one sitting (the other being a conflicting parameter
  annotation), which suggests the rule is worth applying wherever a
  specific diagnostic sits inside a general one.

- **Closure parameters carry an `ast.Param`, not a bare name.** The
  node held `Params []string`, so there was nowhere for an annotation
  to live. Reusing `Param` rather than a parallel `[]*TypeExpr` keeps
  one shape for "a named thing that may have a type"; `Default` is
  simply never set, because a closure parameter cannot have one. The
  evaluator still wants names only, and maps them at closure
  construction — annotations are the checker's business.

- **Every closure had a zero span.** `parseClosure` built
  `&ast.Closure{}` and never set it, so *any* diagnostic pointing at a
  closure printed with no position: "expected fn(a: Int, b: Int) ->
  Int, found fn(a: Int) -> Int" with nothing to say where. Pre-existing
  since M4a and found only because annotating a closure gave the
  checker more to say about one. The span now runs from the opening
  `|` to the end of the body.

- **A conflicting annotation reports once and the expectation wins.**
  `xs.sort_by(|a: String, b| …)` first reported the annotation
  conflict *and* a signature mismatch at the call. Continuing with the
  expectation instead of the annotation is the same rule `checkExpr`
  uses when an assignment fails — return what was wanted, so one
  mistake is one diagnostic rather than a cascade through the body.

- **Exhaustiveness recurses, because top-level-only rejects real
  code.** The first version judged coverage at the top level only and
  rejected `examples/links.gld`:

      match r {
          Ok(resp)              => …
          Err(BadInput{ msg })  => …
          Err(NotFound{ code }) => …
          Err(Store{ cause })   => …
      }

  Those three Err arms cover Err *together* and none of them alone.
  So a case is covered when some arm matches it irrefutably, or when
  the sub-patterns collected under it are exhaustive over its payload
  type. Recursion is capped at four levels — a self-referential type
  would otherwise descend forever — and at the cap everything counts
  as covered.

  Everything the analysis cannot model counts as covered, in both
  directions: a multi-argument variant, a struct pattern with a
  refutable field, a literal arm set. That is the checker's standing
  rule applied here, and it is what makes the analysis safe to be an
  *error* rather than a warning.

- **A struct and a distinct type have exactly one case: themselves.**
  Treating them as un-enumerable (the obvious first reading, since
  neither has variants) demanded a `_` arm on
  `match u { User{ name, role, age } => … }` — a wildcard nothing
  could ever reach. `casesOf` deliberately does *not* go through
  `types.Base`, because a distinct type's cases are its own and not
  its base type's: `match id { NoteId(n) => … }` is total.

- **Exhaustiveness is skipped when the match already errored.** A
  typo'd constructor reports "Rd is not a constructor"; adding "and
  Red and Green are not handled" makes one mistake into two
  diagnostics, the second of which vanishes when the first is fixed.
  `match` records `bag.Len()` before its arms and only checks
  coverage if the count did not move.

- **The runtime's fall-through check stays, as an assertion.** Its
  message lost the "(exhaustiveness checking arrives with the
  compiler)" tail, which was wrong twice over — the checker does it,
  and it does it now. Reaching that panic means a match the *checker*
  could not judge, which is the belt to its braces, per DESIGN.md's
  open question on the dynamically-enforced rules.

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

- **`binop` became a method so ordering could reach `findMethod`.**
  Seven call sites, mechanically converted. The alternative was to
  intercept user-type comparisons at the two call sites that can see
  one and hope the other five never do; putting the dispatch inside
  `binop` means there is one place to read and no site that can be
  forgotten.

- **`sorted()` and `<` share one comparison path.** `in.less` tries
  the user `cmp` and falls back to `builtinCmp`; `binop`'s ordering
  branch calls the same `userCmp`. Two implementations that are
  *meant* to agree about every pair would disagree first about NaN and
  then about someone's reversed `cmp`, and the symptom is output in
  the wrong order with nothing to grep for.

- **`naturalLess` is now defined as `builtinCmp(...) < 0`** rather
  than a parallel switch, for the same reason at the builtin level.
  That is also what made `Float`'s total order a single edit instead
  of two that drift.

- **A primitive's empty method set is now *known*, not unmodelled.**
  `builtinMethod` returned `modelled=false` for `Float`, `Rune`,
  `Bool`, `()` and tuples, which meant conformance saw "cannot judge"
  and stayed silent — so `Bool` satisfied `Ord` vacuously, `T: Ord`
  accepted it, and the call died at runtime with `Bool has no method
  "cmp"`. The tables *are* the whole truth about a primitive, so
  silence there was a wrong answer rather than caution. `Unknown` and
  `Never` still report `false`, which is the only correct silence.

  `Float`/`F32`/`Rune` gained a real `cmp` in the same change
  (`ordMethods`), so their conformance is earned rather than vacuous.

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

Sized numerics, explicit conversion, bound checking, trait conformance,
the `Ord` operator trait, boxed `Option` and match exhaustiveness are
**done** (see the decisions above). Arithmetic operator traits (`Add`,
`Mul`) are not, and nothing yet needs them.

With boxing landed, **every remaining M4 gap is a missing diagnostic
rather than a wrong answer** — the checker stays quiet where it could
speak, but nothing computes the wrong value. Still open, in the
order they are worth doing:

Nothing. **M4 is done.**

The one deferred item is the arithmetic operator traits (`Add`,
`Mul`), which are designed and unbuilt because nothing yet needs
them — the same admission rule the universe traits use.

Next is bootstrap step 3 (`../DESIGN.md`): the compiler frontend
written in Glide, run on the interpreter. The conformance corpus was
widened first, since it is what proves that replacement faithful —
**every one of the 91 diagnostics the checker and the declaration
table can emit is now triggered by a corpus program**, asserted by
`internal/check/coverage_test.go` rather than assumed.

**Two lexer gaps found while testing the conversions**, both small
and both out of scope for the commit that found them: there are no
hex integer literals (`0xFF`), even though `{n:hex}` *formatting*
exists, and no float exponent notation (`1.0e30`). Neither blocks
anything today — `Rune(128512)` works where `Rune(0x1F600)` does
not — but the first is conspicuous the moment anyone writes
byte-level code, which is exactly what the sized types are for.

## The scripting surface: fs, os, process, and the collections

Written after M4 closed, before bootstrap step 3, because the honest
answer to "what is deferred that we could pull forward" turned out not
to be a *language* item at all. Every deferred language feature is a
comptime-era one that genuinely cannot come earlier. The stdlib was
the gap: `os` had two functions, `fs` had one, `Map` had two methods,
and a program could not write a file, read an environment variable,
delete a map key or ask whether a list contained something.

Decisions worth recording:

- **`fs.exists` and `fs.is_dir` return a bare `Bool`, not a Result.**
  Everything else fallible returns Result. These two do not, because a
  Result there is one the caller could only ever unwrap: the question
  *is* the answer, and an IO error reading a directory entry is
  indistinguishable to the caller from "no". Go's `os.Stat` returns
  both and every caller writes `err == nil`.
- **`os.env` returns `String?`.** Set-and-empty is a different state
  from unset — some tools use exactly that distinction — and `??`
  supplies a default in one place. Go's `Getenv`/`LookupEnv` split
  exists for the same reason; Glide only needs the second, because it
  has an Option type.
- **`fs.list_dir` sorts.** Go's `ReadDir` sorts, and the reason
  generalises: a program that iterates a directory must not have its
  output depend on the filesystem's whim. Same reasoning that made
  `Map`'s insertion order a *specified* language property.
- **`os.chdir` ships despite being process-global**, which is a real
  hazard under `spawn` — two tasks calling it interleave, and a third
  resolving a relative path sees whichever won. It ships because
  without it there is no way at all to control where `process.run`
  runs, and a documented sharp edge beats no tool. The single-threaded
  script this exists for cannot hit it.
- **`fs.join` takes a `List<String>`, not a variadic.** The language
  has no variadics, and inventing them for a path helper would be the
  tail wagging the dog. It moves to `path` when that module lands.
- **List's naming rule is now explicit**: a past participle returns a
  new list (`sorted`, `reversed`), a verb mutates (`push`, `pop`,
  `insert`, `remove`, `extend`, `sort_by`). The rule was implicit in
  `sorted`/`sort_by` and is worth stating before the set grows.
- **`pop`/`first`/`last` return `T?`; `remove(i)`/indexing trap.** The
  split is about who named the slot. An empty list is the *loop
  condition* of every worklist algorithm, so popping one is an answer.
  `xs.remove(5)` on a three-element list named a slot that is not
  there, which is a bug — and Glide already traps `xs[5]` for exactly
  that reason. No negative indices anywhere: `xs[-1]` meaning "last"
  turns an off-by-one into a silent read of the wrong end, and
  `last()` says it plainly.
- **`slice` copies.** Nothing in Glide aliases a list; a
  shared-storage view would make `mut` a lie about a second binding
  the reader cannot see.
- **`extend` copies its source first**, so `xs.extend(xs)` doubles the
  list instead of looping forever re-reading a slice it is growing.
- **`Map.contains_key`, not `contains`.** On a Map, `contains` is
  ambiguous about which half it means. On a List it is not.
- **`process.run`**: the three rules are in `../DESIGN.md` (exit codes
  are not errors, no shell, cancellation point) with their lineage in
  `../LINEAGE.md`. The implementation note is that `Output` is a new
  `types.Ctor`, which reserves the name `Output` program-wide — the
  same cost `Response` and `Request` already pay, and the alternative
  (a tuple, or a Map) throws away the typing that makes the checker
  useful here.

### The numeric surface: split by width

`abs`, `min`, `max`, `pow` are methods on every numeric type. `sqrt`,
`floor`, `ceil`, `round`, `trunc`, `is_nan`, `is_infinite`,
`is_finite`, and the constants `pi`/`e`/`inf`/`nan`, are the `math`
module. Landed straight after the scripting surface, on the "there is
no way to take an absolute value" reasoning.

**Built methods-only first, and that was half wrong.** The argument
for it was "consistent with `cmp` and `wrapping_*`" — which is weaker
than it looks: `cmp` is a trait requirement and *has* to be a method,
`wrapping_add` is an operator variant and operators are infix on the
value. Both are operator-shaped. `sqrt` is not, and neither is evidence
about general numeric functions. The second argument, "Go's `math.Abs`
is float-only, so modules are the problem", was simply wrong: that is a
type-system limitation, not a module-vs-method one.

**What survives is narrow and mechanical.** `abs`/`min`/`max`/`pow`
must serve nine numeric types. As methods the receiver binds `Self`
through machinery that already runs. As free functions there is no
receiver, so the checker has to infer from argument one and then unify
an *untyped literal* against a later argument — `math.min(5, x)` where
`x: u8` — which it does nowhere else. `x.min(5)` needs none of that:
the receiver types the literal through the ordinary bidirectional path.
Bespoke inference for one module, pinned by the corpus, reimplemented
in the Glide frontend, is a permanent tax for a spelling.

The rule, stated so it is learnable: **works at every width → method;
Float-only or a constant → `math`.** And explicitly not permanent —
when operator traits make a `Numeric` bound expressible, `math.abs<T:
Numeric>` types itself and the four move. Recorded in `../DESIGN.md`
with the lineage in `../LINEAGE.md`, so that move is a decision already
taken rather than a re-argument.

**Modules can now hold values, not just functions.** `moduleValues` in
the checker, `mathConstants` in the evaluator, one field-resolution
branch each. This is what a module can do that a method cannot: `pi`
has no receiver, so before this it had nowhere in the language to
exist. Kept as a table separate from `modules` rather than a sum type,
because "is this name a function or a value" is the question both the
field path and the call path need, and one map each answers it by
lookup instead of by type switch.

Consequences worth stating:

- **`abs` is signed-only.** On an unsigned type it would be the
  identity, and writing it reads like a sign was handled when there
  was never a sign. The checker says so by name rather than reporting
  a missing method.
- **`abs` traps at a type's minimum**, which has no positive
  counterpart — the same rule as `-x`, and for the same reason.
- **`pow`'s exponent is an `Int` at every receiver width.** It counts
  multiplications; it is not a value of the receiver's type. A `u8`
  raised to the 200th is an overflow, not an unrepresentable exponent.
  The Float form takes a Float exponent, so `(2.0).pow(0.5)` is a
  square root.
- **`pow` multiplies in a loop through the ordinary checked `*`**, so
  it traps at exactly the step that overflows and the message names
  the operands the caller can see. Exponentiation by squaring would
  report a mid-computation product that appears nowhere in the source.
- **`math.sqrt` of a negative is `NaN`, not a trap.** IEEE 754's
  answer, `Float` already admits NaN, and `math.is_nan` is right
  there. Trapping would make `sqrt` the one float operation that
  cannot produce a value its own type has.
- **`min`/`max` use the total order `cmp` and `sorted()` use**, so NaN
  loses `min` and wins `max`. Rust's `f64::min` agrees about the
  first, Go's `math.Max` about the second. One coherent order beats
  matching either piecemeal — in this language `min`, `<` and
  `sorted()` are specified never to disagree.
- **`math.nan` is a value, not a test.** `x == math.nan` is false by
  IEEE 754 and always will be; asking is `math.is_nan(x)`.
- **Both spellings of the split get a diagnostic.** `x.sqrt()` on a
  Float says *write `math.sqrt(x)`*; `5.sqrt()` adds the conversion.
  `math.pi()` says *`math.pi` is a Float constant, not a function*.
  A split the reader has to learn is a split the compiler should
  teach — otherwise it is exactly the kind of arbitrary-looking rule
  that makes a stdlib feel capricious.
- **The "no method" diagnostic now defaults its receiver**, so an
  untyped literal is named by the type it would become. `5.nope()`
  said `untyped integer has no method "nope"` — a type name that
  appears nowhere in the language.

Caught by the test suite and worth recording: splitting the `modules`
map to insert `moduleValues` beside it stranded `json`/`http`/`sql`/
`time` inside the new table. It **compiled**, because `*types.Func`
satisfies `types.Type` — the only thing that noticed was
`TestDocExamples` reporting `unknown module "time"`. An argument for
keeping the doc examples executable.

### Equality had four holes, not one

`==` is specified structural and universal (`../DESIGN.md`), and the
evaluator's switch did not implement that. `Map`, `Result`, `Error`
and `Range` all panicked with "not comparable" while `List`, tuples,
structs and variants worked, and boxing `Option` in M4c had quietly
added a fifth. All closed 2026-08-09.

`Ok(1) == Ok(1)` failing is the one that mattered most: it is what a
test asserts, and the failure mode was a runtime panic in the place a
program is least able to handle one.

**A Map's insertion order is not part of its identity.** The decision,
and the only one here with two defensible sides. Order stays a
specified *iteration* property that the compiled tier must reproduce —
but a map is a set of pairs, and two maps built by different routes to
the same pairs are the same map. Python holds both properties at once
(ordered dicts since 3.7, contents-only `==`) and nobody finds it
surprising, because iteration and identity are different questions. A
`List` stays order-sensitive because a list is a *sequence*: the rule
follows from what the collection is, which is exactly what Java gets
wrong by putting an order-sensitive `List.equals` and an order-blind
`Set.equals` under one `Collection` interface.

`Error` compares by message *and* the whole cause chain, so `context`
is part of identity — two failures with different provenance are
different failures.

The values still not comparable are the ones with no structure:
functions, iterators, and the concurrency handles (`Scope`, `Task`,
`Sender`, `Receiver`). There `==` could only mean identity, and
identity is not a question this language lets you ask.

One more untyped-literal leak fixed alongside: `1 == "one"` reported
*untyped integer and String can never be equal*, naming a type that
appears nowhere in the language. Same defaulting fix as the "no
method" diagnostic.

### Boxing Error

`Error` was erased at the *type* level and at the *value* level. The
first is the design — anything is assignable to `Error`, which is what
makes `Err("config is empty")` and free `?`-propagation work. The
second was an accident of it, and cost three things:

1. **The type could carry no methods.** `let e: Error = "x"` then
   `e.message()` dispatched on the dynamic `String` and reported
   `String has no method "message"`. So `message()` and the `cause()`
   the Errors design already named could not be added at all without
   the answer depending on how the error was made.
2. **Erasure leaked.** `match f() { Err(NotFound(id)) => … }` matched a
   concrete variant straight out of an `Error` slot.
3. **Two errors printed differently.** `Err("msg")` for a program-made
   one (quoted — the slot held a String) against `Err(msg)` for a host
   one.

Boxing fixes all three. The machinery is Option's, reused: the checker
records each coercion site in `Info.IntoError`, and the evaluator boxes
at `eval`'s one chokepoint. Two differences from the Option case, both
deliberate:

- **The boxing is idempotent.** `intoError` passes an existing `*ErrV`
  through untouched, so `IntoError` is a *hint* rather than an
  instruction and can be recorded even where the source type is
  `Unknown`. `Wrap` cannot afford that — double-wrapping an Option is a
  different value — but a double-boxed error is worse than merely
  wrong: its message is the rendering of another error, which reads
  almost right and is very hard to spot. Idempotence removes the whole
  failure mode rather than relying on the checker never over-recording.
- **One coercion site the checker cannot see.** `?` propagating into a
  `Result<_, Error>` boxes at the propagation point in the evaluator,
  because the expression belongs to the *callee* and the expectation is
  the enclosing function's return type. It sits beside the existing
  `E.from` lookup, and takes precedence: `Error` needs no `from` and
  never will.

**`find` takes the type as a value.** `e.find(ConfigError)`, not
DESIGN.md's original `find<ConfigError>()` — which does not parse, and
would not without turbofish, because `e.find<T>()` reads as a field
access followed by `<`. Glide already has types in value position
(`Tree.new()`), so the argument form needs no new syntax and checks
through `types.Meta`. Restricted to *declared* types: `find(String)`
would be a way to read a message that `message()` already gives
properly, and `find(Int)` asks about a representation rather than
about an error.

**The breaking change is reported, not silent.** `Err(NotFound(id))`
against an `Error` used to work and now cannot. A boxed value simply
would not match, turning a live arm into a dead one with no
diagnostic — so `bindCtor` reports it by name and points at
`e.find(MyErr)`. That guardrail is most of the reason this change is
safe to make at all.

`ErrV` gained a `Held` field for the concrete error. It is what `find`
walks for, and it is why boxing does not *lose* the typed error: the
erasure moves from the representation to the API, where the escape
hatch is offered deliberately instead of falling out by accident.
Equality deliberately ignores `Held` — it is a view of the same failure
the message already renders, and two errors whose messages and chains
agree while their payloads differ is a state the boxing cannot
produce.

**Found by using it.** Two things surfaced within ten minutes of
writing the first real script, which is the entire argument for
writing one:

- **A comma between match arms was a parse error**, and a baffling one
  ("expected a pattern, found ','"). Arms were newline-separated only,
  so `match x { A => 1, B => 2 }` could not be written on one line at
  all, and the reflex trailing comma failed. Fixed — the comma is now
  optional in both `match` and the subjectless form. Recorded in
  `../DESIGN.md` under the newline rule.
- **`Int??` does not lex** — the shorthand does not nest, because `??`
  is an operator. `Option<Int?>` is the spelling, which the language
  reference already said. Left alone: the workaround is a documented
  spelling rather than a missing capability, and special-casing the
  lexer here would be worth less than it costs.

Also confirmed while writing it: `fn main() -> Result<(), Error>`
works, and `?` in `main` prints the error and exits 1. That closes
most of the gap where a script wants "this must succeed, crash
otherwise", so no `unwrap`/`expect`-style builtin was added.

### M4d: the last three rules the evaluator enforced alone

The tail-value rule, the nested-shadow ban and `let … else` divergence
were checked only by the evaluator, and only on an executed path. All
three are now static (2026-08-10). Found while correcting the book: a
doc claimed annotations were ignored, checking that claim turned up
the residue, and the residue turned out to be worth closing rather
than documenting.

**A fourth documented gap did not exist.** Both `docs/book/chapter-19.md`
and `docs/reference/language.md` said a bound was unenforced when its
type parameter appeared only inside a container — `fn top<T: Ord>(xs:
List<T>)` accepting a `List<Blob>`. It is enforced, and was: `unify`
recurses through `App`, `Named` and `Tuple` arguments, so the binding
`T := Blob` reaches `checkBounds` from any depth. `List<T>`, `T?`,
`(T, Int)` and `List<List<T>>` were all tested. The prose was written
from the design intent and never re-verified — the exact failure the
conformance corpus exists to prevent, in the one place the corpus does
not reach.

**The scope-structure trap, which is the real content of this
change.** The nested-shadow ban depends only on scope structure, so
enforcing it statically means the checker's scopes must match the
evaluator's exactly — and the evaluator's are not the obvious shape.
`evalBlock` does not push an environment; its *caller* decides. A
function call declares parameters into an env and hands that same env
to the body, so `fn f(x: Int) { let x = g(x) }` is a same-scope
redeclare and legal. The checker pushed a scope for the parameters and
then `block` pushed another, which would have made that program a
nested shadow.

Five sites share a scope this way: a function's parameters and its
body, a closure's, a `for` pattern and its body, an `if let` binding
and its `then`, and a `scope` handle and its block. `block` was split
into `block` (push, then check) and `blockIn` (check in the caller's
scope), and those five call `blockIn`. A match arm is *not* in the
list, and that asymmetry is the evaluator's too: the arm env and a
block-expression body are separate envs there, so a rebinding inside
`Some(v) => { … }` really is a shadow.

Nothing enforces this agreement mechanically. `accept_redeclare.gld`
is the guard — six legal-redeclare shapes, one per sharing site, whose
whole job is to fail if the two ever drift apart. That is the general
lesson for the duplicated rules: an accept-case catches a false
positive, and a false positive is what a scope mismatch produces.

**Divergence is proved, and the proof has a hole the language cannot
fill.** `let … else` needs the else block to leave. The primitives are
a closed set — `return`, `break`, `continue`, and `os.exit`, typed
`Never` — plus composition: an `if` whose every branch leaves, a
`match` whose every arm does. Guards need no special case, and adding
one would have been a mistake in the expensive direction: a guarded
arm cannot count as covering for exhaustiveness, so an unguarded arm
always catches the fall-through, and every-arm-diverges is therefore
sound. Treating a guard as "might fall out" would have rejected
`match k { _ if hot => { return 1 } _ => { return 2 } }`.

A conditionless `for { … }` counts, when nothing breaks out to *here*.
A break aimed at an enclosing labelled loop does not disqualify it —
that leaves this loop without resuming after it, so the block is still
departed — while an unlabelled break, or one naming this loop, does. A
`for cond` never counts, not even `for true`: reading the condition
would mean constant-folding it, and then the rule's edge moves every
time the folder gets cleverer. Go's terminating-statement rule and
Rust's `loop`-versus-`while true` draw the line in the same place.

The hole: a helper ending in `os.exit` is typed `()`, so `else {
die("no config") }` is rejected though it would run. Rust rejects it
too and answers with `-> !` (Swift `-> Never`, Kotlin `Nothing`);
`../DESIGN.md` now carries that as an open question. Not adopted
speculatively — nothing in the corpus, the examples or the book wanted
it. What makes the set *closed* is not its size but that no user code
can join it: there is no way to declare a function that never returns.

`flow.go` is also the groundwork for unreachable-code detection, which
`../DESIGN.md` now records as a goal. The predicate transfers; the
failure direction does not. For `let … else`, a weak analysis rejects
working code, so the analysis is made as strong as it can be. For
unreachable code, a strong analysis condemns live code, so it must be
weak. Reusing the predicate without inverting that judgement is the
mistake waiting to be made there.

Note the direction of the risk, which is inverted from every other
rule here. Elsewhere the checker under-approximates and silence is the
failure mode. Here a *weaker* analysis means rejecting working code:
every case `diverges` fails to see becomes a false positive. That is
why the guard question mattered enough to get it right rather than
get it safe.

## Deliberately absent (after M4)

`Mutex<T>` (stdlib-era; ownership-transfer culture first), `derive`
(comptime era — the json/sql shims stand in), typed json decode / typed
query rows (wait for derive), method values as closures (`x.method`
unapplied), `or |e|` blocks (declined — see DESIGN.md), time
formatting/parsing/calendars (the `time` module's own later design),
error-to-status middleware for http (the one default mapping — Err →
500 — until middleware is designed), streaming a child process's
output / per-call env and cwd / stdin (see DESIGN.md's *Running other
programs* — streaming is not a triviality, since a subprocess writing
to the real file descriptor bypasses the writer the interpreter's
stdout actually is), and the rest of `math` (`log`, `exp`, the
trigonometric set, and the two-argument symmetric `atan2`/`hypot`
that belong in a module on their own merits).
