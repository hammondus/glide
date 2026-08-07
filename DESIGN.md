# Glide — Language Design Document

**Glide**: effortless motion, no visible struggle, real speed —
"human-friendly but performant" in one word. Binary: `glide`.
File extension: `.gld`. Status: pre-implementation, semantics on paper.

One user, no compatibility promise. Breaking changes are free until further
notice — this is a deliberate design asset, not an apology. Go's v1 guarantee
bought trust at the cost of a decade of frozen mistakes (`if err != nil`,
late half-hearted generics, no sum types). We keep the *discipline* of Go
(small, boring, orthogonal) without the freeze.

## Design principles

1. **Human-friendly and performant are both hard requirements.** Where they
   genuinely conflict, the default favours the human and an explicit opt-in
   favours the machine.
2. **One way to do it.** One loop. One branching construct. One formatting
   style. Novelty budget goes to semantics, never syntax.
3. **Compile speed is a feature.** Sub-second dev builds are non-negotiable;
   it's arguably Go's most underrated property.
4. **Auditability over cleverness.** A reader should be able to skim a
   function and know what it can and cannot do.
5. **The toolchain is the language.** Formatter, tests, docs, LSP, package
   manager: one binary, day one.

## Compilation

- AOT, native, single static binary, trivial cross-compilation.
- **Tiered backends**: fast custom backend for dev builds (speed over
  optimisation), LLVM for release builds. This dissolves the classic
  "fast compiler or fast code" dilemma; nobody has shipped it well yet.
- Accepted cost: two backends to maintain. Dev backend comes first; LLVM
  can wait until the language is worth optimising.

## Memory

- **GC by default.** Modern low-pause concurrent collector. Fighting a
  borrow checker is the single biggest reason people bounce off Rust;
  a good GC is fine for 95% of programs.
- **Ownership opt-in for hot paths**: arena/region allocation and
  non-copyable owned types, locally, in marked code. Pay for control only
  where profiling shows the need.
- Knowingly given up: Rust's compile-time "no data races ever" guarantee,
  hard-realtime ceilings, the last ~20% of performance. Target envelope:
  faster than Go, usually competitive with Rust, never beating hand-tuned C.
