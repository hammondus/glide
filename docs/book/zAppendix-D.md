# Appendix D: Implementation Status

The complete ✓/○ table in one place. **✓** runs in the current
interpreter; **○** is designed and recorded in `DESIGN.md` but not
implemented.

Two standing facts that apply to everything below:

1. **Every program is type-checked before it runs, in every tier.**
   There is no `--no-check` and no plan for one. `glide check` reports
   and stops; `glide run` and `glide test` run the identical check
   first. Chapter 19 is the chapter.
2. **The checker under-approximates.** Anything it cannot model passes
   in silence and is caught at runtime instead, so a ✓ row means
   "checked or enforced", and closing a gap can never break a working
   program. The known gaps have their own section below.

The authoritative sources are `docs/reference/language.md` and
`docs/reference/stdlib.md`; this table summarises them for a reader
working through the book.

---

## Milestones

| Milestone | Target | Status |
|---|---|---|
| M1 | `wordfreq` — the expression language, no user types | done |
| M2 | `tree` — types, `match`, `impl`, generators, tests | done |
| M3 | `notes` — concurrency, `distinct`, `defer`, http/sql/json | done |
| M4 | *a property*: every annotation checked, no wrong answers left | done |
| — M4a | the representation: type parameters and bounds reach the AST | done |
| — M4b | the checker core, mandatory in both tiers | done |
| — M4c | bounds, conformance, `Ord`, sized numerics, boxed `Option`, exhaustiveness | done |
| **Next** | **bootstrap step 3 — the frontend rewritten in Glide** | in progress |

---

## Language

