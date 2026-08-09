# Glide's lineage — where each piece came from

`DESIGN.md` records what was decided and why. This file records where
each piece *came from*: the short history of the feature, who invented
it, who adopted it, who tried living without it, and what that
evidence says. The point is confidence — nearly nothing in Glide is
novel, and that's the pitch. Every entry should let a reader conclude
"this survived decades of contact with real programs before we took
it."

Maintenance rule: when a decision lands in `DESIGN.md`, its lineage
entry lands here (see `CLAUDE.md`). Entries are written for a reader
who knows Go and nothing else. Order follows `DESIGN.md`'s sections.

## Compile speed & tiered backends

- **1983** — Turbo Pascal compiles faster than competitors can open a
  file, on a Z80. Anders Hejlsberg proves compile speed is a *product
  feature*; a generation of Delphi programmers never forgets.
- **1990s–2000s** — C++ normalises hour-long builds; the industry
  mistakes this for the price of native code. JIT VMs (HotSpot, 1999;
  V8, 2008) quietly demonstrate *tiering*: a fast baseline compiler
  for responsiveness, an optimising tier for hot code.
- **2009** — Go makes sub-second builds a headline feature again —
  arguably its most underrated property — via ruthless language design
  (no headers, strict dependency DAG, fast parser).
- **2010s** — Rust and C++ trade compile time for optimisation and
  regret it publicly; Rust experiments with Cranelift as a fast debug
  backend — tiering for AOT, never quite shipped as default.

**Glide takes** tiering as an AOT commitment from day one: a fast
custom backend for dev builds, LLVM for release. Nobody has shipped
this well yet; the bet is that the "fast compiler or fast code"
dilemma is a false choice you dissolve by refusing to pick one
backend. Accepted cost: two backends to maintain.

## Memory: GC, and the borrow checker declined

- **1959** — John McCarthy invents garbage collection for Lisp. The
  idea is older than structured programming.
- **1990s–2010s** — generational, then concurrent low-pause
  collectors mature (Java's G1/ZGC lineage; Go's sub-millisecond
  collector, 2015+). GC's cost model becomes well understood:
  proportional to allocation rate × live heap — death is free, birth
  and survival cost.
- **early 2000s** — Cyclone (AT&T/Cornell research C) prototypes
  region-based memory and ownership annotations — the direct ancestor
  of Rust's lifetimes.
- **2015** — Rust 1.0 ships the borrow checker: compile-time memory
  safety *and* data-race freedom, no GC. The achievement is real; so
  is the cost — fighting the borrow checker is the single most cited
  reason people bounce off Rust, and a decade later "async + lifetimes"
  is still described by Rust's own maintainers as a second, harder
  language.
- **2016+** — Zig demonstrates the middle path's other end: explicit
  allocators everywhere, arenas as idiom.

**Glide takes** GC by default (a good concurrent collector is fine for
95% of programs) with ownership as a *local, opt-in* tool for hot
paths — arenas for group-death ("everything in this request dies
together"), non-copyable owned types in marked code. Declined:
manual `free` even as a hint (a tracing GC never visits dead objects;
announcing a death files paperwork with an agency that wasn't going
to inspect you). Recorded sacrifice: Rust's no-data-races guarantee
and the last ~20% of performance.

## The unsafe boundary

- **1978** — Modula-2 puts dangerous operations behind a `SYSTEM`
  module you must import: unsafety as a visible, greppable dependency.
  The idea every safe language since has rediscovered.
- **2000** — C# ships `unsafe { }` blocks: line-granular marking,
  mainstream for the first time.
- **2009** — Go revives the import model (`import "unsafe"`) —
  auditable by grep, but file-granular (one import licenses the whole
  file) and governed by folklore (the six Pointer patterns).
- **2015** — Rust lands the full discipline: `unsafe { }` marks exact
  lines, `unsafe fn` propagates the obligation through signatures, and
  — the real achievement — a *culture* where unsafe exists only to
  build safe abstractions. cargo-geiger (2018) adds the missing
  supply-chain view: which dependencies use unsafe.

**Glide takes** both layers: Rust's granularity (`unsafe fn` +
`unsafe { }`) and Go's auditability upgraded to the manifest (modules
containing unsafe are flagged; the dependency report diffs unsafe
usage between versions — cargo-geiger built in). And less to mark:
comptime serialization never reflects, layout queries are comptime,
so unsafe shrinks to its honest job — the machine and C.

## Type system: explicit signatures, local inference

- **1969/1978** — Hindley, then Milner, give ML full type inference:
  the compiler deduces every type in the program, annotations
  optional. The famous party trick of the ML family.
- **1990s–2000s** — Haskell demonstrates the cost at scale: an error
  detected far from its cause, reported in unification vocabulary.
  Whole-program inference is why Haskell errors baffle; the community
  convention becomes "annotate top-level signatures anyway".
- **2007–2011** — the mainstream converges on the compromise from the
  other direction: C# `var` (2007), C++ `auto` (2011), Go `:=`
  (2009) — inference *inside* function bodies, explicit signatures at
  boundaries.

**Glide takes** the convergence point as doctrine: signatures always
explicit (they are documentation, and they keep errors pointing at
the right line), inference local to bodies. The ML *data model* came
along (sum types, matching); the ML *inference discipline* was
deliberately left behind.

## Sum types & `match`

- **1970s** — Edinburgh. Rod Burstall's NPL and Hope, then Robin
  Milner's **ML** (1973), introduce algebraic data types — "a value is
  exactly one of these shapes, each with its own payload" — and the
  construct that consumes them: pattern matching with compiler-checked
  exhaustiveness. The two are one feature: sum types build the
  either-or, `match` takes it apart.
- **1980s–90s** — every ML descendant carries both: Standard ML,
  OCaml (`match … with`), Haskell (`case … of`), Miranda, Erlang.
  Twenty years as a functional-languages-only feature.
- **2000s** — Scala and F# carry it toward the mainstream.
- **2010s–20s** — the dam breaks. Rust makes `match` a headline
  feature of a systems language (Glide's spelling — `pattern =>` arms
  — is Rust's). Swift rebuilds `switch` around patterns (2014). C#
  grows patterns from 2017, Python adds `match` in 3.10 (2021), Java
  lands switch patterns in 21 (2023). The most-adopted language
  feature of the decade.

Coming from Go, `match` sits in `switch`'s seat but differs in three
load-bearing ways: it **destructures** (`Circle(r) =>` tests and
extracts atomically, where a type-switch tests and leaves extraction
to you), it's **exhaustive** (a missing variant is a compile error,
not a silent fall-through — the property no-null and enumerable
errors lean on), and it's an **expression** (arms yield values; no
mutable variable assigned in every case).

**Glide takes** the ML construct with Rust's spelling — plus Go's
good ideas (multiple values per arm, the subjectless `match` as a
clean if/else-if chain) — and skips Rust's multi-year "match
ergonomics" saga (ref/binding-mode rules) entirely, because no borrow
checker means patterns just bind values and the GC holds them.

## No null, `Option`/`T?`, and `??`

- **1965** — Tony Hoare adds the null reference to ALGOL W "because
  it was so easy to implement". In 2009 he apologises for it by name:
  "my billion-dollar mistake".
- **1973** — ML never needs it: absence is a sum type,
  `Option = Some(x) | None`, and the exhaustiveness checker forces
  the `None` case to be handled. The fix is *older than the languages
  that have the bug* — C, C++, Java, Go all shipped the mistake after
  the correction existed.
- **2005** — C# 2.0 ships the null-coalescing operator `a ?? b`;
  Kotlin later spells it `?:` (the "elvis operator"), Swift adopts
  `??` (2014), JavaScript in ES2020. Same shape everywhere: value if
  present, else the fallback, right side lazily evaluated.
- **2011–14** — Kotlin and Swift prove `Option` can wear lightweight
  clothes: `T?` as the type spelling, sugar for the common
  operations. This is what made no-null *ergonomic* enough to go
  mainstream rather than remaining an ML-family virtue.

