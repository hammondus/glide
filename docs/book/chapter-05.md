# Chapter 5: Primitive Types, Literals, and Operators

The primitive layer of a language is where most of its accumulated
historical damage lives: C's implicit promotion lattice, JavaScript's
truthiness, Go's platform-sized `int`, Python's silent bignum
promotion. Glide's primitives are boring by construction, and each
piece of boringness is a specific bug class that has been deleted.

This chapter is ✓ throughout. The sized integers run: declared,
checked, represented at their own width, trapping at it, and reachable
by explicit conversion. Only `i128`/`u128` remain ○.

---

### 1. Basic Usage

#### The type set

```
Bool · Int · Float · Rune · String · () · Range
```

plus the sized numerics `i8 i16 i32 i64`, `u8 u16 u32 u64`, `f32` (✓),
and `i128`/`u128` (○).

- **`Int` is i64 on every target.** Not platform-sized. Not `usize`.
  One integer type that behaves identically on your laptop and on the
  ARM box.
- **`Float` is f64.**
- **`Rune`** is a Unicode code point and is *its own type*, not an
  integer alias.
- **`()`** is the unit type, and `()` is also its single value. It is
  how "nothing" is spelled where the grammar demands a type.
- **`Range`** is the value produced by `lo..hi`.

#### Number literals

```glide
let n = 42
let big = 10_000_000          // underscores anywhere for readability
let f = 3.14
let million = 1_000_000
```

Underscores may appear anywhere within a numeric literal and are purely
visual.

#### Arithmetic

```glide
println(7 / 2)        // 3     — integer division truncates toward zero
println(7 % 2)        // 1
println(-7 / 2)       // -3    — truncation, not floor
println(-7 % 2)       // -1    — sign follows the dividend
println(7.0 / 2.0)    // 3.5
```

Integer division truncates toward zero and `%` takes the sign of the
dividend, matching C, Go, Rust, and Java. (Python floors instead, so
`-7 // 2` is `-4` there. If you are coming from Python, this is a real
difference.)

#### No implicit numeric conversions

```glide
println(1 + 2.0)
```

```
error: line 1: operator + not defined for Int and Float
```

`Int` and `Float` do not mix, and neither do `i32` and `i64`, or `u8`
and `u16` — every pair of widths is a compile error. This is Go's
rule, and Go proved the strictness is tolerable.

#### Explicit conversion

The type's own name, applied to a value:

```glide
let n = 200
let b = u8(n)          // 200, as a u8
println(i32(b) * 1000) // 200000
println(Int('a'))      // 97
println(Rune(97))      // a
println(Int(3.7))      // 3   — truncates toward zero
```

That is Go's spelling, and Glide was already using it: `NoteId(7)`
constructs a `distinct` type the same way. A type in expression
position was always callable; conversion just widens which types.

One difference from Go, and it is the important one:

```glide
fn main() {
    let n = 300
    println(u8(n))
}
```

```
error: line 3: u8 overflow: 300 does not fit (use wrapping_u8 to truncate)
```

Go's `uint8(300)` is `44`, silently. Glide traps. A language whose `+`
traps at the declared width cannot hand you 44 for the same overflow
spelled as a conversion — and `u8(300)`, with a constant, does not
even reach runtime:

```
error: line 2: 300 does not fit in u8
```

`n.wrapping_u8()` is the truncating form, for when you mean it.

Conversion is defined between numbers and `Rune`, and nowhere else.
`String(65)` is not a conversion — Go's `string(65) == "A"` is the
wart its own vet tool warns about, and interpolation already spells
it as `"{n}"`. `Bool(1)` is C's legacy.

`Rune` is in the set even though it is deliberately *not* an integer
alias. That is not a contradiction: `Rune` is its own type so that it
can never pass for a count *implicitly*, and `Int(c)` is the opposite
of implicit.

Because `u8` now means something in expression position, every
primitive type name is reserved — `fn u8()` and `type Int = …` are
errors. A local `let u8 = 5` still shadows it, exactly as a local
shadows a predeclared identifier in Go, and then `u8(3)` fails with
"Int is not callable".

#### Integer overflow traps

```glide
fn main() {
    let big = 9223372036854775807
    println(big + 1)
}
```

```
error: line 3: Int overflow: 9223372036854775807 + 1 (use wrapping_add for modular arithmetic)
```

Read the parenthetical: **overflow traps — in every build, at every
width.** Not just `+`: also `-`, `*`, `/` of `MinInt / -1`, and unary
`-` on `MinInt`. And not just `Int`: a `u8` traps at 8 bits, so `250 +
10` is an error rather than 260.