| Feature | Status | Chapter |
|---|---|---|
| `#!` shebang skipped on line 1 (`chmod +x` scripts run) | ✓ | 2 |
| `fn`, `let`, `let mut`, `const` | ✓ | 4, 7 |
| Tail expression as return value | ✓ | 3, 7 |
| Tail-value rule enforced | ✓ (by the evaluator, not the checker) | 3, 19 |
| Sequential redeclaration | ✓ | 4 |
| Nested-shadow ban | ✓ (by the evaluator, not the checker) | 4, 19 |
| Blocks as expressions | ✓ | 4, 9 |
| Explicit discard `_ =` | ✓ | 4 |
| Reserved builtin names | ✓ | 4 |
| Import shadowing conflict as an error | ○ | 4, 30 |
| `Int` (i64), `Float`, `Bool`, `String`, `Rune`, `()` | ✓ | 5 |
| Sized numerics `i8`…`u64`, `f32` (represented and trapping at their own width) | ✓ | 5 |
| Explicit numeric conversion `u8(n)`, trapping; `wrapping_u8()` truncating | ✓ | 5 |
| Integer literal range checked against a sized type, both ends | ✓ | 5 |
| Sized numerics `i128`, `u128` | ○ (deferred past M4: Go has no native 128-bit int) | 5 |
| `BigInt`, `Decimal` | ○ | 5 |
| Overflow traps in every tier, at every width | ✓ | 5 |
| `wrapping_*` on every integer width | ✓ | 5, 21 |
| Arbitrary-precision constant expressions (`1 << 100`) | ○ (waits for comptime) | 5, 36 |
| No truthiness, no implicit conversions | ✓ | 5 |
| Ranges `..` and `..=` | ✓ | 5 |
| `abs`, `min`, `max`, `pow` as methods at every width | ✓ | 5 |
| `math` module: `sqrt`, rounding, classification, `pi`/`e`/`inf`/`nan` | ✓ | 5 |
| Module-level *values*, not just functions | ✓ | 5 |
| `Ord` drives `< <= > >=` and `sorted()` from one `cmp` | ✓ | 5, 17 |
| `Float`'s `cmp` is a total order (NaN last); `==` stays IEEE 754 | ✓ | 5, 17 |
| `==` structural and universal; a Map ignores insertion order | ✓ | 5, 11 |
| Comparing two types that can never be equal is an error | ✓ | 5, 19 |
| Arithmetic operator traits (`Add`, `Mul`), `derive Ord` | ○ (deferred: no forcing need yet) | 5, 18 |
| Three string delimiters, interpolation | ✓ | 6 |
| Format specs (the closed set) | ✓ | 5, 6 |
| `Display` / `Debug` split | ○ (Debug renders structurally ✓) | 6 |
| `StringBuilder` | ○ | 6 |
| Four print builtins, unbuffered | ✓ | 6 |
| Buffered writer | ○ | 6, 31 |
| Default parameters, named arguments | ✓ | 7 |
| Nested `fn` (non-capturing, hoisted) | ✓ | 7 |
| `mut` parameters on free functions | ○ (does not parse) | 7, 16 |
| Function type spelling `fn(A) -> B`, in any type position | ✓ | 8 |
| Optional *function* type | ○ (no spelling) | 8 |
| Closures, capture by binding | ✓ | 8 |
| Closure parameter annotations, any subset | ✓ | 8, 19 |
| Loop variables fresh per iteration | ✓ | 8 |
| Task-crossing closures may not capture `mut` | ✓ | 8, 26 |
| Method values (`x.method` unapplied) | ○ | 8, 16 |
| One loop, labeled break/continue | ✓ | 9 |
| Expression `if`, subjectless `match` | ✓ | 9, 10 |
| Struct literals banned in control-flow headers | ✓ | 9 |
| All pattern forms | ✓ | 10 |
| Match arms separated by newline **or comma** | ✓ | 10 |
| Match exhaustiveness, statically | ✓ | 10, 13, 19 |
| Unreachable match arm reported | ✓ | 10, 19 |
| `if let`, `let … else` | ✓ | 10, 14 |
| `List`, `Map` (insertion-ordered), tuples | ✓ | 11 |
| List: `pop`, `insert`, `remove`, `extend`, `first`, `last`, `contains`, `index_of`, `reversed`, `slice` | ✓ | 11 |
| Map: `keys`, `values`, `contains_key`, `remove` | ✓ | 11 |
| Spread in list literals | ✓ | 11 |
| `Set`, `PList`/`PMap` | ○ | 11 |
| Nested tuple access `x.0.1` | ○ (lexer limitation) | 11 |
| Structs, mandatory init, `pub` fields | ✓ | 12 |
| Struct update `..base` | ✓ | 12 |
| Struct value semantics on assignment | ○ (shared today) | 12 |
| `Default` trait | ○ | 12 |
| Sum types, named-field variants, dot shorthand | ✓ | 13 |
| Explicit discriminants | ○ | 13 |
| `T?` / `Option`, `??` | ✓ | 14 |
| `Option` **boxed** — `Option<Option<T>>` works, `Some(None) != None` | ✓ | 14 |
| `distinct` types, checked statically | ✓ | 15 |
| `distinct` as a map key | ○ | 15 |
| `impl` blocks, associated functions | ✓ | 16 |
| `mut self` receivers, checked — **including builtins** | ✓ | 16 |
| Traits, default methods, `impl Trait for Type` | ✓ | 17 |
| Trait *conformance checking* | ✓ | 17 |
| `Self` as a type; trait defaults checked against `Self: Trait` | ✓ | 17 |
| Universe traits `Ord`, `Iterable` | ✓ | 17 |
| Trait composition `trait A: B + C` | ○ | 17 |
| `any Trait` (boxed trait objects) | ○ | 17 |
| Orphan rule | ○ | 17 |
| Generic syntax and bounds | ✓ | 18 |
| Generic bound checking; a bound is the complete method set | ✓ | 18, 19 |
| Bound enforced when `T` appears only inside a container | ○ (silent gap) | 18, 19 |
| Undetermined type parameter reported | ✓ | 18, 19 |
| Use-site type arguments in expressions (`parse<Config>(s)`) | ○ | 18 |
| Monomorphisation | ○ | 18, 37 |
| Const generics | ○ | 18 |
| Mandatory checking in every tier; `glide check` | ✓ | 19 |
| Checker side tables drive variant shorthand, `Option` and `Error` boxing | ✓ | 19 |
| Conformance corpus, coverage measured from the checker's own source | ✓ | 19 |
| `Result`, `?`, `.context()` | ✓ | 20 |
| `?`-conversion via `E.from` | ✓ | 20 |
| `??` on a Result | ✓ | 20 |
| Constructing a dynamic `Error` in user code (`Err("msg")`) | ✓ | 20 |
| `Error` boxed at the value level; `message`/`cause`/`context`/`find` | ✓ | 20 |
| `e.find(SomeType)` chain walking (the type as a **value**) | ✓ | 20 |
| A variant pattern against an `Error` is a reported error | ✓ | 20 |
| Error return traces | ○ | 20 |
| Panics; no `recover` | ✓ | 21 |
| Panic kills the task, not the process | ✓ in a scope; ○ outside | 21, 26 |
| `defer`, `errdefer`, block-scoped | ✓ | 22 |
| `test` blocks, `expect`, property tests + shrinking | ✓ | 23 |
| `require` | ○ | 23 |
| `bench` execution | ○ (parses, skips) | 23 |
| `derive Arbitrary` | ○ | 23 |
| Deterministic schedule fuzzing | ○ | 23 |
| Iterators; `map`/`filter`/`take`/`enumerate`/`zip` | ✓ | 24 |
| `collect`/`count`/`sum` | ✓ | 24 |
| `skip`, `take_while`, `fold`, `any`, `all`, … | ○ | 24 |
| Generators (`yield`, `yield from`) | ✓ | 25 |
| Generator yields checked against the declared `Iterator<T>` | ✓ | 19, 25 |
| `scope`, `spawn`, `join`, the four rules | ✓ | 26 |
| Cancellation, `scope(timeout:)`, `s.deadline()` | ✓ | 27 |
| `Duration`, `Instant`, suffix constructors | ✓ | 27 |
| `time` module (formatting, parsing, calendars) | ○ | 27 |
| `Date`, `TimeOfDay`, `ZonedTime` | ○ | 27 |
| Channels, split halves, mpmc, `for v in rx` | ✓ | 28 |
| A sent `None` is an ordinary element, not end-of-stream | ✓ | 28 |
| Ownership transfer on send (enforced) | ○ | 28 |
| `Mutex<T>` | ○ | 28 |
| `select` with arm guards | ✓ | 29 |
| Modules, `pub`, inert imports | ✓ | 30 |
| Multi-file / multi-module resolution | ○ | 30 |
| `unsafe`, `embed`, `derive` keywords | ○ (parse error today) | 31, 36 |
| Comptime const evaluation | ○ (M2 shim: pure expressions only) | 36 |
| Comptime reflection API | ○ | 36 |
| `derive(Json, Row, Debug, Enum, Arbitrary)` | ○ | 36 |
| Runtime reflection | **never** | 36 |
| AST macros | **never** | 36 |
| `or \|e\| { … }` | **declined** (deferred with a test) | 20 |

