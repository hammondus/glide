# Chapter 10: Pattern Matching

If you have written C, Java, Go, or Python, pattern matching is
probably the biggest genuinely new idea in this book. It is not a
fancier `switch`. A `switch` compares a value against constants;
patterns *describe the shape of data* and take it apart, testing and
binding in one step, with the compiler tracking which shapes you have
covered.

One principle organises everything in this chapter:

> **A pattern is construction run backwards.**

Whatever syntax builds a value, the same syntax takes it apart. That is
the whole mental model, and once you have it, every pattern form in the
language follows without memorisation.

Everything here is ✓, including **static exhaustiveness checking**: a
`match` that forgets a case is a compile error naming the case, not a
runtime surprise. That landed in M4c and it changes how the chapter
reads — see Under the Hood for exactly how far the analysis goes and
where it deliberately stops.

---

### 1. Basic Usage

#### Construction, and its mirror

```glide
let pair = (host, port)              // build a tuple…
let (host, port) = parse_addr(s)?    // …and unbuild one

let user = User{ name: n, age: a }   // build a struct…
let User{ name, .. } = user          // …pull a field out

let xs = [1, 2, 3]                   // build a list…
let [first, ..rest] = xs             // …split one

Some(5)                              // build an Option…
if let Some(n) = maybe { … }         // …take one apart
```

Look at the symmetry. Nothing to memorise: whatever the constructor
looks like, the destructor looks the same.

#### Where patterns can appear

Anywhere a name can be bound:

- `let PATTERN = expr`
- `let PATTERN = expr else { … }`
- `if let PATTERN = expr { … } else { … }`
- `for PATTERN in iterable { … }`
- `match` arms
- closure parameters (plain names, optionally annotated: `|x: Int|`)

```glide
let (host, port) = ("localhost", 8080)
println("{host}:{port}")             // localhost:8080

for (word, n) in entries {           // destructure each element
    println("{n:6}  {word}")
}
```

#### `match`

`match` tests a value against patterns, top to bottom, and runs the
first arm that fits:

```glide
type Shape =
    Circle(Float)
    | Rect(Float, Float)
    | Point

fn area(s: Shape) -> Float {
    match s {
        Circle(r)  => 3.14159 * r * r
        Rect(w, h) => w * h
        Point      => 0.0
    }
}
```

Arms are separated by a **newline or a comma**. The newline form is the
one you will write most; the comma exists so a short match fits on one
line, which is the shape the newline rule would otherwise make
unwritable:

```glide
let n = match c { Red => 1, Green => 2, Blue => 3 }
```

A trailing comma is allowed. A comma *inside* an arm's pattern still
means multiple values (below) — the two never collide, because one
follows a pattern and the other follows a body.

Three properties make `match` more than a `switch`:

**Arms bind as they match.** `Circle(r)` does not merely test the
variant; it extracts the radius into `r` for that arm's body. Test and
disassembly are one step. There is no separate cast-and-hope, and no
possibility of testing for `Circle` and then reading a `Rect`'s fields.

**Case tells you which is which.** Capitalised `Circle` *tests*;
lowercase `r` *binds*. That is why Chapter 3 said case is grammar
rather than style.

**Exhaustiveness is checked, at compile time.** Miss a variant and the
program does not run:

```glide
type Color = Red | Green | Blue

fn f(c: Color) -> String {
    match c {
        Red => "r"
        Green => "g"
    }
}
```

```
app.gld:4:5: match is not exhaustive: Blue not handled
 4 |     match c {
   |     ^^^^^
```

Every non-exhaustive match in the file is reported in one pass, so
adding a variant hands you the complete list of places to visit.

#### Every pattern form

| Pattern | Example | Notes |
|---|---|---|
| Wildcard | `_` | matches anything, binds nothing |
| Binding | `x`, `mut x` | matches anything, binds it |
| Constructor | `Some(x)`, `None`, `Circle(r)`, `Ok(v)` | positional payloads |
| Named-field variant | `NotFound{ id }` | same rules as struct patterns |
| Tuple | `(a, b)` | two or more elements |
| List | `[]`, `[x]`, `[first, ..rest]`, `[.._]` | **exact** unless `..` |
| Struct | `User{ name, .. }`, `User{ role: Admin, name, .. }` | `..` required for partial |
| Literal | `1`, `-1`, `true`, `"GET"` | equality; no interpolation |
| Range | `1..10`, `90..=100`, `'a'..='z'` | Int and Rune |
| Guard | `n if n < 0 =>` | match arms only |

