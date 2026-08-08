# Glide Language Reference

Terse lookup reference, modelled on Go's split between the language
spec and the stdlib docs (the stdlib is [stdlib.md](stdlib.md)). The
book (`docs/book/`) teaches; this file reminds.

**Status markers** — the language is ahead of its implementation:

- ✓ — runs in the current interpreter (M2)
- ○ — designed (recorded in `DESIGN.md`), not yet implemented; using
  it today is a parse or runtime error

Nothing is *type-checked* yet at any tier: annotations are parsed and
ignored until the stage-2 checker. Rules marked ✓ are enforced
dynamically (mut, shadowing, let-else divergence, tail values).

## Source files

- Extension `.gld`, UTF-8. ✓
- Comments: `//` to end of line — the entire comment grammar; there
  is no `/* */`. ✓
- No semicolons. A newline ends a statement when the previous token
  can end an expression (identifier, literal, `)`, `]`, `}`, `?`);
  a trailing operator or `.` continues the line, and so does a line
  *beginning* with `.` — multi-line adapter chains put each
  `.filter(…)` on its own line (`..` at line start does not
  continue). `else` sits on the same line as its `}`. ✓
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

Contextual keywords — only special at top level, followed by a string
literal; otherwise ordinary identifiers (`let test = …` is legal):

| Word | Meaning | Status |
|---|---|---|
| `test` | test block: `test "name" { … }`, property form `test "n" (xs: List<Int>) { … }` | ✓ |
| `bench` | benchmark block (parses; runner skips it) | ✓ |

Designed, not yet in the lexer:

| Keyword | Meaning | Status |
|---|---|---|
| `defer` | block-scoped cleanup: `defer { … }` | ○ |
| `errdefer` | cleanup only on the error path | ○ |
| `const` | comptime-evaluated binding; the only module-level state | ○ |
| `trait` | trait declaration | ○ |
| `distinct` | `type UserId = distinct Int` — no implicit conversion | ○ |
| `unsafe` | `unsafe fn` / `unsafe { }` | ○ |
| `scope` | structured-concurrency scope | ○ |
| `embed` | build-time file embedding | ○ |
| `derive` | comptime derive (Json, Debug, Enum, …) | ○ |

Also unbindable: the free builtin names (`print`, `println`,
`eprint`, `eprintln`, `expect`, `Ok`, `Err`, `Some`) are reserved —
declaring them is an error ✓ — and `None` is a literal.

## Literals

| Form | Example | Status |
|---|---|---|
| Int | `42`, `10_000_000` (underscores anywhere) | ✓ |
| Float | `3.14` | ✓ |
| Bool | `true`, `false` | ✓ |
| String | `"text {expr} {expr:spec}"` — always-interpolating | ✓ |
| Escapes | `\n` `\t` `\r` `\\` `\"` `\{` `\}` | ✓ |
| Unicode escape | `\u{1F600}` | ○ |
| Raw string | `` `no escapes, no interpolation, multiline` `` | ○ |
| Rune | `'a'` (its own type, not an int alias) | ○ |
| List | `[1, 2, 3]`, empty `[]` | ✓ |
| Map | `[:]` (empty; annotation required), inserts via `m[k] = v` | ✓ |
| Tuple | `(a, b)` — parens + comma; `(x)` is grouping | ✓ |
| Unit | `()` | ✓ |
| Range | `lo..hi` (half-open, Int only) | ✓ |
| Struct | `User{ name: "x", id: 7 }` | ✓ |
| Struct update | `Config{ timeout: 5, ..base }` | ○ |
| List spread | `[a, ..xs, b]` | ○ |

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
| `List<T>`, `Map<K, V>` | Map preserves insertion order | ✓ |
| `(A, B)` tuples | fields `.0`, `.1` | ✓ |
| `T?` = `Option<T>` | unboxed in M2: `Some` is identity, so `Option<Option<T>>` is unrepresentable until the checker era | ✓ |
| `Result<T, E>` | `Ok(v)` / `Err(e)` | ✓ |
| `Range` | value of `lo..hi` | ✓ |
| `fn(A) -> B` | one function type for named fns, closures, method values | ○ (closures exist ✓; the *type* is unchecked) |
| `i8…i128`, `u8…u128`, `f32` | sized numerics | ○ |
| `Rune` | | ○ |
| `BigInt`, `Decimal` | stdlib, by name, never silent promotion | ○ |
| `any Trait` | boxed trait object, visible dispatch | ○ |