- **Rejected: manual `free`, even as a hint.** The instinct ("I know
  this is dead") is sound; the mechanism is wrong. A tracing GC's cost
  is proportional to the *live* heap — dead objects are never visited;
  announcing a death files paperwork with an agency that wasn't going
  to inspect you. Locals stop being roots at last use (liveness
  analysis), so `x = nil` hints are noise. And a `free` that really
  freed needs proof no other reference exists — that proof engine is a
  borrow checker, the road not taken; without it, GC overhead *plus*
  use-after-free. Cost model: **allocation rate × live-heap size —
  death is free, birth and survival cost.** The lifetime knowledge
  goes where it's true: (1) un-rooting long-lived holders (`None` the
  field, `clear()` the list — ordinary code); (2) fewer births (value
  structs, escape analysis, buffer reuse); (3) **arenas** — the
  knowledge is usually about a *group* ("everything in this request
  dies together"), where it stops being a hint and becomes an O(1)
  en-masse free the GC never sees. Arena details (handle vs scoped
  block) to be specified when the interpreter forces them.

## Unsafe

Every safe language is a safe API over an unsafe machine; the question
is where the boundary is and how visible crossings are. Go's great idea:
unsafe as an *import* — auditable by grep. Go's mistake: file
granularity (the import licenses everything) and folklore rules (the
six Pointer patterns, string-header tricks unblessed until 1.20).
Rust's great idea: `unsafe { }` blocks mark the exact lines,
`unsafe fn` propagates the obligation, and the culture — unsafe exists
to build safe abstractions — is the real achievement.

Glide takes both layers:
- **`unsafe fn` + `unsafe { }` blocks** — Rust's granularity, nothing
  crosses by accident.
- **Manifest-level visibility**: modules containing unsafe are flagged;
  vet and the dependency report surface which vendored packages use it
  (cargo-geiger built in). A dep that starts using unsafe between
  versions shows in the report diff, not the post-incident grep.
- **Culture rule**: unsafe lives in drivers, FFI, stdlib internals —
  never inline in app logic (vet-tier outside declared low-level
  modules).
- **Contents — less than Go's, and nothing until native**: interpreter
  era has none. Native era: raw pointer type (utterable only inside
  unsafe), reinterpret/transmute with layout rules *stated* (not
  folklore), and **pinning**. `Sizeof`/`Alignof` are comptime type
  queries, not unsafe (Go lumped them in for lack of anywhere else).
- **Recorded coupling: the moving-GC decision is an FFI decision.** If
  the collector compacts, every raw pointer handed to C is invalid at
  the next collection — the FFI story requires a pin API from day one
  of native work. The backend designer inherits this as constraint,
  not surprise.
- Why Glide needs less: Go's real-world unsafe is mostly performance
  folklore dodging reflection and header costs. Comptime serialization
  never reflects; String↔Bytes zero-copy is a blessed stdlib fn (unsafe
  inside, safe surface, contract stated); layout queries are comptime.
  A fast safe language shrinks unsafe to its honest job: the machine
  and C.

## Type system

- Static, strongly typed. **Function signatures always explicit** (they are
  documentation); type inference *inside* bodies only. Local-only inference
  keeps error messages pointing at the right line — whole-program
  Hindley–Milner is why Haskell errors baffle.
- **No null.** `T?` is sugar for `Option<T>`; the compiler forces handling
  of the empty case.
- **Sum types + exhaustive matching** are the star feature. Modelling
  "one of these N shapes" is the most common thing programs do.
- **Errors as values.** `Result` built in, `?` propagation operator.
  Go's philosophy (errors are ordinary values, visible in signatures) with
  the boilerplate removed. Panics exist for bugs only, never control flow.
- **Immutable by default, `mut` explicit.** Cheap to write, huge for
  auditability.
- **Generics from day one**, monomorphized.
- **Nominal interfaces (traits) with explicit `impl`.** Go's structural
  typing is friendlier at small scale but produces accidental-conformance
  confusion at large scale; explicit impl gives findability and better
  diagnostics.
- **No inheritance.** Composition + traits. Inheritance is the feature
  every ecosystem regrets by year five.
- **Mandatory initialisation, no zero values.** Go's zero values are null
  by another name: a `User{}` with an empty ID type-checks and flows until
  something breaks far from the cause. Types can opt in to a default via a
  trait (so `Mutex`, `Builder` still work bare), but domain types get no
  fake instances for free.
- **Integer overflow: trap in dev builds, wrap in release.** Falls out of
  the tiered-backend design. Explicit `wrapping_*` ops for code that wants
  modular arithmetic. Accepted cost: dev and release differ on overflow
  ("test what you ship" is violated) — trade taken knowingly, as Zig did.
- **Strings are UTF-8 bytes, Go-style** — not Rust's enforced-valid
  `String`/`OsString` split, which makes every OS/network boundary a
  conversion ceremony. Iteration yields runes; validity is checked at
  boundaries when you care, not enforced in the type system.

## Closures & anonymous functions

Go taxes closures (`func(x int) int { return x * 2 }` — 40 characters
to double a number) and that ceremony is half of why Go never grew a
map/filter culture. Decisions:

- **Rust's pipes**: `|x| x * 2`, block `|x| { … }`, no-arg `||`.
  Parameter types inferred (annotations allowed, rarely needed). Not
  `=>` (match-arm glyph — one glyph, one meaning). **No trailing
  closures** (Swift/Kotlin) — the gateway to builder DSLs and parser
  pain; closures are arguments, they sit in the parens. **No `$0`/`it`**
  — naming the parameter is documentation.
- **One function type: `fn(Int) -> Int`** — named functions, closures,
  method values (`x.method` is a closure over `x`) alike. Rust's
  Fn/FnMut/FnOnce zoo + `move` + `Box<dyn Fn>` is borrow-checker
  artifact; the GC dividend lands here. Whether a closure captured
  anything is representation, not type. Returning/storing closures is
  boring, as it should be.
- **Capture by reference**, mut rules doing the local work (mutating a
  capture requires the binding be `mut`). Loop variables fresh per
  iteration (recorded) — Go's capture bug already dead.
- **New compile rule: closures crossing task boundaries must not
  capture `mut` bindings.** `s.spawn(|| …)` referencing a local the
  parent still mutates is the data-race archetype, and this case is
  statically visible (mut-ness known, spawn a known boundary). Freeze
  idiom (`let snapshot = counter`) or `.clone()` to cross. Cheap rule,
  whole race class → compile error; race detector backstops what
  escapes via reference-typed immutable captures.
- Big lambda → named function is cut-paste (same type, no migration);
  the formatter nudges by refusing to make big lambdas pretty.

## Functions: defaults & named arguments

Go rejected defaults, named args, and overloading; the ecosystem's
answer — functional options — is 30 lines of ceremony per configurable
function (option type, N closure constructors, application loop,
a slice of function pointers per call) to express what the signature
should have said. The direct answer (Dart/Kotlin/Swift convergence):

```
fn connect(host: String, port: Int = 5432,
           timeout: Duration = 30.s, tls: Bool = true)
connect("db.local", timeout: 5.s)
```

- **Defaults evaluate per call** — Python's once-at-definition shared
  mutable default is the 30-year cautionary tale. Real expressions,
  may reference earlier params (`end: Int = s.len()`, Kotlin-style).
- **Named arguments, Kotlin model**: any param nameable at any call
  site; `copy(from: src, to: dst)` prevents transposition at the point
  of writing. Consequence owned: **parameter names are API** (renaming
  breaks callers). Swift's external-label/internal-name split declined
  — extra concept; param names were already documentation.
- **Overloading stays banned — now painlessly.** Go's ban is right
  (resolution swamps, dissertation errors) but paid in
  `NewX`/`NewXWithTimeout` proliferation. Defaults + named args cover
  overloading's legitimate 90%; generics and honest names cover the
  rest.
- **Defaults are declaration sugar, not type**: `fn(Int, Int = 5)` is
  not a type; function values have full arity; closures have no
  defaults. Filled at direct call sites only — one function type stays
  one.
- Not a one-way violation — the opposite: multiple spellings, one
  function; the alternative is N wrappers or the options ceremony.
- **Boolean-trap lint** (vet-tier): bare `true`/`false` literal args
  nudged toward `tls: true`.
- Line drawn: function knobs → named args; reified configuration
  (passed around, stored) → structs with Default. The line writes
  itself in practice.

## Traits & interfaces

Headline decided early (nominal, explicit); the full model is a
synthesis — neither Go nor Rust. **Conformance is declared; satisfaction
is structural** (Swift had it all along):

```
impl Reader for TcpConn {}   // whole thing — existing read() satisfies
```

One greppable line of intent (accidental conformance gone; "who
implements Reader" is a text search), but no Rust-style forwarding
boilerplate when shapes already match — bodies written only for what's
missing.

Go's implicit model gets three things right; each survives explicitly:
- **Consumer-defined interfaces** (Go's best architectural idea:
  accept interfaces, return structs) — intact: define the trait where
  it's used, `impl` it for the dependency's type in your module.
  The pattern was about who owns the abstraction, never implicitness.
- **Retroactive conformance** — via the orphan rule (Rust's): impl
  your trait for foreign type, or foreign trait for your type, never
  foreign-for-foreign. Retroactivity plus a coherence guarantee Go
  can't offer (exactly one impl per pair; behaviour can't depend on
  link order).
- **Small-interface culture** — culture follows cost; one line is
  cheap. Stdlib seeds it Go-style: one-method `Reader`/`Writer` at the
  bottom.

What Go's model gets wrong, fixed by explicitness:
- Accidental satisfaction (any `Close() error` "is" your Closer).
- Implementations undiscoverable without whole-program tooling.
- **Interfaces can never grow** — adding a method breaks every
  implementor; Go stdlib interfaces are fossils. **Default methods**
  fix evolution: adding with a default breaks nobody. This alone
  justifies the model.
- Typed-nil trap (nil `*T` in a non-nil interface — Go's most-asked
  confusion): unrepresentable. No nil to smuggle; absence is
  `(any Reader)?`.

Mechanics:
- Operators, derive, associated constants ride conformance (already
  presupposed by earlier decisions).
- Composition: `trait ReadWriter: Reader + Writer` — declared bounds,
  not embedding-aggregation.
- **Dispatch is chosen, visibly**: generics monomorphize (default,
  static); heterogeneous collections say `any Reader` (Swift's
  spelling — honest about box and vtable). Go boxes every interface
  value invisibly; we don't hide that cost.
- **No bare `any`/top type in v0** — the escape hatch reflection
  crawls through. Sum types + generics + `any Trait` cover the needs;
  a top type can be added deliberately, never removed.
- Costs, knowingly: one impl line per conformance (the point, not a
  tax); the orphan rule is a new concept, and newtype-wrapping to
  conform foreign-to-foreign is real ceremony.

## Primitive types

`Bool` · `Int` (= i64) · `i8 i16 i32 i64 i128` · `u8 u16 u32 u64 u128` ·
`Float` (= f64) · `f32` · `Rune` · `String` · `()` (unit)

- **`Int` is i64 on every target, not platform-sized.** Go's `int` makes
  arithmetic behaviour vary by target — "works on the mac, overflows on
  the deploy box" is a deletable bug class in a cross-compiling world.
  Sized names (`i32`) should feel like a decision, not a habit.
- **Lengths and indices are signed `Int`. No `usize`.** Unsigned sizes
  don't prevent `len - 1` underflow, they launder it into eighteen
  quintillion; countdown loops with `i >= 0` never terminate. C++'s own
  architects call unsigned sizes an irreversible mistake. Signed errors
  produce `-1`, which dev-build bounds checks trap and which can never
  silently address memory. And a separate index type means Rust's
  `as usize` confetti — a permanent tax expressing a distinction that
  died with 32-bit. `u64`/`u128` are for modular arithmetic, hashes, and
  bit patterns — not for sizing collections.
- **i128/u128 are primitives**: a `u128` *is* an IPv6 address, a UUID, a
  128-bit hash. Lowered to 64-bit op pairs (no hardware support; LLVM
  does this well). Beware the historical i128 ABI mismatch at the C FFI
  boundary.
- **No implicit numeric conversions.** `i32 + i64` is a compile error.
  C's silent promotion lattice is a 40-year bug factory; Go proved the
  strictness is tolerable.
- **No truthiness.** Conditions take `Bool` only. `if x != 0` costs five
  characters and removes an ambiguity class. JS's version is scar tissue
  (`"0"` truthy, `[] == false`, `document.all`); even Python's principled
  version conflates *absent* with *empty* — the exact distinction Option
  exists to make. Truthiness would erase at every `if` what the type
  system fought for. Every legitimate use gets an explicit substitute:
  - **Presence** → binding conditions, Swift-style (no `Some` ceremony):
    `if let user = find_user(id) { … }` — checks and unwraps in one
    move, and yields the typed value, which `if user:` never did.
  - **Emptiness** → `if !xs.is_empty()` — you chose which question you
    were asking, which is the point.
  - **Defaulting** → `x ?? fallback` on Options — absent-or-not, never
    value-dependent (JS's `||` breaks on `0` and `""`).
- **Literals are arbitrary-precision until they land in a type** (Go's
  untyped constants / Zig's `comptime_int`; falls out of comptime).
  `1 << 100` is fine in constant math; `let x: u8 = 300` is a compile
  error, not a wrap.
- **`Rune` is its own type, not an `i32` alias** (Go's aliasing is
  sloppy). Ordered, rangeable (`'a'..'z'`), but never passes where a
  count belongs. `string.runes()` yields U+FFFD for invalid sequences.
- **`BigInt` is stdlib, chosen by name — never silent promotion.**
  Python's auto-bignum puts a branch in every add, boxes hot loop
  counters, and is incompatible with trap-in-dev overflow (overflow
  becomes an allocation, not an error). Costs don't hide inside `+`.
  Ergonomics: arbitrary-precision literals materialise into it
  (`let k: BigInt = 1 << 4096` just works), and operator traits make
  arithmetic look native — Go's `a.Add(a, b)` API is why people avoid
  `math/big`. Same machinery serves `Decimal` (money) later.
- **Operator traits, scoped hard**: arithmetic and comparison only
  (`Add`, `Mul`, `Ord`, …). Not `&&`/`||` (short-circuit is control
  flow), not assignment, no user-invented operators (Haskell's operator
  soup; C++'s `<<`-for-IO). Rust's decade at exactly this scope shows it
  doesn't get abused.
- **Deliberately absent**: platform-sized ints, `f16` (ML niche,
  stdlib-or-never), `complex64/128` (Go shipped them as primitives;
  barely used — a library's job).

## Strings & formatting

Storage decided elsewhere (UTF-8 bytes, `runes()`, U+FFFD). API and
formatting:

- **Three delimiters, three meanings** (Go's allocation, kept):
  `'a'` is a `Rune` literal (the delimiter is type information; `'ab'`
  is a compile error), `"…"` is the working string (escapes + always-on
  interpolation; braced unicode `\u{1F600}`, one form), `` `…` `` is
  raw (no escapes, no interpolation, multiline; can't contain a
  backtick — accepted, by definition of raw). Python/JS burn two
  delimiters on synonyms and the "choice" gets confiscated by
  formatters anyway; JS then gave interpolation to the least ergonomic
  delimiter. No prefix zoo (`r"…"`, `f"…"`, `"""`) — prefixes are what
  you need after squandering delimiters on synonymy.
- **Interpolation, always-on, compile-checked** — printf dies.
  `"user {user.name} has {count} items"`; full expressions, not Rust's
  identifiers-only. Not Python's opt-in `f"…"` — the forgotten-prefix
  bug is endemic; one string syntax that just works. The `{{` escaping
  tax lands on embedded JSON/templates, which belong in **raw strings**
  anyway (backticks, Go-style: no escapes, no interpolation, multiline)
  — the split we already need for regexes and Windows paths absorbs the
  brace problem.
- **Zero runtime machinery**: interpolation desugars at compile time to
  builder/writer calls through traits. Go parses format strings at
  runtime per call and walks args with reflection (banned here); its
  mismatches print `%!d(string=hi)` in production. Ours are compile
  errors. C: UB. Go: runtime noise. Rust: right, via macro. Glide:
  right, as a language feature.
- **`Display` vs `Debug` are separate traits** (Rust's split). Display:
  deliberate, user-facing, hand-written. Debug: structural, `derive
  Debug` (comptime reflection's second customer), for logs and tests.
  `{x}` = Display, `{x:?}` = Debug. Go's `%v` conflation is why Go
  programs print struct guts at users. No Display → no interpolation:
  compile error, not `%!v`.
- **Format specs: small readable set** — `{price:.2}`, `{id:04}`,
  `{n:hex}`. Deliberately small; a mini-language growing inside braces
  is printf reborn.
- **No integer indexing: `s[i]` does not exist.** Go's byte-indexing
  surprises; Python's char-indexing costs representation tricks; Rust's
  refusal is right. "What's at position i" is underspecified (byte?
  rune? grapheme?) — the language won't guess: `s.bytes()`,
  `s.runes()`, explicit byte-offset slicing for parsers. Graphemes are
  a stdlib segmentation library, never a primitive — that table changes
  with Unicode releases.
- **Immutable strings; building is a named type** (`StringBuilder`).
  Loop `+` is the O(n²) classic; the accumulator's cost is visible in
  the type — same doctrine as BigInt and views. Interpolation
  eliminates most concatenation anyway.
- **Comparison is byte equality.** No locale collation or case-folding
  in `==` — Turkish-i has ended careers. Locale is a library invoked on
  purpose.

## Generics syntax

**Angle brackets — `List<T>`, `fn max<T: Ord>(a: T, b: T) -> T` — not
Go's square brackets.** Go's `[T]` was driven by parser convenience;
the human wins instead:

- Square brackets already mean indexing, and the collision hits
  *readers*, not just parsers: Go's `a[T]` is element access or type
  instantiation depending on what `a` is — invisible at a glance. We've
  spent whole sections making kinds and costs visible at the use site;
  type application looking identical to indexing runs the wrong way.
  `<T>` is unambiguously "types here" to every programmer alive.
- The `f<T>(x)` vs `(f < T) > (x)` ambiguity has a proven, turbofish-free
  fix: C#/TypeScript's 20-year-old disambiguation (after `ident <`,
  tentatively parse a type-arg list; commit based on what follows the
  `>`). Parser pays once; Go's choice makes every reader pay forever.
  Lexer splits `>>` in type context (the C++11 fix). **No turbofish,
  ever** — Rust's `::<>` is the wart this paragraph exists to avoid.
- The ambiguity barely arises anyway: declarations are never ambiguous
  (`<` after the declared name), and with local inference, explicit type
  args in expressions (`parse<Config>(s)`) are rare.
- **Constraints: inline colon bounds only** — `T: Ord + Hash`.
  Unconstrained is bare `<T>`; Go's mandatory `[T any]` is ceremony its
  bracket choice created (bare `[T]` collides with array sizes). No
  `where` clause in v0 — two ways to write bounds violates house rules;
  add it only if nested bounds make signatures genuinely unreadable.
- **Const generics deferred** (`Matrix<T, const N>`): real machinery for
  numeric-library needs we don't have. Array `[N]T` needs only a
  comptime-constant N, which we have.
- Accepted cost: the parser buys lookahead heuristics — real but small,
  paid in exactly one place, proven for two decades.

## Enums

A C-style enum is a sum type where no variant carries a payload — not a
separate feature, just the degenerate case kept ergonomic. Go's failure
here is both its diseases at once: `iota` is a counter macro, not an
enum — any int converts (`Color(42)` compiles and flows), no
exhaustiveness, no enumeration, names require the external `stringer`
codegen, and the zero value silently means the first variant. Java got
enum *semantics* right and drowned them in ceremony; Rust/Swift did it
without the ceremony. That's the lineage.

- `type Color = Red | Green | Blue` — one line, same form as any sum
  type; a variant can gain a payload later without changing feature.
- **Exhaustive matching** — free, from the sum-types decision.
- **No implicit int conversion in either direction.** From wire data:
  `Color.from_int(n) -> Color?` — total, the invalid case handled where
  it enters. To int: explicit method. An enum is a set of names, not an
  integer in a costume.
- **Namespaced variants, Swift's dot shorthand**: `Color.Red` in full;
  `.Red` where the expected type is known (match arms, assignments,
  args). One character that says "resolve in the expected type" —
  proven ergonomic; bare names would make shared variant names
  ambiguous, `use`-dumping pollutes.
- **Enumeration via derive** (comptime reflection's first easy
  customer): `derive Enum` → `Color.all()`, `c.name()`,
  `Color.from_name(s) -> Color?`. Go needs an external tool for this.
- **Explicit discriminants allowed** for wire/FFI stability:
  `type Status = Ok = 200 | NotFound = 404`; fixed representation only
  where declared, compiler's choice otherwise.
- **Flags are not enums.** "One of" and "set of" are different types:
  `Set<Color>`, or stdlib `BitSet` where performance demands. An enum
  that's secretly a bitfield is the int costume again.

## Mutability & methods

- **Receiver mutability is declared**: `fn len(self)` is read-only (the
  default), `fn push(mut self, item: T)` may mutate. A `mut self` method
  is callable only through a `mut` path — otherwise "immutable by default"
  is a lie (`let` would mean "can't reassign, but anyone can gut the
  object through a method").
- **One axis, one annotation.** Go's pointer-vs-value receivers conflate
  *may this mutate?* with *how is it passed?* and enforce neither (value
  receiver "mutating" a copy is the classic footgun). Representation is
  the compiler's business; mutability is the declared, checked axis.
- **Free-function `mut` params are marked at the call site too**:
  `fn sort(mut xs: List<Int>)` is called as `sort(mut xs)`. Pure
  auditability — every place your data can change under you is visible
  when skimming. This is Rust's `&mut x` without the reference machinery.
- **Method receivers take no call-site marker** (`xs.push(3)`, not
  `mut xs.push(3)`). Asymmetric, deliberately: method names carry intent
  (`push`, `clear`), and marking every receiver is noise that trains
  people to ignore markers. Rust made the same compromise; a decade of
  use says it's right.
- **Mutability is transitive through paths**: `a.b.c` is mutable only if
  `a` is `mut`. No per-field mutability declarations — that's a
  `Cell`-shaped rabbit hole; revisit only on real pain.
- **Recorded sacrifice — `mut` is a path property, not an object
  guarantee.** With reference semantics, GC, and no borrow checker, two
  bindings can alias one object; the object can change under a `let`
  binding via the other path. `let` means "no mutation through this
  name". Strong auditability, real bugs prevented — but not a frozen
  object. Frozen guarantees come from immutable stdlib data structures,
  not from pretending `mut` is Rust.

## Shadowing

**One live binding per name, always — redeclare in sequence freely,
never in nesting.** "Shadowing" is two features wearing one name:

- **Sequential (same scope): allowed, idiomatic** (Rust's quiet win).
  `let input = read_line()?; let input = input.trim(); let input =
  parse<Config>(input)?` — a refinement pipeline keeps one honest name
  (vs `input_raw`/`input_trimmed`), each stage a fresh immutable
  binding, stages free to change type (String → Config — impossible
  with `mut` reassignment). Safe because after each `let` exactly one
  binding is alive — no "other" variable to have meant.
- **Nested (inner block over live outer name): compile error.** The Go
  bug shape — `result, err := bar()` inside a block silently declaring
  new variables, the outer ones unchanged, the change evaporating at
  block end — requires *two live bindings*. C#/Java ban exactly this;
  nobody lists it among their regrets. Scope of the ban:
  function-local (params + enclosing locals). Module-level names stay
  shadowable — protecting the whole module namespace is why Go's vet
  shadow-checker is too noisy to enable.
- **Root cause treated separately**: Go's trap needed `:=`'s dual
  personality (declare-or-assign). Here `let` always declares, `=`
  always assigns, assigning to an undeclared name is an error —
  "meant assign, got declare" is unspellable.
- Builtins/types protected by existing machinery: enforced casing means
  `let Int = 5` is already a violation; `len` is a method, not a
  shadowable global.
- **Loop variables: fresh binding per iteration** — Go's most expensive
  shadowing-adjacent bug (closure capture), fixed in their 1.22
  semantic break; ours from day one.
- The freeze idiom composes for free:
  `let mut acc = …; …; let acc = acc` — mut during construction,
  sealed after.

## Errors

Go and Rust each shipped half the answer: Rust gave `Result` and no
default error type (eight years of `error-chain`→`failure`→`anyhow`/
`thiserror` churn); Go gave only the dynamic half, so failure modes can
never be enumerated and `errors.Is` is pattern matching rebuilt from
pointer comparisons. Ship both halves in the stdlib, day one:

- **Libraries define sum-type errors.** Failure modes are sum types' best
  use case: the signature documents what can go wrong, `match` is
  exhaustive, adding a variant breaks callers at compile time. No
  sentinel error values; no `errors.Is`/`As` — variant matching replaces
  them.
- **Applications use the stdlib dynamic `Error` type** — holds any
  concrete error plus a context chain (`anyhow`'s lesson, made official).
  `fn run() -> Result<(), Error>` and everything flows.
- **`?` converts error types via a conversion trait** (declared
  conversions; anything converts into dynamic `Error` for free). The one
  implicit conversion in the language, and it only fires at `?` — the
  alternative is `.map_err` on every `?`, which is `if err != nil`
  reincarnated. Rust tried life without it; nobody would go back.
- **Context is a method**: `open(path).context("loading config")?`.
  The `Error` trait carries `cause() -> Error?`; printing renders the
  chain. (Go's `%w` gets these semantics right with the worst possible
  syntax — wrapping controlled by a format verb.)
- **Chain-walking downcast**: `err.find<ConfigError>()` walks the cause
  chain for a typed error. Needing it deep in application code is a smell
  that a boundary should have been typed — but the escape hatch is cheap
  and Go's `Is` proves it gets used.
- **Backtraces: captured in dev builds, skipped in release.** The
  "errors-as-values lose the stack trace" complaint, solved where it
  matters, free with the tiered backends.
- **Panics kill the task, not the process; no `recover`.** Structured
  concurrency gives panics a principled boundary: a panicking task fails
  its scope, which cancels siblings and propagates. Go's `recover` exists
  because unstructured goroutines gave panics nowhere to go — a problem
  we designed out, so we skip the escape hatch people abuse into
  exception handling.

## Defer & resource cleanup

`defer` is the right primitive for a GC language — the alternatives fail
first: RAII/`Drop` needs deterministic destruction, which GC doesn't
give (finalizers running "eventually" is how fds leak; Drop is a
borrow-checker dividend we declined). `with`/`using` blocks nest — three
resources, three indent levels — and would be a second way to do what
defer does flat. Go's defer, with its three defects fixed (Swift and
Zig already shipped the fixes; we take theirs):

- **Block-scoped, not function-scoped** (Zig/Swift). Go's function-scoped
  defer in a loop accumulates every file until return — the classic
  fd-exhaustion bug, invisible because the code looks correct. Defer
  runs at end of enclosing block; function-end cleanup is a defer at
  function scope, where it reads as what it is.
- **Defer takes a block, only a block**: `defer { conn.close() }`
  (Swift's form). No call-with-args form, so Go's argument-evaluation
  puzzle (`defer f(x)` evaluates `x` now, calls later) cannot be
  written. Everything runs at scope exit, closing over variables like
  any closure.
- **The discarded-error problem** (Go's worst: `defer f.Close()` drops
  the error that surfaces buffered-write failures — silent data loss as
  folklore-workaround territory). Two parts:
  1. Unused-Result rules apply inside defer blocks — discard is visible:
     `defer { f.close() or log_err(…) }` or explicit `_ =`.
  2. **`errdefer`** (Zig's contribution): runs only when exiting on the
     error path. The missing construct behind Go's awkward cleanup:
     rollback-if-failed, delete-partial-file. Success path does explicit
     `f.close()?` / `tx.commit()?` — no error discarded on either route.
     Not two ways to one thing: `defer` = always, `errdefer` = only on
     failure; conflating them is the bug Go keeps hand-writing.
- LIFO order; defers run during panic unwind (required: a panicking task
  must release locks as the failure propagates to its scope, or dead
  tasks deadlock the program).
- **Rejected: linear "must-close" types.** Provable-cleanup type systems
  are heavier than the problem in a GC language; a vet-tier "resource
  never closed on some path" lint can retrofit most of the value.

```
fn store(path: String, data: Bytes) -> Result<(), Error> {
    let f = fs.create(path)?
    errdefer { _ = fs.remove(path) }   // no partial files on failure
    f.write(data)?
    f.close()?                          // success: error propagates
    Ok(())
}
```

## Control flow

### One loop: `for`

Go got this exactly right. A single keyword covers every iteration shape:

```
for { ... }                      // infinite
for cond { ... }                 // while
for i := 0; i < n; i++ { ... }   // C-style
for item in items { ... }        // range over collection
for i, item in items { ... }     // with index
```

No `while`, no `do/while`, no `loop`, no `repeat`. Note the `in` form
replaces Go's `range` keyword — same semantics, reads more naturally.
Iteration over user types goes through an iterator trait (decide the exact
protocol later; Go's function-iterators arrived late and awkward — design
this in from the start).

### One branch construct: `match`

Go's `switch` flexibility and Rust's `match` exhaustiveness are the same
construct waiting to be unified. `match` is the only multi-way branch
(plus plain `if`/`else` for the two-way case):

```
match x {                        // match on a value
    1, 2, 3   => small()         // multiple values per arm (Go-style)
    n if n<0  => negative(n)     // guards
    _         => other()
}

match {                          // no subject: first true arm wins —
    x < 0     => ...             // Go's expressionless switch, i.e. a
    x == 0    => ...             // clean if/else-if chain
    _         => ...
}

match shape {                    // sum types: compiler enforces
    Circle(r)     => ...         // exhaustiveness, no _ needed when
    Rect(w, h)    => ...         // all variants are covered
}
```

- **No fallthrough, no `break`.** Go's default (implicit break) is correct;
  we drop even the explicit `fallthrough` escape hatch until a real need
  shows up.
- Exhaustiveness enforced on sum types; open matches (integers, strings)
  require `_`.
- `match` and `if` are **expressions** — they return values.

### No goto; labeled break/continue survive its estate

Go quietly kept goto (function-scoped; stdlib uses it ~two dozen times,
scanners and state machines) because Go lacked the replacements. Its
three surviving legitimate uses, each covered:

1. **Cleanup-on-error** (`goto fail`, the C kernel idiom — goto's
   strongest modern case): killed twice over by `defer`/`errdefer`
   (structural cleanup) and `?` (early exit).
2. **Escaping nested loops**: **labeled break/continue, adopted** —
   `search: for … { for … { break search } }`. The alternatives are a
   flag checked at every level or a function extracted just to `return`.
   Labels name loops; break exits outward through enclosing loops only —
   never arbitrary jumps. Go/Rust both have it; used rarely and
   gratefully.
3. **Threaded dispatch** (computed goto in C interpreters):
   `loop { match state { … } }` *is* the state machine; compiling it to
   threaded dispatch is a release-backend optimization obligation, not
   a user syntax obligation. Recorded on the backend requirements list.

What's left of goto — backward jumps, jumps into blocks — is the part
nobody defends. Every reason Go kept the keyword is a hole already
filled here.

### No ternary operator — expression-`if` is both halves

Go is half right (`?:` nests unreadably, its precedence breeds bugs, a
second cryptic branching syntax) and C is half right (conditionals ARE
expressions). Go refused the operator without the compensation — hence
four lines and a mutable variable for the innocent case, and a
permanent FAQ entry. Here:
`let status = if ok { "active" } else { "disabled" }`. The pieces that
make it a replacement, not an approximation: the formatter keeps
one-liners that fit; value-position `if` requires `else` (type
checker); the commonest ternary (`x != nil ? x : y`) is already `??`;
the second commonest (chains) is expressionless `match` — what nested
ternaries were always trying to be.

## Pattern matching depth & destructuring

Organizing principle: **patterns are construction run backwards** —
anything buildable by expression is disassemblable by the same shape.
(Go: constructors for structs, patterns for nothing — every disassembly
is manual field-picking.)

- **Destructuring `let`, everywhere irrefutable**:
  `let (host, port) = parse_addr(s)?`, `let User{name, ..} = u`.
- **Tuples are first-class types** — multi-return was always tuples
  (`let a, b = f()`), and Map iteration needs `(K, V)`. Go's
  multi-values-that-aren't-values (unstorable, unlistable) is a
  permanent wart. Doctrine: **tuples are for transport** — two things
  briefly; crossing >1 API boundary or reaching 3 elements means it
  should be a named struct (vet-tier nudge).
- **Nested patterns, arbitrary depth, exhaustiveness follows the
  nesting**: `Some(User{role: Admin, name, ..}) =>` — where match earns
  its keep; Go's type-switch can never (one level, no payloads, no
  coverage).
- **Struct patterns require `..` for partial** (Rust's rule): silent
  omission would mean new fields change nothing at match sites —
  defeats tell-me-when-the-world-changes. Two characters for "and the
  rest, deliberately".
- **`let … else`** (Swift's guard let): `let user = find(id) else {
  return Err(.NotFound) }` — body must diverge (enforced; what makes
  flat happy-path *safe*). `if let` scopes inward, `let else` onward;
  Option ergonomics complete without ever writing `Some`.
- **Ranges and literals**: `1..10 =>`, `'a'..'z' =>`, `"GET" =>`
  (equality only — no regex-in-match). **List patterns, basic set**:
  `[]`, `[x]`, `[first, ..rest]` — argv/path territory; no nested rest
  gymnastics.
- **Declined**: nested or-patterns (arm-level comma alternatives stay;
  `|` inside patterns is a second alternation syntax for a rare case —
  revisit on pain); `x @ pattern` (guards cover); patterns in function
  params (signatures stay flat; destructure on body line one);
  ref/binding modes (Rust's match-ergonomics saga doesn't exist —
  patterns bind values, GC holds them, `let (mut a, b)` works — the
  biggest pattern dividend of no-borrow-checker).
- **Guards are opaque to exhaustiveness**: coverage computed as if all
  guards fail; a guarded arm never completes coverage — the compiler
  demands the unguarded case rather than trusting your predicate.

### The construction side (spread), and no variadics

Patterns are construction run backwards, so every pattern shape gets
its literal:
- **List literals**: `[1, 2, 3]` (retires `List.of`). **Spread in
  literals**: `[a, ..xs, b]` — any position, honestly a copy (the
  range-indexing doctrine).
- **Struct update**: `Config{ timeout: 5.s, ..base }` — in an
  immutable-by-default language, copy-with-changes is *the* idiom, not
  a convenience. Plain copy with overrides; `..base` last, exactly one;
  mandatory init composes (the literal accounts for every field,
  `..base` accounts for the unchanged).
- **Not adopted: JS object-merge spread** (`{...a, ...b}`, silent
  right-wins). Map merge is a method with a stated collision strategy;
  structs have one `..base` so no collision question exists.
- The `..` glyph serves ranges, rest-patterns, struct update — three
  context-distinct uses (Rust runs the same triple). A decision, not an
  accident.
- **No variadic functions.** Go's every variadic customer is dead here:
  `Printf(args...)` → interpolation; `append(xs, a, b, c)` →
  `push`/`extend`; `New(1,2,3)` → list literals; `max(a,b,c)` → the
  two-arg form or a list. Against the empty benefit column: a second
  function-type shape (vs the one-function-type doctrine), miserable
  interaction with defaults/named args, and Go's `f(xs...)`-vs-`f(xs)`
  both-compile confusion. A function takes `List<T>`; callers write two
  brackets. Revisit only if the brackets grate — bet: never.

## Iteration

- **The protocol is a trait** — external iteration, Rust-style:
  `trait Iterator<T> { fn next(mut self) -> T? }`. One method, `Option`
  return: no invalid states, no `hasNext`/`next` desync, no `(value, ok)`
  tuples. `for x in xs` desugars to `next()` until `None`.
- **Adapters** (`map`, `filter`, `take`, `zip`, …) are lazy default
  methods on the trait. External iteration is what makes `zip` trivial —
  the thing Go's callback-style `iter.Seq` makes miserable.
- **Generators are how you write iterators**: `yield` in a function body
  makes it a generator; the compiler builds the state machine
  implementing `Iterator`. Sugar for the trait, not a second protocol.
  Hand-writing `next()` for a tree traversal means manually maintaining
  the stack the compiler should build — this is why Go resisted
  iterators for a decade and then shipped the awkward callback form.
- **Why we get this cheaply**: generators are hard in Rust (a decade
  unstabilised) because yielded references borrow from suspended stack
  state. No lifetimes here — in a GC language generators are almost
  boring, and in the tree-walking interpreter a suspended frame *is* the
  state machine. Build around this asymmetry.
- **`Iterable` is separate from `Iterator`**: `for` accepts either an
  iterator or anything with `fn iter(self) -> Iterator<T>`. A `List` is
  not an iterator; it can be iterated many times. Collapsing the two
  bites every language that tries.
- **Channels are not the iterator protocol.** A green thread + channel
  per loop means synchronisation per element, and early exit leaks a
  thread. Iteration is control flow, not communication.
- Accepted costs: adapter-chain type errors (`Map<Filter<Take<…>>>`) are
  ugly; release backend must compile generator state machines well
  (known-hard-but-solved — C# has done it for 20 years); laziness
  surprises (`xs.map(f)` does nothing until consumed).

## Concurrency

**Green threads and channels, not async/await — then fix what Go got
wrong about them.**

Why async/await loses: it makes concurrency a viral type-system property
of every function ("What Color Is Your Function?"). The evidence is
brutal: Python maintains two parallel ecosystems (`requests`/`aiohttp`);
async Rust is described by its own maintainers as a second, harder
language (`Pin`, `Send + 'static`, executor fragmentation). async/await
exists as a compiler transform to stackless state machines — essential
when you can't have a runtime (Rust, embedded) or threads (JS). Neither
constraint is ours; green threads are what the GC-and-runtime purchase
buys. Decisive recent evidence: Java built reactive frameworks for 15
years, shipped virtual threads (Loom, Java 21) — goroutines, essentially
— and its ecosystem is migrating back to plain blocking style.

Go's model, with its defects designed out:

- **`go` is unstructured — the goroutine has no parent.** It outlives
  its spawner (leak), its panic kills the program, its errors vanish;
  every mature codebase reinvents errgroup. Structure is primitive here
  — nurseries (Trio's insight; since adopted by Kotlin, Swift, Loom):
  `scope s { let a = s.spawn(|| fetch(u)); … }` — scope exit waits for
  children; one failure cancels siblings and propagates. Leaks are
  unrepresentable.
- **`context.Context` is function coloring by hand.** Go escaped async's
  viral annotation, then cancellation forced `ctx` as first parameter of
  every serious function — the same disease, manual and unchecked.
  Cancellation belongs to the scope: `scope(timeout: 5.s) { … }`;
  blocking operations are cancellation points; no parameter threading.
- **Channels: keep them, keep `select` (Go's crown jewel), fix the
  sharp edges.** Send-on-closed, double-close, nil-channel-blocks — all
  symptoms of "anyone can close anything." Sender half and receiver
  half are distinct types; the sender closes; misuse is a compile-shape
  error, not runtime roulette. Sends transfer ownership (the sent value
  is dead to the sender) — kills the both-sides-mutate race at the root.
- **Races: honest mitigation, no false promise** (no borrow checker —
  recorded sacrifice). Ownership-transfer makes the default pattern
  race-free; **`Mutex<T>` wraps the data it guards** (Rust's best
  non-borrow-checker idea — unguarded access doesn't compile), unlike
  Go's `sync.Mutex` sitting beside the data hoping; race detector runs
  in `glide test`, not behind an opt-in flag learned after the incident.
- Accepted costs: stackful tasks cost KBs where Rust futures cost bytes
  — at extreme task counts (millions of connections on tiny hardware)
  async wins on memory; workload conceded. FFI stays the ugly corner.

## Metaprogramming

- **Comptime, not macros** (Zig's insight): run ordinary language code at
  compile time. Covers ~90% of macro use without creating a second
  language that tooling can't see through.
- No AST macros, ever. They're how ecosystems become unreadable.
- **Comptime is NOT a second generics system — the fence matters more
  than the feature.** No user-written functions that take or return
  types. `List<T>` comes from trait-bounded generics (bounds checked at
  the declaration, errors at the call site), never from a comptime
  function. Zig's comptime-as-generics gives C++-template-tier errors
  deep inside the callee; Zig had no choice — comptime is all they have.
  We do. Without the fence, hard generic bounds get "solved" by escaping
  to comptime and the ecosystem ends up duck-typed and undiagnosable.
- Comptime is for exactly two things:
  1. **Const evaluation** — ordinary functions run at compile time in
     const positions (array sizes, tables, compile-checked format
     strings). Same function, either phase; no `constexpr` sub-language.
  2. **Derive via comptime reflection** — a comptime-only API exposing
     type structure (fields, names, variants). `derive Json` is an
     ordinary comptime function that walks the struct and emits a plain
     encoder, optimised like hand-written code. Replaces Rust's proc
     macros (a second compiler, slow, opaque) and Go's runtime reflect
     (an interpretive loop per call, unauditable).
- **No runtime reflection. Absent, not discouraged.** Everything Go uses
  `reflect` for happens at comptime against static types. Real cost: no
  deserialising into a type named by a runtime string — the rare dynamic
  case hand-rolls a registry, visibly. Runtime reflection is the biggest
  hole in Go's auditability story and a permanent performance tax.
- **Discipline rules**: no IO at comptime (hermetic, reproducible
  builds — codegen-from-schema is a build step, not a comptime trick);
  fuel-limited evaluation (instruction quota, exceeding is a compile
  error, explicitly raisable); deterministic by construction, so caching
  comptime results is always sound (the fast dev backend leans on this).
- **v0 scope**: const eval + reflection API + `derive`. The reflection
  API is the genuinely hard design problem (prior art: Zig `@typeInfo`,
  C# source generators — neither fully right); prove it in the
  interpreter before any backend exists.
- **Known gap, accepted forever**: DSL-style macros (`html!` templates,
  new syntax). Comptime functions over string constants get us
  compile-checked SQL; they don't get new syntax. Embedded custom-syntax
  DSLs are exactly the "second language tooling can't see" we banned.

## Syntax

- Braces, not significant whitespace (whitespace breaks codegen and
  copy-paste).
- **Statement termination: Go's newline rule.** No semicolons; a
  newline ends a statement when the token before it can end an
  expression (identifier, literal, `)`, `]`, `}`, `?`). A trailing
  operator or dot continues the line. `else` sits on the same line as
  its `}` — the canonical formatter guarantees it, so the rule never
  bites. (Forced by the interpreter; ratified.)
- **Interpolation escape: `\{` for a literal brace**, joining the
  existing backslash family (`\n`, `\"`) — not Rust's `{{` doubling,
  which is a second escaping mechanism for one character.
- Source files: `.gl`.
- Expression-oriented: `if`, `match`, blocks return values.
- **A function body is a block; its tail expression is the return
  value.** Not a separate feature — the consistent consequence of
  expression-orientation: `let status = if ok { … } else { … }`
  already requires blocks to yield their last expression, and carving
  out function bodies (Kotlin's halfway position: expression-bodied
  functions only) makes the same braces mean different things by
  position. Lineage: Lisp, the ML family, Ruby, Rust; `return` stays
  for early exits. The CoffeeScript failure mode (accidentally
  returning a leftover internal value) needs dynamic typing —
  mandatory signatures are the guardrail: the tail must match the
  declared return type.
- **No semicolons, so the value/statement distinction Rust hangs on
  `;` hangs on the signature instead.** Arrow declared → the tail
  expression must have that type. No arrow → the function returns
  nothing, and a tail expression with a meaningful value is a compile
  error, not silently discarded — discard explicitly with `_ =`
  (consistent with the defer rule). Editing is protected the same
  way: append a statement after the old tail and the types no longer
  line up.
- Small keyword count. Boring on purpose.
- **Boolean operators: `&&`, `||`, `!` — fully conventional.**
  Short-circuiting, conventional precedence (`!` > `&&` > `||`), both
  operands `Bool` (no truthiness), not overloadable (short-circuit is
  control flow), no `and`/`or` word forms. The C `if (b = 1)` typo is
  doubly unwritable: assignment is a statement, and conditions demand
  Bool.
- **No `++`/`--`.** Go's statement-only postfix fix removes the C
  cleverness but leaves a second spelling of `+= 1` whose whole value is
  two characters. Python/Rust/Zig never had it; Swift had it and paid to
  remove it (Swift 3) — the operator is worse than its absence. Also a
  Glide-specific reason Go didn't have: in an expression-oriented
  language, a statement-only magic operator is a grammatical special
  case. `i += 1` is the way.
- **Assignment is a statement, not an expression.** No `a = b = c`, no
  `while x = next()` — the same cleverness family as expression-`++`,
  killed at the root. Compound assignment (`+=`, `-=`, …) stays: it
  names read-modify-write clearly.
- **No `:=` — `let` is the only declaration form.** Right for Go in
  2009 (escaping `FooFactory foo = new FooFactory()`), wrong purchase
  here: (1) Go ends up with five declaration spellings (`:=` can't even
  work at package level); (2) `x, err := f()` declares x while
  *assigning* err — unless a new scope makes it declare both, silently:
  one operator, three behaviours, resolved by non-local context;
  (3) one character of ink carrying declaration semantics is a typo
  trap; (4) **decisive: `:=` has no slot for mutability — it's why Go
  has no immutable locals at all.** The keyword is what
  immutable-by-default lives in; `let ` costs four characters and buys
  the highest-value auditability feature in the language. Multi-value
  by fiat: `let a, b = f()` declares both, `a, b = f()` assigns both,
  mixed is unwritable.
- **No `if` init clause.** `if v, ok := m[k]; ok` existed to scope `v`
  to the branch; `if let v = m[k]` covers the dominant case better
  (maps return Options — the comma-ok dance is gone), and the rest
  declare on the line above: with the shadowing trap unspellable, a
  variable living one line longer is a non-cost.

### Formatting: fully canonical — further than gofmt

gofmt only ended half the argument: it canonicalised indentation and
braces but preserves author line breaks, so Go teams still argue about
column width and where to split. Glide goes all the way:

- **The formatter is a pure function from AST to bytes.** Same code →
  byte-identical output, everywhere. Width-aware wrapping at a fixed
  column (~100), Prettier/Black/dart-format style, not gofmt style.
- **Zero configuration. No config file format exists.** rustfmt shipped
  `rustfmt.toml` and reinstated, one knob at a time, every argument the
  tool existed to end.
- **Trailing comma forces one-element-per-line** — the author keeps
  structural intent (matrix-shaped literals etc.) through grammar, not
  through whitespace the formatter would erase.
- Accepted cost: a width-aware formatter is a constraint-solving
  pretty-printer (Wadler-style), a genuinely harder program than gofmt.
  Dart rebuilt theirs this way; it's the best-regarded formatter
  shipping. Occasionally the machine wraps worse than a human — the
  trailing-comma rule covers most such cases.

### Style baked into the grammar

The strongest "one way to do it" is when the alternative doesn't parse
(Go's `{`-on-same-line is enforced by the lexer, not gofmt). Hunt for
every such spot while the grammar is wet:

- `{` on the same line — grammatical, via newline rules.
- **Braces mandatory on every block** — no single-statement `if`/`for`
  bodies, so dangling-else and the `goto fail` bug shape never exist.
- Trailing commas allowed everywhere (clean diffs; enables the wrapping
  rule).
- **No semicolons** — newline-terminated statements with explicit
  continuation rules. "Semicolons or not" isn't a style; it's the
  grammar.

### Enforcement

- Format-on-save via the shipped LSP; `glide fmt -check` runs inside
  `glide test`, so unformatted code fails CI.
- **Not a compile error** — code is legitimately misformatted mid-edit;
  a compiler that yells about whitespace while you think is hostile.
  The compiler compiles; saving formats.
- Canonical formatting is migration infrastructure: `glide fix` rewrites
  produce zero-noise diffs when only one output is possible — breaking
  changes stay cheap.

## Unused variables & imports

Go's insight is right: **no warning-rot.** A codebase compiling with 400
warnings trains everyone to ignore #401, the real bug. Hygiene issues
must be errors *somewhere*. But Go makes them errors everywhere including
mid-debug — comment out one line to bisect and the compiler faults you
through a cascade (unused var → unused import → restore everything).
The `_ = x` incantation is the tell: an error routinely silenced by a
magic no-op is a warning with extra steps. Zig copied strict-everywhere;
it became their most resented decision. Same principle as formatting
enforcement: never block the edit loop for hygiene; never let hygiene
rot into what ships.

- **Dev builds: loud warning.** The bisect loop never breaks.
- **Release builds and `glide test`: error.** Unused code cannot ship or
  land — Go's guarantee preserved at the same boundary as formatting.
  (Tiered backends keep paying rent: overflow, backtraces, hygiene.)
- **Deliberate unused: `_` prefix** (`_conn`) — declared at the
  declaration, visible in review; not Go's `_ = x` silencing at a
  distance.
- **Humans don't curate imports.** goimports proved the import list is
  derivable from usage; Go's error enforces a list no human edits. The
  Glide formatter/LSP owns the import block: save, and it's correct.
  The release/test error exists underneath but is rarely seen.
- Scope: locals and imports only. Unused parameters stay legal (trait
  signatures force them). Dead declarations across module boundaries are
  a lint (`glide vet`-tier), not a compile decision.

## Declarations & ordering

**Declarations are a set; statements are a sequence — at every nesting
level.**

- **Module level: order-independent** (Go got this deeply right; kept
  exactly). No forward declarations, no arrange-the-file-for-the-
  compiler. File order becomes *narrative* order — important function
  first, helpers after; the formatter deliberately does NOT reorder
  declarations (sequence is the author's storytelling channel).
- **Nested `fn` allowed — and it does not capture** (Rust's rule).
  A nested fn is a module-level function with visibility shrunk to one
  body: explicit signature, recursion works, no access to enclosing
  locals. Fixes Go's wart pair: the recursive-closure two-step
  (`var f func(...)` pre-declaration) and helpers force-promoted to
  package level, where readers must assume any caller. A nested fn has
  *provable* single ownership — scope is information.
- **Not two ways to one thing — the distinction is semantic**: `fn` =
  explicit signature, never captures, at every depth; closures = 
  capture. The choice is forced by what you're doing (defer/errdefer
  doctrine). Python/JS capturing named functions refused: capture
  becomes invisible at the declaration and closures grow a second
  syntax.
- **Nested fns are items, not statements**: hoisted within their
  block, order-independent among themselves (mutual recursion works),
  callable from anywhere in the body — the module rule applied
  fractally. Reader's contract stays uniform: `fn` callable anywhere
  in scope; `let` exists only downstream of its line.

## Constants & module-level state

**Flexible consts (comptime-evaluated), and — load-bearing — module
level is `const` only: no module-level `let`, no `init()`, no life
before `main`.**

- Go's scalar-only consts pushed everything structured into `var` —
  mutable globals built at runtime — which then needed `init()`, which
  bred import-for-side-effect (`import _ "lib/pq"` running hidden
  registration). All downstream pressure from "const can't hold a map."
- `const` here is a binding evaluated at comptime (fuel-limited, no
  IO, deterministic — machinery recorded). `const table =
  make_crc_table()` — same function, either phase. Results land in
  read-only data: shared, zero startup cost, immutable by memory
  protection. `const re = regex.compile("…")`: bad pattern = compile
  error, automaton ships in rodata (vs Go's runtime MustCompile
  panic).
- **Imports are inert.** Importing executes nothing. Registration
  magic and what-runs-on-import: unrepresentable. (The sql design
  already assumed this: drivers passed explicitly.)
- **No initialization-order fiasco** (C++ static-init, Go's init
  graph): nothing runs before `main`. `main` is line one of runtime.
- Runtime state: created in `main`, passed down (the design's existing
  grain — injectable clock, explicit drivers). Rare lazy global:
  stdlib `Lazy<T>`, chosen by name (BigInt/Ref doctrine).
- Const names are `snake_case` like any binding — SCREAMING_CASE is
  C-preprocessor scar tissue; an earlier evaluation time is not a
  siren.
- Tar-pit guards already recorded: fuel, no IO, comptime-is-not-
  generics fence.

## Modules & visibility

- **`pub` keyword, not Go's capitalisation trick.** The trick works in Go
  only because nothing else competes for the case distinction. Here,
  pattern matching needs it: capitalised = type/variant/constructor,
  lowercase = binding. `match shape { Circle(r) => ..., point => ... }`
  is only readable if case tells you which names bind and which match.
  Go never hit this conflict because Go has no pattern matching.
  Second argument: with `pub`, a visibility change is a one-line diff that
  *says* "this became public" — reviewable; with capitalisation it's a
  whole-codebase rename.
- Accepted cost: no exported-ness signal at use sites. Small in practice —
  cross-module calls are already qualified (`http.get`), and within a
  module everything is accessible anyway.
- **Case convention is enforced by the formatter**: types and variants
  capitalised, functions/values/fields lowercase. Match arms are then
  unambiguous by rule, not habit.
- **Directory = module, Go-style.** All files in a directory share one
  namespace; no intra-module imports; no in-file `mod` declarations.
  Rust's module tree (`mod.rs`, `pub(super)`, path attributes) is ceremony
  that buys almost nothing.
- **Exactly two levels**: module-private by default, `pub` visible outside.
  No `pub(crate)` zoo. Struct fields take `pub` individually, so a public
  type with private fields is the natural encapsulation story. Wanting a
  third level usually means the module boundary is drawn wrong.

## Testing

Go's model is the best mainstream one for precise reasons, all kept:
**colocation** (tests live with code), **no framework** (a test is just
code; one universal command), **culture from tooling** (testing is table
stakes because the toolchain made it frictionless). Three upgrades:

- **Tests are a language construct** (Zig's form), not a naming
  convention: `test "list grows when full" { … }`. No magic prefix, no
  `t` parameter to thread, LSP lists them, and "tests excluded from
  release builds" is a statement about a language construct, not a
  filename pattern. Allowed in any file — inline next to small
  invariants reads as documentation; `_test.gld` files for larger
  suites. Placement is style, not semantics. Tests see module
  internals; black-box API testing is what a separate test module is
  for.
- **One compiler-known assertion — the testify war dissolved.** Go
  refused an assertion DSL (right instinct), landed on `t.Errorf`
  boilerplate, and the community made testify one of the most-imported
  modules — the stdlib lost the argument. The fix isn't 40 matchers:
  `expect(got == want)` is compiler-instrumented; on failure it reports
  the expression's parts (`left: 2, right: 3`). Power-assert semantics
  as a builtin, not a macro (Swift needed macros for `#expect`; we
  banned them; builtins are for exactly this). `expect` fails and
  continues; `require` fails and stops — Go's one good distinction,
  kept.
- **Benchmarks: the runner owns iteration.** Go's `b.N` protocol hands
  users the measurement loop and they get it wrong (Go conceded via
  `b.Loop()` in 1.24). `bench "name" { … }`: runner controls timing and
  count; setup sits before the body, naturally excluded.

Kept from Go: test caching (sounder here — hermetic comptime, no IO),
built-in coverage, fuzzing eventually (stdlib-tier, not v0), Example
blocks (see Documentation). Kept deliberately: serial within a module by
default, parallelism opt-in — default-parallel breaks every suite that
touches a port or a file; the race detector in `glide test` catches
sharing bugs without making the default flaky.

## Documentation

Go doc's core transplants directly; three weaknesses get fixed; one
popular "improvement" is rejected.

- **Docs are ordinary comments above the declaration.** No tag language —
  `@param`/`@returns` is write-only noise restating a signature that's
  already explicit. Prose says what the signature can't.
- **First sentence starts with the identifier name.** Load-bearing, not
  fussy: it's what makes one-line summaries readable in search, tooltips,
  and listings. Lint at test tier.
- **`glide doc` in the one binary**: terminal lookup + local HTML server.
  Uniform docs come from the tool being universal, not from diligence.
- **Markdown subset from day one** (headings, lists, fences, links). Go
  spent 12 years on plain text before conceding in 1.19.
- **Checked identifier links**: `[Config]` resolves against real
  declarations; a stale link fails at the test tier, same boundary as
  formatting and unused code. Docs that reference code must break when
  the code changes — Go's rotted silently for a decade as strings.
  (Yes: renaming a function can fail `glide test` over a comment. That's
  correct.)
- **Example functions, Go-style**: real compiled, output-checked code in
  test files, rendered into docs. Examples that cannot rot. Rust's
  doc-tests are rejected: runnable code inside comments is code invisible
  to the formatter, LSP, refactoring, and grep — a second place code
  lives, the exact thing we banned macros for.
- **Undocumented `pub` items: vet-tier lint, advisory not gating.** Full
  strictness breeds `// Foo does foo.` — worse than no doc because it
  occupies the space where a real one would go.
- Cost: `glide doc` is a real program (resolver-backed), not a comment
  scraper. Fine — it shares the compiler's AST and resolver; that's the
  payoff of one binary.

## Time & dates

Most datetime bugs are type errors the API refused to make
unrepresentable: a timestamp, a calendar date, and a wall-clock time are
different concepts. One type moonlighting as all of them (Go's
`time.Time`) manufactures bugs; `java.time` is the gold standard and the
one part of Java stolen here without irony.

Kept from Go:
- **`Duration` as a real type** with literals (`5.s`, `100.ms`) — never
  bare ints.
- **The monotonic hybrid**: `now()` carries wall + monotonic readings;
  elapsed math automatically uses monotonic. Kills the
  measured-a-timeout-across-an-NTP-step bug in the default path. Rust
  makes you choose correctly; Go makes the correct thing happen when you
  don't think. (Monotonic part drops on serialisation; equality is wall.)

The type set — four, sized by what backends store:
- **`Time`** — an instant on the physical timeline (UTC). Timestamps,
  logs, `created_at`.
- **`Date`** — calendar date, no time, no zone. Invoices, due dates.
  Go's lack of this is why every Go business app has a
  truncate-to-midnight helper and a latent timezone bug.
- **`TimeOfDay`** — 09:00. Schedules, opening hours.
- **`ZonedTime`** — civil time + IANA zone name. Critical: **future
  events are civil + zone, never instants** — "9am Sydney next March"
  isn't a fixed point until it happens (DST legislation moves the
  instant, not the 9am). Storing future appointments as UTC is the
  subtlest widespread datetime bug in production.

Conversions explicit, demanding what they mathematically require:
`date.at(tod).in_zone(sydney) -> Time`. No implicit assume-UTC —
Python's naive/aware mess is one type pretending to be two.

- **`Duration` vs `Period`** (Java's split): physical seconds vs
  calendar years/months/days with defined month-end rules. "Add 24h"
  and "add 1 day" differ across DST; "add 1 month" isn't physics. Two
  types because two operations — the defer/errdefer doctrine.
- **Format tokens: `"YYYY-MM-DD HH:mm"`** (Java/Moment family), not
  Go's mocked reference date, not strftime's `%`-cryptics.
  Compile-checked for literal patterns; constants for RFC 3339;
  **Display = RFC 3339** — the debugging string is the wire string.
- **IANA tzdata embedded by default** (updatable; opt-out). Static
  binaries in scratch containers must not discover zoneinfo is missing
  at runtime — Go's opt-in embedding is the wrong default for the
  static-binary century.
- **The clock is injectable**: stdlib time flows through an ambient
  `Clock`; `glide test` can freeze or step it. Go's untestable global
  `time.Now` is why serious codebases wrap time and time-tests flake.
  Testable time is a stdlib duty, not user cleverness.

## Logging

Structured, stdlib, day one (Go's 14-year journey to slog is the whole
argument). The design:

- **Constant message + typed fields**:
  `log.info("user logged in", { user_id: id, ip: req.addr })`. The
  message is an event name — greppable, aggregatable — not a sentence
  with data in it. **Interpolated log messages are a lint** (the one
  place interpolation is an antipattern): infinite-cardinality messages
  can't be grouped, counted, or alerted on.
- **Fields are a checked literal, not slog's alternating varargs**
  (odd-length lists and key typos at runtime, papered over by a vet
  check). Anonymous-struct-shaped: arity is grammar, keys are
  identifiers, values implement the log-value trait — comptime-checked,
  serialised by the JSON derive machinery. No reflection; zap's
  zero-alloc heroics were working around reflection we don't have.
- **Four levels: debug, info, warn, error.** No trace (nobody applies
  the distinction consistently). No fatal — `log.Fatal` is a hidden
  `os.Exit` inside a logging call, control flow disguised as
  observability, skipping defers. Log, then exit, in two honest lines.
- **Adaptive default output**: pretty on a TTY, JSON lines when piped
  (what docker logs → aggregator consumes). **Configuration is code,
  only code** — no config files, no env DSLs. Log4Shell was a logging
  config feature; a logger is five lines in `main`.
- **The logger is ambient and scope-inherited** — a Logger parameter in
  every signature is coloring again, ctx's disease. The one legitimate
  non-cancellation thing ctx carried (request-scoped values) lands
  here: `scope(log: { request_id: id }) { … }` — fields attach to the
  scope, child tasks inherit, every log line in the subtree carries
  them. HTTP server does this per request: correlation with zero
  plumbing, riding the structured-concurrency tree. `glide test`
  captures the stream per-test: failures print their lines, passes
  stay silent.
- **The principled line on ambience**: logging is observation, not
  behaviour — no program result may depend on what the logger did.
  That's why an ambient logger is fine and ambient config never is.
  Recorded so the precedent doesn't generalise.

## JSON

Go's `encoding/json` is five diseases: runtime reflection; stringly
struct tags (`json:"nmae"` ships); missing field → silent zero value
(pointer fields reintroduce nil to fake Option); case-insensitive field
matching (security-relevant surprise); `map[string]interface{}` for
anything dynamic. Four were cured upstream here:

- **`derive Json` at comptime** — the marquee use case cashing in.
  Encoder/decoder generated as plain code, optimised like hand-written.
  serde-class speed, no proc-macro second compiler.
- **Options are typed comptime arguments, never string tags.**
  `derive Json(rename_all: camel)` type-level; checked per-field
  annotations for the rest. Typo'd option = compile error. (Annotation
  syntax lands with the grammar; the principle is the decision.)
- **Required-by-default falls out of mandatory init + Option.** `Int`
  field absent from input → decode error with a good message. `Int?` →
  may be absent or null. Absent-vs-zero is unrepresentable; no tag
  needed. (`T?` collapses JSON's missing-vs-null tri-state — what every
  API wants; rare protocols that distinguish get an explicit wrapper.)
- **Exact-case matching, always. Unknown fields ignored by default,
  `strict` opt-in** (right default for evolving APIs; strict for config
  files, where "you typo'd `porrt`" beats silence).
- **Dynamic JSON is a sum type** — `Null | Bool | Number | String |
  Array(List<Json>) | Object(Map<String, Json>)`. Exhaustive match over
  shapes replaces type-assertion ladders; sum types' best demo.
  Insertion-ordered Map preserves key order on round-trip.
- **Numbers keep their lexical form.** Go's decode-to-float64 silently
  corrupts int64 IDs past 2⁵³. `JsonNumber` holds the digits;
  `as_int() -> Int?` / `as_float()` convert where chosen. Optional
  string-encoding of big ints for JS-facing APIs.
- **Rejected: serde-style format-generic framework.** Elegant, but the
  30-method trait dance is the enterprise abstraction we keep declining.
  Each format is its own comptime derive over the same reflection API;
  if duplication across four formats ever hurts, the shared-walk
  refactor fits behind the derives without touching user code.

## Standard library

**Batteries included — because nothing is frozen.** The three-way
evidence: Rust's minimal std → 300 transitive deps per web service,
each a supply-chain surface (the opposite of auditable). Python froze
its batteries and they died on the shelf (`urllib` is stdlib; everyone
installs `requests`; PEP 594 hauled out corpses). Go proved the upside —
stdlib `net/http` made Handler the ecosystem's shared currency — but
v1 chains it to fossils (`container/list`, `math/rand` v2, crypto
strata). Synthesis: stdlib versions with the language, `glide fix`
migrates callers mechanically (what canonical formatting was for), and
wrong modules get fixed, not embalmed beside their replacements.
Python's disease wasn't batteries; it was batteries plus immortality.

**In** (principle: what a networked backend or CLI needs, audited once,
by us): HTTP server+client, TLS, crypto, JSON, time (steal Go's
monotonic-clock handling), fs/os/process, testing, **regex with RE2
semantics — linear-time guaranteed, no backtracking** (Go's
least-celebrated great decision: ReDoS unrepresentable; lookbehind is
not worth readmitting it), **structured logging day one** (Go took 14
years to ship slog), HTML templating **with contextual auto-escaping**,
flags/CLI, base64/hex, compression, thin `database/sql`-style interface
(drivers as packages, **no ORM ever** — an ORM is a language dispute,
not a battery).

**Crypto: misuse-resistant by default** (libsodium/age philosophy):
`seal`/`open`, `hash`, `sign`/`verify` — right construction chosen;
primitives underneath, marked as the sharp layer. **One rng: `rand` is
cryptographically secure, period.** Go's math/crypto rand split has a
CVE trail; the fast insecure generator is the specialist tool, so it
gets the scary name (`rand.insecure_fast()`).

**Out, permanently**: GUI, ORM, ML, image processing, XML beyond
minimal, SMTP-sending in core (protocol clients with living ecosystems
age badly in stdlibs — Go's net/smtp rotted; see extended library).
Out-list discipline matters as much as the in-list.

### HTTP specifics

- **Handlers return values: `fn(Request) -> Result<Response, Error>`.**
  Go's `(w, r)` shape can't return errors — hence reinvented
  error-middleware and the forgotten-`return`-after-`http.Error` bug
  class. Returned Response composes: `?` propagates to one
  error-to-status mapping; middleware is `fn(Handler) -> Handler`.
  Streaming: Response body can be a reader/generator (iterators pay).
- **Router in stdlib at Go-1.22 level** — methods + wildcards. Shared
  currency includes routing; not a DSL, not a framework.
- **Client defaults are production defaults.** Go's default client has
  no timeout — infinite hang out of the box, an incident generator.
  Ours: timeouts set, sane pooling, and cancellation ambient via scopes
  — `scope(timeout: 5.s) { http.get(url)? }` cancels the request when
  the scope dies. The ctx-replacement covers HTTP for free.
- Green thread per request; HTTP/2 in; HTTP/3 when it earns entry.

## Build & CLI tooling

- **No build scripts. Ever. This is the hill.** Cargo's `build.rs` and
  npm lifecycle scripts mean compiling someone's code executes their
  program on your machine — most supply-chain attacks in both
  ecosystems ride exactly this, and hermeticity dies with it. Go proved
  builds don't need it. `glide build` executes no user code, reads
  nothing outside module tree + vendor, touches no network. Codegen is
  a step you run, visibly, committed (Makefile territory).
- **The manifest is data, not a program.** `glide.mod`: module name,
  toolchain pin, deps with hashes. No scripts, hooks, profiles, or
  feature flags (Cargo features = 2ⁿ build variants, most never
  compiled by anyone). Conditional compilation: platform-suffix files
  (`net_linux.gld` — whole-file, greppable, no preprocessor) +
  comptime constants (`if target.os == .linux`) for small forks.
- **Cross-compilation is a flag**: `glide build -target linux/arm64`,
  any host → any target, no sysroots. Stays trivial *because* FFI was
  exiled — cgo is what collapses Go's story. Static by default,
  `FROM scratch`-ready.
- **`embed` is declarative** — grammar, not Go's magic comment:
  `embed "static/**" as assets` names files in the module tree; the
  build system provides bytes; comptime never does IO, hermeticity
  survives. Embedded trees serve through the same `fs` interface as
  disk — dev-from-disk vs prod-from-binary is one constructor swap;
  content hashes at build give the immutable-URL caching story.
- **Toolchain pinning** (Go's 1.21 lesson, from day one): manifest pins
  the Glide version; newer toolchains build *as* the pinned one or
  refuse. Breaking-changes-are-free makes pinning more necessary, not
  less.
- **Script mode**: `glide run tool.gld`, shebang
  `#!/usr/bin/env glide run` — the interpreter makes type-checked
  scripting nearly free. **REPL**: likely interpreter byproduct, not a
  commitment (REPL semantics in a static language is real design work).
- **Command surface, closed**: build, run, test (contains fmt-check,
  lints, race detector, examples — the enforcement boundary), fmt, vet,
  doc, fix, get/tidy, version. No plugin system, no `glide-*`
  subcommand discovery — cargo's extension mechanism reopens the
  arbitrary-code door.

## Packages & the extended library

- **Imports are URLs, no central registry** (Go's model, minus the proxy
  to start): `import "github.com/…"` resolves via git. Registries are
  infrastructure to run and a supply-chain single point of failure;
  decentralised imports + lockfile hashes give integrity without a
  middleman. A read-through proxy/sumdb is a later scaling concern (Go
  added theirs a decade in).
- **Vendoring by default**: manifest names deps, `vendor/` contains
  them, builds read only from vendor. No network at build time; the
  vendored tree is the auditable artifact — what you read is what
  links.
- **The `x/` porch** (`glide.x/smtp`, `glide.x/mail`): first-party code
  — same authorship, review bar, tooling, `glide doc` — outside the
  toolchain distribution and its stability promise, versioned
  independently, allowed to churn at the speed of the outside world.
  That cadence is the point: SMTP's pain isn't the four-verb protocol,
  it's the living periphery (XOAUTH2, TLS posture changes, provider
  quirks). Go froze `net/smtp` at 2011 assumptions and it rotted *in*
  the stdlib — location doesn't preserve batteries, maintenance cadence
  does. `x/mail` owns the real 90% (MIME multipart, attachments, header
  folding — the part `net/smtp` never did, hence gomail). `x/` modules
  are ordinary packages, no magic import handling. Stdlib entry is
  earned by "every backend needs it", not "it's useful" — the porch is
  for useful.

## SQL & databases

No ORM (recorded), no query-builder DSL (a worse SQL that compiles to
SQL serves the library, not you). Make raw-SQL-plus-mapping ergonomic in
stdlib; leave schema-codegen open outside it.

- **`derive Row`** — comptime reflection's third customer. Column-name
  mapping generated at compile time:
  `q.query<User>("select … where org = :org", …)`. Go's positional
  `rows.Scan(&u.ID, &u.Name)` silently misassigns on reorder; sqlx-style
  runtime reflection banned anyway.
- **NULL dies like absent-JSON died.** `sql.NullString` is the
  zero-value disease in a database costume. Nullable column = `String?`
  field; NULL into non-Option = decode error. One doctrine, third
  application.
- **Named parameters, comptime-verified — the unoccupied sweet spot.**
  Schema checking needs a DB; *placeholder* checking needs only the
  literal query string — pure comptime parse, no IO, hermetic. `:org`
  must match the argument struct's keys: missing/extra/typo'd params
  are compile errors (Go finds them in production as `sql: expected 2
  arguments, got 3`). One canonical placeholder syntax; drivers
  translate (`?`-vs-`$1` roulette is interface negligence).
- **No live-schema checking, on purpose.** Rust's sqlx validates
  against a running DB at compile time — build now depends on a DB
  being up and migrated; the hermeticity we defend in three sections.
  Schema-aware codegen goes the sqlc way: schema in repo, explicit
  codegen, committed output — the schema becomes a versioned artifact,
  not a compile-time network dependency. `x/` tool eventually.
- **Transactions are a closure**: `db.tx(|tx| { … })` — commit on Ok,
  rollback on Err or panic. Go's `defer tx.Rollback()` idiom
  (rollback-after-commit as silent no-op) becomes structure.
- **No `QueryContext` zoo.** Go duplicated its whole API to retrofit
  cancellation; scopes make it ambient — queries are cancellation
  points, one method name each.
- **Drivers are packages** against a small driver trait. Postgres/MySQL
  speak wire protocols — pure Glide, cross-compilation untouched.
  **SQLite is C — FFI's first paying customer**; different
  cross-compile ergonomics, exactly like cgo-sqlite in Go. (Interpreter
  era: the Go-hosted interpreter can shim SQLite via its host — see
  implementation notes. Pure-Glide SQLite is someday-maybe, a heroic
  project.)

## Tooling & ecosystem philosophy

- One binary: compiler, formatter, test runner, doc generator, LSP,
  package manager.
- **Vendoring by default.** A dependency is a liability to justify.
- Culture is set by stdlib scope, not by policy documents.

## Sketch

```
fn fetch_user(id: UserId) -> Result<User, ApiError> {
    let resp = http.get("/users/{id}")?      // ? propagates the error
    match resp.status {
        Ok        => resp.json<User>()
        NotFound  => Err(ApiError.Missing(id))
        s         => Err(ApiError.Unexpected(s))
    }                                         // exhaustive: `s` binds the rest
}
```

## Deliberate sacrifices (recorded so we stop re-litigating them)

1. The last ~20% of performance — no pervasive borrow checker.
2. Hard realtime — GC pauses are small, not zero.
3. Easy C FFI — green threads and GC make the boundary ugly.
4. Structural typing's low-ceremony feel — traits require explicit impl.
5. Macros — comptime only.

## Ambient state (task-local storage): a closed set

**No general goroutine/task-local storage.** Go's refusal was right;
its escape hatch proves the disease — `ctx.Value` is an untyped,
stringly grab bag where auth, transactions, and loggers travel
invisibly. Java's 25-year arc (ThreadLocal → framework load-bearing
wall → broken by virtual threads → Loom's ScopedValue) taught: if it
exists at all, it must be scope-shaped. Ours is, and it's closed:

1. **Cancellation/deadlines** → scopes (read via `s.deadline()` —
   explicit handle, not hidden).
2. **Observation** (log fields, trace/span context, metrics) → scope
   fields, inherited by children.
3. **Clock** → ambient Clock, injected in main, swapped by tests.

Everything else — auth principal, tenant, transaction, request config
— is behaviour-affecting and travels in parameters or struct fields,
visibly. **The droppability razor** (the logging section's line, now
enforceable): could program output change if this ambient value were
dropped? No → observation, ambient OK. Yes → parameter. Middleware
case answered honestly: the authenticated user rides `req.principal`
— typed, visible on Request — which is what well-factored Go does
after the second ctx.Value bug anyway. Performance TLS (RNG shards,
pool caches) is the runtime's private business under stdlib
primitives; never a language surface.

## Debugging

Line-by-line debugging, staged with the implementation path:

- **Interpreter era: DAP server in the interpreter** (Debug Adapter
  Protocol — LSP's sibling, universal). A tree-walker is a debugger
  that hasn't been asked: breakpoints = per-statement check, stepping
  = eval-loop flags, inspection = the environment we already hold.
  Weeks, not months; every editor gets it free; one-binary doctrine.
- **Transpiler era — day-one design constraint, not a retrofit: emit
  `//line` directives and preserve identifier names.** Go's line
  directives exist for exactly this; delve then debugs Glide at
  `.gld`-line granularity with sane names, and its goroutine
  awareness covers Glide tasks (they ARE goroutines). Twenty years of
  debugger investment ridden for two transpiler disciplines. Honest
  seam: stepping inside desugared constructs (generator state
  machines) — keep lowering line-faithful.
- **Native era**: full DWARF is the folklore-sized cost, on the
  far-mountain timeline, with the standard cut: **complete debug info
  is a dev-tier guarantee; release is best-effort** (tiered builds
  pay again).
- **The scope tree beats the goroutine list**: debugger shows
  `main → serve → request → query` — structure mandated for other
  reasons, surfaced. Delve's 40,000 flat anonymous goroutines is the
  anti-pattern.
- **Deterministic-seed replay is the concurrency debugging story**: a
  race caught in `glide test` is a seed; stepping through the replay
  reproduces the exact interleaving — the bug class where debuggers
  normally fail, covered by two recorded decisions touching.
- Balance note: the design leans away from daily debugger need
  (return traces, per-test logs, expect diffs, typed holes); the
  debugger is for the 5% watching state evolve.

## Borrowed from the wider world

Beyond the Go/Rust/Swift/Zig quadrilateral. Six adopts (five ride
machinery we already own — the compounding-interest phase), one flag,
four declines:

- **Distinct types** (Nim): `type UserId = distinct Int` — same
  representation, zero cost, no implicit conversion. UserId≠OrderId,
  metres≠seconds as compile errors. Closes the hole the day-one sketch
  opened (`UserId` never specified). Cheapest safety-per-character in
  the document.
- **Deterministic scheduling in tests** (FoundationDB/TigerBeetle
  lineage): seeded deterministic scheduler mode in `glide test` —
  failing interleavings become rerunnable seeds; the runner fuzzes
  schedules hunting races. Possible because we own scheduler + clock +
  hermetic builds; Go/Rust can't without heroic external simulation.
  Possibly Glide's most differentiating capability.
- **Property-based testing in stdlib** (QuickCheck, 30 years proven,
  never mainstream due to setup friction): test blocks with generated
  params, shrinking, printed seeds; `derive Arbitrary` via comptime
  deletes the friction. Composes with schedule fuzzing.
- **Error return traces** (Zig's real novelty): record the chain of
  `?` propagation points an error travelled, not just where it was
  created. Dev-tier, rides the backtrace machinery.
- **Typed holes** (Haskell/Idris): `?` in expression position →
  compiler reports needed type + in-scope candidates, keeps checking.
  Judge → collaborator. LSP/compiler UX, no language surface.
- **Persistent collections** (Clojure): stdlib `PList`/`PMap` with
  structural sharing — the concrete meaning of the mutability
  section's promised "immutable stdlib data structures". Module, not
  default (Clojure's defaults-everywhere constant factor not paid
  silently).
- **Flagged for stdlib: supervision policies** (Erlang) —
  `supervise(restart: .on_failure, backoff: …)` scope variant;
  let-it-crash landing on structure already built. For always-on
  services, likely the first-used adopt.

Declined: **pipe `|>`** (method chains are the pipeline; second
spelling); **extension methods/UFCS** (fragments "what can this type
do" by import; trait impl is the sanctioned mechanism); **algebraic
effects** (coloring generalised — rejected the special case on
purpose); **content-addressed code** (Unison — most original idea in
modern design; abandons files, and with them grep/git/diff — the whole
auditability story); **Pony reference capabilities** (full race-freedom
costs more human budget than the borrow checker; our 90% at 10% the
concepts).

## Implementation path & self-hosting

Two mountains wearing one name; the compiler is a hill with a shortcut,
the runtime is the actual decade — deferred indefinitely by the
shortcut.

The bootstrap sequence:
1. **Go tree-walking interpreter** — proves semantics (generators,
   structured concurrency, comptime reflection are the three riskiest).
   Stdlib as host shims (Go code behind Glide interfaces — which is
   also how SQLite works day one, via modernc).
2. **Compiler frontend written in Glide** (lexer/parser/checker), run
   on the interpreter. Dogfooding starts here — compilers are the
   best-case workload for this feature set (ASTs are sum types, a
   checker is exhaustive matching, `?` threads the phases; the ML
   family was designed for this). The compiler in Glide will be nicer
   code than the interpreter that runs it.
3. **First native backend: a Glide→Go transpiler**, which then
   compiles itself; the interpreter retires to bootstrap seed. Our
   runtime model was Go's from day one — green threads, tracing GC,
   defer, channels, value structs — so Glide lowers onto Go source
   nearly 1:1 and every hellish part is prepaid: GC + scheduler (Go's,
   battle-tested), cross-compilation (GOOS/GOARCH), static binaries,
   readable/debuggable output. Auditable bootstrap chain from a
   mainstream toolchain — no binary seed, no trusting-trust anxiety.
   Resolves the tiered-backend plan for years: interpreter = dev tier,
   transpiled Go = shipping tier.
4. **Someday, optionally: LLVM + own runtime** — the real mountain.
   Runtime code runs underneath the language's guarantees (no-GC
   unsafe dialect, compiler special-casing — Go's runtime is Go with a
   hundred pragmas). Only mandatory when we want our own release
   backend; halfway camps exist (LLVM code against a minimal C or
   Go-linked runtime). The unsafe section's primitives (raw pointers,
   pinning, moving-GC coupling) are exactly what this needs.

Known wrinkle to de-risk **early**: lowering Glide semantics Go lacks —
sum types → tagged structs (mechanical), matches → switches
(mechanical), **generators → goroutine pairs or CPS (the fiddly one —
prototype before depending on the transpiler)**, dev-tier overflow
traps → explicit checks in emitted code.

## Open questions (decide before/while building)

- (none currently — all original questions resolved; new ones land here
  as they arise)