#### List patterns are exact

```glide
let xs = [1, 2, 3, 4]
match xs {
    []              => println("empty")
    [x]             => println("one: {x}")
    [first, ..rest] => println("first {first}, rest {rest:?}")
}
```

```
first 1, rest [2, 3, 4]
```

`[a, b]` matches a list of **exactly two** elements — it rejects three
as surely as it rejects one:

```glide
let [a, b] = [1, 2, 3]
```

```
error: line 2: let pattern does not match [1, 2, 3]
```

"And whatever else" is never implied. If you want two-or-more, say so
with a rest binding: `[a, b, ..rest]`, or `[a, b, .._]` if you do not
care about the extras. **The absence of `..` is the exactness.**

This is exactly why `let [_, path] = os.args()` is a good argument
check: it rejects both no-arguments and too-many-arguments in one line.

#### Struct patterns require `..` for partial matches

```glide
type P = struct { x: Int, y: Int }

let P{ x } = P{ x: 1, y: 2 }
```

```
error: line 3: struct pattern P{…} names 1 of 2 fields;
       mention them all, or end with `..` for a deliberate partial match
```

Write `let P{ x, .. } = p` — two characters that say "and the rest,
deliberately". Field shorthand binds (`name` binds the `name` field),
and `field: pattern` nests:

```glide
type Role = Admin | User | Guest
type Account = struct { name: String, role: Role, age: Int }

fn describe(a: Account) -> String {
    match a {
        Account{ role: Admin, name, .. } => "admin {name}"
        Account{ age: 0..18, name, .. }  => "minor {name}"
        Account{ name, .. }              => "user {name}"
    }
}
```

```
admin a
minor b
user c
```

Look at what the second arm does: it tests a *range* on one field while
binding another, in one pattern. That composition is where `match`
earns its keep — Go's type switch can never do it (one level, no
payloads, no coverage).

#### Nesting is arbitrary

```glide
match result {
    Ok(User{ role: Admin, name, .. }) => grant(name)
    Ok(User{ name, .. })              => deny(name)
    Err(e)                            => report(e)
}
```

Three levels of structure, two tests, one binding, in one line.

#### Guards

An extra condition after the pattern:

```glide
let opt = Some(5)
match opt {
    Some(n) if n > 3 => println("big {n}")
    Some(n)          => println("small {n}")
    None             => println("nothing")
}
```

```
big 5
```

A guard that fails falls through to the next arm. Guards are **opaque
to the exhaustiveness checker**: coverage is computed as if every guard
fails, so a guarded arm never completes coverage. The compiler demands
the unguarded case rather than trusting your predicate. That is why the
`Some(n)` arm above is required even though `n > 3` and its negation
look exhaustive to a human.

#### Literal, range, and string patterns

```glide
println(match s {
    "GET"  => "read"
    "POST" => "write"
    _      => "other"
})

println(match c {
    'a'..='m' => "early"
    'n'..='z' => "late"
    _         => "other"
})
```

String patterns are **equality only**. There are no regexes in
patterns, and no interpolation inside a pattern literal.

#### Multiple values per arm

```glide
println(match n {
    1, 2, 3 => "small"
    4..10   => "medium"
    _       => "large"
})
```

Go-style value alternatives. None of them may bind a name — that is a
parse error, because it would be ambiguous which alternative bound
what.

#### Subjectless `match`

```glide
let level = match {
    errors > 0   => "error"
    warnings > 0 => "warn"
    _            => "ok"
}
```

With no subject, arms are `Bool` conditions and the first true one
wins. `_` is always-true. This is Go's expressionless `switch`, and it
is what nested ternary chains were always trying to be.

If no arm is true and there is no `_`, it is a runtime error.

#### `if let` — check and unwrap in one move

```glide
let m = ["a": 1]

if let v = m["a"] {
    println("got {v}")
} else {
    println("none")
}
```