The parenthetical also tells you the fix. `wrapping_add`,
`wrapping_sub`, `wrapping_mul` and `wrapping_neg` exist on every
integer width, and code that genuinely wants modular arithmetic —
hashes, checksums, PRNGs, wrapping counters — calls them by name.

This is Swift's model, and it is a reversal. Glide originally took
Zig's: trap in dev builds, wrap in release. The argument against it is
that the same program then computes different answers under `glide
run` and from the compiled binary, and the whole point of one shared
frontend is that the tiers cannot drift. The accepted cost is now the
other way round — release-tier arithmetic carries the check forever —
and `DESIGN.md` records it as the cheaper mistake: a check is a branch
the hardware predicts, while a silent wrap is a wrong answer that
propagates.

#### Arithmetic beyond the operators

Four operations live as **methods on the number**, at every width:

```glide-run
fn main() {
    println((-5).abs())          // 5
    println(3.min(7))            // 3
    println(3.max(7))            // 7
    println(2.pow(10))           // 1024
    println((2.0).pow(0.5))      // 1.4142135623730951
}
```

| Method | Signature | Notes |
|---|---|---|
| `abs()` | `() -> Self` | **signed only**; traps at the type's minimum |
| `min(o)` / `max(o)` | `(Self) -> Self` | ordered by the same comparison `<` and `sorted()` use |
| `pow(e)` | `(Int) -> Self` on integers, `(Float) -> Float` on floats | traps on overflow; a negative integer exponent is an error |

`abs` is signed-only on purpose: an unsigned `abs` is the identity
function, and writing it reads like a sign was handled. It traps at the
minimum, which has no positive counterpart — the same rule as `-x`.

Everything **Float-only** lives in the `math` module instead:

```glide-run
import math

fn main() {
    println(math.sqrt(2))            // 1.4142135623730951
    println(math.floor(-1.5))        // -2
    println(math.ceil(-1.5))         // -1
    println(math.round(-1.5))        // -2   — half away from zero
    println(math.trunc(-1.5))        // -1
    println(math.pi)                 // 3.141592653589793
    println(math.is_nan(math.nan))   // true
}
```

`math.pi`, `math.e`, `math.inf` and `math.nan` are **values, not
calls** — `math.pi()` is an error naming the constant. Modules can hold
values as of this module; before it they held functions only, which is
why `pi` had nowhere in the language to exist.

**The dividing line is width, and it is worth understanding.** A method
earns its place on a number by needing the receiver to say *which of
the nine numeric types* it is: `abs` must work at `u8` and at `Int`
alike, and a `Self` receiver binds that for free. `sqrt` needs none of
that — there is only ever one type involved — so it is a plain
function.

Reaching for one in the wrong place says where it went:

```glide
println((2.0).sqrt())   // error: sqrt lives in the math module — write math.sqrt(x)
println(5.sqrt())       // error: sqrt lives in the math module and takes a Float
                        //        — write math.sqrt(Float(n))
```

Go's history is the corroboration rather than the counterexample:
`math.Min` was float64-only because Go could not write it generically,
and Go 1.21's fix was not a generic `math.Min` — it was to make
`min`/`max` universe *builtins*. That third option is worse here for
its own reason: it would reserve `min` and `max` program-wide, and both
are common variable names.

None of this is permanent. When operator traits (`Add`, `Mul`) make a
`Numeric` bound expressible ○, `math.abs<T: Numeric>(v: T) -> T` types
itself with no special case, and the four can move.

#### Comparison, and Float's total order

`< <= > >=` route through `Ord` (Chapter 17), and so does `sorted()`,
so a comparison and a sort can never disagree. For `Float` that
requires a decision, and Glide's is a **total** order:

```glide-run
import math

fn main() {
    let nan = math.nan
    println(nan == nan)                 // false  — IEEE 754
    println(nan.cmp(nan))               // 0      — total order
    println(nan.min(1.0))               // 1
    println(nan.max(1.0))               // NaN
    println([3.0, nan, 1.0].sorted())   // [1, 3, NaN]
}
```

NaN sorts after every number and equals itself under `cmp`; `-0.0`
compares equal to `0.0`. Meanwhile `==` obeys IEEE 754, so `nan == nan`
is `false`.

That split is deliberate and it is what Java's `Double.compare` and
Rust's `total_cmp` also ship. A *partial* `cmp` would let sorting a
list containing NaN silently drop elements — a far worse outcome than
one surprising comparison. Rust's `f64::min` agrees about
`nan.min(1.0)` and Go's `math.Max` about `nan.max(1.0)`; one coherent
order beats matching either piecemeal, because here `min`, `<` and
`sorted()` must never disagree.

