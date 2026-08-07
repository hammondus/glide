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
who knows Go and nothing else.

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
  (Project Loom, Java 21) are green threads again, M:N this time.

**Glide takes** Go's model wholesale — green-threaded, GC, one
binary — because the transpiler-to-Go bootstrap gets the
battle-tested runtime for free, and because blocking-code-just-blocks
beats async/await's two-colored functions for the server programs
Glide targets.

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

**Glide takes** the ML construct with Rust's spelling — and skips
Rust's multi-year "match ergonomics" saga (ref/binding-mode rules)
entirely, because no borrow checker means patterns just bind values
and the GC holds them.

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
site, every time.

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
firing only at the propagation point, and `or |e|` for handle-in-place
(the one construct with no direct precedent — flagged unratified in
GRAMMAR.md accordingly).

## String interpolation

- **1957–72** — the format-string era: Fortran's FORMAT, then C's
  `printf`. Format and data live apart; the compiler checks nothing;
  mismatches are UB in C and runtime noise (`%!d(string=hi)`) in Go.
- **1960s–90s** — shells, then Perl (and Ruby's `#{…}`, 1995) put
  the expression *in* the string. Ergonomic, dynamically typed, no
  checking.
- **2013–16** — the typed mainstream converges in one burst: Scala
  `s"…"` (2013), Swift `\(…)` (2014), JS template literals (2015),
  C# `$"…"` (2016), Python f-strings (2016). Every new language since
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
here (`for x in` owns iteration). `i += 1` is the way. (Sole standing
challenge: Craig, pending real-world Glide mileage — the test is
counting would-be `++` sites in real code.)

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
parties. (Motivating bite: Craig's Go query string named `sql`
shadowing `database/sql`, and the diagnosis fog that followed.)

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
beside them.