```
got 1
```

Note there is no `Some(…)` wrapper in the source — the pattern binds
the inner value directly. Swift users will recognise this exactly.

`if let` chains work in both directions:

```glide
if let user = find_user(id) {
    …
} else if let guest = find_guest(id) {
    …
} else {
    …
}
```

One important restriction: **only a `None` scrutinee routes to `else`.**
A non-`None` value that fails the pattern is still a panic. `if let` is
*unwrapping*, not variant dispatch — variant dispatch is `match`'s job.

#### `let … else` — unwrap or bail

```glide
let Some(v) = m["z"] else {
    println("missing")
}
println(v)
```

```
missing
error: line 3: the else block of `let … else` must diverge (return or exit),
       but it ran off the end
```

The `else` block ran, printed, and then fell off the end — which is
illegal, because past that line `v` must exist. The block must
`return`, `os.exit`, `break`, `continue`, or panic.

```glide
let Some(v) = m["z"] else {
    return Err(.Missing{ key: "z" })      // diverges: legal
}
```

This is Swift's `guard let`, and it is the construct that keeps the
happy path unindented.

#### Choosing between the three

| Situation | Use |
|---|---|
| A default value is meaningful | `x ?? default` |
| Both cases have code | `if let … else` |
| Absence aborts the function | `let … else` |
| More than two shapes | `match` |

---

### 2. Under the Hood

#### How far exhaustiveness goes

Static exhaustiveness checking is the headline benefit of sum types,
and it runs:

```glide
type Shape = Circle(Float) | Square(Float) | Tri(Float, Float)

fn area(s: Shape) -> Float {
    match s {
        Circle(r) => 3.141592653589793 * r * r
        Square(w) => w * w
    }
}
```

```
app.gld:4:5: match is not exhaustive: Tri not handled
 4 |     match s {
   |     ^^^^^
```

Four things are worth knowing about the analysis.

**It covers sum types, `Option`, `Result` and `Bool`** — everything
whose values can be enumerated. `Int`, `String` and structs cannot be,
so they need a `_` arm, and forgetting it is still a runtime
fall-through.

**Coverage recurses one constructor deep.** `Err(A)` without `Err(B)`
reports `Err(B) not handled` rather than treating `Err(_)` as one case.

**An arm that cannot run is also an error** — after a catch-all, or a
duplicate constructor. A dead arm is nearly always a bug you meant to
be live.

**A guarded arm covers nothing.** `Circle(r) if r > 0.0 =>` may not
fire, so it does not discharge the `Circle` case. That is the price of
guards being opaque to the analysis, and the alternative is an SMT
solver — see *Why guards are opaque* below.

Anything the analysis cannot judge passes in silence, and the runtime
keeps its own fall-through check as an assertion. That is the
checker-wide rule from Chapter 19: report only what is certain.

A `_ =>` arm is legal but **spends the guarantee** — it makes every
future variant handled, silently. Write it only when "anything else" is
genuinely the meaning.

#### How patterns are compiled ○

In the designed compiler, a `match` over a sum type compiles to a jump
table on the variant tag, with the payload read directly from the
matched arm — no dynamic type test, no downcast. Nested patterns
compile to a decision tree, and the exhaustiveness checker is the same
analysis run at compile time.

In the interpreter, a pattern is matched structurally against a value
at runtime: check the shape, bind the names into the arm's environment,
run the body.

#### Why `if let` panics on a non-`None` mismatch

This is a deliberate asymmetry and a slightly sharp edge.

`if let Some(x) = e` means "unwrap `e`, and if it is absent take the
else branch". It does not mean "if `e` happens to have this shape". So
when the scrutinee is a value that is neither `None` nor matchable by
the pattern, that is a *bug in the program*, not a case to route.

If you want shape dispatch, use `match`. The rule keeps `if let` from
quietly becoming a second, weaker `match` with no exhaustiveness
story.

#### `Option` is boxed

The interpreter represents `T?` **boxed**: every `T?` is `None` or
`Some(v)`, never a bare `v`. It was unboxed through M4b — `Some` was
the identity — because without static types the interpreter could not
see where the implicit `T -> T?` coercion belonged. The checker
removed that constraint, and M4c collected: the checker records each
coercion site and the evaluator builds the box there.