#### `==` is structural and universal

```glide-run
fn main() {
    println([1, 2] == [1, 2])                    // true
    println(["a": 1, "b": 2] == ["b": 2, "a": 1]) // true  — order is not identity
    println(Some(1) == Some(1))                  // true
    println(1 == 1.0)                            // error: Int and Float can never be equal
}
```

There is no `Eq` trait to declare and no way to redefine `==`. It
compares byte-for-byte on strings, reaches inside the `Option` box, and
recurses through structs, variants, tuples, `List`, `Map`, `Result`,
`Error`, `distinct` and `Range`.

**A `Map` ignores insertion order.** Order is a specified *iteration*
property, not part of a map's identity, so two maps with the same pairs
are equal however they were built — Python's rule. A `List` stays
order-sensitive, because a list is a sequence.

The only values that are *not* comparable are the ones with no
structure: functions, iterators, and the concurrency handles (`Scope`,
`Task`, `Sender`, `Receiver`). There `==` could only mean identity,
which is not a question this language lets you ask.

Comparing two types that can never be equal is a compile error rather
than a `false` — a silent `false` is how `interface{} == interface{}`
bugs survive in Go.

#### Booleans, and the absence of truthiness

```glide
let ok = true
let done: Bool = false
println(1 < 2 && 2 < 3)     // true
println(!(1 == 2))          // true
```

Conditions take a `Bool` and nothing else:

```glide
fn main() {
    if 1 { println("yes") }
}
```

```
error: line 2: if condition must be Bool, got Int
```

`&&`, `||`, and `!` are conventional: short-circuiting, precedence
`!` > `&&` > `||`, both operands must be `Bool`, not overloadable
(short-circuiting is control flow), and there are no `and`/`or` word
forms.

#### Runes

```glide
let c = 'a'
println(c)           // a
println("{c:?}")     // 'a'
```

Single quotes are a `Rune` literal — the delimiter carries type
information. `'ab'` is a lex error. Runes are ordered and rangeable
(`'a'..='z'` as a pattern), but a `Rune` will never pass where a count
belongs. Escapes work: `'\n'`, `'\u{1F600}'`.

#### Ranges

```glide
let r = 0..5           // half-open: 0, 1, 2, 3, 4
for i in 0..=3 {       // inclusive: 0, 1, 2, 3
    print("{i} ")
}
```

```
0 1 2 3
```

Two forms, both usable as expressions and as patterns, for `Int` and
`Rune`:

- `lo..hi` — half-open, excludes `hi`
- `lo..=hi` — inclusive

`..=` desugars to `hi + 1`, which means an inclusive range up to the
maximum `Int` is a loud error rather than a silent wrap.

#### Operator precedence

Loosest to tightest:

| Level | Operators | Notes |
|---|---|---|
| 1 | `??` | Option/Result coalescing |
| 2 | `..` `..=` | range construction |
| 3 | `\|\|` | short-circuit or |
| 4 | `&&` | short-circuit and |
| 5 | `==` `!=` | byte equality on strings |
| 6 | `<` `<=` `>` `>=` | |
| 7 | `+` `-` | |
| 8 | `*` `/` `%` | |

Unary: `!` (Bool), `-` (numeric). Postfix: call `f(x)`, index `xs[i]`,
field `.name`, tuple field `.0`, try `?`.

Note that `??` binds *loosest*. `a ?? b + c` is `a ?? (b + c)`, which is
almost always what you want, since `??` supplies a fallback for the
whole expression on its right.

#### Format specs

Interpolation takes an optional spec after a colon. The set is
deliberately closed:

```glide
println("{255:hex}")        // ff
println("{1234567:,}")      // 1,234,567
println("{3.14159:.2}")     // 3.14
println("{42:05}")          // 00042
println("{7:-5}|")          // 7    |
println("{7:5}|")           //     7|
```

| Spec | Meaning | Applies to |
|---|---|---|
| `{n:6}` | width, right-aligned | any |
| `{s:-6}` | width, left-aligned | any |
| `{id:04}` | zero-pad | numbers |
| `{x:.2}` | decimal places | Float |
| `{x:8.2}` | width + precision | Float |
| `{n:,}` | thousands grouping | Int, or Float with precision |
| `{n:hex}` | lowercase hex, no prefix | Int |
| `{v:?}` | Debug (structural) | any |

A spec that does not fit the value's type is an error, never noise.
Deliberately declined: fill characters, centering, sign control,
binary/octal, scientific notation, `_` grouping. Chapter 6 covers
interpolation properly.

