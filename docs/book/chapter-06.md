# Chapter 6: Strings, Runes, and Interpolation

Glide has three string delimiters and each means something different.
It has no `printf` and no format verbs. It has no `s[i]`. And its print
functions are unbuffered with no `flush()` anywhere in the language.

Every one of those is a decision with a body count behind it. This
chapter covers all of them, and it is the chapter where Glide's
"pricing" pillar — the expensive thing gets a longer name — is most
visible.

Everything here is ✓ except `StringBuilder`, marked where it appears.

---

### 1. Basic Usage

#### Three delimiters, three meanings

```glide
let c = 'a'                        // Rune literal
let s = "hello, {name}"            // working string: escapes + interpolation
let r = `C:\raw\path {not} interpolated`   // raw string
```

- **`'…'`** is a `Rune` — exactly one code point. `'ab'` is a lex
  error. The delimiter is type information.
- **`"…"`** is the working string: backslash escapes and **always-on**
  interpolation.
- **`` `…` ``** is raw: no escapes, no interpolation, multiline. It
  cannot contain a backtick, by definition of raw.

There is no prefix zoo. No `r"…"`, no `f"…"`, no `"""…"""`. Prefixes
are what you need after you have squandered two delimiters on synonyms,
which is what Python and JavaScript did.

#### Interpolation is always on

```glide-run
fn main() {
    let name = "world"
    let count = 3
    println("hello, {name}")
    println("{count} items")
    println("{count * 2} doubled")
    println("{name.to_upper()} shouted")
}
```

```
hello, world
3 items
6 doubled
WORLD shouted
```

Any expression goes in the braces — not just an identifier. There is no
opt-in prefix, because the forgotten-prefix bug (writing `"{x}"`
instead of `f"{x}"` in Python and getting a literal `{x}` in
production) is endemic.

To write a literal brace, escape it:

```glide
println("brace: \{literal\}")      // brace: {literal}
```

`\{` joins the existing backslash family (`\n`, `\t`, `\r`, `\\`,
`\"`, `\}`) rather than being a second escaping mechanism the way
Rust's `{{` doubling is.

#### Escapes

| Escape | Meaning |
|---|---|
| `\n` `\t` `\r` | newline, tab, carriage return |
| `\\` `\"` | backslash, double quote |
| `\{` `\}` | literal braces |
| `\u{1F600}` | Unicode code point, braced hex, one form |

```glide
println("emoji: \u{1F600}")        // emoji: 😀
```

#### Raw strings

Raw strings take no escapes and do no interpolation, and they span
lines:

```glide
let raw = `no \n escapes {here}`
println(raw)                       // no \n escapes {here}
```

```glide
let sql = `
    select id, title
    from notes
    where org = :org
`
```

Raw strings are not a stylistic option — they are load-bearing in two
places you will meet constantly:

```glide
r.get(`/notes/{id}`, handler)                  // HTTP route patterns
let body = `{"title": "first", "body": "hi"}`  // JSON literals
```

In an always-interpolating string, `{id}` would try to interpolate a
variable named `id` and `{"title"...}` would be a syntax disaster. This
was discovered the hour the HTTP shim landed and is recorded in
`glide/DESIGN-DECISIONS.md`. If a string contains braces that are not
interpolations, it wants backticks.

Note the trade this makes explicit: the `{{`-escaping tax that Rust
pays lands on embedded JSON and templates, which belong in raw strings
anyway.

#### Regular strings are single-line

```glide
println("multi
line")
```

```
error: line 26:19: unterminated string (opened at column 13)
```

A `"…"` string may not contain a literal newline. Use `\n`, or use a
raw string. The error names the *column* where the string opened,
because strings are single-line and a bare line number could not
distinguish which of several quotes on the line is at fault.

#### String methods

```glide-run
fn main() {
    let s = "héllo wörld"

    println(s.len())               // 13   — BYTES, not characters
    println(s.runes().count())     // 11   — code points
    println(s.to_upper())          // HÉLLO WÖRLD
    println(s.contains("wör"))     // true
    println(s.split(" ").len())    // 2
    println("  pad  ".trim() + "|")            // pad|
    println("prefix-x".trim_prefix("prefix-")) // x
    println("x".repeat(3))                     // xxx
    println("abc".cmp("abd"))                  // -1
    println("42 ".parse_int() ?? -1)           // 42
    println("x".parse_int() ?? -1)             // -1
}
```

The full surface today:

| Method | Returns | Notes |
|---|---|---|
| `len()` | `Int` | **bytes** |
| `trim()` | `String` | strip leading/trailing whitespace |
| `trim_prefix(p)` / `trim_suffix(s)` | `String` | unchanged if absent |
| `contains(sub)` / `starts_with(p)` / `ends_with(s)` | `Bool` | |
| `split(sep)` | `List<String>` | empty separator panics |
| `split_whitespace()` | `List<String>` | splits on any whitespace run |
| `lines()` | `List<String>` | Rust semantics: split on `\n`, strip trailing `\r`, no phantom empty last line |
| `replace(old, new)` | `String` | all occurrences |
| `to_upper()` / `to_lower()` | `String` | Unicode simple case mapping, no locale |
| `repeat(k)` | `String` | `k < 0` panics |
| `parse_int()` | `Int?` | base 10, surrounding whitespace tolerated |
| `runes()` | `Iterator<Rune>` | lazy; `U+FFFD` per invalid byte |
| `bytes()` | `Iterator<Int>` | lazy |
| `cmp(other)` | `Int` | three-way: negative / 0 / positive |

There is no `find` or `index_of`, deliberately: a byte offset is
useless until byte-offset slicing exists, and
`contains`/`starts_with`/`ends_with` cover the real uses.

#### There is no `s[i]`

```glide
let c = s[0]      // does not exist, permanently
```

"What is at position i" is underspecified — byte? code point?
grapheme cluster? — and the language will not guess. Use `s.bytes()`,
`s.runes()`, or (○) explicit byte-offset slicing for parsers.

```glide
let s = "héllo"
let first3 = s.runes().take(3).collect()
println("{first3:?}")              // ['h', 'é', 'l']
```

#### Strings are immutable; building is a named type

```glide
// Bad — O(n²)
let mut out = ""
for w in words {
    out += w
}
```

This works and is quadratic: each `+=` allocates a new string and
copies everything so far. The designed answer is a named accumulator:

```glide
// ○ Good
let mut sb = StringBuilder.new()
for w in words {
    sb.push(w)
}
let out = sb.build()
```

`StringBuilder` is ○ — not implemented yet. Until it lands, use
`List<String>` and `join`:

```glide
// Good, today
let mut parts: List<String> = []
for w in words {
    parts.push(w)
}
let out = parts.join("")
```

#### Comparison is byte equality

`==` on strings compares bytes. No locale collation, no case folding.
Locale-aware comparison is a library invoked on purpose — Turkish-i has
ended careers, and an operator that quietly consults a locale is an
operator you cannot reason about.

#### The print family is exactly four names

| | stdout | stderr |
|---|---|---|
| **no newline** | `print` | `eprint` |
| **newline** | `println` | `eprintln` |

`e` prefix means stderr; `ln` suffix means newline. That 2×2 grid is
the whole family and it cannot grow. All four take exactly one
argument, because formatting lives in the interpolated string:

```glide
println("{n:6}  {word}")           // right
println("%6d  %s", n, word)        // there is no such thing
```

All four are **unbuffered**. There is no `flush()` in the language.

---

### 2. Under the Hood

#### Storage: UTF-8 bytes, Go-style

A `String` is a UTF-8 byte sequence. `len()` returns bytes. Iteration
yields runes on request. Validity is checked at boundaries when you
care, not enforced by the type system.

This is deliberately *not* Rust's model. Rust splits `String`
(guaranteed valid UTF-8) from `OsString` from `Vec<u8>`, which makes
every operating-system and network boundary a conversion ceremony. Go
proved that "bytes that are usually UTF-8" is the pragmatic answer for
a systems-adjacent language, and Glide takes it.

The consequence you must internalise: **`len()` is bytes.**
`"héllo wörld".len()` is 13 while `.runes().count()` is 11, because
`é` and `ö` are two bytes each in UTF-8.

#### `runes()` and invalid input

`runes()` is lazy and yields `U+FFFD` (the replacement character) once
per invalid byte. It never errors. This is a recorded decision: lossy
input should not become a crash in a program that is merely trying to
count words.

#### Interpolation compiles away ○

In the designed compiler, `"user {u.name} has {n} items"` desugars at
compile time into a sequence of writer calls through the `Display`
trait. There is no runtime format-string parsing, no argument list to
walk, and no reflection. A type with no `Display` implementation is a
compile error at the interpolation site.

In the interpreter it is done at runtime, which is the correct shortcut
for a tree-walker but is not the shipping cost model. When reading
performance claims in this book, remember which tier they describe.