That closed three *silent wrong answers*: a present-but-`None` map
entry read as absent, a `None` sent over a channel ended the stream,
and `Option<Option<T>>` collapsed a level.

Consequences, all of them the ones you want:

- **`Option<Option<T>>` is representable**, and `Some(None)` differs
  from `None`. Spell it long-form (`Option<Int?>`); `T??` cannot lex.
- **A present-but-`None` map entry reads as present**, not absent.
- **A `None` sent over a channel is an ordinary element**
  (Chapter 28), not end-of-stream.
- `==` reaches inside the box, so `Some(1) == Some(1)`.

The implicit `T -> T?` promotion is unchanged, because the checker
knows where the coercion happens and the evaluator boxes exactly there
— the load-bearing checker of Chapter 19.

#### Struct-pattern strictness is a design lever

Requiring `..` for partial struct matches is not pedantry. Without it,
adding a field to a struct would change nothing at any match site,
silently — which defeats the entire "tell me when the world changes"
property that makes exhaustiveness valuable.

With it, `User{ name, .. }` is an explicit statement: "I know there are
other fields and I do not care about them here." Adding a field does
not break that arm, correctly. Whereas `User{ name, email }` *does*
break when a field is added, correctly, because that pattern claimed to
account for everything.

---

### 3. Why This Design?

#### Why "patterns are construction run backwards"

Because the alternative is memorising two grammars.

Go has constructors for structs and patterns for nothing: every
disassembly is manual field-picking (`u.Name`, `u.Role`), every variant
test is a type switch that gives you an interface you must then use,
and there is no way to test-and-extract in one step. That is not a
missing feature so much as a missing *symmetry*.

Once the principle is in place, every new construct comes with its
pattern for free. Tuples got `(a, b)`. Struct update got `..base`, and
struct patterns got `..`. List literals got `[a, ..xs, b]` spread, and
list patterns got `[first, ..rest]`. The `..` glyph does three jobs —
ranges, rest-patterns, struct update — and they are context-distinct,
which is a decision rather than an accident. Rust runs the same triple.

#### Why exact list patterns

Because "and whatever else" is a different claim from "exactly this",
and a language that conflates them cannot express argument validation.

`let [_, path] = os.args() else { usage() }` is a complete argument
check in one line *because* it rejects extra arguments. If list
patterns were prefix-matching by default, that line would silently
accept `prog file extra junk` and the program would need a separate
length check — at which point the pattern has bought nothing.

#### Why guards are opaque to exhaustiveness

Consider:

```glide
match n {
    x if x > 0  => "positive"
    x if x <= 0 => "non-positive"
}
```

A human can see this is total. Proving it requires the compiler to
reason about arithmetic — and once you start down that road, you are
building an SMT solver into the exhaustiveness checker and the answer
to "is this match exhaustive?" becomes undecidable in general.

So the checker assumes any guard can fail. The cost is that you write
one more arm. The benefit is that "exhaustive" means something precise
and the compiler never has to guess.

#### Why no or-patterns inside patterns

Rust allows `Some(1 | 2 | 3)`. Glide allows arm-level alternatives
(`1, 2, 3 =>`) but not `|` inside a pattern.

The reasoning: `|` inside patterns is a *second* alternation syntax for
a rare case, and the house rule is one way to do it. Arm-level commas
cover the common case. `DESIGN.md` marks this "revisit on pain" —
declined, not forbidden forever.

#### Why no `x @ pattern` bindings

Rust's `n @ 1..=5` binds the whole value while also matching a
sub-pattern. Guards cover the use case (`n if n >= 1 && n <= 5`), at
the cost of some repetition. One fewer concept.

#### Why no patterns in function parameters

Signatures stay flat. `fn area(Circle(r): Shape)` would be a partial
function — what happens for a `Rect`? — and it puts a refutable
construct in a position where refutation has nowhere to go. Destructure
on line one of the body instead.

Note that `for` headers *are* pattern positions, because they are
`let`-like rather than signature-like, and the patterns there must be
irrefutable.

#### Why no ref/binding modes

This is the biggest pattern-matching dividend of not having a borrow
checker.