#### No `++`, no ternary, no user-defined operators

```glide
i += 1          // the only increment
let s = if ok { "on" } else { "off" }    // the only conditional expression
```

Compound assignment (`+=`, `-=`, `*=`, `/=`, `%=`) works on names,
fields, and index targets, and requires a `mut` path.

---

### 2. Under the Hood

#### `Int` is a Go `int64` today

The interpreter's `Int` is a Go `int64`, and overflow detection is an
explicit check after each arithmetic operation. The designed compiler
emits the same check, in every build — the two tiers agree on every
answer, which is the point.

`u64` gets its own runtime representation, because it is the one
integer width an `int64` cannot hold. The six narrow widths — `i8`,
`i16`, `i32`, `u8`, `u16`, `u32` — share one carrier that records the
width alongside the value. That matters more than it sounds: generics
are type-erased, so inside `fn double<T>(v: T) -> T { v + v }` there
is no static type left by the time `+` runs. The only thing that can
say "trap at 8 bits" is the value itself.

`Float` is a Go `float64` with IEEE 754 semantics, including the parts
nobody enjoys: `0.1 + 0.2 != 0.3`, `NaN != NaN`, and signed zero. Glide
does not attempt to fix floating point, and money belongs in `Decimal`
(○, stdlib, chosen by name).

#### Literals are magnitudes, and range is checked

A literal carries no type until something gives it one, and the sign is
a separate token. That is what makes both ends of every type's range
writable:

```glide
let a: u64 = 18446744073709551615     // fine — larger than Int's maximum
let b: i8  = -128                     // fine — no positive counterpart
let c: Int = -9223372036854775808     // fine
let d: u8  = 300                      // 300 does not fit in u8
let e: u64 = -1                       // -1 does not fit in u64
```

The last two are **compile errors**, reported by the checker before
anything runs. A literal larger than `u64`'s maximum is rejected by the
lexer, since no type could hold it.

Constant *expressions* wider than 64 bits are still ○:

```glide
const k = 1 << 100        // ○ waits for comptime const evaluation
```

Arbitrary-precision constant arithmetic — Go's untyped constants, Zig's
`comptime_int` — falls out of the comptime design (Chapter 36) and
arrives with it.

#### Why the interpreter prints `1` for `1.0`

```glide
println(1.0)      // 1
```

`Float` Display uses Go's shortest-round-trip formatting, which drops a
trailing `.0`. `{x:.1}` gives you `1.0` when you want it. This is a
formatting choice, not a type confusion — the value is still a `Float`,
and `1.0 + 1` is still an error.

#### Rune representation

A `Rune` is a Go `rune` (an `int32` code point) wrapped in its own
value type so that dispatch and operators can distinguish it. `==` and
ordering work only against other `Runes`. Display prints the character;
Debug quotes it.

`String.runes()` yields `U+FFFD` (the replacement character) once per
invalid byte, rather than erroring — a recorded decision that keeps
lossy input from becoming a crash.

#### The widths that are not there

`i8`–`i64` and `u8`–`u64` all run. Two notes on what is missing and
what was never coming:

- **`i128`/`u128` are ○, and deliberately deferred past M4.** They are
  ratified as *primitives*, not a library type — a `u128` *is* an IPv6
  address, a UUID, a 128-bit hash — but Go has no native 128-bit
  integer, so the interpreter would need a software implementation
  that the compiled tier would then throw away. (Watch the historical
  i128 ABI mismatch at C boundaries when they land.)
- **There is no `usize`, and there will not be.** Lengths and indices
  are signed `Int`. The reasoning is in the next section and it is one
  of the more contrarian decisions in the language.

---

### 3. Why This Design?

#### Why `Int` is i64 everywhere, not platform-sized

Go's `int` is 32 bits on 32-bit targets and 64 on 64-bit ones. That
makes arithmetic *behaviour* vary by target: "works on the Mac,
overflows on the deploy box" is a real bug class, and it is a
particularly nasty one because your tests pass.

In a world where cross-compilation is the normal case — and Glide
treats cross-compilation as a flag, not a project — a
platform-dependent integer is a deletable bug class. Sized names like
`i32` should feel like a *decision*, not a habit you fell into.

#### Why lengths and indices are signed

This is the decision most likely to make a C++ or Rust programmer
object, so here is the full argument.

Unsigned sizes are supposed to prevent negative lengths. They do not
prevent the actual bug, which is *underflow*:

```
len - 1        // when len == 0
```

