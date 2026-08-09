# Glide Language Reference

Terse lookup reference, modelled on Go's split between the language
spec and the stdlib docs (the stdlib is [stdlib.md](stdlib.md)). The
book (`docs/book/`) teaches; this file reminds.

**Status markers** — the language is ahead of its implementation:

- ✓ — runs in the current interpreter (M4b)
- ○ — designed (recorded in `DESIGN.md`), not yet implemented; using
  it today is a parse, check, or runtime error

Annotations are checked as of M4b: every program is type-checked
before it runs, in every tier, with no way to skip it (`glide check`
exists as a report-and-stop convenience; `--no-check` never will).
The checker reports only what it is certain of — anything it does not
yet model is treated as unknown and passes in silence — so a ✓ row
means "checked or enforced", and the rows still marked ○ are the ones
where the *evaluator* is the only thing standing between you and the
mistake. Generic bound checking, trait conformance, `Ord`, boxed
`Option` and match exhaustiveness all landed in **M4c**.

## Source files

- Extension `.gld`, UTF-8. ✓
- Comments: `//` to end of line — the entire comment grammar; there
  is no `/* */`. ✓
- No semicolons. A newline ends a statement when the previous token
  can end an expression (identifier, literal, `)`, `]`, `}`, `?`);
  a trailing operator or `.` continues the line, and so does a line
  *beginning* with `.` — multi-line adapter chains put each
  `.filter(…)` on its own line. Two exceptions, both case-decided:
  `..` at line start is a range token, and `.Red` (capitalised after
  the dot) is the variant shorthand starting a new statement —
  methods and fields are lowercase, so the cases cannot collide.
  `else` sits on the same line as its `}`. ✓
- Braces are mandatory on every block, even one-line bodies. ✓
- Case is meaningful: Capitalised = type / variant / constructor;
  lowercase = binding / function / field. In patterns, `Circle(r)`
  matches, `point` binds. ✓

## Keywords

Reserved words; none can be used as an identifier.

