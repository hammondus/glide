# Appendix D: Implementation Status

The complete ✓/○ table in one place. **✓** runs in the current
interpreter; **○** is designed and recorded in `DESIGN.md` but not
implemented.

Two standing caveats that apply to everything below:

1. **Nothing is type-checked yet.** Type annotations are parsed, stored
   as strings, and ignored. M4 — the checker — is in progress; as each
   rule becomes static, its row here changes with it.
2. Until then, rules a checker would enforce statically are enforced
   **dynamically** instead, so programs cannot cheat: `mut`, the
   nested-shadow ban, `let … else` divergence, the tail-value rule,
   and match exhaustiveness. They just find out late.

The authoritative sources are `docs/reference/language.md` and
`docs/reference/stdlib.md`; this table summarises them for a reader
working through the book.

---

## Milestones

| Milestone | Target program | Status |
|---|---|---|
| M1 | `wordfreq` — the expression language, no user types | done |
| M2 | `tree` — types, `match`, `impl`, generators, tests | done |
| M3 | `notes` — concurrency, `distinct`, `defer`, http/sql/json | done |

---

## Language

| Feature | Status | Chapter |
|---|---|---|
| `fn`, `let`, `let mut`, `const` | ✓ | 4, 7 |
| Tail expression as return value | ✓ | 3, 7 |
| Tail-value rule enforced | ✓ | 3 |
| Sequential redeclaration | ✓ | 4 |
| Nested-shadow ban | ✓ | 4 |
| Blocks as expressions | ✓ | 4, 9 |
| Explicit discard `_ =` | ✓ | 4 |
| Reserved builtin names | ✓ | 4 |
| Import shadowing conflict as an error | ○ | 4, 29 |
| `Int` (i64), `Float`, `Bool`, `String`, `Rune`, `()` | ✓ | 5 |
| Sized numerics `i8`…`u64`, `f32` (represented and trapping at their own width) | ✓ | 5 |
| Explicit numeric conversion `u8(n)`, trapping; `wrapping_u8()` truncating | ✓ | 5 |
| Sized numerics `i128`, `u128` | ○ | 5 |
| `BigInt`, `Decimal` | ○ | 5 |
| Overflow traps in every tier, at every width | ✓ | 5 |
| `wrapping_*` on every integer width | ✓ | 5 |
| Arbitrary-precision literals until typed | ○ | 5, 34 |
| No truthiness, no implicit conversions | ✓ | 5 |
| Ranges `..` and `..=` | ✓ | 5 |
| Operator traits (`Add`, `Ord`, …) | ○ | 5 |
| Three string delimiters, interpolation | ✓ | 6 |
| Format specs (the closed set) | ✓ | 5, 6 |
| `Display` / `Debug` split | ○ (Debug renders structurally ✓) | 6 |
| `StringBuilder` | ○ | 6 |
| Four print builtins, unbuffered | ✓ | 6 |
| Buffered writer | ○ | 6, 30 |
| Default parameters, named arguments | ✓ | 7 |
| Nested `fn` (non-capturing, hoisted) | ✓ | 7 |
| `mut` parameters on free functions | ○ (does not parse) | 7, 16 |
| Function type spelling `fn(A) -> B` | ○ (closures work ✓) | 8 |
| Closures, capture by binding | ✓ | 8 |
| Loop variables fresh per iteration | ✓ | 8 |
| Task-crossing closures may not capture `mut` | ○ | 8, 25 |
| Method values (`x.method` unapplied) | ○ | 8, 16 |
| One loop, labeled break/continue | ✓ | 9 |
| Expression `if`, subjectless `match` | ✓ | 9, 10 |
| Struct literals banned in control-flow headers | ✓ | 9 |
| All pattern forms | ✓ | 10 |
| Match exhaustiveness | ✓ dynamic, ○ static | 10, 13 |
| `if let`, `let … else` | ✓ | 10, 14 |
| `List`, `Map` (insertion-ordered), tuples | ✓ | 11 |
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
| `Option` boxing (so `Option<Option<T>>` works) | ○ | 14 |
| `distinct` types | ✓ | 15 |
| `distinct` as a map key | ○ | 15 |
| `impl` blocks, associated functions | ✓ | 16 |
| `mut self` receivers, checked | ✓ (user types; builtins ○) | 16 |
| Traits, default methods, `impl Trait for Type` | ✓ | 17 |
| Trait *conformance checking* | ✓ | 17 |
| `Self` as a type; trait defaults checked against `Self: Trait` | ✓ | 17 |
| Generic bound checking; a bound is the complete method set | ✓ | 18 |
| Universe traits `Ord`, `Iterable` | ✓ | 17 |
| `Ord` drives `< <= > >=`; `sorted()` shares the path | ✓ | 17, 18 |
| `==` structural, no `Eq` trait, not redefinable | ✓ | 5, 17 |
| Arithmetic operator traits (`Add`, `Mul`), `derive Ord` | ○ | 18 |
| Trait composition `trait A: B + C` | ○ | 17 |
| `any Trait` (boxed trait objects) | ○ | 17 |
| Orphan rule | ○ | 17 |
| Generic syntax and bounds | ✓ parsed, ○ checked | 18 |
| Monomorphisation | ○ | 18 |
| Const generics | ○ | 18 |
| `Result`, `?`, `.context()` | ✓ | 19 |
| `?`-conversion via `E.from` | ✓ | 19 |
| `??` on a Result | ✓ | 19 |
| Constructing a dynamic `Error` in user code | ○ | 19 |
| `err.find<T>()` chain walking | ○ | 19 |
| Error return traces | ○ | 19 |
| Panics; no `recover` | ✓ | 20 |
| Panic kills the task, not the process | ✓ in a scope; ○ outside | 20, 25 |
| `defer`, `errdefer`, block-scoped | ✓ | 21 |
| `test` blocks, `expect`, property tests + shrinking | ✓ | 22 |
| `require` | ○ | 22 |
| `bench` execution | ○ (parses, skips) | 22 |
| `derive Arbitrary` | ○ | 22 |
| Deterministic schedule fuzzing | ○ | 22 |
| Iterators; `map`/`filter`/`take`/`enumerate`/`zip` | ✓ | 23 |
| `collect`/`count`/`sum` | ✓ | 23 |
| `skip`, `take_while`, `fold`, `any`, `all`, … | ○ | 23 |
| Generators (`yield`, `yield from`) | ✓ | 24 |
| `scope`, `spawn`, `join`, the four rules | ✓ | 25 |
| Cancellation, `scope(timeout:)`, `s.deadline()` | ✓ | 26 |
| `Duration`, `Instant`, suffix constructors | ✓ | 26 |
| `time` module (formatting, parsing, calendars) | ○ | 26 |
| `Date`, `TimeOfDay`, `ZonedTime` | ○ | 26 |
| Channels, split halves, mpmc, `for v in rx` | ✓ | 27 |
| Ownership transfer on send (enforced) | ○ | 27 |
| `Mutex<T>` | ○ | 27 |
| `select` with arm guards | ✓ | 28 |
| Modules, `pub`, inert imports | ✓ | 29 |
| Multi-file / multi-module resolution | ○ | 29 |
| `unsafe`, `embed`, `derive` keywords | ○ (parse error today) | 30, 34 |
| Comptime const evaluation | ○ (M2: pure expressions only) | 34 |
| Comptime reflection API | ○ | 34 |
| `derive(Json, Row, Debug, Enum, Arbitrary)` | ○ | 34 |
| Runtime reflection | **never** | 34 |
| AST macros | **never** | 34 |
| `or \|e\| { … }` | **declined** (deferred with a test) | 19 |