---

## Standard library

### Builtins ✓

`print`, `println`, `eprint`, `eprintln`, `Ok`, `Err`, `Some`, `None`,
`expect`, `channel`.

### Modules

| Module | Status |
|---|---|
| `os` — `args`, `exit`, `env`, `set_env`, `cwd`, `chdir` | ✓ |
| `fs` — `read_string`, `write_string`, `append_string`, `exists`, `is_dir`, `remove`, `remove_all`, `mkdir_all`, `rename`, `list_dir`, `join` | ✓ |
| `process` — `run(cmd, args)` → `Output` with `status`/`ok`/`stdout`/`stderr` | ✓ |
| `math` — `sqrt`, `floor`, `ceil`, `round`, `trunc`, `is_nan`, `is_infinite`, `is_finite`, `pi`, `e`, `inf`, `nan` | ✓ |
| `json` — `encode`, `decode` (dynamic) | ✓ shim |
| `http` — router, server, client, response constructors | ✓ shim |
| `sql` — SQLite via pure-Go `modernc.org/sqlite` | ✓ shim |
| `time` — `now`, `sleep`, `after` | ✓ |
| `log`, `regex`, `crypto`, `tls`, `flag`, `template`, `rand`, `path`, compression | ○ |

### Methods

**String** ✓: `len`, `trim`, `trim_prefix`, `trim_suffix`, `contains`,
`starts_with`, `ends_with`, `split`, `split_whitespace`, `lines`,
`replace`, `to_upper`, `to_lower`, `repeat`, `parse_int`, `runes`,
`bytes`, `cmp`. No `s[i]`, permanently. No `find`/`index_of` yet.