With a signed type, that is `-1`: a value that a dev-build bounds check
traps on and that can never silently address memory. With an unsigned
type, it is 18,446,744,073,709,551,615 — a number that passes every
`>= 0` check, indexes far outside the array, and produces a
segmentation fault or a security hole a long way from the cause.

Unsigned sizes also break the countdown loop. `for i := len-1; i >= 0;
i--` never terminates with an unsigned `i`, because `i >= 0` is always
true. Every C++ codebase has hit this.

And a separate index type is a permanent tax: Rust's `as usize`
confetti expresses a distinction that died with 32-bit addressing.

C++'s own architects — Bjarne Stroustrup and Herb Sutter among them —
have publicly called unsigned sizes in the standard library an
irreversible mistake. Glide takes the lesson. `u64` and `u128` exist
for modular arithmetic, hashes, and bit patterns; they are not for
sizing collections.

#### Why no truthiness

Truthiness looks like a convenience and is a category error: it decides
that a value of *any* type can answer a yes/no question, using a rule
the type did not choose.

JavaScript's version is famously scar tissue: `"0"` is truthy, `[]` is
truthy but `[] == false` is true, and `document.all` is falsy for
web-compatibility reasons. But even Python's principled version — empty
is false, non-empty is true — conflates **absent** with **empty**,
which is the exact distinction `Option` exists to make. `if items:`
cannot tell you whether the list is missing or merely empty, and in a
language with no null that distinction is load-bearing.

Truthiness would erase, at every `if`, what the type system fought for.
So every legitimate use gets an explicit substitute:

| You want to ask | Write |
|---|---|
| Is this value present? | `if let user = find_user(id) { … }` |
| Is this collection empty? | `if !xs.is_empty() { … }` |
| Give me this or a fallback | `x ?? fallback` |
| Is this number nonzero? | `if n != 0 { … }` |

The last one costs five characters and removes an ambiguity class. Note
particularly that `??` is Option-based rather than
falsiness-based, so unlike JavaScript's `||` it does not break on `0`
or `""`.

#### Why overflow always traps

Three positions exist and each is defensible:

- **Always wrap** (C's unsigned, Go, Java): fast, and silently wrong.
- **Always trap** (Swift, and Glide): correct, and taxes every
  arithmetic operation in production.
- **Trap in dev, wrap in release** (Zig, Rust): catches the bug at the
  moment it happens during development, costs nothing in production.

Glide originally took the third position and reversed to the second.
The third one's cost is that dev and release differ, which violates
"test what you ship" — and in a two-tier language that cost is much
worse than it first appears. The interpreter and the compiler share
one frontend precisely so that a program cannot mean two things; an
overflow policy that varies by build reintroduces exactly the drift
the design exists to prevent. Worse, the overflow bug that only
appears in release is the hardest kind to reproduce, because the build
that would have caught it is not the build that ran.

Swift is the evidence that the second position is affordable: a decade
of shipped iOS, trapping in release, with `&+` `&-` `&*` for the
modular cases. Glide spells those `wrapping_add`, `wrapping_sub`,
`wrapping_mul` and `wrapping_neg` — longer on purpose, because
modular arithmetic in code that did not mean to ask for it is a bug,
and it should be conspicuous.

The first position stays rejected for the reason it always was: a
wrong answer that keeps going is worse than a stopped program.

#### Why `BigInt` is a named type and not automatic promotion ○

Python promotes silently: `2 ** 100` just works. That is genuinely
convenient and it costs three things. A branch in every integer
addition. Boxed hot-loop counters. And — decisive here —
incompatibility with trap-in-dev overflow, because with automatic
promotion an overflow becomes an *allocation*, not an error.

Costs do not hide inside `+`. So `BigInt` is a stdlib type chosen by
name, with two ergonomic concessions: arbitrary-precision literals
materialise into it (`let k: BigInt = 1 << 4096` just works), and
operator traits make the arithmetic look native. Go's
`a.Add(a, b)` API is exactly why people avoid `math/big`, and the same
machinery serves `Decimal` for money later.

#### Why operators can be overloaded, but only some

Operator traits exist (○) and are scoped hard: arithmetic and
comparison only — `Add`, `Mul`, `Ord`, and friends.

Not `&&`/`||`, because short-circuiting is control flow and an
overloadable control-flow operator is a trap. Not assignment. And **no
user-invented operators**, which is the difference between Rust's
decade at exactly this scope (fine) and Haskell's operator soup or
C++'s `<<`-for-IO (not fine).

#### Why `Rune` is not an `i32` alias

Go's `rune` is an alias for `int32`, which is sloppy in a small way
that compounds: a code point can be passed where a count is expected,
arithmetic on it silently works, and the type tells you nothing at a
signature boundary. Making `Rune` its own type costs one conversion
call in the rare case where you genuinely want the numeric value, and
buys a signature that means something.

---

### 4. Competing Approaches

**C.** The implicit promotion lattice is a 40-year bug factory: integer
promotion, usual arithmetic conversions, and implementation-defined
signedness of `char`. Signed overflow is undefined behaviour, which
means the optimiser may assume it never happens — the source of some of
the most surprising miscompilations in practice. Glide's answer: no
implicit conversions at all, and overflow is defined at both tiers.

**Go.** Very close to Glide, and the model for most of it: no implicit
conversions, `Bool`-only conditions, no truthiness, defined wrapping
overflow. Differences: Go's `int` is platform-sized (Glide fixes at
64), Go's `rune` is an alias (Glide makes it a type), Go wraps in all
builds (Glide traps in all builds), and Go has no `??` so nil-map reads
return zero values.