---

## Standard library

### Builtins ✓

`print`, `println`, `eprint`, `eprintln`, `Ok`, `Err`, `Some`, `None`,
`expect`, `channel`.

### Modules

| Module | Status |
|---|---|
| `os` — `args()`, `exit(code)` | ✓ |
| `fs` — `read_string(path)` | ✓ |
| `json` — `encode`, `decode` (dynamic) | ✓ shim |
| `http` — router, server, client, response constructors | ✓ shim |
| `sql` — SQLite via pure-Go `modernc.org/sqlite` | ✓ shim |
| `time` — `now`, `sleep`, `after` | ✓ |
| `log`, `regex`, `crypto`, `tls`, `process`, `flag`, `template`, `rand`, compression | ○ |

### Methods

**String** ✓: `len`, `trim`, `trim_prefix`, `trim_suffix`, `contains`,
`starts_with`, `ends_with`, `split`, `split_whitespace`, `lines`,
`replace`, `to_upper`, `to_lower`, `repeat`, `parse_int`, `runes`,
`bytes`, `cmp`. No `s[i]`, permanently. No `find`/`index_of` yet.

**Int** ✓: `cmp`.

**List** ✓: `len`, `push`, `sorted`, `sort_by`, `repeat`, `join`,
`iter`. Indexing reads and writes; out of bounds panics.

**Map** ✓: `len`, `entries`. Indexing returns `V?`.

**Result** ✓: `context`.

**Iterator** ✓: `take`, `map`, `filter`, `enumerate`, `zip`, `collect`,
`count`, `sum`.

---

## Known interpreter warts

Recorded so nobody mistakes them for semantics. Each is a tier artifact
with a designed fix.

| Wart | Effect | Fix |
|---|---|---|
| **Receiver-mut unenforced on builtins** | `xs.push(3)` works through a `let` | the checker |
| **Defaults fill through function values** | `let f = connect; f("db")` works | the checker (defaults are declaration sugar, not type) |
| **Decoded JSON keys are sorted** | encode preserves order, decode does not | `derive Json` with an ordered map |
| **Structs share on assignment** | `let b = a; a.x = 1` changes `b` | value semantics in the compiled tier |
| **Nested tuple access `x.0.1`** | lexes `0.1` as a float | lexer special case |
| **String literal inside an interpolation** | `"{xs.join(\", \")}"` fails to lex | hoist to a `let` |
| **Failing SQL panics** | a driver-level error raises `cancelUnwind` instead of returning `Err`, because the host context is released before the cancellation check (`sqlmod.go`) | one-line fix; see Chapter 33 |

Two behaviours that look like warts and are **not**:

- **Normal scope exit joins children without cancelling them**, so a
  blocked child deadlocks. That is rule 1 (Chapter 25); use an early
  exit (`return`) to cancel first.
- **Generators cost a goroutine each**, one per `yield from` level.
  That is the tree-walker's chosen implementation, replaced by a state
  machine in the compiled tier.

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
| Generics | not specialised | monomorphised |
| Overflow | trapped | wrapped |
| `derive`d codecs | structural walk at runtime | generated straight-line code |

**Do not tune against the interpreter.** Use it to check semantics.