**Int, and every integer width** ✓: `cmp`, `abs` (signed only), `min`,
`max`, `pow`, `wrapping_add`, `wrapping_sub`, `wrapping_mul`,
`wrapping_neg`, plus one truncating conversion per width
(`wrapping_u8`, …).

**Float / f32** ✓: `cmp`, `abs`, `min`, `max`, `pow`. Everything
Float-only is in `math`.

**List** ✓: `len`, `push`, `pop`, `insert`, `remove`, `extend`,
`first`, `last`, `contains`, `index_of`, `sorted`, `reversed`, `slice`,
`sort_by`, `repeat`, `join`, `iter`. Indexing reads and writes; out of
bounds panics; mutation requires a `mut` path.

**Map** ✓: `len`, `entries`, `keys`, `values`, `contains_key`,
`remove`. Indexing returns `V?`.

**Result** ✓: `context`.

**Error** ✓: `message`, `cause`, `context`, `find`.

**Output** (from `process.run`) ✓: `status`, `ok`, `stdout`, `stderr`.

**Iterator** ✓: `take`, `map`, `filter`, `enumerate`, `zip`, `collect`,
`count`, `sum`.

---

## Known checker gaps

All three are silent, all three under-approximate, and all three are
safe to close later because closing them cannot reject a program that
works today.

| Gap | Effect |
|---|---|
| **A bound is not enforced through a container** | `fn top<T: Ord>(xs: List<T>)` accepts a `List<Blob>` where `Blob` has no `Ord`. Passing a bare `T` *is* checked; the failure arrives at runtime as "Blob has no method cmp" |
| **The tail-value rule is enforced by the evaluator** | A no-arrow function whose body ends in a value is an error — but only when that function is *called*, so `glide check` does not report it |
| **The nested-shadow ban is enforced by the evaluator** | A shadowing `let` on a branch that never runs is never reported, even though the rule depends only on scope structure |

---

## Known interpreter warts

Recorded so nobody mistakes them for semantics. Each is a tier artifact
with a designed fix.

| Wart | Effect | Fix |
|---|---|---|
| **Structs share on assignment** | `let mut a = P{x:1}` then `let b = a` then `a.x = 99` makes `b.x` 99 | value semantics in the compiled tier |
| **Defaults fill through function values** | `let f = connect; f("db")` supplies `connect`'s defaults | the checker (defaults are declaration sugar, not type) |
| **Decoded JSON keys are sorted** | encode preserves order, decode does not | `derive Json` with an ordered map |
| **Nested tuple access `x.0.1`** | lexes `0.1` as a float | lexer special case |

Two behaviours that look like warts and are **not**:

- **Normal scope exit joins children without cancelling them**, so a
  blocked child deadlocks. That is rule 1 (Chapter 26); use an early
  exit (`return`) to cancel first.
- **Generators cost a goroutine each**, one per `yield from` level.
  That is the tree-walker's chosen implementation, replaced by a state
  machine in the compiled tier.

Three warts earlier editions listed have been **fixed**: builtin
methods now enforce receiver-`mut`; a string literal inside an
interpolation lexes correctly — `"{xs.join(", ")}"` works; and a
failing SQL statement returns `Err` instead of escaping as a raw
cancellation panic (the host context was released before the
cancellation check in `sqlmod.go`, so every driver failure looked like
a cancellation). (Escaping
those inner quotes still does not, and should not: the backslash makes
them the outer string's own.)

---

## Performance, by tier

| | Interpreter (today) | Compiled (○) |
|---|---|---|
| General speed | ~2 orders of magnitude slower than Go | target: faster than Go |
| Compute parallelism | **none** (one interpreter lock) | true parallelism |
| IO concurrency | works correctly | works correctly |
| Generators | goroutine + channel each | state machine |
| Field access | hash lookup | fixed offset |
| Calls | environment map allocation | registers, inlining |
| `match` | structural comparison | jump table |
| Generics | not specialised (type-erased) | monomorphised |
| Overflow | trapped | trapped — the same answer, by design |
| `derive`d codecs | structural walk at runtime | generated straight-line code |

**Do not tune against the interpreter.** Use it to check semantics —
and note that for scripts (Chapter 32) the interpreter's speed is
almost never the number that matters, because a script's runtime is
dominated by the processes it starts.