**Rust.** Also very close. Rust traps in debug and wraps in release —
the model Glide started with and reversed, for the reason above.
Differences: Rust's `usize` for indexing
(Glide says signed `Int`), Rust's `as` casts (Glide will require named
conversions), and Rust's rich integer method surface
(`checked_add`, `saturating_sub`, `wrapping_mul`) which Glide will
partially adopt.

**Zig.** The source of the trap-in-dev design, and stricter still:
Zig's `+` traps in safe builds and `+%` wraps explicitly, always.
Zig's `comptime_int` is the direct ancestor of Glide's
arbitrary-precision literals.

**Python.** Automatic bignum promotion, truthiness on every type, floor
division, and one numeric tower. Ergonomically lovely for scripts;
each of those four is a decision Glide goes the other way on, and the
reasoning for each is above.

**JavaScript.** One number type (f64) until `BigInt` arrived, so
integer IDs past 2^53 silently corrupt — a bug Glide addresses even in
its JSON design (Chapter 33 keeps numbers in lexical form for exactly
this reason). Truthiness as discussed. `==` versus `===`. It is the
cautionary tale in almost every section of `DESIGN.md`.

**Java.** No unsigned types at all until the awkward static methods in
Java 8; silent overflow; `int` versus `Integer` boxing. Java's
`java.time` is the one part of Java that Glide steals without irony
(Chapter 27).

---

### 5. Common Mistakes

**Mixing `Int` and `Float`.** The most common first-hour error:

```glide
let avg = total / count.len()        // fine if both Int
let avg = total / 2.0                // error if total is Int
```

There is no implicit widening. Convert explicitly:
`Float(total) / Float(count.len())`.

**Expecting integer division to produce a Float.** `7 / 2` is `3`, not
`3.5`. If you want the second, both operands must be `Float`.

**Assuming `%` follows Python's sign rule.** `-7 % 2` is `-1` in Glide
(sign of the dividend), `1` in Python (sign of the divisor). If you are
porting a modular-arithmetic algorithm from Python, this will bite.

**Writing `if x` for a number or a collection.** No truthiness. Write
`if x != 0` or `if !xs.is_empty()`. The compiler's message names the
type it got, which usually makes the fix obvious.

**Being surprised that `println(1.0)` prints `1`.** That is Display
formatting, not a type change. Use `{x:.1}` if the decimal point
matters for output.

**Assuming an inclusive range can reach `Int` max.** `..=` desugars to
`hi + 1`, so `0..=9223372036854775807` overflows and errors loudly. If
you genuinely need to iterate to the maximum integer, you have a
different problem.

**Expecting `??` to bind tightly.** It binds loosest of all binary
operators:

```glide
let n = counts[word] ?? 0 + 1        // this is counts[word] ?? (0 + 1)
let n = (counts[word] ?? 0) + 1      // this is what you meant
```

The parentheses in the second line are not optional, and this exact
mistake appears in the word-frequency program that Chapter 11 builds.

**Using `u64` because a value "can't be negative".** That is not what
unsigned types are for here. Use `Int` and validate. Unsigned types are
for modular arithmetic, hashes, and bit patterns.

---

### 6. Performance Considerations

**`Int` arithmetic in the interpreter** costs an interface dispatch, a
type check, the operation, and an overflow check. That is
several-hundred-times-Go slow, and it is a tree-walker cost, not a
language cost.

**Overflow checks in the compiled dev tier** cost roughly one extra
instruction and a well-predicted branch per operation — typically a
few percent, occasionally more in tight numeric loops. Release builds
pay nothing.

**Fixing `Int` at 64 bits** costs nothing on 64-bit targets and costs a
pair of 32-bit operations on 32-bit targets. That is the deliberate
trade: uniform behaviour, paid for by embedded and legacy targets that
Glide does not prioritise.