#### `Display` versus `Debug` ○

Two separate traits, Rust's split:

- **`Display`** — deliberate, user-facing, hand-written. `{x}`.
- **`Debug`** — structural, `derive Debug`, for logs and tests.
  `{x:?}`.

```glide
let u = User{ name: "grace", age: 37 }
println("{u:?}")       // User{ name: "grace", age: 37 }
```

Debug output works today because the interpreter renders values
structurally. `Display` for user types waits for traits to be checked.

The reason for the split is Go's `%v`: one verb that prints struct guts
at end users, because there was no way to distinguish "render this for
a human reading a UI" from "render this for me, debugging". Glide makes
"no `Display`" a compile error rather than printing `%!v(…)`.

#### Why print has no buffer

Each print call formats the whole string in memory and issues **one
write syscall**. Nothing is buffered, so there is nothing to flush, so
the two classic footguns cannot exist:

1. A no-newline prompt (`print("continue? ")`) always appears *before*
   the `stdin` read. In C and Rust, it does not, unless you remember
   the flush.
2. A debug print always lands, even if the process dies on the next
   instruction. Buffered schemes fail exactly when output is most
   load-bearing.

Behaviour is identical for tty, pipe, and file. C's tty-detection
heuristic — line-buffered on a terminal, block-buffered otherwise —
produces "works in my terminal, silent under `| tee`", which is a
second footgun layered on the first.

One write per call also gives **per-call atomicity under green
threads**: two tasks printing concurrently cannot interleave half-lines.

The sacrifice is recorded honestly: naive print loops are syscall-bound
at roughly a microsecond per call, so a million-line loop takes seconds
rather than tens of milliseconds. Bulk output opts into an explicit
buffered writer (○) whose `flush`/`close` is visible at the use site
via `defer`.

#### A current lexer limitation

An interpolation cannot contain a string literal:

```glide
let joined = ["a", "b"].join("-")
println(joined)                    // fine

println("{[\"a\", \"b\"].join(\"-\")}")
```

```
error: line 12:44: unterminated interpolation (opened at column 14)
```

The lexer does not recurse into nested string literals inside `{…}`.
Hoist the expression to a `let` — which is better style anyway, since a
long expression inside an interpolation is hard to read.

---

### 3. Why This Design?

#### Why interpolation instead of printf

`printf` is a second, untyped language embedded in a string, parsed at
runtime, on every call.

Go parses the format string at runtime per call and walks the arguments
with reflection — which is banned in Glide anyway. When the format and
the arguments disagree, Go prints `%!d(string=hi)` into production
logs. C's version is undefined behaviour. Rust got it right, but needed
a macro to do it.

Glide gets it right as a language feature: compile-time expansion,
compile-time checking, zero runtime machinery. A mismatch is a compile
error.

The concrete daily benefit is smaller and more constant: `"user
{u.name} has {n} items"` reads in source order. `Printf("user %s has %d
items", u.Name, n)` requires your eye to shuttle between the format
string and the argument list, and the shuttle is where transposition
bugs live.

#### Why always-on rather than an opt-in prefix

Python's `f"…"` prefix is opt-in, and forgetting it is one of the most
common Python bugs — it fails silently, printing `{x}` literally.

The counter-argument is that always-on interpolation taxes strings
containing literal braces. That tax lands on embedded JSON, HTML
templates, and regexes — all of which belong in **raw strings** anyway,
for the independent reason that they are full of backslashes. The
delimiter split Glide already needs for Windows paths and regexes
absorbs the brace problem for free.

#### Why three delimiters and no prefixes

Python and JavaScript each burn two delimiters (`'…'` and `"…"`) on
*synonyms* — two spellings, one meaning — and the "choice" between them
is then confiscated by a formatter anyway. Having spent both, they need
prefixes for the meanings that actually differ: `r"…"`, `f"…"`,
`b"…"`, `"""…"""`.

JavaScript then compounded it by giving interpolation to the
least-ergonomic delimiter (backtick), so the common case requires the
awkward key.

Glide's allocation: one delimiter per *meaning*. Rune, working string,
raw string. No synonyms, so no prefixes needed.

#### Why no `s[i]`

Because the question is underspecified and every language that answered
it picked a different answer:

- **Go** indexes bytes, which surprises everyone the first time
  `s[0]` on a non-ASCII string produces a partial code point.