**Glide takes** ML's semantics with Swift/Kotlin's spelling: `T?` is
sugar for `Option<T>`, `??` is the coalescing operator retargeted at
Option (there's no null to coalesce), and the unwrap toolkit —
`if let`, `let … else`, `match` — replaces Go's zero-values-and-hope.
The map read `counts[word] ?? 0` is the emblem: Go hands back `0`
whether or not that's sensible; Glide makes you say it is, at the
site, every time. Mandatory initialisation is the same decision
applied to structs: Go's `User{}` with an empty ID is null wearing a
struct costume.

## Errors as values: `Result`, `?`, `context`

- **1990s** — Haskell's `Either` establishes failure-as-sum-type.
- **2009/2012** — Go commits to errors-as-values in a mainstream
  language: no exceptions, failure visible in signatures. Right
  philosophy, half the machinery — failure modes can never be
  enumerated (`error` is dynamic), and `if err != nil { return err }`
  is the tax on every call.
- **2012–16** — Rust ships `Result<T, E>` (the enumerable half Go
  lacks), discovers the propagation boilerplate anyway (`try!`
  macro), and graduates it to the `?` operator (stabilised 1.13,
  2016) with implicit error conversion at exactly that point — after
  trying life without conversion: "nobody would go back".
- **2016–19** — Rust's ecosystem churns through error-library
  generations — `error-chain` → `failure` → `anyhow`/`thiserror` —
  eight years of discovering the two-sided answer: libraries want
  enumerated sum-type errors, applications want one dynamic error
  with a context chain. Go independently validates context chains
  with `%w` wrapping (1.13, 2019) — right semantics, wrapping
  controlled by a format verb.

**Glide takes** both proven halves, in the stdlib on day one:
sum-type errors for libraries (failure modes enumerable, `match`
exhaustive), dynamic `Error` with a `.context()` chain for
applications (anyhow's lesson, made official), `?` with conversion
firing only at the propagation point, and — after the fight GRAMMAR.md
asked for — no dedicated handle-in-place construct: the sketched
`or |e|` (Zig's `catch |err|`, the near-precedent) was declined
because `?`-conversion covers its flagship wrap-and-propagate use,
`??` on Result covers fallback, and `match` carries the rare inline
inspection. Deferred with a count-the-residue test, the `++`
methodology. Panics kill the task, not the process, and
there is no `recover` — structured concurrency gives failures a
principled boundary, so the escape hatch Go's unstructured goroutines
made necessary is deleted rather than inherited.

## Closures & one function type

- **1936/1958** — Church's lambda calculus; Lisp makes anonymous
  functions real code. The "funarg problem" (what does a nested
  function see?) takes until **Scheme (1975)** to solve properly:
  lexical scoping, close over the defining environment.
- **1980** — Smalltalk blocks `[:x | x * 2]` put the
  parameters-between-delimiters spelling on the map; **Ruby (1995)**
  carries the pipes into mainstream syntax; **Rust** adopts `|x| x*2`.
  That's the glyph lineage Glide sits in.
- **2007–14** — the mainstream retrofit wave: C# lambdas, JS arrow
  functions, Java 8 (2014) — every language learns that closure
  ceremony determines whether a map/filter culture forms.
- **2009** — Go charges 40 characters to double a number
  (`func(x int) int { return x * 2 }`) — half the reason Go never
  grew that culture.
- **2015** — Rust's `Fn`/`FnMut`/`FnOnce` + `move` + `Box<dyn Fn>`
  zoo demonstrates what closures cost *with* a borrow checker.

**Glide takes** Rust's pipe syntax with inference, and — the GC
dividend — **one function type**: `fn(Int) -> Int` covers named
functions, closures, and method values alike; whether something
captured is representation, not type. Declined: trailing closures
(Swift/Kotlin's gateway to builder DSLs) and `$0`/`it` (naming the
parameter is documentation). One new rule with no direct precedent:
closures crossing task boundaries must not capture `mut` bindings —
the data-race archetype made a compile error.

## Named arguments & defaults

- **1980/1983** — Smalltalk keyword messages and Ada named parameters
  prove call-site labels prevent argument transposition; Objective-C
  carries the idea for forty years.
- **1991** — Python ships defaults + keyword args — and the
  evaluated-once-at-definition mutable default becomes the language's
  most famous gotcha, still biting thirty years later.
- **2009–14** — Go rejects defaults, named args, *and* overloading;
  the ecosystem's answer is the functional-options pattern (Pike and
  Cheney's 2014 blog posts) — 30 lines of ceremony per configurable
  function to express what the signature should have said.
- **2011–14** — Kotlin, Dart, and Swift converge on the same design
  independently: defaults evaluated per call, any parameter nameable
  at any call site. Convergent evolution is strong evidence.

**Glide takes** the Kotlin model: per-call defaults (Python's mistake
inverted), any param nameable, parameter names becoming API —
consequence owned. Overloading stays banned (Go's instinct was right;
resolution swamps) but now painlessly: defaults + named args cover
overloading's legitimate 90%. Swift's external/internal name split
declined — one name, already documentation.

## Traits: declared conformance, structural satisfaction

- **1989** — Wadler & Blott's "How to make ad-hoc polymorphism less
  ad hoc" invents type classes in Haskell: interfaces resolved at
  compile time, implementable *after* the type exists.
- **1995/2014** — Java interfaces demonstrate nominal conformance at
  scale — and the evolution problem: an interface can never grow.
  Java 8's default methods (2014) are the admission and the fix,
  nineteen years late.
- **2009** — Go bets on structural typing: any type with
  `Close() error` *is* a Closer. Low ceremony, two real diseases at
  scale: accidental conformance and undiscoverable implementations.
  And Go interfaces still can't grow — its stdlib interfaces are
  fossils.
- **2014** — Swift quietly ships the synthesis: conformance is
  *declared* (`extension TcpConn: Reader {}`) but satisfaction is
  *structural* — existing methods count; you write only what's
  missing. Rust's orphan rule (impl your trait or your type, never
  foreign-for-foreign) supplies the coherence guarantee.

**Glide takes** Swift's declared-conformance/structural-satisfaction
split with Rust's orphan rule and Java's default methods: one
greppable `impl Reader for TcpConn {}` line, no forwarding
boilerplate, interfaces that can grow without breaking implementors.
Go's genuinely good ideas survive explicitly: consumer-defined
interfaces and small-interface culture. **No inheritance at all** —
Simula (1967) invented it, the Gang of Four (1994) already advised
"favor composition", and it's the feature every ecosystem regrets by
year five; traits + composition are the whole story.

## Numbers: sized ints, signed sizes, honest literals

- **1972** — C's `int` means "whatever the machine likes"; forty
  years of code breaks on width assumptions. Java (1995) fixes the
  sizes but keeps silent overflow; C's promotion lattice
  (`i32 + i64` quietly widens) becomes a 40-year bug factory.
- **1990s–2010s** — unsigned sizes (`size_t`) get their verdict from
  C++'s own architects: using unsigned for sizes doesn't prevent
  `len - 1` underflow, it launders it into eighteen quintillion.
  Publicly called an irreversible mistake; Rust's `usize` inherits it
  anyway and adds `as usize` confetti.
- **2009** — Go: fixed-size names, platform-sized `int` (the "works
  on the mac, overflows on the deploy box" residue), untyped
  constants — arbitrary-precision literals that materialise on
  landing. That last idea is quietly excellent; Zig's `comptime_int`
  (2016+) is the same insight.
- **2016+** — Zig traps overflow in debug builds, wraps/UB in
  release: "test what you ship" knowingly traded for catching
  overflow where it's cheap.

**Glide takes**: `Int` = i64 *everywhere* (cross-compilation deletes
the platform-int bug class), lengths and indices **signed** (a `-1`
error traps in dev and can never address memory; `u64`/`u128` are for
hashes and bit patterns, not sizing), no implicit conversions (Go
proved the strictness is tolerable), Go/Zig's arbitrary-precision
literals via comptime, Zig's trap-in-dev/wrap-in-release, and
`BigInt` chosen by name — Python's silent auto-bignum puts a branch
in every add and is incompatible with overflow trapping. No
truthiness: conditions take `Bool` only — JS's coercion table is scar
tissue, and even Python's principled version conflates *absent* with
*empty*, the exact distinction Option exists to make.

## Strings: UTF-8 bytes, immutable, no `s[i]`

- **1992** — Ken Thompson designs UTF-8 with Rob Pike on a New
  Jersey diner placemat: ASCII-compatible, self-synchronising,
  byte-oriented. It wins the encoding wars completely.
- **1995** — Java makes strings immutable — ending the C tradition
  of strings mutating under you — but bets on UTF-16, the wrong
  horse, permanently.
- **2008–2018** — Python 3's decade-long str/bytes migration shows
  what enforcing "text is not bytes" costs an ecosystem. Rust (2015)
  enforces UTF-8 validity in the type system — correct and pure, but
  every OS/network boundary becomes an `OsString` conversion ceremony.
- **2009** — Go: strings are immutable UTF-8 byte sequences; validity
  checked where you care, not enforced by type. Iteration yields
  runes. Pragmatic middle ground; a decade of production use finds
  the sharp edge is only `s[i]` returning a surprise byte.

**Glide takes** Go's model minus the sharp edge: UTF-8 bytes,
immutable, `runes()` iteration with U+FFFD for invalid sequences —
and `s[i]` **does not exist**, because "what's at position i" is
underspecified (byte? rune? grapheme?) and the language won't guess.
Comparison is byte equality — Turkish-i has ended careers; locale is
a library invoked on purpose. Building is `StringBuilder`, a named
type, because loop `+` is the O(n²) classic and costs shouldn't hide.

Two glosses the sentence above compresses:
- **U+FFFD** is Unicode's REPLACEMENT CHARACTER (�), *defined* for
  this job: stand in for bytes that don't decode. The three options
  on invalid input are throw (Python's decade of
  `UnicodeDecodeError` on real-world data), silently delete
  (Unicode's own security report TR-36 forbids it — deleted junk
  bytes let `<scr␡ipt>` reassemble into `<script>` after the
  filter), or substitute U+FFFD and continue — the WHATWG-mandated
  browser behavior, and Go's (`utf8.RuneError` is U+FFFD).
- **Turkish-i**: Turkish has four letter-i's — dotted `i`↔`İ` and
  dotless `ı`↔`I` are *different letters*. In a Turkish locale,
  `uppercase("quit")` is `"QUİT"`, so every case-insensitive
  comparison via locale-aware folding (`command.toUpperCase() ==
  "QUIT"`) silently fails — only on Turkish machines, unreproducible
  from your desk. Java is the canonical victim (`toUpperCase()` uses
  the platform locale); "the Turkey test" became shorthand for
  locale-safety. Byte-equality `==` makes the bug unwritable by
  accident.

## String interpolation

- **1957–72** — the format-string era: Fortran's FORMAT, then C's
  `printf`. Format and data live apart; the compiler checks nothing;
  mismatches are UB in C and runtime noise (`%!d(string=hi)`) in Go.
- **1960s–95** — shells, then Perl and Ruby (`#{…}`, 1995) put the
  expression *in* the string. Ergonomic, dynamically typed, no
  checking.
- **2013–16** — the typed mainstream converges in one burst: Scala
  `s"…"` (2013), Swift `\(…)` (2014), JS template literals (2015),
  C# `$"…"` (2015), Python f-strings (2016). Every new language since
  ships it; the arguments end.
- The stragglers demonstrate the costs: Python's opt-in `f` prefix
  makes the forgotten-prefix bug endemic; Rust checks at compile time
  but via macro, and interpolates identifiers only; Go still has
  `Printf` verbs in 2026.

**Glide takes** always-on interpolation (no prefix to forget), full
expressions in the braces, compile-checked via the Display/Debug
trait split (Rust's), desugared at compile time to builder calls —
printf dies, and `%v`-printing-struct-guts-at-users dies with it.
Format specs stay deliberately tiny (`{price:.2}`, `{n:6}`, `{x:hex}`)
because a mini-language growing inside braces is printf reborn.

## The print family & unbuffered output

- **1970s** — C stdio ships buffered stdout with tty-detection:
  line-buffered at a terminal, block-buffered in a pipe. Two footguns
  are born: the prompt stuck in a buffer, and "works in my terminal,
  silent under `| tee`". Every C programmer learns `fflush` the hard
  way.
- **2009** — Go's `fmt.Print` writes straight to fd 1, unbuffered.
  Nothing to flush; debug prints always land, even mid-crash. The
  cost — a syscall per print — is real and accepted; bulk output
  opts into `bufio` explicitly.
- **2015** — Rust chooses line-buffering for speed and inherits the
  C prompt footgun; `io::stdout().flush()` is a documented
  incantation in every Rust book. Rust also settles the *naming*
  grid: `print!`/`println!`/`eprint!`/`eprintln!`.

**Glide takes** Go's buffering (unbuffered, one atomic write per
call, no `flush()` in the language) with Rust's four names — a closed
2×2 grid (`e` = stderr, `ln` = newline) that cannot grow, since
formatting lives in interpolation and other streams are writer APIs.
Rejected: a `newline:` parameter (no good default; a bool selecting
two behaviours is two functions hiding in one signature) and Python's
`end=` (the terminator is just string content).

## Generics: angle brackets, monomorphized

- **1973/1983** — ML has parametric polymorphism from birth; Ada
  ships explicit generics for the mainstream.
- **1990s** — C++ templates deliver monomorphization (fast code,
  legendarily bad errors) and the `>>` lexing bug, unfixed until
  C++11.
- **2004–05** — Java adds generics by erasure (types vanish at
  runtime); C# 2.0 does them reified — and solves the `f<T>(x)`
  parsing ambiguity with a lookahead rule that TypeScript later
  inherits: twenty years of production proof that angle brackets
  don't need Rust's `::<>` turbofish.
- **2022** — Go ships generics after thirteen years, with square
  brackets chosen for parser convenience — making `a[T]` element
  access or type instantiation depending on what `a` is, invisible at
  a glance — and mandatory `[T any]` ceremony that the bracket
  collision created.

**Glide takes** angle brackets (`List<T>` is "types here" to every
programmer alive), the C#/TypeScript disambiguation (parser pays
once; no turbofish, ever), monomorphization (from day one — Go's
twelve-year absence is the cautionary tale), and inline colon bounds
only (`T: Ord + Hash`; no `where` clause until signatures genuinely
demand it). Const generics deferred — machinery for a numeric-library
need Glide doesn't have yet.

## Enums

- **1970** — Pascal ships enumerated types: a closed set of names,
  not integers. C (1972) immediately regresses: `enum` is an int with
  aliases, any value converts, nothing is checked.
- **2004** — Java 5 gets enum *semantics* right (real types, methods,
  exhaustive switch) and drowns them in ceremony.
- **2009** — Go declines enums entirely: `iota` is a counter macro —
  `Color(42)` compiles and flows, no exhaustiveness, names need the
  external `stringer` codegen, and the zero value silently means the
  first variant. Both Go diseases (int costume + zero value) in one
  feature.
- **2010s** — Rust and Swift land the synthesis: an enum is just a
  sum type whose variants happen to carry no payload — full semantics,
  no ceremony. Swift adds the dot shorthand (`.Red` where the type is
  known).

**Glide takes** the degenerate-sum-type view (`type Color = Red |
Green | Blue` — a variant can gain a payload later without changing
feature), exhaustive matching for free, no implicit int conversion in
either direction (`from_int` returns `Color?` — the invalid case
handled where it enters), Swift's dot shorthand, and enumeration via
`derive Enum` — the codegen Go outsources to stringer, done by
comptime. Flags are `Set<Color>`, never a bitfield enum.

## Mutability: `let`/`mut`, declared receivers

- **1973** — ML makes immutable the default and mutation opt-in
  (`ref` cells) — in a *functional* language, where it's cheap.
- **1985–95** — C++ `const` and Java `final` offer opt-in
  immutability; almost nobody opts in; default wins, as defaults do.
- **2014–15** — Swift (`let`/`var`) and Rust (`let`/`let mut`) prove
  immutable-by-default works in imperative languages, and that the
  audit value is enormous: skim a function, `mut` shows you what can
  change. Swift's `mutating func` and Rust's `&mut self` make
  *method* mutation declared too.
- **2009** — Go's counterexample: pointer-vs-value receivers conflate
  "may this mutate?" with "how is it passed?" and enforce neither —
  the value-receiver-mutates-a-copy footgun — and `:=` has no slot
  for mutability, which is why Go has no immutable locals at all.

**Glide takes** Rust's spelling (`let`/`let mut`) and doctrine:
receiver mutability declared (`mut self`), callable only through
`mut` paths; free-function `mut` params marked at the call site
(`sort(mut xs)` — Rust's `&mut x` without the reference machinery);
mutability transitive through paths. Recorded honestly: `mut` is a
path property, not an object guarantee — no borrow checker means two
bindings can alias one object. Frozen guarantees come from persistent
collections, not from pretending `mut` is Rust.

## Shadowing: one live binding per name

- **1995–2000** — Java, then C#, ban shadowing a local with a local
  in a nested block. Twenty-five years on, nobody lists the ban among
  their regrets — the strongest kind of language evidence.
- **2009** — Go allows it, and `:=`'s declare-or-assign dual
  personality industrialises the bug: `result, err := f()` in a
  nested block silently declares fresh variables; the outer ones keep
  their stale values; vet's shadow checker exists but is too noisy to
  enable.
- **2015** — Rust demonstrates the *other* shadowing is a quiet win:
  sequential rebinding in the same scope (`let input = input.trim()`)
  gives refinement pipelines one honest name instead of
  `input_raw`/`input_trimmed`.

**Glide takes** both halves of the evidence: sequential redeclare is
idiomatic (exactly one binding alive at a time), nested shadowing of
a live name is a compile error (the Go bug shape needs *two* live
bindings — made unwritable), and the root cause is gone anyway
(`let` always declares, `=` always assigns). Free builtins (the
prints, `expect`) are reserved names outright — a tiny fixed set
nobody wants as locals. Imports stay shadowable — `sql` is a prime
variable name in a file importing `sql` — but using the module
*through* a live shadow is a checker-era compile error naming both
parties.

## Defer & `errdefer`

- **1960s** — Lisp's `unwind-protect`: run cleanup no matter how the
  block exits. The primitive is ancient.
- **1985** — C++ RAII: destructors as cleanup, deterministic and
  automatic. The gold standard — *if* you have deterministic
  destruction, which GC languages don't (finalizers running
  "eventually" is how fds leak; Rust's `Drop` is a borrow-checker
  dividend).
- **2000/2006** — C# `using` and Python `with` scope cleanup to a
  block — and nest: three resources, three indent levels.
- **2009** — Go's `defer`: flat, readable, cleanup next to
  acquisition. Three defects ship with it: function-scoped (the
  defer-in-a-loop fd exhaustion classic), call-with-args (the
  `defer f(x)` argument-evaluation puzzle), and `defer f.Close()`
  silently discarding the error that surfaces buffered-write failures.
- **2014/2016** — Swift and Zig fix the defects piecemeal: Swift's
  `defer` is block-scoped and takes a block; Zig adds **`errdefer`** —
  cleanup that runs only on the error path, the missing construct
  behind every hand-written rollback-if-failed.

**Glide takes** Go's primitive with all three fixes applied from
Swift and Zig: block-scoped, block-only (`defer { conn.close() }`),
unused-Result rules apply inside (no silent error discard), plus
`errdefer` for failure-path cleanup — `defer` = always, `errdefer` =
only on failure; conflating them is the bug Go keeps hand-writing.
Rejected: linear must-close types (heavier than the problem in a GC
language).

## Control flow: one loop, goto's estate, no ternary

- **1968/1974** — Dijkstra's "Go To Statement Considered Harmful",
  then Knuth's rebuttal cataloguing goto's few legitimate uses. The
  next fifty years of language design is the process of giving each
  legitimate use its own construct.
- **1972** — C establishes the loop zoo (`for`/`while`/`do-while`)
  and `?:` — which nests unreadably and whose precedence breeds bugs.
- **1995** — Java adopts labeled break/continue: goto's
  escape-nested-loops use case, tamed (exits outward through
  enclosing loops only, never arbitrary jumps).
- **2009** — Go collapses the loop zoo into one `for` — correct and
  kept — refuses `?:` *without compensation* (hence four lines and a
  mutable variable for the innocent case, a permanent FAQ entry), and
  quietly keeps `goto` because it lacked the replacements.
- **2014** — Apple's `goto fail` bug: an unbraced `if` body plus a
  duplicated line silently disables TLS certificate verification on
  every Apple device. The case for mandatory braces, written in CVE.

**Glide takes** Go's single `for` (with `in` replacing `range`),
Java's labeled break/continue, mandatory braces everywhere (the
`goto fail` shape unwritable), and — the compensation Go refused —
expression-`if`: `let status = if ok { "active" } else { "disabled" }`.
Value-position `if` requires `else` (type checker), the commonest
ternary is already `??`, and chains are subjectless `match` — what
nested ternaries were always trying to be. `goto` itself: every
reason Go kept it is a hole already filled here (`defer`/`errdefer`
+ `?` for cleanup-on-error, labels for nested loops, `loop { match }`
for state machines).

## Ranges: half-open `..`, inclusive `..=`

- **1958+** — the off-by-one wars: inclusive ranges (Pascal's
  `1..10`, Ruby's `..`) read naturally but make empty ranges and
  length arithmetic awkward; half-open (Dijkstra's argument, C's
  idiom, Python's `range`) composes (`[a,b) + [b,c) = [a,c)`) and
  makes `len = hi - lo`, but excludes the endpoint people name.
- **Ruby (1995)** — ships both with the confusing allocation: `..`
  inclusive, `...` exclusive — one dot of visual difference flipping
  the endpoint, a documented source of bugs.
- **Rust (2015→2018)** — ships `..` half-open only; real code keeps
  needing the closed end (matching `1..=9` digits, `u8::MIN..=MAX` —
  inexpressible half-open at the type's top). Adds `..=` in 1.26
  (2018) after deprecating a `...` experiment *because* of the Ruby
  confusion. Swift made the same split (`..<` / `...`) in 2014.
- The half-open-only diet fails on two recurring cases: ranges of
  the type's maximum value, and rune/letter ranges — `'a'..'z'`
  excluding `'z'` turns the flagship example into the puzzle
  `'a'..'{'`.

**Glide takes** Rust's exact pair: `..` half-open (the default that
composes), `..=` inclusive (visually unmistakable, unlike Ruby's
third dot), in expressions and patterns alike, for Int and Rune.
`..=` desugars to `hi + 1`; at the maximum Int that is a loud error,
not a wrap. Adopted the day rune range patterns landed — the
`'a'..='z'` case is exactly what forced Rust's hand.

## Patterns everywhere: destructuring, `let…else`, tuples

- **1973** — ML: patterns aren't just for `match` — every binding
  position destructures, and **patterns are construction run
  backwards** (anything buildable is disassemblable by the same
  shape). Tuples are first-class from birth.
- **2009** — Go's counterexample: multi-return values that aren't
  values (unstorable, unlistable — a permanent wart), constructors
  for structs, patterns for nothing.
- **2015** — Swift's `guard let`: check-and-bind where the failure
  path must diverge, keeping the happy path flat. ES2015 brings
  destructuring to the mainstream the same year.
- **2022** — Rust, seven years after 1.0, adds `let…else` — evidence
  the construct is load-bearing, not sugar: flat early-exit chains
  are how real programs read.

**Glide takes** the ML principle whole: destructuring `let`
everywhere (irrefutable), real tuples (with the doctrine: tuples are
for transport; >2 elements or >1 API boundary means a named struct),
`let…else` (Swift's guard, body must diverge — enforced), nested
patterns with exhaustiveness following the nesting, struct patterns
requiring `..` for partial (Rust's rule — new fields must break match
sites that ignore them), and spread as patterns-run-forward
(`[a, ..xs]`, `Config{ timeout: 5.s, ..base }`). Declined: or-
patterns in patterns, `x @ pattern`, params-as-patterns, and Rust's
ref/binding modes (the GC dividend again). **No variadics**: every Go
variadic customer is dead here (Printf → interpolation, append →
push, max(a,b,c) → a list) — a second function-type shape bought
nothing.

## Iterators & generators

- **1975–77** — Barbara Liskov's CLU invents the iterator as a
  language construct — and implements it as a *generator* (`yield`),
  establishing fifty years ago that the two are one feature. Icon
  (1977) builds a whole language on generation.
- **2001–05** — Python (PEP 255) then C# 2.0 bring `yield` to the
  mainstream: the compiler builds the state machine you'd otherwise
  hand-maintain. C# proves it compiles efficiently; twenty years of
  production.
- **2015** — Rust ships external iterators (`next() -> Option<T>`)
  with lazy adapters — zero-cost, but generators stay unstable for a
  decade because yielded references borrow from suspended stack
  frames. The lifetime problem, not the concept, is what's hard.
- **2009–24** — Go resists iterators for fifteen years (hand-rolling
  `hasNext`/`(value, ok)` protocols per container), then ships
  range-over-func (1.23, 2024) — callback-style internal iteration,
  the form that makes `zip` miserable.

**Glide takes** Rust's protocol (`trait Iterator<T> { fn next(mut
self) -> T? }` — one method, no invalid states) with CLU's insight
restored: generators are *how you write* iterators (`yield` in a
body builds the state machine implementing the trait — sugar, not a
second protocol). No lifetimes means generators are almost boring —
the asymmetry Glide builds around, where Rust spent a decade stuck.
`Iterable` is separate from `Iterator` (a List can be iterated many
times; collapsing the two bites every language that tries), and
channels are explicitly *not* the iteration protocol (sync per
element, leaked producer on early exit — see the generator-truncation
bug that motivated the M1 cleanup hook).

## Green threads & the runtime model

- **1978** — Tony Hoare publishes CSP (Communicating Sequential
  Processes), the theory behind "share memory by communicating".
- **1986** — Erlang ships lightweight runtime-scheduled processes;
  telecoms run millions of them. Proof at industrial scale that
  cheap-threads-plus-messages works.
- **~1991–97** — Sun's "Green Team" (hence the name) builds Java's
  first threading: *green threads*, M:1 — many logical threads
  multiplexed on **one** OS thread. No parallelism; one blocking
  syscall stalls everything. Ruby 1.8 has the same model. This is
  where the term picked up its "fake threads" stigma.
- **2009** — Go ships goroutines: green threads done right — M:N over
  all cores, growable stacks, blocking parks the goroutine instead of
  the OS thread. Channels descend from CSP via Pike's earlier
  languages (Newsqueak, Alef, Limbo).
- **2014** — Rust, pre-1.0, **removes** its green-thread runtime
  (RFC 230): a systems language needs zero-overhead FFI and no
  mandatory runtime, so it takes async/await instead — and function
  coloring with it. The lesson is about constraints, not superiority:
  green threads want a runtime; Rust couldn't afford one, Glide (like
  Go) happily pays.
- **2023** — Java capitulates after 25 years: virtual threads
  (Project Loom, Java 21) are green threads again, M:N this time —
  and the reactive-framework ecosystem starts migrating back to plain
  blocking style.

**Glide takes** Go's model wholesale — green-threaded, GC, one
binary — because the transpiler-to-Go bootstrap gets the
battle-tested runtime for free, and because blocking-code-just-blocks
beats async/await's two-colored functions for the server programs
Glide targets. Recorded sacrifice: at extreme task counts (millions
of connections on tiny hardware) async's bytes-per-task wins; that
workload is conceded.

## Structured concurrency, channels, `Mutex<T>`

- **2016–18** — Martin Sústrik (libdill) and then Nathaniel J.
  Smith's "Notes on structured concurrency, or: go statement
  considered harmful" (Trio's nursery concept, 2018) make the case:
  a spawned task with no parent is `goto` all over again — it
  outlives its spawner, its errors vanish, its panic has nowhere to
  go. Every mature Go codebase reinventing `errgroup` is the evidence.
- **2018–23** — the idea sweeps the industry in five years: Kotlin
  coroutine scopes, Swift task groups (2021), Java's
  StructuredTaskScope (Loom). Faster adoption than pattern matching.
- **2014–16** — meanwhile Go's `context.Context` demonstrates the
  cancellation problem: having escaped async's viral annotation, Go
  reinvents it by hand — `ctx` as first parameter of every serious
  function, unchecked.
- **2015** — Rust's `Mutex<T>` (wrapping the *data*, not sitting
  beside it) proves the best non-borrow-checker race mitigation:
  unguarded access doesn't compile.
- **Channel ergonomics across three generations** — Go (2009) ships
  mpmc channels + `select` and they become the language's crown
  jewel, but with four famous runtime landmines: send-on-closed
  panics, double-close panics, receive-from-nil blocks forever, and
  anyone holding the channel can close it (the `sync.Once`-guarded
  close is a standard Go idiom precisely because the language
  doesn't help). Rust std (2015) ships `mpsc` with the halves split
  (`Sender`/`Receiver` as distinct types) — proving the split works —
  but picks unbounded-by-default (the classic slow-consumer memory
  leak) and single-consumer; the ecosystem migrates wholesale to
  crossbeam's mpmc, and `std::sync::mpsc` is quietly regarded as a
  mistake frozen by stability promises. Kotlin/Swift reuse the same
  lessons in their channel/AsyncStream APIs: close is sender-side,
  end-of-stream is a value (`null`/`nil`), not an exception.
- **`select`'s forty-year lineage** — Occam's `ALT` (1983, the first
  CSP language on real hardware) offered guarded alternatives:
  boolean condition + channel input per branch. Rob Pike carried the
  construct through Newsqueak (1988), Alef, and Limbo into Go's
  `select` (2009) — but dropped the guards, leaving Go programmers
  to disable a case by setting its channel to nil, an idiom built on
  the language's worst channel behavior (nil blocks forever). Go
  added uniform-random choice among ready cases — the
  anti-starvation rule Occam left implementation-defined. Kotlin's
  `select` has sat in experimental status for years; Java shipped
  virtual threads with *no* select at all (StructuredTaskScope can't
  wait on multiple queues — users fall back to polling or merge
  queues), proving the construct is hard to retrofit and worth
  designing in from day one.
- **What cancellation *is*** — nobody made it a first-class thing.
  Trio (2018) delivers it as a `Cancelled` exception raised at
  checkpoints, with documentation begging users to always re-raise
  it; Kotlin ships `CancellationException`, which every `catch
  (e: Exception)` block in the ecosystem accidentally swallows —
  the standard library's own docs carry the "always rethrow" warning.
  Both prove the same lesson: cancellation-as-catchable-exception is
  a convention, and conventions get violated at scale. Java Loom uses
  `Thread.interrupt` (a 1990s mechanism famous for being ignored);
  Go has no task cancellation at all — `ctx.Done()` is manual polling
  that every function must opt into, forever. Swift's cooperative
  `Task.checkCancellation()` throws a `CancellationError` — catchable,
  same weakness as Trio/Kotlin.
- **How values leave a nursery** — every adopter answered differently,
  and the differences are the evidence. Trio (2018): no values from
  spawn at all; results travel via captured mutables or a channel —
  the same wart as Go's `errgroup`, where every user writes
  `results[i] = …` by hand. Kotlin: split into `launch` (no value) and
  `async` (a `Deferred` you `await`) — needed because JVM exceptions
  are invisible control flow, and its `async` carries the library's
  most-complained-about gotcha: a child's failure cancels the scope
  *immediately*, even when the parent was about to `await` and handle
  it. Java Loom: `fork` returns a `Subtask`, but reading it before
  `scope.join()` throws `IllegalStateException`, and failure handling
  grew a policy zoo (`ShutdownOnFailure`, `ShutdownOnSuccess`). Swift
  task groups (2021): child results/throws surface when awaited, and
  anything unawaited is implicitly awaited at group exit — the closest
  relative to what Glide does.

**Glide takes** nurseries as the *primitive* (`scope s { s.spawn(…) }`
— scope exit waits, one failure cancels siblings, leaks
unrepresentable), cancellation as a scope property (no parameter
threading), Go's channels and `select` (the crown jewel) with the
sharp edges typed away — sender/receiver halves distinct, sender
closes, ownership transfers on send — and Rust's `Mutex<T>`. Race
detector in `glide test` by default, not behind a flag learned after
the incident. For values-out (ratified 2026-08-09): one spawn
primitive returning a `Task` handle; `t.join()` yields exactly what
the closure returned, so a child's `Err` is a value in the handle,
not an event — but any *unjoined* `Err` fails the scope at exit, so
errors can't vanish; a panic cancels siblings immediately ("values
wait, bugs don't"). Swift's group-exit surfacing is the near
precedent; Kotlin's immediate-cancel gotcha is the evidence for
errors-as-values; Trio and errgroup are the evidence that values-out
must be first-class. For cancellation (ratified 2026-08-09): a third
unwind — neither error nor panic, uncatchable by user code (defers
and errdefers run), delivered only at blocking operations, and only
*scopes* cancel (no `t.cancel()`); since cancellation implies the
whole scope is going down, user code never observes a cancelled
task, so no `Cancelled` type leaks into signatures — designing out
the leak that Trio and Kotlin both paper over with "always rethrow"
conventions. `scope(timeout:)` is clock-driven cancellation
evaluating to `Result<T, Timeout>`, composing with `?`-conversion so
timeouts stay non-viral where Go needed `ctx` in every signature.
For channels (ratified 2026-08-09): Go's mpmc semantics with Rust's
halves — `let (tx, rx) = channel()`, rendezvous default, bounded
only; `rx.recv() -> Option<T>` (`None` = closed-and-drained, so
`for v in rx` is Go's `range ch`); `rx` can't close (type-shaped
away), `tx.close()` is idempotent (ships what Go users hand-roll
with `sync.Once`), send-on-closed stays a panic — it's a sender
coordination bug, and shutdown flows down the scope tree via
cancellation, not up via send failures. For `select` (ratified
2026-08-09): Go's engine (operands once at entry, uniform-random
among ready, `else` for non-blocking) wearing match's syntax
(patterns over `recv`'s `Option`, line-separated arms), with
Occam's guards restored (`if cond` disables an arm — replacing
Go's nil-channel trick) and Go's most common arm, `<-ctx.Done()`,
designed away entirely: a blocked select is a cancellation point,
so the scope cancels it.

## Comptime, not macros

- **1963** — Lisp macros: full compile-time metaprogramming,
  arbitrary syntax. Fifty years of evidence they're how ecosystems
  become unreadable to tooling and newcomers alike.
- **1970s–90s** — C's preprocessor (textual, unhygienic) and C++
  templates (accidentally Turing-complete; error messages from deep
  inside the expansion) demonstrate the failure modes of *accidental*
  compile-time languages.
- **2007–11** — D's CTFE, then C++ `constexpr`, grope toward the
  principled version: run *the same language* at compile time, no
  second sub-language.
- **2016+** — Zig's comptime lands the insight cleanly: ordinary
  code, compile-time execution, covers ~90% of macro use. But Zig
  also uses comptime *as* its generics system — giving
  C++-template-tier errors deep in callees. Zig had no choice;
  comptime is all it has.
- **2015+** — Rust's proc macros: a second compiler, slow builds,
  opaque expansion; Go's runtime `reflect`: an interpretive loop per
  call, the biggest hole in Go's auditability story.

**Glide takes** Zig's comptime with a fence Zig couldn't build:
comptime is const evaluation + derive-via-reflection, **never** a
generics system — `List<T>` comes from trait-bounded generics with
errors at the call site. `derive Json` is an ordinary comptime
function emitting plain code (serde-class speed, no proc-macro second
compiler). No runtime reflection at all; no AST macros, ever; no IO
at comptime (hermetic builds); fuel-limited and deterministic.
Accepted forever: DSL macros (`html!`) don't exist — embedded custom
syntax is exactly the second-language-tooling-can't-see that's banned.

## Syntax: the newline rule, `let`, and what didn't parse

- **1991/1995** — the two cautionary tales ship almost together:
  Python's significant whitespace (breaks codegen and copy-paste) and
  JavaScript's semicolon insertion (`return\n{…}` returns undefined —
  ASI as minefield).
- **2009** — Go threads the needle: semicolons inserted by a *lexer
  rule* (newline after a token that can end a statement), `{` forced
  onto the same line by the same rule — style enforced by grammar,
  not linter. Ten years of use finds essentially no complaints.
- **2009** — Go also ships `:=` — five declaration spellings, the
  shadowing trap, and (decisive here) no slot for mutability, which
  is why Go has no immutable locals.

**Glide takes** Go's newline rule verbatim (ratified after the
interpreter forced the question), mandatory braces (see `goto fail`),
struct-literals banned in control headers (Rust's rule, hit for
Rust's reason), and `let` as the only declaration form — the keyword
is what immutable-by-default lives in. Assignment is a statement
(the `if (b = 1)` typo family unwritable), boolean operators are
conventional `&&`/`||`/`!` with no truthiness and no overloading
(short-circuit is control flow).

## No `++`/`--`

- **~1969** — Ken Thompson invents `++` in B — famously *not* for
  PDP-11 auto-increment hardware (B predates the machine); it was
  just shorter than `x = x + 1`, and it composed into expressions:
  `a[i++]`.
- **1972→** — in C, expression-`++` becomes both idiom and bug
  farm: sequence-point UB, `a[i++] = i`, interview-question
  cleverness.
- **2009** — Go keeps the spelling but neuters it: statement-only,
  no value, so there is no pre/post distinction at all — Go's `i++`
  is purely a two-character synonym for `i += 1`.
- **Never-had-it club** — Python, Rust (RFC declined), Zig. No user
  revolt on record.
- **2016** — the decisive data point: Swift *had* `++`/`--` and paid
  the migration cost of **removing** them (Swift 3): rarely used
  beyond `+= 1`, confusing pre/post semantics, not worth the grammar.
  Languages almost never pay to delete working syntax.

**Glide declines** the operator: Go's version is only a synonym, the
C version is the cleverness family (assignment-as-expression) killed
at the root, and the habitat — C-style `for` loops — doesn't exist
here (`for x in` owns iteration). `i += 1` is the way.

## Comments: `//` only

- **1964–67** — both forms are born within three years of each
  other: PL/I introduces `/* */`, BCPL introduces `//`.
- **1972** — C takes only `/* */`; the non-nesting trap ships with
  it (comment out code containing a block comment; the comment ends
  early; live code left behind, still compiling).
- **1983** — C++ resurrects `//`; C99 re-adopts it; every brace
  language since carries both forms and the trap. Rust makes block
  comments nest instead — fixing the bisect case, breaking on a stray
  `*/` in prose. Either specification is a trap.
- **2016** — Zig ships with line comments only, no block form:
  simpler lexer, no nesting question, and the region-deadening job
  had long since moved to the editor's toggle-comment key.

**Glide takes** Zig's position: `//` to end of line is the entire
comment grammar. Editor toggling produces lines that are
individually, greppably dead in any diff hunk; doc comments are
ordinary `//` above the declaration; a second comment spelling would
be delimiter synonymy. (Zig's every-line-lexes-standalone bonus is
not claimed — backtick raw strings already span lines.)

## Blocks are expressions

- **1958–60** — Lisp: every form yields a value; `progn` returns its
  last. The idea is as old as structured programming.
- **1968** — ALGOL 68 makes it systematic: everything is an
  expression, blocks included. BCPL's `valof`, ML's `let … in`, Ruby
  carry it forward.
- **2015** — Rust proves it coexists with braces-and-semicolons
  syntax: blocks yield their tail expression; `let x = { … }` is
  ordinary code. (GCC had prototyped it decades earlier as the
  statement-expression extension `({ … })` — useful enough that
  people tolerated the ugliness.)
- Go, meanwhile, keeps blocks statement-only and compensates with
  per-statement carve-outs: the `if` init clause, three-part `for`
  headers — each with its own scope rule to memorise (and its own
  surprises: the init variable is visible in `else`).

**Glide takes** the general rule and deletes the carve-outs: a bare
`{ … }` anywhere is a scope whose tail is its value. `let size =
{ let num = …; if num > 50 { "big" } else { "small" } }`
computes-and-hides; a freestanding block scopes a region as in Go.
One rule where Go has several, and it composes with everything else
(`if`/`match` as expressions, function tails) instead of existing
beside them. Function bodies are the same rule (tail expression is
the return value — Lisp/ML/Ruby/Rust lineage), with mandatory
signatures as the guardrail against CoffeeScript's
accidental-return failure mode.

## The canonical formatter

- **1970s–2000s** — `indent(1)` and its config flags demonstrate the
  failure mode: a formatter with options just mechanises the style
  war.
- **2009/2013** — gofmt ends half the argument by having zero
  options — "gofmt's style is no one's favourite, yet gofmt is
  everyone's favourite" — but preserves author line breaks, so Go
  teams still argue about width and where to split.
- **2017–18** — Prettier and Black finish the job for JS and Python:
  the formatter owns line breaks too, wrapping at a width via
  Wadler-style pretty-printing (the constraint-solving algorithm from
  his 1997 "A prettier printer"). Dart rebuilds its formatter on the
  same principle; it's the best-regarded formatter shipping.
- **2016+** — rustfmt ships `rustfmt.toml` and reinstates, one knob
  at a time, every argument the tool existed to end.

**Glide takes** the full-canonical position: formatter as a pure
function from AST to bytes, width-aware wrapping, **zero
configuration — no config file format exists**. Trailing comma forces
one-per-line (structural intent through grammar, not whitespace).
Enforcement at the right boundary: format-on-save + `glide test`
fails unformatted code, but misformatted code still *compiles* — a
compiler that yells about whitespace mid-edit is hostile. Canonical
bytes are also migration infrastructure: `glide fix` rewrites produce
zero-noise diffs.

## Hygiene: unused code, tiered

- **1990s–2000s** — the warning-rot era: C/C++ codebases compile
  with 400 warnings; everyone learns to ignore #401, the real bug.
  `-Werror` fixes rot by breaking every edit loop instead.
- **2009** — Go makes unused variables/imports hard errors:
  no rot, guaranteed — and mid-debug, comment out one line and the
  compiler faults you through a cascade (unused var → unused import →
  restore everything). The `_ = x` incantation is the tell: an error
  routinely silenced by a no-op is a warning with extra steps.
- **2016+** — Zig copies strict-everywhere; it becomes the
  community's most-resented decision. goimports, meanwhile, proves
  the import list is *derivable* — Go enforces by error a list no
  human should edit.

**Glide takes** the tiered answer the backends already pay for: dev
builds warn loudly (the bisect loop never breaks), release and
`glide test` error (unused code cannot ship — Go's guarantee, kept at
the same boundary as formatting). Deliberate unused is `_conn`,
declared at the declaration, not silenced at a distance. The
formatter/LSP owns the import block entirely.

## Declarations: order-free modules, no `init()`, comptime consts

- **1970s** — C and Pascal require declaration-before-use;
  programmers arrange files for the compiler, not the reader.
- **1980s–90s** — C++'s static initialisation order fiasco: globals
  initialise in link order across translation units; programs crash
  before `main`. The canonical life-before-main horror.
- **2009** — Go gets module-level order-independence deeply right —
  and then `init()` + import-for-side-effect (`import _ "lib/pq"`)
  reintroduce life-before-main as a feature. All downstream pressure
  from one gap: `const` can't hold a map, so structured data became
  runtime `var` + `init()`.

**Glide takes** order-independent declarations at *every* nesting
level (nested `fn`s are items — hoisted, mutually recursive, and
non-capturing, Rust's rule), and closes Go's gap at the root:
`const` is comptime-evaluated and can hold anything (`const table =
make_crc_table()` lands in read-only data; a bad regex is a compile
error, not a `MustCompile` panic). Module level is const-only — no
module `let`, **no `init()`, imports are inert, nothing runs before
`main`**. SCREAMING_CASE dies (an earlier evaluation time is not a
siren); const names are `snake_case` like any binding.

## Modules & `pub`

- **1995–2009** — Java's package-visibility matrix, then Rust's
  module tree (`mod.rs`, `pub(crate)`, `pub(super)`, path attributes)
  demonstrate ceremony scaling faster than value.
- **2009** — Go: directory = package, capitalisation = exported.
  Two great simplicities — and the capitalisation trick works only
  because nothing else competes for the case axis.

**Glide takes** Go's directory-is-module model (no intra-module
imports, no in-file module declarations) with Rust's `pub` keyword,
forced by a real conflict: pattern matching needs capitalisation —
`match shape { Circle(r) => …, point => … }` is only readable if case
says which names match and which bind. Go never hit this because Go
has no patterns. Bonus: a visibility change becomes a one-line
reviewable diff, not a whole-codebase rename. Exactly two levels
(private, `pub`); wanting a third usually means the module boundary
is drawn wrong.

## Testing

- **1989/1997** — Kent Beck's SUnit, then JUnit, establish the
  xUnit framework model: test classes, assertion libraries, runners.
- **2009** — Go strips it to the studs: tests are functions next to
  the code, one universal command, no framework. Culture follows
  tooling — testing becomes table stakes *because* it's frictionless.
  Two gaps ship too: `t.Errorf` boilerplate (the community makes
  testify one of the most-imported modules — the stdlib lost the
  argument) and `b.N` (users own the measurement loop and get it
  wrong; Go concedes via `b.Loop()` in 1.24).
- **2007+** — the power-assert lineage (Groovy, then pytest's
  assertion rewriting) shows the third way between one assert and 40
  matchers: instrument `assert got == want` to report the parts on
  failure.
- **2016+** — Zig makes tests a language construct: `test "name" {}`
  — no magic filename, no naming convention, "excluded from release"
  is a statement about grammar.
- **2000** — Claessen & Hughes publish QuickCheck; property-based
  testing spends 25 years proven-but-niche because setup friction
  (write generators by hand) never went away.

**Glide takes** Go's colocation and no-framework culture, Zig's
`test`-as-construct, power-assert semantics in one compiler-known
builtin (`expect(got == want)` reports `left: 2, right: 3` —
the testify war dissolved), runner-owned benchmarks, and QuickCheck
with the friction deleted: `derive Arbitrary` via comptime writes
the generators, test parameters generate/shrink/reprint seeds.
Serial-by-default within a module (default-parallel breaks every
suite touching a port); race detector always on in `glide test`.

## Documentation

- **1995** — Javadoc: doc generation from source ships with Java —
  and with it the tag language (`@param`/`@returns`) that restates
  signatures as write-only noise.
- **2009** — godoc: docs are ordinary comments, prose only, first
  sentence starts with the identifier name. The right core. Plain
  text for 12 years (markdown conceded in 1.19, 2022), links rot
  silently as strings, and doc examples — compiled, output-checked —
  are Go's best contribution to the genre.
- **2015** — rustdoc doubles down on doc-*tests*: runnable code
  inside comments. Popular, and a category error — code invisible to
  the formatter, LSP, refactoring, and grep.

**Glide takes** godoc's core (ordinary comments, name-first summary,
one tool in the one binary) with the three fixes: markdown subset
from day one, **checked identifier links** (`[Config]` resolves
against real declarations; a stale link fails `glide test` — docs
that reference code must break when the code changes), and Go's
example functions kept while Rust's doc-tests are rejected — examples
live in test files where tooling can see them.

## Time & dates

- **2002–14** — Stephen Colebourne's Joda-Time, matured into
  `java.time` (JSR-310, Java 8): the type-per-concept model — an
  instant, a calendar date, a wall-clock time, and a zoned civil time
  are *different types* with explicit conversions. The one part of
  Java everyone steals without irony.
- **2009** — Go ships one `time.Time` moonlighting as all of them.
  Every Go business app grows a truncate-to-midnight helper and a
  latent timezone bug; `time.Now` is a global nobody can test.
- **2017** — the leap-second incident: Cloudflare's DNS goes down
  when wall-clock subtraction yields a negative duration. Go 1.9
  responds with the monotonic hybrid — `now()` carries both readings;
  elapsed math silently uses the monotonic one. The correct thing
  happens when you don't think — better than Rust's
  choose-correctly-or-else split (`Instant` vs `SystemTime`).

**Glide takes** java.time's type set (`Time`, `Date`, `TimeOfDay`,
`ZonedTime` — future events are civil+zone, *never* instants: "9am
Sydney next March" isn't a fixed point until it happens), Go's
monotonic hybrid and Duration-as-type (`5.s`, never bare ints),
Java/Moment format tokens (`"YYYY-MM-DD"`, compile-checked) over
Go's mocked reference date, tzdata embedded by default (static
binaries in scratch containers must not discover zoneinfo missing at
runtime), and — stdlib duty, not user cleverness — an injectable
Clock that `glide test` can freeze. Tzdata *updates* diverge from
Go deliberately: Go's lookup chain silently prefers the host's
`/usr/share/zoneinfo` over the embedded copy — so a fresh binary on
a stale base image quietly computes with older DST rules. Glide's
only external source is one explicit env var: the
rebuild-the-container-not-the-app ops pattern still works, but the
override is visible in the Dockerfile instead of ambient in the
filesystem.

## Logging

- **1980s** — syslog establishes levels; the trace/debug/info/warn/
  error/fatal ladder calcifies without anyone applying trace vs
  debug consistently.
- **2001–21** — log4j defines configurable logging for a generation;
  its config-as-capability philosophy terminates in Log4Shell (2021)
  — remote code execution via a *logging config feature*. A logger
  is five lines in `main`; it was never supposed to be a platform.
- **2010s** — the structured-logging consensus forms (12-factor
  apps, JSON to aggregators): the message is an event name, the data
  rides in fields. Go arrives in 2023 (slog, 14 years in), with
  alternating-varargs fields checked only by vet.

**Glide takes** structured logging in the stdlib on day one:
constant message + typed field literal (arity is grammar, keys are
identifiers, comptime-checked — no reflection; zap's zero-alloc
heroics were working around reflection Glide doesn't have). Four
levels — no trace, no fatal (`log.Fatal` is `os.Exit` disguised as
observability, skipping defers). Adaptive output (pretty on TTY,
JSON piped); configuration is code, only code. The logger is ambient
via scopes — request fields attach once, the subtree inherits — with
the principled line recorded: logging is *observation*; nothing
behaviour-affecting rides ambient. Interpolated log messages are a
lint: the one place interpolation is an antipattern
(infinite-cardinality messages can't be aggregated).

## JSON

- **2001–06** — Crockford names JSON; RFC 4627 standardises it. The
  interchange format of everything thereafter.
- **2010** — Twitter adds `id_str` to its API because JavaScript
  silently corrupts int64 IDs past 2⁵³ — the canonical
  numbers-are-floats casualty.
- **2009+** — Go's `encoding/json` accumulates its five diseases:
  runtime reflection, stringly tags (`json:"nmae"` ships), missing
  field → silent zero, case-insensitive matching
  (security-relevant), `map[string]interface{}`.
- **2015** — Rust's serde: derive-generated codecs, fast and typed —
  via proc macros (a second compiler) and a 30-method format-generic
  trait dance.

**Glide takes** serde's outcome through comptime instead of macros:
`derive Json` emits plain code; options are typed comptime arguments
(typo'd option = compile error, not a shipped `nmae`).
Required-by-default falls out of mandatory init + Option — absent
`Int` is a decode error, `Int?` collapses JSON's missing-vs-null
tri-state. Exact-case always; unknown fields ignored by default with
`strict` opt-in (right for APIs; strict for config files, where
"you typo'd `porrt`" beats silence). Dynamic JSON is a sum type —
exhaustive match replaces type-assertion ladders. Numbers keep their
lexical form (`JsonNumber` holds the digits; Twitter's bug
unrepresentable). Serde's format-generic framework itself: rejected
as the enterprise abstraction it is.

## Standard library: batteries, with maintenance

- **1990s** — Python coins "batteries included" and wins a decade of
  adoption on it; then freezes the batteries. `urllib` is stdlib;
  everyone installs `requests`; PEP 594 (2019) hauls out the corpses.
  The disease wasn't batteries — it was batteries plus immortality.
- **2009** — Go proves the upside: stdlib `net/http` makes `Handler`
  the ecosystem's shared currency. And the v1 freeze chains it to
  fossils: `container/list`, the `math/rand` v2 dance, `net/smtp`
  frozen at 2011 assumptions.
- **2016** — the other pole gets its verdict: npm's left-pad (11
  lines, unpublished, half the internet's builds break) shows what
  minimal-stdlib ecosystems are made of. Rust's minimal std → 300
  transitive deps per web service, each a supply-chain surface.
- **2007–10** — Russ Cox's RE2: regex with linear-time guarantee, no
  backtracking. Go ships it as *the* regex; ReDoS becomes
  unrepresentable. (Stack Overflow 2016 and Cloudflare 2019 both went
  down to catastrophic backtracking — in ecosystems that chose the
  other kind.)

**Glide takes** batteries *with* the maintenance cadence Python
lacked: stdlib versions with the language, `glide fix` migrates
callers mechanically, wrong modules get fixed rather than embalmed
beside their replacements. RE2 semantics only (lookbehind is not
worth readmitting ReDoS). Crypto misuse-resistant by default
(libsodium's philosophy: `seal`/`open`, right construction chosen);
**one rng, cryptographically secure** — the fast insecure generator
gets the scary name (`rand.insecure_fast()`), inverting Go's
CVE-trailed math/crypto split. HTTP handlers return
`Result<Response, Error>` (Go's `(w, r)` can't return errors — hence
reinvented error middleware everywhere); client defaults are
production defaults (Go's default client has no timeout — an
incident generator). The out-list is discipline too: no GUI, no ORM,
no ML, ever; protocol clients with living peripheries (SMTP) live on
the `x/` porch where they can churn at the world's speed.

## Build & packages: no scripts, vendored, pinned

- **1976→** — make, then autotools: builds as arbitrary programs.
  Fifty years later this is the supply-chain attack surface —
  `build.rs` and npm lifecycle scripts mean *compiling* someone's
  code runs their code on your machine. event-stream (2018), colors
  (2022), and most of the npm attack catalogue ride install/build
  hooks.
- **1995–2010** — CPAN invents the central registry; npm scales it
  and left-pad (2016) demonstrates the single point of failure.
  Bundler's Gemfile.lock (2010) establishes lockfiles; every
  ecosystem copies it.
- **2009–23** — Go runs the counter-experiment: no build scripts
  (builds execute no user code — and it works), imports as URLs (no
  registry to run or trust), vendoring, minimal version selection
  instead of SAT-solver resolution, and — after the 1.21 lesson —
  toolchain pinning in the manifest.

**Glide takes** Go's whole posture, harder: **no build scripts ever
(this is the hill)**, manifest as data (no hooks, no profiles, no
Cargo-style feature flags — 2ⁿ build variants most of which nobody
ever compiled), vendoring by default (the vendored tree is the
auditable artifact), imports as URLs with hash lockfiles, toolchain
pinned from day one (breaking-changes-are-free makes pinning more
necessary, not less). `embed` is grammar, not a magic comment;
conditional compilation is platform-suffix files + comptime
constants, no preprocessor. Cross-compilation stays a flag *because*
FFI was exiled — cgo is what collapses Go's story. The command
surface is closed: no plugin subcommands (cargo's extension mechanism
reopens the arbitrary-code door).

## SQL & databases

- **2001–06** — Hibernate, then ActiveRecord, define the ORM era;
  Ted Neward names the outcome in 2006: "the Vietnam of computer
  science". The object/relational mismatch doesn't go away; it gets
  amortised into N+1 queries and a DSL that's a worse SQL compiling
  to SQL.
- **2009** — Go's `database/sql`: the right thin-interface instinct,
  with positional `rows.Scan(&u.ID, &u.Name)` (silently misassigns
  on reorder) and placeholder mistakes discovered in production
  (`sql: expected 2 arguments, got 3`). Cancellation retrofitted as
  a whole duplicate `QueryContext` API.
- **2020** — Rust's sqlx: compile-time checked queries — against a
  *live database at build time*, making the build depend on a DB
  being up and migrated. sqlc takes the hermetic road: schema in
  repo, explicit codegen, committed output.

**Glide takes** raw-SQL-plus-mapping made ergonomic: `derive Row`
(comptime column mapping — reorder-safe), named parameters verified
at comptime against the argument struct (*placeholder* checking needs
only the query literal — pure parse, no IO, the unoccupied sweet
spot between "nothing checked" and "build needs a database"),
nullable columns as `String?` (the `sql.NullString` zero-value
disease cured by the same doctrine as JSON), transactions as a
closure (commit on Ok, rollback on Err or panic — Go's
`defer tx.Rollback()` idiom become structure), cancellation ambient
via scopes. No ORM ever; no query-builder DSL; live-schema checking
deliberately declined for hermeticity — the sqlc road when codegen
is wanted.

## Ambient state: a closed set

- **1998** — Java's ThreadLocal ships; over 25 years it becomes a
  framework load-bearing wall (transactions, security contexts,
  request state all riding invisible thread affinity) — then virtual
  threads break the assumption and Loom has to invent ScopedValue
  (2023) to rescue the pattern in scope-shaped form.
- **2014–16** — Go's `context.Context`: having rightly refused
  goroutine-local storage, Go ships `ctx.Value` — an untyped,
  stringly grab bag where auth, transactions, and loggers travel
  invisibly, threaded by hand through every function signature.
- **1960s** — the deep precedent: dynamic scoping in early Lisp —
  variables resolved by call stack — lost to lexical scoping for
  exactly this reason: you can't audit what you can't see.

**Glide takes** a *closed* set of scope-shaped ambients — the
droppability razor decides membership: could program output change if
this value were dropped? Cancellation/deadlines, observation (log
fields, traces), and the Clock ride scopes; **everything
behaviour-affecting travels in parameters or fields, visibly** (the
authenticated user is `req.principal`, which is what well-factored Go
does after its second ctx.Value bug anyway). No general task-local
storage, ever.

## Debugging

- **2014–16** — two quiet revolutions: Mozilla's rr makes
  record-and-replay debugging practical (the interleaving that
  crashed is the interleaving you step through), and Microsoft's
  Debug Adapter Protocol (with VS Code) does for debuggers what LSP
  did for language servers — implement one server, every editor
  works.
- **2010s** — FoundationDB's deterministic simulation testing (later
  TigerBeetle, Antithesis) demonstrates the strongest concurrency
  story available: own the scheduler and the clock, and every failure
  is a rerunnable seed.
- **Since forever** — Go's `//line` directives (inherited from C's
  preprocessor line markers) let generated code debug as its source.

**Glide takes** the staged path its bootstrap makes natural:
interpreter era, a DAP server inside the tree-walker (a tree-walker
is a debugger that hasn't been asked); transpiler era, `//line`
directives + preserved names ride twenty years of delve investment
free (Glide tasks ARE goroutines); native era, DWARF as a dev-tier
guarantee. Differentiators the recorded decisions compound into: the
debugger shows the *scope tree* (`main → serve → request`), not
delve's 40,000 flat anonymous goroutines, and deterministic-seed
replay makes races steppable — possible because Glide owns scheduler
+ clock + hermetic builds; Go and Rust can't do this without heroic
external simulation.

## Adopted from the wider world

Beyond the Go/Rust/Swift/Zig quadrilateral — each riding machinery
already paid for:

- **Distinct types** (Nim; the idea is Ada's derived types, 1983,
  and Haskell's newtype): `type UserId = distinct Int` — same
  representation, no implicit conversion, UserId≠OrderId as a compile
  error. Cheapest safety-per-character in the document.
- **Deterministic scheduling in tests** (FoundationDB/TigerBeetle
  lineage): seeded scheduler in `glide test`; failing interleavings
  become rerunnable seeds; the runner fuzzes schedules hunting races.
  Possibly Glide's most differentiating capability.
- **Property-based testing** (QuickCheck, 2000 — 25 years proven,
  never mainstream because generator-writing friction): `derive
  Arbitrary` deletes the friction; composes with schedule fuzzing.
- **Error return traces** (Zig's real novelty): the chain of `?`
  points an error travelled, dev-tier, riding backtrace machinery.
- **Typed holes** (Haskell/Idris): `?` in expression position turns
  the compiler from judge to collaborator — reports the needed type
  and in-scope candidates, keeps checking.
- **Persistent collections** (Clojure 2007; the scholarship is
  Okasaki 1996 and Bagwell's HAMTs): stdlib `PList`/`PMap` with
  structural sharing — the concrete meaning of "immutable stdlib
  data structures". A module, not the default (Clojure's
  constant-factor tax not paid silently).
- **Supervision policies** (Erlang/OTP, 1998): `supervise(restart:
  .on_failure)` as a scope variant — let-it-crash landing on
  structure already built. For always-on services, likely the
  first-used adopt.

Declined, with reasons recorded: pipe `|>` (method chains are the
pipeline), extension methods/UFCS (fragments "what can this type do"
by import), algebraic effects (coloring generalised — the special
case was already rejected), content-addressed code (Unison — the
most original idea in modern language design, and it abandons files,
and with them grep/git/diff, the whole auditability story), Pony
reference capabilities (full race-freedom at more human budget than
the borrow checker — Glide's 90% at 10% of the concepts).

## Bootstrap: interpreter → self-hosted frontend → transpile to Go

- **1973** — Pascal's P-code: ship a tiny interpreter, bootstrap the
  real compiler through it. The pattern Glide's M1 follows.
- **1983** — Stroustrup's cfront compiles C++ *to C*: transpiling to
  a mature language rides its entire backend, runtime, and debugger
  investment. TypeScript (2012) and Nim run the same play against JS
  and C.
- **1984** — Ken Thompson's "Reflections on Trusting Trust": a
  compiler binary can carry an invisible backdoor through every
  regeneration. The auditable-bootstrap-chain argument, made by the
  man who built Glide's runtime model's ancestor.
- **2015** — Go 1.5 self-hosts by mechanical conversion from C —
  demonstrating both that self-hosting is a milestone worth
  engineering toward and that the runtime is the actual mountain
  (Go's runtime is Go with a hundred pragmas).

**Glide takes** the shortcut with eyes open: tree-walking interpreter
in Go proves the risky semantics (generators, structured concurrency,
comptime); the compiler frontend gets written in Glide and run on the
interpreter (compilers are the ML feature set's best-case workload —
ASTs are sum types, a checker is exhaustive matching); then a
Glide→Go transpiler compiles itself. The runtime model was Go's from
day one *precisely so this lowering is nearly 1:1* — GC, scheduler,
defer, channels all prepaid and battle-tested, cross-compilation and
static binaries free, bootstrap chain auditable from a mainstream
toolchain. No binary seed, no trusting-trust anxiety. LLVM + own
runtime stays the someday-mountain, optional.

## Embedding: the interpreter as a scripting library

- **1988** — Tcl. John Ousterhout builds the first language designed
  as a C *library* first, application second: embed the interpreter,
  extend the host. The "extension language" category starts here.
- **1993** — Lua, at PUC-Rio: a small embeddable extension language
  built for Petrobras data-entry tools becomes *the* game-industry
  scripting layer. Its premise matches Glide's: the host provides the
  capabilities, the script provides the logic.
- **1994** — GNU declares Guile its official extension language;
  adoption stays thin for decades. Evidence that embedding is won by
  being small and pinnable, not by decree.
- **2006→today** — Lua 5.1 freezes in the wild: LuaJIT never leaves
  5.1, Redis embeds 5.1 and stays there, World of Warcraft likewise,
  while Lua-the-language moves on to 5.4. Proof that a *frozen*
  embedded version is a fully-lived life — the precedent for Glide's
  freeze-at-self-hosting plan.
- **2012–2024** — JS interpreters written in Go: otto (2012), goja
  (2016, ES5.1 in pure Go, much of ES6+ later), sobek (2024, Grafana's
  goja fork that scripts k6). The exact structural precedent — an interpreter in Go,
  embedded in Go programs, pinned as an ordinary Go module — except
  the language they embed is untyped.
- **Counter-evidence** — embedding CPython: a large runtime, a GIL,
  and an embedding API that tracks the evolving language. Blender and
  GIMP carry it; nobody calls it light. Tracking a moving language
  from inside host binaries is the expensive branch — the one Glide
  declines by freezing.

**Glide takes** the Lua/goja lane: the tree-walking interpreter (which
must exist for bootstrap anyway) gets a small public Go embedding API;
hosts pin it as a Go module version; stdlib shims are injectable so
the embedder chooses capabilities (untrusted scripts are never handed
`fs` or `net`); and at self-hosting the embedded interpreter freezes
rather than tracking the language, so the evolving language keeps
exactly one implementation. Embedding never argues a language change —
influence flows one way, compiled language to script, by decree.