Rust has an entire saga around "match ergonomics": `ref`, `ref mut`,
default binding modes, and the rules for when a pattern binds by value
versus by reference. It exists because Rust must track whether matching
moved the value out.

In Glide, patterns bind values, the GC holds them, and `let (mut a, b)
= pair` just works. The concept does not exist.

---

### 4. Competing Approaches

**Go.** No pattern matching. `switch` compares values; type switches
test an interface's dynamic type at one level with no payload
destructuring and no coverage check. Destructuring is manual field
access. The gap is the single largest reason Glide is a different
language rather than a Go proposal (Chapter 1).

**Rust.** The direct model. `match`, exhaustiveness, guards, nested
patterns, `if let`, `let … else`, struct patterns with `..`. Glide
takes almost all of it and declines four things: or-patterns inside
patterns, `@` bindings, patterns in function parameters, and
ref/binding modes. Rust's `match` also has the `|` alternation and
`matches!` macro that Glide does without.

**Swift.** `if let` and `guard let` come from here, including the "no
`Some` wrapper in the source" ergonomic. Swift's `switch` has
exhaustiveness, `where` clauses (guards), and value binding. Swift's
enum-with-associated-values is the same idea as a sum type.

**Haskell / ML.** The origin. Function definitions *are* pattern
matches (`f [] = 0; f (x:xs) = x + f xs`), which is more powerful and
is the thing Glide declines by keeping signatures flat.

**Python.** `match`/`case` since 3.10, with structural patterns,
class patterns, and guards. Genuinely good, and undermined by the
absence of exhaustiveness checking (there is nothing to be exhaustive
*over* in a dynamically typed language) and by the notorious
capture-versus-constant footgun: a bare lowercase name in a `case`
always *binds*, so `case RED:` matches everything if `RED` is a plain
module constant. Glide avoids that with the case rule — capitalised
tests, lowercase binds — which Python could not adopt because Python
constants are conventionally uppercase.

**Java.** Pattern matching for `switch` arrived in Java 21, with sealed
interfaces providing exhaustiveness. It works and it is verbose:
`case Circle c when c.radius() > 0 ->`. The arrival of sealed types
plus patterns in a mainstream enterprise language is good evidence the
idea has won.

**C / C++.** `switch` on integers with fallthrough by default.
`std::variant` plus `std::visit` plus overload sets approximates a sum
type match, in a way that makes the case for a language feature.

---

### 5. Common Mistakes

**Using commas between arms.** They are line-separated:

```glide
// Bad
match c { Red => "r", Green => "g" }

// Good
match c {
    Red => "r"
    Green => "g"
}
```

The error is `expected a pattern, found ','`, which is confusing until
you know the rule.

**Writing a multi-statement arm without a block.** An arm body is a
single expression:

```glide
// Bad
match state {
    Scanning => count += 1
                .InWord
    …
}

// Good
match state {
    Scanning => {
        count += 1
        .InWord
    }
    …
}
```

**Reaching for `_ =>` to silence a non-exhaustive match.** This is the
most damaging habit you can form:

```glide
// Bad — you have now opted out of the compiler telling you about
// every new variant, forever
match shape {
    Circle(r) => …
    _ => 0.0
}

// Good
match shape {
    Circle(r)  => …
    Rect(w, h) => …
    Point      => 0.0
}
```

Write `_ =>` only when "anything else" is genuinely the meaning — an
open-ended input like an HTTP method string or an integer, where
enumeration is impossible.

**Forgetting `..` in a struct pattern.** The error message tells you
exactly what to do, and the fix is two characters.

**Expecting a list pattern to prefix-match.** `[a, b]` is exactly two.
Use `[a, b, ..rest]` or `[a, b, .._]` for two-or-more.

**Falling off the end of a `let … else` block.** The block must
diverge. If your else case genuinely has a value to supply, you wanted
`??` or `if let`, not `let … else`.

**Using `if let` for variant dispatch.** It unwraps; it does not
dispatch. A non-`None` value that fails the pattern panics. Use
`match`.

**Naming a catch-all binding after the scrutinee.** Arm bindings are a
nested scope, so this hits the shadow ban:

```glide
// Bad — `e` is the parameter; the arm cannot shadow it
fn simplify(e: Expr) -> Expr {
    match e {
        Add(Num(0), x) => simplify(x)
        e              => e
    }
}

// Good
fn simplify(e: Expr) -> Expr {
    match e {
        Add(Num(0), x) => simplify(x)
        other          => other
    }
}
```

Rust permits `match x { x => … }` and it is idiomatic there. Glide does
not, because Rust allows nested shadowing and Glide bans it
(Chapter 4).

**Binding in a multi-value arm.**

```glide
// Bad — parse error
match n {
    1, x => …
}
```

Which alternative bound `x`? The question has no answer, so it is
banned.

**Assuming an unmatched value gives a nice type error.** Today it is a
runtime error naming the value that did not match. Read it as "your
match is not exhaustive", not as "this value is wrong".

---

### 6. Performance Considerations

**A `match` over a sum type compiles to a tag switch** (○) — a jump
table, same as a C `switch`. Payload access is a direct field read from
the matched arm, with no dynamic type test. This is strictly cheaper
than Go's type switch, which does an interface type comparison per
case.

**Nested patterns compile to a decision tree** that tests each
discriminant once. A well-written match does not re-test what it has
already established.

**Guards run in arm order** and are ordinary expressions. A guard that
calls an expensive function runs on every arm attempt that reaches it,
so put cheap discriminating patterns first.

```glide
// Bad — expensive_check runs for every non-Admin account
match a {
    Account{ .. } if expensive_check(a) => …
    Account{ role: Admin, .. }          => …
}

// Good — the cheap tag test comes first
match a {
    Account{ role: Admin, .. }          => …
    Account{ .. } if expensive_check(a) => …
}
```

**In the interpreter**, pattern matching is structural comparison at
runtime with an environment allocation per arm attempt that binds. It
is not fast. Do not benchmark match-heavy code against the tree-walker
and conclude anything about the language.

**Bindings are not copies.** Destructuring a struct or list binds
references to the existing values under the GC; no deep copy occurs.
`let User{ name, .. } = user` does not copy the name string.

**List patterns with a rest binding allocate.** `[first, ..rest]`
creates a new list for `rest`. If you are pattern-matching in a hot
loop over a large list, that is O(n) per iteration — use indexing or an
iterator instead. This is the one pattern form with a hidden
allocation, and it is worth knowing.

---

### 7. Best Practices

**Let the compiler enumerate for you. Avoid `_` on closed types.**

The single highest-value habit in Glide. When you add a variant to a
sum type, you want a list of every place that needs updating. Every
`_ =>` arm is a place that will silently do the wrong thing instead.

**Match on the thing, not on a derived boolean.**

```glide
// Bad — three booleans, eight combinations, three meaningful
if is_loading && !has_error { … }
else if has_error { … }
else if is_loaded { … }

// Good
match state {
    Loading      => …
    Loaded(data) => …
    Failed(e)    => …
}
```

Chapter 13 develops this properly; it is the core argument for sum
types.

**Destructure at the top of the function, not throughout it.**

```glide
// Bad
fn render(cfg: Config) -> String {
    let header = build(cfg.title, cfg.width)
    let body = fill(cfg.rows, cfg.width)
    let footer = sign(cfg.author)
    …
}

// Good
fn render(cfg: Config) -> String {
    let Config{ title, width, rows, author, .. } = cfg
    let header = build(title, width)
    let body = fill(rows, width)
    let footer = sign(author)
    …
}
```

The second version names its dependencies once, at the top, where a
reader looks.

**Use `let … else` for guard clauses; use `if let` when both branches
have work.**

```glide
// Good — absence aborts
fn get_note(db: Db, id: Int) -> Result<Response, ApiError> {
    let Some(row) = db.query_one(sql, ["id": id])? else {
        return Ok(http.not_found())
    }
    Ok(http.json(row))
}

// Good — both cases do something
if let cached = cache[key] {
    serve(cached)
} else {
    let fresh = compute(key)
    cache[key] = fresh
    serve(fresh)
}
```

**Order arms from most specific to most general.** Arms are tried top
to bottom, so a broad pattern above a narrow one makes the narrow one
dead code — and today nothing warns you about it.