- **Python** indexes characters, which costs a representation trick
  (PEP 393's flexible string representation) and still gets grapheme
  clusters wrong.
- **Rust** refuses, which is right.

Glide refuses too, permanently. `s.bytes()` and `s.runes()` say which
question you are asking. Graphemes are a stdlib segmentation library,
never a primitive — that table changes with every Unicode release, and
a primitive whose behaviour changes with a data-file update is not a
primitive.

#### Why immutable strings and a named builder

The loop-concatenation O(n²) bug is universal and invisible: the code
looks linear. Making the accumulator a *named type* puts the cost in
the type, which is the same doctrine as `BigInt` (Chapter 5) and views.

Interpolation eliminates most concatenation anyway. The cases that
remain — building a large document, streaming output — are exactly the
cases where you want an explicit builder.

#### Why exactly four print functions

Why not two functions with a `newline: Bool` parameter? Because that
knob has no good default. `true` makes `print`'s name lie; `false`
taxes every call site. And a boolean that picks between two behaviours
is two functions hiding in one signature — precisely the shape the
designed boolean-trap lint exists to catch.

Why not Python's `end:` parameter? Because the terminator is *string
content*, and the string is already the general mechanism:
`println(s)` is exactly `print("{s}\n")`.

The grid cannot grow, and that is the point. Formatting variants are
impossible because interpolation owns formatting. Stream variants go
through writer APIs, so there is no `fprint` row. Four is the ceiling.

---

### 4. Competing Approaches

**Go.** UTF-8 bytes, `len` in bytes, `range` over a string yields
runes, immutable strings, `strings.Builder` for accumulation.
Structurally almost identical to Glide. The differences are all in
formatting: Go's `fmt` verbs are runtime-parsed and reflection-driven,
`%v` conflates Display and Debug, and Go's byte indexing (`s[i]`) is
the thing Glide removes.

**Rust.** `String`/`&str`, guaranteed-valid UTF-8, no indexing (agreed),
`format!` macro with compile-time checking (agreed on the outcome,
disagreed on the mechanism). Rust's interpolation is identifiers-only,
so `format!("{}", a.b())` needs a positional argument where Glide
writes `"{a.b()}"`. Rust's `String`/`OsString`/`PathBuf`/`Vec<u8>`
split is the ceremony Glide declines.

**Python.** Three-plus string forms with prefixes; f-strings are
excellent and opt-in, which is the flaw. Character indexing and slicing
are ergonomic and cost a complicated internal representation. `str` vs
`bytes` is a genuine improvement over Python 2 and a genuine daily
tax.

**JavaScript.** Template literals do interpolation on the backtick
delimiter only, so the two common delimiters cannot interpolate. UTF-16
internally, so `"😀".length` is 2 and indexing can split a surrogate
pair — a worse version of Go's byte-indexing surprise.

**C.** `printf` with no type checking (before compiler extensions),
null-terminated strings, `strcat` in a loop as the canonical O(n²)
bug, and `%n` as an actual security vulnerability class. This is the
baseline that everything else improves on.

**Swift.** Interpolation via `\(expr)`, which is expression-capable
like Glide's. Swift's `String` is grapheme-cluster-indexed, which is
arguably the most *correct* answer and costs an `Index` type that is
not an integer — an ergonomic price Glide declines to pay.

---

### 5. Common Mistakes

**Using `len()` when you mean characters.**

```glide
let s = "héllo"
println(s.len())               // 6  — bytes
println(s.runes().count())     // 5  — characters
```

If you are validating a maximum length for a database column, bytes is
probably right. If you are truncating for display, runes is probably
right. The point is that you have to choose.

**Forgetting raw strings for routes and JSON.** This is the single most
common mistake in real Glide programs:

```glide
// Bad — {id} tries to interpolate a variable named id
r.get("/notes/{id}", get_note)

// Good
r.get(`/notes/{id}`, get_note)
```

```glide
// Bad
let body = "{\"title\": \"first\"}"      // and this doesn't even lex

// Good
let body = `{"title": "first"}`
```

**Concatenating in a loop.** Quadratic, and invisible:

```glide
// Bad
let mut out = ""
for line in lines {
    out += line + "\n"
}

// Good (today)
let out = lines.join("\n")

// Good (○, when it lands)
let mut sb = StringBuilder.new()
for line in lines { sb.push(line); sb.push("\n") }
let out = sb.build()
```

**Escaping the quotes of a string inside an interpolation.** Do not.
The lexer tracks the nesting, so the inner quotes open a new string on
their own:

```glide
// Bad — the backslash makes them the outer string's quotes, and the
// interpolation never terminates
println("[{xs.join(\", \")}]")

// Good
println("[{xs.join(", ")}]")
```

**Expecting a literal newline inside `"…"`.** Regular strings are
single-line. Use `\n` or backticks.

**Assuming `to_upper()` is locale-aware.** It is Unicode simple case
mapping with no locale. In Turkish, uppercase `i` is `İ`, not `I`.
Glide will not guess your locale inside a method call; that is a
library you invoke on purpose.

**Using `split("")` to get characters.** The empty separator panics.
Use `runes()`.

**Expecting `trim_prefix` to fail if the prefix is absent.** It returns
the string unchanged — Go's semantics. If you need to know whether the
prefix was there, check `starts_with` first.

---

### 6. Performance Considerations

**`len()` is O(1).** It is a stored byte count. `runes().count()` is
O(n) because it decodes.

**`runes()` and `bytes()` are lazy iterators**, so
`s.runes().take(3).collect()` decodes three code points, not the whole
string.

**Concatenation with `+` allocates and copies.** In a loop this is
O(n²) in total bytes copied. This is the single most common performance
bug in string-heavy code, in every language, and it is why the builder
is a named type.

**`join` is O(n)** with one allocation, because it can compute the
total length first. Prefer it.

**Interpolation** is compile-time expansion (○) into writer calls. In
the interpreter it builds a string at runtime. Either way it is cheaper
than repeated `+`, because it computes the pieces and assembles once.

**Printing is one syscall per call, roughly a microsecond.** For
console output at human speeds this is free. For a million-line dump it
is seconds. When you are writing bulk output, that is the moment to
reach for a buffered writer (○) — and the fact that you have to reach
for it is the design working as intended.

**Comparison is `memcmp`.** Byte equality on strings is as fast as it
can be, precisely because no locale is consulted. `cmp` is a three-way
byte comparison.

**`replace` allocates** a new string. So does `to_upper`, `trim` (when
it actually trims), and every other method returning a `String` —
strings are immutable, so any transformation is a new allocation.

---

### 7. Best Practices

**Interpolate; do not concatenate.**

```glide
// Bad
let msg = "user " + name + " has " + "{count}" + " items"

// Good
let msg = "user {name} has {count} items"
```

**Reach for raw strings whenever a string contains braces or
backslashes.** Routes, JSON, SQL, regexes, Windows paths. The rule of
thumb: if you find yourself typing `\\` or `\{`, you wanted backticks.

```glide
// Good
let query = `
    select id, title, created
    from notes
    where org = :org
    order by created desc
`
```

**Keep interpolations short.** An interpolation is for *inserting a
value*, not for computing one. If the expression needs a nested call
chain, hoist it — the lexer will make you do it for nested strings
anyway, and the result reads better.

```glide
// Bad
println("top: {entries.iter().take(3).map(|e| e.0).collect().join(\", \")}")

// Good
let top = entries.iter().take(3).map(|e| e.0).collect().join(", ")
println("top: {top}")
```

**Use the `e` variants deliberately.** Diagnostics, progress, warnings,
and usage messages go to stderr. Program output goes to stdout. This is
what makes your program composable in a pipeline, and it costs one
character.

**Do not interpolate log messages** (○, when logging lands). This is
the one place where interpolation is an antipattern:

```glide
// Bad — infinite-cardinality message, ungroupable and unalertable
log.info("user {id} logged in from {ip}")

// Good — constant message, typed fields
log.info("user logged in", { user_id: id, ip: ip })
```

The message is an *event name* — greppable, aggregatable, countable.
Data goes in fields. `DESIGN.md` makes this a lint.

**Choose bytes or runes explicitly, and say which in the name.**

```glide
// Bad — which is it?
fn truncate(s: String, n: Int) -> String

// Good
fn truncate_runes(s: String, n: Int) -> String
```

**Do not build a formatting helper.** The format-spec set is closed on
purpose. If you find yourself writing a `pad_left` function, check
whether `{s:-6}` covers it. If it genuinely does not — centering, for
instance, which is deliberately absent — that is a signal you are
building a table, and a table builder is a different abstraction than a
string helper.

---

### 8. Examples

**A word-frequency counter, exercising most of the string surface:**

```glide-run
fn main() {
    let text = `
        the quick brown fox
        jumps over the lazy dog
        the dog sleeps
    `

    let mut counts: Map<String, Int> = [:]
    for word in text.split_whitespace() {
        let word = word.to_lower()
        counts[word] = (counts[word] ?? 0) + 1
    }

    let mut entries = counts.entries()
    entries.sort_by(|a, b| b.1.cmp(a.1))

    for (word, n) in entries.iter().take(3) {
        println("{n:4}  {word}")
    }
}
```

```
   3  the
   2  dog
   1  quick
```

Notice `(counts[word] ?? 0) + 1` and its parentheses. `??` binds
loosest, so without them this would parse as `counts[word] ?? (0 + 1)`
— which happens to give the same answer on the first sighting of a word
and the wrong answer on every subsequent one. This is the mistake from
Chapter 5, made concrete.

**A safe display truncation, showing bytes-versus-runes:**

```glide-run
fn preview(s: String, max_runes: Int) -> String {
    let rs = s.runes().collect()
    if rs.len() <= max_runes {
        return s
    }
    let mut out = ""
    for r in rs.iter().take(max_runes) {
        out += "{r}"
    }
    out + "…"
}

fn main() {
    println(preview("héllo wörld", 7))
    println(preview("short", 7))
}
```

```
héllo w…
short
```

The `out += "{r}"` loop is quadratic and is exactly the pattern the
Best Practices section warns about — for a bounded `max_runes` of, say,
80, it is fine, and pointing that out is the honest version of the
advice. If `max_runes` could be large, this wants a builder.

**Bad versus good, on the same task:**

```glide
// Bad — printf thinking, transposed arguments, silent in Go, broken here
fn report(name: String, count: Int) -> String {
    "user %s has %d items"        // there is no printf; this is a literal
}
```

```glide
// Also bad — concatenation with manual conversion
fn report(name: String, count: Int) -> String {
    "user " + name + " has " + count + " items"
}
```

The second one does not even work: `String + Int` is not defined, for
the same no-implicit-conversions reason as Chapter 5.

```glide
// Good
fn report(name: String, count: Int) -> String {
    "user {name} has {count} items"
}
```

Interpolation is also the *conversion* mechanism. `"{count}"` turns an
`Int` into a `String` through the `Display` trait; there is no separate
`to_string` ceremony for the common case.

---

### 9. Summary & Exercises

**Summary**

- Three delimiters, three meanings: `'a'` is a `Rune`, `"…"` is the
  working string (escapes + always-on interpolation), `` `…` `` is raw
  (no escapes, no interpolation, multiline).
- Interpolation takes full expressions, is always on, and is
  compile-time expanded in the designed compiler. There is no `printf`
  and no runtime format-string parsing.
- Raw strings are load-bearing for HTTP route patterns, JSON literals,
  SQL, and regexes. If a string contains braces or backslashes, it
  wants backticks.
- Strings are UTF-8 bytes. `len()` is bytes; `runes()` decodes code
  points lazily and yields `U+FFFD` for invalid bytes.
- **`s[i]` does not exist**, permanently. The question is
  underspecified; `bytes()` and `runes()` say which one you mean.
- Strings are immutable. Loop concatenation is O(n²); use `join`
  today, `StringBuilder` (○) when it lands.
- `==` is byte equality. Locale collation and case folding are a
  library invoked on purpose.
- `Display` (`{x}`) and `Debug` (`{x:?}`) are separate traits. No
  `Display` implementation is a compile error, not `%!v(…)` in
  production.
- The print family is exactly four names in a closed 2×2 grid, all
  unbuffered, one syscall per call, with no `flush()` in the language.

**Exercises**

1. **Break the interpolation lexer.** Find three expressions that are
   legal Glide but cannot be written inside `{…}`. For each, decide
   whether the right fix is a smarter lexer or a `let` on the line
   above — and note that "the language made me name the intermediate
   value" is sometimes a feature.

2. **Write a CSV splitter.** Parse a line like `a,"b,c",d` into three
   fields, honouring quoted commas. You will immediately want
   character-by-character iteration, which means `runes()`, which
   means thinking about whether you want bytes instead (you do — CSV
   delimiters are ASCII). Write both versions and compare them.

3. **Cost the print decision.** Write a program that prints one million
   lines and time it. Then write the same program accumulating into a
   `List<String>` and doing a single `join`-and-print. Measure the
   difference. That number is the price of unbuffered output — decide
   whether you would have made the same trade, given that it buys
   prompts appearing before reads and debug prints surviving a crash.