**`i128`/`u128`** lower to pairs of 64-bit operations. Addition is
cheap; multiplication and division are noticeably not. Use them because
the *value* is 128 bits (an IPv6 address, a UUID), not to be safe.

**Float** is hardware f64. No surprises, including the ones you already
know: denormals are slow on some hardware, and division is
substantially more expensive than multiplication.

**Ranges are values, not materialised sequences.** `for i in
0..1_000_000` allocates nothing; the range yields lazily. `(0..n)
.iter().collect()` is the spelling that actually builds a list, and it
is longer on purpose.

**Interpolation** in the designed compiler desugars at compile time to
writer calls, so `"{n:6} {word}"` costs the writes and nothing else. Go
parses the format string at runtime on every `Printf` call and walks
arguments with reflection; that gap is why Glide's approach is a
language feature rather than a library.

---

### 7. Best Practices

**Use `Int` unless you have a reason.** A sized type should mark a
decision: this is a wire-format field, this is a hash, this is a
bitmask. Reaching for `i32` "to save memory" in a struct that exists in
the thousands is premature; reaching for `u128` because the value *is*
a UUID is correct.

**Never use unsigned to mean "non-negative".** Validate instead:

```glide
// Bad — the type does not actually prevent the bug
fn resize(n: u64) { … }

// Good — the check is where the meaning is
fn resize(n: Int) -> Result<(), Error> {
    if n < 0 {
        return Err(.Invalid("size must be non-negative"))
    }
    …
}
```

**Name your constants, and use `snake_case` for them.**

```glide
const max_retries = 3
const default_timeout = 30.s
```

There is no SCREAMING_CASE convention. `DESIGN.md` is blunt about it:
SCREAMING_CASE is C-preprocessor scar tissue, and an earlier evaluation
time is not a siren.

**Prefer named comparisons over clever arithmetic.**

```glide
// Bad
if (flags / 4) % 2 == 1 { … }

// Good
if flags.contains(.Verbose) { … }
```

Flags are not enums and enums are not integers in a costume — "one of"
is a sum type, "set of" is a `Set<T>` (Chapter 13).

**Let the format spec do the work.**

```glide
// Bad — manual padding
let padded = " ".repeat(6 - "{n}".len()) + "{n}"

// Good
let padded = "{n:6}"
```

**Do not reach for `Float` for money.** Use `Decimal` when it lands
(○); until then, integer cents. `0.1 + 0.2 != 0.3` is not a bug you
want in an invoice.

**Write ranges half-open unless the closed end is the meaning.**
`0..n` is the idiomatic loop over `n` items. `'a'..='z'` is idiomatic
because the closed end *is* the meaning — the letters through z. The
two forms exist so that neither case has to be written as a puzzle;
under a half-open-only rule the letters pattern would be `'a'..'{'`,
which is genuinely how Rust's history forced them to add `..=`.

---

### 8. Examples

**A numeric-formatting tour, complete and runnable:**

```glide-run
fn main() {
    let bytes = 1_048_576
    let ratio = 0.8734
    let id = 42

    println("size:    {bytes:,} bytes")
    println("hex:     0x{bytes:hex}")
    println("ratio:   {ratio:.1}")
    println("percent: {ratio * 100.0:.1}%")
    println("id:      {id:06}")
    println("table:   |{id:8}|{id:-8}|")
}
```

```
size:    1,048,576 bytes
hex:     0x100000
ratio:   0.9
percent: 87.3%
id:      000042
table:   |      42|42      |
```

Two things to notice. `{ratio * 100.0:.1}` — the braces take a full
expression, not just an identifier, which is a concrete advantage over
Rust's interpolation (identifiers only). And the literal is `100.0`,
not `100`: even inside an interpolation, `Float * Int` is an error.
Writing `{ratio * 100:.1}` gives you

```
error: line 8: operator * not defined for Float and Int
```

which is the no-implicit-conversions rule showing up in the place you
least expect it. Interpolation is not an escape hatch from the type
rules.

**A safe integer average, showing the conversion discipline:**

```glide
fn mean(xs: List<Int>) -> Float? {
    if xs.len() == 0 {
        return None
    }
    let mut total = 0
    for x in xs {
        total += x            // traps on overflow, in every tier
    }
    // Integer division would truncate; the sum is converted first.
    Some(Float(total) / Float(xs.len()))
}
```

```
Some(2.5)
```

The conversion is a *call*, visible on the line. You cannot write
`total / xs.len()` and accidentally get truncation when you wanted a
mean, because the return type is `Float?` and an `Int` does not fit
it.