| Keyword | Meaning | Status |
|---|---|---|
| `fn` | function declaration | ✓ |
| `let` | binding declaration (the only one; no `:=`) | ✓ |
| `mut` | mutable marker (bindings, receivers, params) | ✓ |
| `if` / `else` | branch; an expression when both arms present | ✓ |
| `for` / `in` | the only loop: `for {}`, `for cond {}`, `for pat in xs {}` | ✓ |
| `match` | multi-way branch over patterns; an expression | ✓ |
| `return` | early exit (tail expression is the normal return) | ✓ |
| `type` | struct / sum type declaration | ✓ |
| `struct` | struct body marker in `type X = struct { … }` | ✓ |
| `impl` | method block: `impl Type { … }`, `impl Trait for Type { … }` | ✓ |
| `import` | module import, top of file only, inert (runs nothing) | ✓ |
| `yield` | emit from a generator; `yield from iter` delegates | ✓ |
| `pub` | visibility (not Go's capitalisation trick) | ✓ |
| `true` / `false` | Bool literals | ✓ |
| `break` / `continue` | loop control; parse error outside a loop | ✓ |
| `defer` | block-scoped cleanup: `defer { … }` — runs LIFO at exit of the *enclosing block* (per-iteration in a loop body), on normal exit, `return`/`break`/`continue`, and panic unwind; skipped by `os.exit`. Body may not `return` (runtime error) or `break`/`continue` an enclosing loop (parse error) | ✓ |
| `errdefer` | cleanup only on the error path: a `return` carrying an `Err` (what `?` propagates), or a panic — not a plain return, not loop control | ✓ |
| `trait` | trait declaration: bodied methods are defaults, inherited by any type declaring `impl Trait for Type` unless overridden; body-less methods are required signatures, **verified** — an impl that does not satisfy them is a compile error naming the method ✓; all trait methods take `self`. `Self` is the receiver's type inside a trait or impl ✓. A default body is checked against `Self: ThisTrait`, so it may call the trait's own methods and nothing else ✓. Two traits supplying the same unoverridden default is an error at the call, naming both | ✓ |
| `const` | module-level `const name = expr` (lowercase — it's a binding): evaluated once at load, declaration order, immutable | ✓ (M2 shim: initializers are restricted to pure expressions — no fn/module calls; full comptime evaluation arrives with the compiler ○) |
| `scope` | structured-concurrency scope, an expression: `scope [(config)] [handle] { body }`. `s.spawn(f) -> Task`, `t.join()` returns what the closure returned; exit joins all children on every path, early exit cancels first; unjoined `Err` fails the scope at normal exit (first spawned wins, `?`-conversion applies); child panic cancels siblings and re-panics at exit; cancellation is uncatchable, runs `defer`+`errdefer`, delivered at blocking ops (`join`, generator handoffs; channel ops and `sleep` when they land) | ✓ (`scope(timeout: 5.s)` / `scope(deadline: t)` evaluate to `Result<T, Timeout>`: `Ok(v)` on completion, `Err(Timeout)` when the clock wins — `Timeout` matches the bare pattern `Timeout` and converts via `E.from(Timeout)`) |
| `select` | an expression; arms line-separated like match: `pat = rx.recv() => expr`, `tx.send(v) => expr`, `else => expr` (non-blocking), optional `if cond` guard per arm (evaluated once at entry; false removes the arm — the nil-channel trick, replaced). Same channel may appear in several recv arms (`Some`/`None` split); the delivered value tries their patterns in order, no match → runtime error. Operands evaluate once at entry; uniformly random among ready; blocking select is a cancellation point (no `ctx.Done` arm exists or is needed). All arms disabled with no `else` → runtime error; zero arms → parse error | ✓ |

Contextual keywords — only special at top level, followed by a string
literal; otherwise ordinary identifiers (`let test = …` is legal):

| Word | Meaning | Status |
|---|---|---|
| `test` | test block: `test "name" { … }`, property form `test "n" (xs: List<Int>) { … }` | ✓ |
| `bench` | benchmark block (parses; runner skips it) | ✓ |

Reserved for later eras (using one today is a parse error):

| Keyword | Meaning | Status |
|---|---|---|
| `unsafe` | `unsafe fn` / `unsafe { }` | ○ |
| `embed` | build-time file embedding | ○ |
| `derive` | comptime derive (Json, Debug, Enum, …) | ○ |

Also unbindable: the free builtin names (`print`, `println`,
`eprint`, `eprintln`, `expect`, `channel`, `Ok`, `Err`, `Some`) are
reserved — declaring them is an error ✓ — and `None` is a literal.

## Literals

| Form | Example | Status |
|---|---|---|
| Int | `42`, `10_000_000` (underscores anywhere) | ✓ |
| Float | `3.14` | ✓ |
| Bool | `true`, `false` | ✓ |
| String | `"text {expr} {expr:spec}"` — always-interpolating | ✓ |
| Escapes | `\n` `\t` `\r` `\\` `\"` `\{` `\}` | ✓ |
| Unicode escape | `\u{1F600}` — braced hex, one form; also in rune literals | ✓ |
| Raw string | `` `no escapes, no interpolation, multiline` `` — cannot contain a backtick (by definition of raw) | ✓ |
| Rune | `'a'` (its own type, not an int alias; `'ab'` is a lex error; escape family + `\u{…}` work) | ✓ |
| List | `[1, 2, 3]`, empty `[]` | ✓ |
| Map | `[:]` (empty; annotation required), inserts via `m[k] = v` | ✓ |
| Tuple | `(a, b)` — parens + comma; `(x)` is grouping | ✓ |
| Unit | `()` | ✓ |
| Range | `lo..hi` half-open, `lo..=hi` inclusive (Int only; `..=` desugars to `hi + 1`, so it cannot include the maximum Int — loud error) | ✓ |
| Struct | `User{ name: "x", id: 7 }` | ✓ |
| Struct update | `Config{ timeout: 5, ..base }` — copy-with-changes; base untouched; `..base` last | ✓ |
| List spread | `[a, ..xs, b]` — splices any iterable (list, range, iterator) | ✓ |

Format specs inside interpolation, all ✓ — the complete set,
deliberately closed: `{n:6}` width (right-align) · `{s:-6}`
left-align · `{id:04}` zero-pad (numbers) · `{x:.2}` decimal places
(Float) · `{x:8.2}` width+precision · `{n:,}` thousands grouping
(Int, or Float with precision: `{x:,.2}`; comma literally — no
locale) · `{n:hex}` (Int, lowercase, no prefix) · `{v:?}` Debug
(structural render; strings quoted/escaped, structs/variants named).
A spec that doesn't fit the value's type is an error, never noise.
Declined: fill characters, centering, sign control, bin/oct,
scientific, `_`-grouping.

## Types

Type annotations are written but unchecked in M2.

| Type | Notes | Status |
|---|---|---|
| `Int` | i64 on every target (the M2 value is Go int64) | ✓ |
| `Float` | f64 | ✓ |
| `Bool`, `String`, `()` | | ✓ |
| `List<T>`, `Map<K, V>` | **Map iteration is insertion order, and that is specified** — not incidental, and the compiled tier must reproduce it. Deleting a key drops it from the order; re-inserting appends. `json.encode` emits object keys in this order | ✓ |
| `(A, B)` tuples | fields `.0`, `.1`. Two members minimum: `(T)` is a parse error, since a 1-tuple has no constructor and parenthesising a type buys nothing | ✓ |
| `T?` = `Option<T>` | **boxed**: every `T?` is `None` or `Some(v)`, never a bare `v`. `Option<Option<T>>` is representable and `Some(None)` differs from `None`; spell it long-form (`Option<Int?>`) since `T??` cannot lex. Implicit `T -> T?` still applies. `Some(x)` renders as `Some(x)`, matching `None` | ✓ |
| `Result<T, E>` | `Ok(v)` / `Err(e)` | ✓ |
| `Range` | value of `lo..hi` | ✓ |
| `fn(A) -> B` | one function type for named fns, closures, method values | The *type* exists inside the checker — a closure passed to `filter`/`sort_by`/`map` is checked against the parameter's signature ✓ — but it cannot yet be **written** in an annotation: `fn apply(f: fn(Int) -> Int, …)` is a parse error ○. Method values unapplied (`x.method`) ○ |
| `i8…i64`, `u8…u64`, `f32` | sized numerics | ✓ — declared, checked, and represented at their own width. Literal range is enforced exactly at both ends (`let x: u8 = 300` and `let x: u64 = -1` are compile errors; `let x: u64 = 18446744073709551615`, `let x: i8 = -128` and `let x: Int = -9223372036854775808` are not), no implicit conversions between widths (`u8 + u16` is a compile error), and arithmetic **traps at the declared width** — `let x: u8 = 250` then `x + 10` is a positioned runtime error, not 260. The width lives on the value, not on the checker's annotation, so it survives type erasure: `+` inside `fn double<T>(v: T) -> T` traps at 8 bits when called with a `u8`. Conversion is **explicit and only explicit**: `u8(n)`, `Int(c)`, `Float(k)` — the type's own name applied to a value, Go's spelling. Out of range **traps** (`u8(300)` where 300 is a constant is a compile error; where it is a value it is a positioned runtime error) rather than truncating silently as Go does, and `n.wrapping_u8()` is the truncating form. Conversion is defined between the integer widths, the floats and `Rune`, and nowhere else — `String(65)` and `Bool(1)` are errors. Float→integer truncates toward zero. Every primitive type name is **reserved**, since `u8` now means something in expression position; a local `let u8 = 5` still shadows it, as in Go ✓ |
| `i128`, `u128` | 128-bit integers | ○ — ratified, deferred past M4 (Go has no native 128-bit integer; see `glide/DESIGN-DECISIONS.md`) |
| `Rune` | own type; `==`/ordering with other Runes only; Display prints the character, Debug quotes it | ✓ |
| `distinct` | `type NoteId = distinct Int` — nominal wrapper: explicit construction `NoteId(7)` (wrong base type errors), **no inherited operators** (`NoteId(1) + 1` errors — an id is not a quantity), `==` within the same distinct type only, pattern `NoteId(n)` destructures, `.value()` unwraps, `impl NoteId { … }` works like any user type. Codecs (json/sql) unwrap at the boundary | ✓ (dynamic; the checker makes it static) |
| `Duration`, `Instant` | see stdlib Concurrency/Time | ✓ |
| `BigInt`, `Decimal` | stdlib, by name, never silent promotion | ○ |
| `any Trait` | boxed trait object, visible dispatch | ○ |

## Operators

Binary, loosest to tightest (levels from the parser):

| Prec | Operators | Notes |
|---|---|---|
| 1 | `??` | coalescing, lazily: `None ?? d` and `Err(_) ?? d` take the default; `Ok(v) ?? d` unwraps to `v`. On a Result the error is discarded deliberately |
| 2 | `..` `..=` | range construction: half-open / inclusive |
| 3 | `\|\|` | short-circuit or |
| 4 | `&&` | short-circuit and |
| 5 | `==` `!=` | byte equality on strings |
| 6 | `<` `<=` `>` `>=` | |
| 7 | `+` `-` | |
| 8 | `*` `/` `%` | |

All ✓. Unary: `!` (Bool), `-` (numeric) ✓. Postfix: call `f(x)`,
index `xs[i]` / `m[k]`, field `.name`, tuple field `.0`, try `?` ✓.

- `?` unwraps a `Result` or returns the `Err` to the caller ✓
  (on `Option` — not adopted; use `??` / `if let`). Conversion fires
  at the propagation point ✓ (M2 shim): when the enclosing fn
  declares `Result<_, E>` and the error isn't already an `E`,
  `E.from(err)` converts it — declare `fn from(e: …) -> E` in
  `impl E`. No `from` → the error propagates untouched. Closures
  never convert (no declared return type).
- No `++`/`--` (use `+= 1`), no ternary (use expression-`if`), no
  assignment-as-expression, no user-defined operators.
- String indexing `s[i]` does not exist, by design.
- `or |e| { … }` — fought and declined (DESIGN.md, Errors): use
  `?`-conversion, `??`, or `match`. A residue test in DESIGN.md's
  open questions can revive it.

Assignment (statements, not expressions): `=` ✓, `+=` `-=` `*=`
`/=` `%=` ✓ (on names, fields, and index targets; requires a `mut`
path). Discard is explicit: `_ = expr` ✓.

## Declarations

- `let name = expr` — immutable binding; `let mut` to allow
  reassignment/mutation. ✓
- Destructuring `let`: `let (a, b) = pair`, `let [x] = xs`,
  `let [first, ..rest] = xs` (patterns below). ✓
- `let PATTERN = expr else { … }` — else body must diverge
  (return/exit); enforced. ✓
- Sequential redeclare in the same scope: idiomatic ✓. Nested shadow
  of a live outer name: error ✓. Locals shadowing this file's
  imports: legal until used through the shadow (checker-era error) ○.
- `fn name(param: Type, …) -> Ret { … }` — signatures written in
  full; no arrow = returns nothing; tail expression is the value ✓.
  `mut self` receivers require a `mut` call path ✓. Nested `fn` ✓ — Rust's rule: a plain function, private to its enclosing block, that does NOT capture enclosing locals (capture is what closures are for); hoisted to block entry, so helpers read fine below their callers and siblings can be mutually recursive.
  Parameter defaults + named arguments ✓ — Kotlin model:
  `connect("db", tls: false)`; any param nameable; positionals
  before named; defaults re-evaluate per call, left to right, and
  may reference earlier params (`width: Int = s.len()`). Direct call
  sites of declared functions/methods only — function values,
  closures, and builtins stay full-arity positional (defaults are
  declaration sugar, not type). Parameter names are API. No
  overloading, no variadics — permanent.
- `type Name = struct { field: Type, … }` ✓ — mandatory init: every
  field, no zero values ✓.
- `type Name = VariantA | VariantB(T) | NotFound{ id: Int } | …` —
  sum type with positional or named-field payloads ✓. Variants are
  namespaced: `Color.Red` in full, `.Red` where the shorthand reads
  — resolved in the *expected type* as of M4b ✓ (M1–M3 resolved it in
  a file-wide variant namespace; where there is no expected type to
  resolve in, that fallback still applies, since variant names are
  file-unique). Bare variant names are
  pattern-only — in an expression they error with the fix. Named
  fields: construct `.NotFound{ id: 7 }`, read `e.id`, match
  `NotFound{ id }` under the same mention-all-or-`..` rule as
  structs.
- `impl TypeName { fn method(self) … }` ✓; `impl Trait for Type` ✓ —
  **conformance is declared, satisfaction is structural**: the `impl`
  line is mandatory, so nothing conforms by accident, but its body
  carries only what is missing. `impl Ord for Blob { }` is correct
  when an inherent `cmp` of the right shape already exists ✓. An
  unsatisfied requirement, a wrong signature, a wrong receiver kind
  (`self` vs `mut self`) and a wrong generic argument are all compile
  errors ✓. An `impl` for a trait nobody declared is an error ✓.
  Declaring conformance still inherits the trait's *default* methods;
  a trait default `iter()` makes implementors `for`-able.
- Module-level: functions and types only, order-independent ✓;
  `const` ✓ (pure initializers, load-time); no `init()`, no life before `main` — permanent.

## Generics

Angle brackets, never square. **No turbofish, ever.**

- Declaration sites bind parameters: `fn max<T: Ord>(a: T, b: T) -> T`,
  `type Pair<A, B> = struct { … }`, `trait Container<T> { … }` ✓.
- **Bounds are checked, and a bound is the complete method set** ✓. On
  a `T: Ord`, `t.cmp(u)` resolves through the bound and anything else
  is a compile error naming the bound. An **unbounded** `<T>` stays
  fully opaque — the checker genuinely knows nothing about it, so it
  says nothing. That asymmetry is the point: a bound is what turns a
  type parameter from a hole into a surface.
- **Ordering routes through `Ord`** ✓: `< <= > >=` on a user type
  require a declared `impl Ord`, and on a `T: Ord` they are checked.
  `a < b` on a `T` bounded by something else is an error naming the
  bound. An *unbounded* `<T>` stays silent, consistently with methods.
  Arithmetic operator traits (`Add`, `Mul`) are still ○.
- Bounds are inline colon lists only: `<T: Ord + Hash>`. Unconstrained
  is bare `<T>` — no `[T any]` ceremony. **No `where` clause** in v0:
  two ways to write bounds violates house rules ✓ (parsed).
- Parameter names are capitalised, like all type names ✓.
- `impl` headers take type *arguments*, not a separate binder:
  `impl Tree<T>`, `impl Iterable<T> for Tree<T>` ✓. Rust's
  `impl<T> Tree<T>` double-mention is deliberately absent. Whether a
  concrete `impl Stack<Int>` is legal is undecided — it parses; the
  checker will rule.
- **Bounds are checked at the declaration**, not the use site: a
  generic body is verified once against its bounds, and callers get
  "`Blob` does not implement `Ord`, required by `T`" at the call ✓.
  A type parameter satisfies another's bound directly — `fn
  outer<T: Ord>` may call `fn inner<U: Ord>(a: T)` ✓ — and an
  unbounded one does not.
- Monomorphised in the compiled tier; the interpreter runs generics
  **type-erased**, which is why it needs no specialisation to enforce
  every rule ○.
- Const generics (`Matrix<T, const N>`) deferred ○.
- Use-site type arguments in *expressions* (`parse<Config>(s)`) are ○.
  They need the C#/TypeScript tentative-parse disambiguation; nothing
  needs it yet because declarations are never ambiguous — `<` always
  follows the declared name. Likewise the C++11 `>>` split is not yet
  required: Glide has no shift operators, so `List<List<Int>>` already
  lexes as two `>` ✓. **Both become necessary the day `<<`/`>>` land.**

## Statements & expressions

- Expression-oriented: `if`/`else`, `match`, and blocks yield
  values; a bare block `{ … }` is an expression whose tail is its
  value and whose locals die at `}` ✓.
- `if cond { } else if { } else { }` ✓; value-position `if` requires
  `else`.
- `if let PATTERN = expr { … } else { … }` — binding exists in the
  `else if let` chains, in both directions (`if … else if let …`,
  `if let … else if …`) ✓. Only a None scrutinee routes to else — a
  non-None value that fails the pattern is still a panic (unwrapping,
  not variant dispatch; that's `match`'s job).
- `for { }` / `for cond { }` / `for pat in iterable { }` ✓.
  Iterables: List, Map (yields `(k, v)`), Range, any Iterator, any
  value with an `iter()` method ✓. Fresh binding per iteration ✓.
  `break` / `continue` ✓ (parse error outside a loop; a closure body
  is its own function, so an enclosing loop is out of reach). Labeled
  forms ✓: `search: for … { break search }` — labels name loops only,
  must name an enclosing loop (parse error otherwise, including
  through closure/defer boundaries), no duplicate active labels;
  unlabeled break/continue still target the nearest loop.
- `match subject { pattern [if guard] => expr … }` ✓ — arms are
  single expressions (use a block expression for multi-statement
  arms). **Exhaustiveness is checked statically** ✓: a sum type, an
  `Option`, a `Result` or a `Bool` with a case unhandled is a compile
  error naming it, and coverage recurses one constructor deep so
  `Err(A)` without `Err(B)` reports `Err(B) not handled`. A type with
  too many values to enumerate (Int, String, a struct) needs a `_`
  arm. An arm that cannot run — after a catch-all, or a duplicate
  constructor — is an error too ✓. Anything the analysis cannot judge
  passes in silence, and the runtime keeps its own fall-through check
  as an assertion.
  Multi-value arms (`1, 2 =>`) ✓ — Go-style value alternatives; none
  may bind a name (parse error). Literal / range / string patterns ✓
  (see Patterns). Subjectless `match { cond => … }` ✓ — arms are
  Bool conditions, first true wins, `_` is always-true; falls through
  to a runtime error if no arm is true.
- Closures: `|x| expr`, `|x| { … }`, `||` for no args ✓. Capture by
  reference, by *binding* (a redeclare doesn't retarget) ✓. Closures
  may reuse outer names (function boundary resets the shadow rule) ✓.
- **Parameter annotations are optional**: `|x: Int| x + 1`, and any
  subset may be annotated (`|a: Int, b| a + b`) ✓. Rarely needed — a
  closure passed to a typed slot takes its parameter types from the
  slot — but a closure nothing constrains has no other way to be
  checked, so `let f = |x| x + 1` is unchecked where `let f = |x: Int|
  x + 1` is. Where an annotation contradicts an expectation, the
  conflict is reported once at the annotation.
- Generators: any `fn` whose body contains `yield` returns an
  Iterator when called ✓; `yield from` delegates ✓.
- `return` / `return expr` ✓; tail-value rule: a no-arrow function
  whose tail is a meaningful value is an error — discard with `_ =` ✓.

## Patterns

Where they appear: `let`, `let…else`, `if let`, `for … in`, `match`
arms, closure params (plain names only).

| Pattern | Example | Status |
|---|---|---|
| Wildcard | `_` | ✓ |
| Binding | `x`, `mut x` | ✓ |
| Constructor | `Some(x)`, `None`, `Circle(r)`, `Ok(v)` | ✓ |
| Tuple | `(a, b)` (≥2 elements) | ✓ |
| List | `[]`, `[x]`, `[first, ..rest]`, `[.._]` — exact unless `..` | ✓ |
| Guard | `n if n < 0 =>` (match arms). A guarded arm **covers nothing** for exhaustiveness — it may not fire — so a sum type whose only arm for a case is guarded is not exhaustive | ✓ |
| Struct | `User{ name, .. }` — shorthand binds; `field: pat` nests (`role: "admin"`, `age: 0..18`); `mut name` shorthand. Without `..` every field must be mentioned — partial-without-`..` is an error, not a no-match | ✓ |
| Literal / range | `1`, `-1`, `true`, `"GET"` (equality; plain literals only, no interpolation), `1..10` half-open / `90..=100` inclusive — Int and Rune (`'a'..='z'`) | ✓ |

Not adopted (permanent): or-patterns inside patterns, `x @ pattern`,
patterns in function signatures, ref/binding modes.

## Testing constructs

- `test "name" { … }` ✓ — run with `glide test file.gld`.
- Property form: `test "name" (xs: List<Int>) { … }` — 100 generated
  cases, fixed seed, greedy shrinking; case 0 is the simplest value ✓.
  Generatable parameter types: `Int`, `Bool`, `String`, and `List<T>`
  for any generatable `T` (so `List<List<Bool>>` works) ✓. Anything
  else is an error naming the type; user types wait on `derive
  Arbitrary` ○.
- `expect(cond)` — compiler-known: failure reports both sides
  (`left: 2, right: 3`) and continues ✓. `require` (stop on failure)
  ○. Benchmarks `bench "name" { … }` parse and skip ✓.

## Semantics quick-list

- Every program is type-checked before it runs, in every tier. ✓
  (M4b). What is checked today: call argument types, arity, named
  arguments and defaults; struct and variant literals (fields
  present, no extras, no zero values); field, method and associated
  function existence; operator operand types (including the
  `Duration`/`Instant` set and `distinct`'s refusal to inherit any);
  `if`/`for`/guard conditions; branch and match arms agreeing;
  list/map element homogeneity; iterability; declared return types
  and the tail-value rule; `?` on a Result with a reachable error
  type; `.Shorthand` against the expected type; `mut` paths;
  integer-literal range against a sized type; and every name being
  defined. What is *not* yet checked, and stays dynamic until M4c:
  a generator's element type ○. Generic bounds, trait conformance and
  match exhaustiveness are checked as of M4c ✓.
- No null; no zero values; mandatory initialisation. ✓ (by
  construction in M2)
- Errors are values; `?` propagates; panics are for bugs, kill the
  task (M2: the program), and cannot be caught — no `recover`,
  permanent.
- Immutable by default; `mut` is a path property, not an object
  guarantee (aliasing is possible — no borrow checker, recorded).
- Integer overflow **traps, in every tier and at every width** ✓:
  `+` `-` `*` `/` (minimum ÷ -1) and unary `-` on a type's minimum
  all raise a positioned error, for `Int`, `u64` and the six narrow
  widths alike. There is no build mode that wraps — that split was
  reversed (DESIGN.md). Modular arithmetic is explicit:
  `wrapping_add`, `wrapping_sub`, `wrapping_mul`, `wrapping_neg`,
  available on every integer width ✓, and named in the overflow
  message so the fix is a different operator rather than a different
  build.
- Integer literals are magnitudes: the sign is a separate token, so
  both ends of every type's range are writable, and the type a
  literal lands in decides what it becomes — `let f: Float = 5` is a
  float ✓. A literal larger than `u64`'s maximum is rejected by the
  lexer, since no type could hold it. Constant *expressions* wider
  than 64 bits (`1 << 100`) wait for comptime ○.
- Structured concurrency: `scope`/`spawn`/`join`, cancellation,
  channels, `select`, the time types, and `scope(timeout:)` all run
  in the interpreter ✓ (see the keyword table and stdlib
  Concurrency).
  M2 implementation note: tasks interleave exactly at blocking
  operations (one interpreter lock, released while blocked), which
  coincides with the ratified cancellation-point rule; release
  backends add true parallelism without changing observable
  semantics.
  Ratified shape for `scope`/`spawn`:
  `scope [(config)] [handle] { body }` — config keys `timeout:
  Duration` / `deadline: Instant` via named-args syntax; handle
  needed only to spawn.
  `s.spawn(f)` returns a `Task`; `t.join()` blocks and returns exactly
  what the closure returned (a `Result`-returning closure yields its
  `Result` — `?` it like any call). Scope exit joins all children on
  every exit path, cancelling them first on early exit; an unjoined
  child that finished with `Err` fails the scope at normal exit (first
  failure wins; discard explicitly with `let _ = t.join()`); a child
  panic cancels siblings immediately and re-panics at exit. No spawn
  outside a scope handle. Cancellation: a third unwind, neither error
  nor panic — uncatchable, runs `defer`s and `errdefer`s, delivered
  only at blocking operations (channel ops, `join`, `sleep`, IO);
  only scopes cancel (no `t.cancel()`), so user code never observes
  a cancelled task. `scope(timeout: 5.s) { … }` evaluates to
  `Result<T, Timeout>`; body `?` propagates through the scope, so
  body errors never nest inside it. `s.deadline()` reads the
  effective deadline. Channels: `let (tx, rx) = channel()`
  (rendezvous) / `channel(cap: n)` (buffered; no unbounded); mpmc,
  both halves clone; `tx.send(v)` (panics if closed), `tx.close()`
  (idempotent; only `tx` has it), `rx.recv() -> Option<T>` (`None` =
  closed-and-drained), `for v in rx` consumes until closed.
  `select`: an expression; arms line-separated like match —
  `pat = rx.recv() => expr`, `tx.send(v) => expr`, `else => expr`
  (non-blocking), optional `if cond` guard per arm (evaluated once
  at entry; false removes the arm). Same op may appear in several
  arms; a ready op's arms try in order, no match → runtime error.
  Operands evaluated once at entry; uniformly random among ready
  arms; blocking select is a cancellation point (no `ctx.Done` arm
  exists or is needed). Zero-arm `select {}` is a parse error.
- Comptime (const eval, derive, reflection): ○.