```glide
// Bad — the second arm is unreachable
match a {
    Account{ name, .. }              => "user {name}"
    Account{ role: Admin, name, .. } => "admin {name}"
}
```

**Keep patterns shallow enough to read.** Three levels of nesting is
usually the limit before a reader has to count brackets. If a pattern
is deeper than that, the data model may be deeper than it needs to be.

**Do not put side effects in guards.** A guard may be evaluated on arms
that do not ultimately run, and it may be evaluated more than once as
the checker era changes evaluation strategy. Guards should be pure
predicates.

---

### 8. Examples

**A tiny expression evaluator — the canonical demonstration:**

```glide-run
type Expr =
    Num(Int)
    | Add(Expr, Expr)
    | Mul(Expr, Expr)
    | Neg(Expr)

fn eval(e: Expr) -> Int {
    match e {
        Num(n)    => n
        Add(a, b) => eval(a) + eval(b)
        Mul(a, b) => eval(a) * eval(b)
        Neg(a)    => -eval(a)
    }
}

fn show(e: Expr) -> String {
    match e {
        Num(n)    => "{n}"
        Add(a, b) => "({show(a)} + {show(b)})"
        Mul(a, b) => "({show(a)} * {show(b)})"
        Neg(a)    => "-{show(a)}"
    }
}

fn main() {
    // (2 + 3) * -4
    let e = Expr.Mul(
        .Add(.Num(2), .Num(3)),
        .Neg(.Num(4)),
    )
    println("{show(e)} = {eval(e)}")
}
```

```
((2 + 3) * -4) = -20
```

This is the workload the ML family was designed for, and it is why
`DESIGN.md` says the Glide compiler will be nicer code than the Go
interpreter that runs it: an AST is a sum type, a checker is exhaustive
matching, and `?` threads the phases.

Notice `Expr.Mul(...)` at the outermost level and `.Add(...)` inside —
the dot shorthand works where the expected type is known.

**Simplification — patterns doing real work:**

```glide
fn simplify(e: Expr) -> Expr {
    match e {
        Add(Num(0), x) => simplify(x)
        Add(x, Num(0)) => simplify(x)
        Mul(Num(0), _) => .Num(0)
        Mul(_, Num(0)) => .Num(0)
        Mul(Num(1), x) => simplify(x)
        Mul(x, Num(1)) => simplify(x)
        Neg(Neg(x))    => simplify(x)
        Add(a, b)      => .Add(simplify(a), simplify(b))
        Mul(a, b)      => .Mul(simplify(a), simplify(b))
        other          => other
    }
}

fn main() {
    let e = Expr.Add(.Num(0), .Mul(.Num(1), .Neg(.Neg(.Num(7)))))
    println(show(simplify(e)))
}
```

```
7
```

Each rule is one line, reads as the algebraic identity it is, and the
nesting (`Neg(Neg(x))`, `Add(Num(0), x)`) is doing the structural
matching that would otherwise be a page of `if` statements and type
assertions.

The last arm binds `other` rather than using `_`, which is a small
readability win — it says "anything else, unchanged" and names the
thing it returns. It is *not* called `e`, and that is not a style
choice: **a match arm's bindings live in a nested scope**, so
`e => e` inside a function whose parameter is `e` hits the
nested-shadow ban from Chapter 4:

```
error: line 36: cannot shadow "e" from an enclosing block
       (redeclaring in the same scope is fine; nested shadowing is not)
```

This catches people, because `match x { x => … }` is idiomatic in Rust,
which permits nested shadowing. Here the catch-all arm needs its own
name.

**Bad versus good on the same problem:**

```glide
// Bad — Go-in-Glide: a kind field and manual dispatch
type Node = struct {
    kind: String
    value: Int
    left: Node?
    right: Node?
}

fn eval(n: Node) -> Int {
    if n.kind == "num" {
        n.value
    } else if n.kind == "add" {
        eval(n.left ?? panic()) + eval(n.right ?? panic())
    } else {
        0
    }
}
```