**Demonstrating the trap, deliberately:**

```glide
fn checked_double(n: Int) -> Int {
    n * 2
}

fn main() {
    println(checked_double(1000))
    println(checked_double(9_000_000_000_000_000_000))
}
```

```
2000
error: line 2: Int overflow: 9000000000000000000 * 2 (use wrapping_mul for modular arithmetic)
```

The error names the line inside `checked_double`, not the call site.
That is correct — the overflow happened there — and in the designed
compiler an error return trace (Chapter 20, ○) would show the
propagation path.

**Bad code, and why:**

```glide
// Bad
fn parse_flags(raw: Int) -> (Bool, Bool, Bool) {
    (raw % 2 == 1, (raw / 2) % 2 == 1, (raw / 4) % 2 == 1)
}
```

Three problems. The bit positions are magic numbers with no names. The
return type is a three-tuple of `Bool`, so the caller must remember the
order — a transposition bug waiting to happen. And it uses arithmetic
where the domain concept is a *set*.

```glide
// Good
type Flag = Verbose | Debug | Quiet

fn parse_flags(raw: Int) -> Set<Flag> { … }      // ○ Set
```

Now the caller writes `flags.contains(.Verbose)`, transposition is
impossible, and adding a fourth flag does not change any existing
signature.

---

### 9. Summary & Exercises

**Summary**

- `Int` is i64 on every target — never platform-sized. `Float` is f64.
  `Rune` is its own type, not an integer alias. `()` is the unit type
  and its single value.
- There are no implicit numeric conversions, in any direction. `1 +
  2.0` is an error.
- Integer overflow **traps, in every build and at every width** — a
  `u8` traps at 8 bits, not at 64. This is Swift's model; the
  `wrapping_*` methods are the explicit escape, and there is no build
  mode that wraps silently.
- There is no truthiness. Conditions take `Bool` only, and every
  legitimate use of truthiness has an explicit substitute: `if let` for
  presence, `is_empty()` for emptiness, `??` for defaulting, `!= 0` for
  nonzero.
- Lengths and indices are signed `Int`. There is no `usize`. Unsigned
  types are for modular arithmetic, hashes, and bit patterns.
- Ranges come in two forms — `..` half-open and `..=` inclusive — and
  work as both expressions and patterns, for `Int` and `Rune`.
- Format specs are a small closed set: width, alignment, zero-pad,
  precision, thousands, hex, Debug. A mismatched spec is an error, not
  noise.
- No `++`, no ternary, no assignment-as-expression, no user-defined
  operators. Arithmetic operator traits (`Add`, `Mul`) are ○ and scoped
  hard; `Ord` ✓ already drives `< <= > >=` and `sorted()` from one
  `cmp`.
- **`abs`, `min`, `max` and `pow` are methods on the number** ✓,
  because they need the receiver to say which of the nine numeric
  types it is. Everything Float-only — `sqrt`, the rounding family, the
  classification family, and the constants `pi`/`e`/`inf`/`nan` — is in
  the **`math` module** ✓.
- **`Float`'s `cmp` is a total order** ✓ (NaN sorts last and equals
  itself) while `==` obeys IEEE 754 (`nan == nan` is `false`). One
  coherent order, so `min`, `<` and `sorted()` never disagree.
- **`==` is structural and universal** ✓ — no `Eq` trait, not
  redefinable, recursing through every structured value. A `Map`
  ignores insertion order; a `List` does not. Comparing two types that
  can never be equal is a compile error, not a silent `false`.
- `??` binds loosest of all binary operators. Parenthesise when mixing
  it with arithmetic.

**Exercises**

1. **Find your unsigned bugs.** In a C, C++, or Rust codebase you know,
   grep for `size_t`, `usize`, or `unsigned` in loop bounds and search
   for any `- 1` applied to a length. For each, work out what happens
   when the length is zero. Then decide whether Glide's
   signed-everything rule would have prevented it, or merely moved
   where it fails.

2. **Implement `wrapping_add` in user code.** Given that Glide traps on
   overflow in dev, write a function `wrapping_add(a: Int, b: Int) ->
   Int` that produces the wrapped result without ever triggering the
   trap. (Hint: you will need to detect the overflow *before*
   performing the addition, which tells you something about why the
   builtin version has to be a primitive.)

3. **Argue the tier split.** Write down the strongest case you can for
   "always trap, even in release", including the performance data you
   would need to refute it. Then write the strongest case for "always
   wrap". `DESIGN.md` picked neither; decide whether you agree, and
   what evidence would change your mind.