## Operators

Binary, loosest to tightest (levels from the parser):

| Prec | Operators | Notes |
|---|---|---|
| 1 | `??` | option-coalescing; right side lazy |
| 2 | `..` | range construction |
| 3 | `\|\|` | short-circuit or |
| 4 | `&&` | short-circuit and |
| 5 | `==` `!=` | byte equality on strings |
| 6 | `<` `<=` `>` `>=` | |
| 7 | `+` `-` | |
| 8 | `*` `/` `%` | |

All ✓. Unary: `!` (Bool), `-` (numeric) ✓. Postfix: call `f(x)`,
index `xs[i]` / `m[k]`, field `.name`, tuple field `.0`, try `?` ✓.

- `?` unwraps a `Result` or returns the `Err` to the caller ✓
  (on `Option` — not adopted; use `??` / `if let`).
- No `++`/`--` (use `+= 1`), no ternary (use expression-`if`), no
  assignment-as-expression, no user-defined operators.
- String indexing `s[i]` does not exist, by design.
- `or |e| { … }` — handle-in-place error block — ○ **and unratified**
  (GRAMMAR.md flags it as the biggest open fight).

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
  `mut self` receivers require a `mut` call path ✓. Nested `fn` ○.
  Parameter defaults + named arguments (`connect("db", timeout: 5.s)`)
  ○. No overloading, no variadics — permanent.
- `type Name = struct { field: Type, … }` ✓ — mandatory init: every
  field, no zero values ✓.
- `type Name = VariantA | VariantB(T) | …` — sum type ✓ (positional
  payloads; named-field variants ○, dot-shorthand `.NotFound` ○).
- `impl TypeName { fn method(self) … }` ✓; `impl Trait for Type` ✓
  (conformance asserted, not verified, until the checker).
- Module-level: functions and types only, order-independent ✓;
  `const` ○; no `init()`, no life before `main` — permanent.

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
  is its own function, so an enclosing loop is out of reach); labeled
  forms ○.
- `match subject { pattern [if guard] => expr … }` ✓ — arms are
  single expressions (use a block expression for multi-statement
  arms); exhaustiveness checked dynamically on sum types ✓.
  Multi-value arms (`1, 2 =>`) ✓ — Go-style value alternatives; none
  may bind a name (parse error). Literal / range / string patterns ✓
  (see Patterns). Subjectless `match { cond => … }` ✓ — arms are
  Bool conditions, first true wins, `_` is always-true; falls through
  to a runtime error if no arm is true.
- Closures: `|x| expr`, `|x| { … }`, `||` for no args ✓. Capture by
  reference, by *binding* (a redeclare doesn't retarget) ✓. Closures
  may reuse outer names (function boundary resets the shadow rule) ✓.
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
| Guard | `n if n < 0 =>` (match arms; opaque to exhaustiveness) | ✓ |
| Struct | `User{ name, .. }` | ○ |
| Literal / range | `1`, `-1`, `true`, `"GET"` (equality; plain literals only, no interpolation), `1..10` / `-5..-1` (half-open, like `..` everywhere) | ✓ (rune ranges `'a'..'z'` wait on Rune ○) |

Not adopted (permanent): or-patterns inside patterns, `x @ pattern`,
patterns in function signatures, ref/binding modes.

## Testing constructs

- `test "name" { … }` ✓ — run with `glide test file.gld`.
- Property form: `test "name" (xs: List<Int>) { … }` — 100 generated
  cases, fixed seed, greedy shrinking; case 0 is the simplest value ✓.
- `expect(cond)` — compiler-known: failure reports both sides
  (`left: 2, right: 3`) and continues ✓. `require` (stop on failure)
  ○. Benchmarks `bench "name" { … }` parse and skip ✓.

## Semantics quick-list

- No null; no zero values; mandatory initialisation. ✓ (by
  construction in M2)
- Errors are values; `?` propagates; panics are for bugs, kill the
  task (M2: the program), and cannot be caught — no `recover`,
  permanent.
- Immutable by default; `mut` is a path property, not an object
  guarantee (aliasing is possible — no borrow checker, recorded).
- Integer overflow: trap in dev, wrap in release ○ (M2 shims on Go
  int64 semantics).
- Green threads, channels, structured concurrency scopes: ○ (M3).
- Comptime (const eval, derive, reflection): ○.