Everything is wrong here and each thing is instructive. The `kind`
field is a string, so a typo is a runtime surprise. `left` and `right`
are optional on *every* node, including numbers where they are
meaningless — illegal states are representable. There is no coverage
check, so a new node kind falls into the `else`. And the `?? panic()`
is the tell: the code knows the fields are present and cannot prove it.

The sum-type version above makes each of those unrepresentable. That is
the argument for Chapter 13 in one comparison.

**Matching on structure and value together:**

```glide
type Method = Get | Post | Delete
type Request = struct { method: Method, path: String, body: String }

fn route(r: Request) -> String {
    match r {
        Request{ method: Get,  path: "/health", .. } => "ok"
        Request{ method: Get,  path, .. }            => "read {path}"
        Request{ method: Post, body: "", .. }        => "400 empty body"
        Request{ method: Post, path, .. }            => "write {path}"
        Request{ method: Delete, path, .. }          => "delete {path}"
    }
}

fn main() {
    println(route(Request{ method: .Get, path: "/health", body: "" }))
    println(route(Request{ method: .Get, path: "/notes", body: "" }))
    println(route(Request{ method: .Post, path: "/notes", body: "" }))
    println(route(Request{ method: .Post, path: "/notes", body: "x" }))
    println(route(Request{ method: .Delete, path: "/notes/1", body: "" }))
}
```

```
ok
read /notes
400 empty body
write /notes
delete /notes/1
```

Five routing rules, each one line, each testing a mix of variant and
literal and binding what it needs. The equivalent in Go is a nested
`switch` with a `strings.HasPrefix` ladder inside.

---

### 9. Summary & Exercises

**Summary**

- **A pattern is construction run backwards.** Whatever builds a value,
  the same syntax takes it apart. Nothing else in this chapter needs
  memorising.
- Patterns appear in `let`, `let … else`, `if let`, `for` headers, and
  `match` arms.
- `match` arms are separated by a **newline or a comma** — the comma is
  optional, including a trailing one, and it is what makes a one-line
  `match x { A => 1, B => 2 }` writable at all. Arms bind as they match,
  and an arm body is a **single expression** (use a block for several
  statements; `return` is a statement, so it needs one).
- Case is grammar: capitalised patterns test, lowercase patterns bind.
- **Exhaustiveness** is the payoff — adding a variant breaks every match
  that does not handle it, **statically** ✓, in one pass, naming the
  case. Coverage recurses one constructor deep, an unreachable arm is
  an error too, and a guarded arm covers nothing. Avoid `_ =>` on closed
  types; it spends the guarantee.
- **List patterns are exact** unless they contain `..`. **Struct
  patterns must mention every field** unless they end with `..`. Both
  rules exist so that adding data breaks the patterns that claimed to
  account for everything.
- **Guards** are extra conditions, and they are opaque to
  exhaustiveness: coverage assumes every guard fails.
- Literal, range, and string patterns work; string matching is equality
  only. Multiple values per arm (`1, 2, 3 =>`) may not bind.
- Subjectless `match` is a first-true-wins condition chain.
- `if let` unwraps into a scope; `let … else` unwraps or diverges;
  `??` supplies a default. Choose by what the absent case should do.
- Declined permanently: or-patterns inside patterns, `x @ pattern`,
  patterns in function parameters, ref/binding modes.

**Exercises**

1. **Convert a type switch.** Take a Go type switch over an interface —
   an AST walker, a JSON decoder, a protocol handler — and write the
   Glide equivalent as a sum type plus a `match`. Count the lines and,
   more importantly, count the states the Go version could represent
   that the Glide version cannot.

2. **Make the compiler find your bug.** Write a sum type with three
   variants and three functions that `match` on it. Add a fourth
   variant. Note which functions the interpreter complains about and
   when (hint: only the ones you actually execute). Then write down
   what the static checker would have given you instead. This exercise
   is the difference between the two tiers, made concrete.

3. **Design the pattern that is missing.** Find a destructuring you
   want to write that Glide's pattern set cannot express. Candidates:
   matching a map by key, matching a string by prefix, matching "a list
   whose last element is X", or or-patterns. For each, decide whether
   the right answer is a new pattern form, a guard, or a restructured
   data model. `DESIGN.md` marks or-patterns as "revisit on pain" — see
   whether you can generate the pain.
