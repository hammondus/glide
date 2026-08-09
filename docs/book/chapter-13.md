# Chapter 13: Sum Types

This is the chapter the language exists for.

`DESIGN.md` calls sum types "the star feature", and the reasoning is
one sentence: *modelling "one of these N shapes" is the most common
thing programs do.* A payment is card or transfer or cash. A config
value is a string or a number or a list. An AST node is one of the node
kinds. A request either succeeded or failed. A job is queued, running,
done, or failed.

If you have only used C-lineage languages, you have been approximating
this your whole career — with an enum tag plus a struct of
mostly-unused fields, or an interface plus type switches, or a
`kind` string and a comment saying which fields are valid when. Every
approximation relies on discipline. A sum type makes the discipline a
type.

This chapter teaches sum types from zero. Everything is ✓ except
`derive Enum` and explicit discriminants.

---

### 1. Basic Usage

#### The idea

A **sum type** says: a value of this type is **exactly one of these N
shapes**, and each shape may carry its own data.

```glide
type Shape =
    Circle(Float)
    | Rect(Float, Float)
    | Point
```

A `Shape` is a `Circle` holding one float, *or* a `Rect` holding two,
*or* a `Point` holding nothing. Never two of them. Never none of them.
Never a shape you did not list.

The alternatives are called **variants**. `Circle`, `Rect`, and `Point`
are the variants of `Shape`.

If that sounds like an enum: it is an enum whose variants can carry
payloads, checked by the compiler. If it sounds like a C union: it is a
union that always knows which member is live and refuses to read the
wrong one.

#### Constructing

Variants are **namespaced**. In an expression you write the full form
or the leading-dot shorthand:

```glide
let a = Shape.Circle(2.0)
let b = Shape.Rect(3.0, 4.0)
let c = Shape.Point
```

Where the expected type is already known — a match arm, an assignment
with an annotation, a function argument — the dot shorthand reads
better:

```glide
fn area(s: Shape) -> Float { … }

println(area(.Rect(3.0, 4.0)))
println(area(.Point))
```

A **bare** variant name in an expression is an error:

```glide
let s = Circle(2.0)
```

```
error: line 4: variants are namespaced: write .Circle or Shape.Circle
       (bare variant names are pattern-only)
```

In *patterns*, bare names are correct — `Circle(r)` — because the
scrutinee's type already says which namespace applies. That asymmetry
is deliberate and section 3 defends it.

#### Consuming: `match`

Using a sum type means `match`, and that pairing is where the payoff
compounds:

```glide
fn area(s: Shape) -> Float {
    match s {
        Circle(r)  => 3.14159 * r * r
        Rect(w, h) => w * h
        Point      => 0.0
    }
}
```

Each arm tests the variant *and* extracts its payload in one step. Miss
a variant and the program does not compile:

```
error: line 2: match is not exhaustive: Blue not handled
```

Coverage recurses one constructor deep, so `Err(A)` handled without
`Err(B)` reports `Err(B) not handled` rather than shrugging at `Err`.
A guarded arm covers nothing — it may not fire — and a type with too
many values to enumerate (`Int`, `String`, a struct) needs a `_`.

#### Named-field payloads

Positional payloads are fine for one or two values. Beyond that, name
them:

```glide
type ApiError =
    NotFound{ id: Int }
    | BadInput{ msg: String }
    | Db{ cause: String }
```

Construct with the struct-literal form, read with field access, and
match under the same mention-all-or-`..` rule as structs:

```glide
let e = ApiError.NotFound{ id: 7 }
println(e.id)

match e {
    NotFound{ id }   => "no note {id}"
    BadInput{ msg }  => "bad input: {msg}"
    Db{ cause }      => "database: {cause}"
}
```

This is the same doctrine as tuples-versus-structs (Chapter 11):
positional for two-things-briefly, named once it stops being obvious.

#### Enums are the degenerate case

A variant with no payload is just a variant with no payload. A C-style
enum is therefore not a separate feature:

```glide
type Color = Red | Green | Blue
```

One line, the same form as any sum type, and a variant can gain a
payload later without changing feature:

```glide
type Color = Red | Green | Blue | Custom{ r: Int, g: Int, b: Int }
```

Every existing `match` on `Color` becomes a compile error, listing
exactly the places that need to think about `Custom`. That is the
feature.

**There is no implicit integer conversion in either direction.**
`Color(42)` is not a thing. From wire data, the designed spelling is
`Color.from_int(n) -> Color?` (○) — total, with the invalid case
handled where the data enters. To an integer, an explicit method. An
enum is a set of names, not an integer in a costume.

#### `Option` and `Result` are sum types

They are not magic. They are ordinary two-variant types that happen to
be built in, so the whole ecosystem agrees on one:

```glide
type Option<T> = Some(T) | None                 // conceptually
type Result<T, E> = Ok(T) | Err(E)              // conceptually
```

Everything you learn about matching sum types applies to them directly.
Chapters 14 and 19 cover their ergonomics.

#### Recursive sum types

A variant may hold the type being defined, which is how you model
trees, lists, and grammars:

```glide
type Expr =
    Num(Int)
    | Add(Expr, Expr)
    | Mul(Expr, Expr)
    | Neg(Expr)
```

```glide
fn eval(e: Expr) -> Int {
    match e {
        Num(n)    => n
        Add(a, b) => eval(a) + eval(b)
        Mul(a, b) => eval(a) * eval(b)
        Neg(a)    => -eval(a)
    }
}
```

Chapter 10 built this example out; it is the workload the ML family was
designed for.

#### Explicit discriminants ○

For wire and FFI stability:

```glide
type Status = Ok = 200 | NotFound = 404 | Error = 500      // ○
```

Fixed representation only where declared; the compiler's choice
otherwise.

#### `derive Enum` ○

Comptime reflection's first easy customer:

```glide
type Color = Red | Green | Blue derive(Enum)      // ○
```

gives you `Color.all()`, `c.name()`, and `Color.from_name(s) ->
Color?`. Go needs an external `stringer` tool for this.

---

### 2. Under the Hood

#### Representation ○

In the designed compiler, a sum type is a **tagged union**: a small
integer discriminant plus enough storage for the largest payload,
with the compiler choosing the layout. A `match` compiles to a jump
table on the tag, and payload access in an arm is a direct field read —
no dynamic type test, no downcast, no allocation.

Two consequences worth knowing:

- A sum type is as large as its largest variant plus the tag. A type
  with one tiny variant and one enormous one wastes space in every
  value. If that matters, box the large variant.
- Niche optimisation (○): `Option<T>` where `T` is a reference can use
  a null pointer as the `None` tag, so `T?` costs the same as `T`.
  Rust does this and it is why `Option<&T>` is free there.

In the interpreter, a variant is a small record holding the type name,
the variant name, and the payload. Matching compares names.

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

#### `Timeout` is a synthetic variant

An interesting corner: `scope(timeout: 5.s)` produces `Err(Timeout)`,
where `Timeout` is a variant with no declared type behind it. The
interpreter synthesises it so that `Err(Timeout)` matches the bare
pattern `Timeout`, renders as `Timeout`, and converts through a user's
`fn from(t: Timeout)` — all without any global type declaration the
program did not write. The checker era makes `Timeout` a real stdlib
type and nothing about programs changes. Chapter 27 covers this.

#### Exhaustiveness, and why guards are opaque

Coverage is computed structurally over the variant set. A `match` that
names every variant is exhaustive; one that misses any is not. Nested
patterns compose: `Ok(User{ role: Admin, .. })` contributes coverage of
one point in a two-dimensional space, and the checker tracks the rest.

Guards are excluded from coverage, on the assumption that any guard can
fail (Chapter 10). This makes exhaustiveness decidable at the cost of
one extra arm.

---

### 3. Why This Design?

#### What sum types actually buy

Three things, and it is worth separating them because people usually
name only the first.

**1. Illegal states become unrepresentable.**

Take the four-boolean job status from Chapter 12:

```glide
type Job = struct {
    queued: Bool
    running: Bool
    done: Bool
    failed: Bool
    error: String
}
```

Sixteen representable states. Four meaningful. The other twelve —
running *and* done, failed with an empty error, none of the four — are
states your code must either handle or hope never occur. Somewhere,
something will produce one, because nothing prevents it.

```glide
type JobState =
    Queued
    | Running{ since: Instant }
    | Done{ took: Duration }
    | Failed{ reason: String }
```

Four states. Exactly four. And notice the second benefit that fell out:
`took` exists only when the job is `Done`, `reason` only when it
`Failed`. **The data is attached to the state that owns it**, so there
is no "is this field meaningful right now?" question at all.

**2. Adding a case produces a work list.**

This is the one people underestimate. When you add a fifth state, every
`match` in the codebase that does not handle it becomes a compile error
— and the compiler hands you the complete list of places the change
touches.

In the C lineage, adding a case means grepping for the `switch`
statements and praying you found them all. The failure mode is not a
crash; it is a silent fall into a `default` branch that does something
plausible and wrong.

This is what `DESIGN.md` means by "tell me when the world changes", and
it is why the guidance about `_ =>` arms is so insistent. A `_ =>`
converts a compile error into a silent default. Use it only where
"anything else" is genuinely the meaning.

**3. Test-and-extract is one step.**

```glide
match payment {
    Card{ last4, .. }     => "card ending {last4}"
    Transfer{ iban }      => "transfer to {iban}"
    Cash                  => "cash"
}
```

There is no path where you have checked "it's a card" and are holding
something that might not be. Compare Go:

```go
switch p := payment.(type) {
case Card:
    // p is a Card here, but only because the compiler special-cases
    // this one syntax; and there is no coverage check
}
```

Go's type switch does approximately this at one level. It cannot
destructure a payload, cannot nest, and does not check coverage.

#### Why variants are namespaced

Three options were available: bare names, namespaced names, or
`use`-style importing of variants into scope.

Bare names lose immediately: two types with a `NotFound` variant would
collide, and every sum type would be competing for the global name
space. Since sum types are meant to be *cheap* — you should define one
whenever three things are one of three shapes — that would not scale.

`use`-dumping (Rust's `use Color::*`) pollutes and makes the reader
hunt for where a bare name came from.

Namespacing plus Swift's leading-dot shorthand gets both: `Color.Red`
when the context is unclear, `.Red` when the expected type is known —
one character that means "resolve in the expected type".

#### Why bare names work in patterns but not expressions

Because the scrutinee tells you the namespace. In `match shape { Circle(r) => … }`,
the type of `shape` determines where `Circle` is looked up, so there is
nothing to disambiguate.

In an expression there is no scrutinee. `let x = Circle(2.0)` has no
context to resolve against, so the dot form is required — and where
there *is* context (a typed parameter, a match arm's expected value),
the shorthand `.Circle(2.0)` supplies it.

`.Circle` resolves **in the expected type**, as of M4b — the checker
records which type each shorthand landed in and the evaluator
constructs that variant. Where there is no expected type to resolve in,
a file-wide fallback still applies, since variant names are unique
within a file.

#### Why flags are not enums

```glide
// Bad
type Permission = Read = 1 | Write = 2 | Execute = 4      // a bitfield
```

"One of" and "set of" are different types. An enum whose values are
powers of two, combined with `|`, is an integer in a costume — the
exact thing enums exist to stop.

```glide
// Good
type Permission = Read | Write | Execute
let perms: Set<Permission> = …                             // ○ Set
```

Or a `BitSet` where performance demands it. The set-ness lives in the
collection type where it is visible.

#### Why Go's `iota` is the anti-pattern

`DESIGN.md` says Go's enum story is "both its diseases at once", which
is harsh and accurate. Enumerate them:

- `iota` is a counter macro, not an enum. The resulting type is an
  integer alias.
- Any integer converts: `Color(42)` compiles and flows.
- No exhaustiveness: a `switch` missing a case is silent.
- No enumeration: you cannot ask for all the values.
- Names need the external `stringer` code generator.
- **The zero value silently means the first variant.** A struct field
  of type `Color` is `Red` whether or not anyone chose red.

That last one is the interaction with zero values from Chapter 12, and
it is why the two decisions have to be made together.

---

### 4. Competing Approaches

**Go.** No sum types. The approximations, in decreasing order of
respectability: an interface with a private marker method and a type
switch (works, no coverage check, one level, no payload
destructuring); a struct with a `kind` field and mostly-nil pointers
(the flag soup); `iota` constants (see above). A sum-type proposal has
been open for years.

**Rust.** `enum` with variants carrying data — the direct model. Glide
takes the semantics wholesale and differs only in spelling
(`type X = A | B` rather than `enum X { A, B }`) and in declining
or-patterns and `@` bindings. Rust's niche optimisation is the
representation trick Glide plans to adopt.

**Swift.** `enum` with associated values, plus the leading-dot
shorthand Glide takes. Swift's `indirect enum` keyword for recursive
cases is machinery Glide does not need (GC).

**Haskell / ML.** The origin — algebraic data types, from the 1970s.
`data Shape = Circle Float | Rect Float Float`. Glide's `|` separator
comes straight from here.

**Java.** Sealed interfaces (Java 17) plus records plus pattern
matching for `switch` (Java 21) add up to sum types, verbosely. The
fact that a language as conservative as Java arrived here in 2021 is
strong evidence the idea has won.

**C.** `enum` plus `union` plus a manually maintained tag, with nothing
checking that the tag matches the live member. This is the failure mode
that everything above is a response to.

**TypeScript.** Discriminated unions —
`{ kind: "circle", r: number } | { kind: "rect", w: number, h: number }`
— with exhaustiveness via the `never` type. Genuinely good, and a
useful demonstration that the pattern is valuable even bolted onto a
dynamic language.

**Python.** `Union` types plus `match`/`case`, without exhaustiveness
(nothing to be exhaustive over). `enum.Enum` for the payload-free case.

---

### 5. Common Mistakes

**Writing a bare variant in an expression.** The most common first-hour
error. The message tells you the fix.

**Reaching for `_ =>` to make the error go away.**

```glide
// Bad — you just opted out of the feature
match shape {
    Circle(r) => area_circle(r)
    _         => 0.0
}
```

You added `_` because the interpreter complained about an unmatched
variant. What you have done is guarantee that every future variant
silently returns `0.0`. Write the arms.

**Using a struct with a `kind` field.** This is the Go habit and it
survives translation:

```glide
// Bad
type Node = struct {
    kind: String
    value: Int
    left: Node?
    right: Node?
}

// Good
type Node = Leaf(Int) | Branch(Node, Node)
```

The tells: a `kind`/`type`/`tag` field; several `Option` fields where
only certain combinations are valid; a comment explaining which fields
apply when.

**Modelling with booleans that are not independent.** Two booleans
means four states. If only three are meaningful, it is a three-variant
sum type.

**Putting a giant payload in one variant of a small type.** Every value
of the type is sized for the largest variant (○). If one variant holds
a 1 KB buffer and the others hold an `Int`, every value costs 1 KB.
Box the large one.

**Naming a catch-all binding after the scrutinee.**

```glide
// Bad — arm bindings are a nested scope; this hits the shadow ban
fn simplify(e: Expr) -> Expr {
    match e {
        Add(Num(0), x) => simplify(x)
        e              => e
    }
}

// Good
        other          => other
```

Chapter 10 covers this.

**Reaching for `Option<Option<T>>` when you want three named states.**
It works — `Option` is boxed, and `Some(None) != None` — but the type
tells a reader nothing about what the two absences mean. Spell it out
where the distinction matters:

```glide
type Field<T> = Missing | Null | Present(T)
```

Also note that `T??` cannot lex; the nested form is written
`Option<Int?>`.

**Expecting variant names to be globally unique.** Two types may both
have a `NotFound` variant, and that is fine in patterns (the scrutinee
disambiguates). Do not rely on the `.NotFound` shorthand resolving
correctly in an ambiguous position today — the interpreter uses a
global namespace and the checker will not.

**Using an enum where a `Bool` would do — or vice versa.** Two named
states are often clearer than a boolean (`Direction.Ascending` beats
`descending: false`), and a boolean is often clearer than a
two-variant enum with no payloads and no plans to grow. The
discriminator: will it ever grow a third case, or a payload? If yes,
sum type now.

---

### 6. Performance Considerations

**A `match` on a sum type is a jump table** (○) — the same cost as a C
`switch`, with no dynamic type test. This is strictly cheaper than
Go's type switch, which compares interface type descriptors per case.

**Payload access in an arm is a direct field read.** No downcast, no
bounds check, no branch.

**A sum type is sized for its largest variant plus a tag.** Balance the
variants, or box the outlier:

```glide
// Every Message costs as much as the largest variant
type Message =
    Ping
    | Pong
    | Payload(List<Int>)        // a List is a reference — fine, one word

// Watch out for inline aggregates
type Message =
    Ping
    | Snapshot(BigInlineStruct)  // now every Message is that big
```

Because Glide's collections are references, this is less of a hazard
than in Rust, where a `Vec` is three words and an inline array is
however big it is.

**Niche optimisation** (○) means `Option<T>` for reference-typed `T` is
free — a null pointer is the `None` tag, so `T?` and `T` are the same
size. For value types like `Int`, `Option<Int>` costs an extra word.

**Recursive types allocate per node.** `Add(Expr, Expr)` holds
references, so an expression tree is a heap graph. That is the same
cost as any tree in any GC language.

**In the interpreter**, a variant is a small record and matching
compares strings. Slow, and irrelevant to the compiled tier.

**Construction is cheap.** `.Circle(2.0)` writes a tag and a payload.
No allocation for a value-typed payload in the compiled tier; one small
allocation in the interpreter.

---

### 7. Best Practices

**Reach for a sum type whenever you catch yourself writing "or".** If
the sentence describing your data contains the word "or", the type
should too.

**Attach data to the state that owns it.**

```glide
// Bad — every field must be checked for relevance
type Connection = struct {
    state: State
    socket: Socket?
    error: String?
    retry_at: Instant?
}

// Good
type Connection =
    Disconnected
    | Connecting{ retry_at: Instant }
    | Connected{ socket: Socket }
    | Failed{ error: String }
```

**Enumerate your failure modes.** This is sum types' best use case and
Chapter 20 is built on it:

```glide
// Bad — every caller must handle "something went wrong"
fn get_note(id: Int) -> Result<Note, Error>

// Good — the signature documents what can go wrong
type ApiError =
    NotFound{ id: Int }
    | BadInput{ msg: String }
    | Db{ cause: String }

fn get_note(id: Int) -> Result<Note, ApiError>
```

The caller can now `match` and handle `NotFound` differently from `Db`,
without string matching or `errors.Is` archaeology.

**Prefer named fields once there are more than two, or once the types
repeat.**

```glide
// Bad — which Float is which?
Rect(Float, Float)

// Good
Rect{ width: Float, height: Float }
```

**Use the dot shorthand where the type is obvious, the full name where
it is not.**

```glide
// Good — the parameter type says it
fn area(s: Shape) -> Float
println(area(.Circle(2.0)))

// Good — a bare `let` has no context
let s = Shape.Circle(2.0)
```

**Do not add variants speculatively.** A sum type with a variant that
nothing constructs is a `match` arm everyone has to write for no
reason. Add the variant when the case exists.

**Keep the variant count readable.** Beyond seven or eight, ask whether
two dimensions have been flattened into one. `HttpGet | HttpPost |
HttpPut | GrpcUnary | GrpcStream` is probably `Protocol` × `Method`.

**Do not use a sum type for open-ended sets.** Sum types are closed by
definition, and that closedness is the feature. If new cases can arrive
at runtime from configuration or plugins, you want a trait
(Chapter 17) or a map, not a sum type.

---

### 8. Examples

**The transformation, worked end to end.** Start with the shape a Go
programmer would write:

```glide
// Version 1 — the flag soup
type Download = struct {
    pub url: String
    pub started: Bool
    pub bytes_done: Int
    pub total_bytes: Int
    pub finished: Bool
    pub error: String
}

fn describe(d: Download) -> String {
    if d.error != "" {
        "failed: {d.error}"
    } else if d.finished {
        "done ({d.total_bytes} bytes)"
    } else if d.started {
        "{d.bytes_done}/{d.total_bytes}"
    } else {
        "queued"
    }
}
```

Count the representable states: two booleans and a string-as-flag give
eight combinations, of which four are meaningful. What is a `Download`
that is `finished` with a non-empty `error`? What is one that is not
`started` but has `bytes_done: 500`? The type says nothing, so the
answer is "whatever the code happens to do", and `describe`'s ordering
of the `if`s *is* the specification — undocumented, and easy to get
wrong when someone adds a case.

```glide-run
// Version 2 — one axis extracted
type Progress =
    Queued
    | Running{ done: Int, total: Int }
    | Finished{ total: Int }
    | Failed{ reason: String }

type Download = struct {
    pub url: String
    pub progress: Progress
}

fn describe(d: Download) -> String {
    match d.progress {
        Queued                => "queued"
        Running{ done, total } => "{done}/{total}"
        Finished{ total }      => "done ({total} bytes)"
        Failed{ reason }       => "failed: {reason}"
    }
}

fn main() {
    let urls = ["a", "b", "c", "d"]
    let states = [
        Progress.Queued,
        .Running{ done: 512, total: 2048 },
        .Finished{ total: 2048 },
        .Failed{ reason: "connection reset" },
    ]
    for (url, p) in urls.iter().zip(states) {
        println("{url}: {describe(Download{ url: url, progress: p })}")
    }
}
```

```
a: queued
b: 512/2048
c: done (2048 bytes)
d: failed: connection reset
```

Four states, exactly four. `done` and `total` exist only while running;
`reason` only on failure. The `match` is exhaustive, so adding a
`Cancelled` variant produces an error here rather than a silent fall
through to `"queued"`.

**A configuration value — the dynamic-data case:**

```glide
type Value =
    Str(String)
    | Num(Int)
    | Flag(Bool)
    | Items(List<Value>)

fn render(v: Value) -> String {
    match v {
        Str(s)   => "\"{s}\""
        Num(n)   => "{n}"
        Flag(b)  => "{b}"
        Items(xs) => {
            let parts = xs.iter().map(|x| render(x)).collect()
            "[{parts.join(", ")}]"
        }
    }
}
```

A string literal inside an interpolation lexes correctly, **unescaped**:
`"[{parts.join(", ")}]"`. The lexer tracks the nesting, so the inner
`"` opens a new string rather than closing the outer one. Escaping them
(`\"`) is what fails — the backslash makes them the *outer* string's
quotes, and the interpolation never terminates.

```glide
fn main() {
    let cfg = Value.Items([
        .Str("hello"),
        .Num(42),
        .Flag(true),
        .Items([.Num(1), .Num(2)]),
    ])
    println(render(cfg))
}
```

```
["hello", 42, true, [1, 2]]
```

This is exactly the shape Glide's designed JSON module uses for dynamic
data (Chapter 33): `Null | Bool | Number | String | Array | Object`.
Exhaustive matching over shapes replaces the type-assertion ladder that
`map[string]interface{}` forces in Go.

**A state machine as a sum type, showing the compile-error payoff:**

```glide-run
type Door = Open | Closed | Locked{ key_id: Int }

fn act(d: Door, action: String) -> Door {
    match d {
        Open   => if action == "close" { .Closed } else { d }
        Closed => {
            match action {
                "open" => .Open
                "lock" => .Locked{ key_id: 1 }
                _      => d
            }
        }
        Locked{ key_id } => {
            if action == "unlock" { .Closed } else { d }
        }
    }
}

fn main() {
    let mut d = Door.Open
    for a in ["close", "lock", "open", "unlock", "open"] {
        d = act(d, a)
        println("{a:8} -> {d:?}")
    }
}
```

```
   close -> Closed
    lock -> Locked{ key_id: 1 }
    open -> Locked{ key_id: 1 }
  unlock -> Closed
    open -> Open
```

(`{a:8}` is width 8, *right*-aligned — `{a:-8}` is the left-aligned
form. Chapter 6 has the table.)

Note the third line: trying to open a locked door does nothing, and
that behaviour is *stated* in the `Locked` arm rather than emerging
from the ordering of an `if` chain. Now imagine adding an `Ajar`
variant. Every `match` on `Door` in the program lights up.

---

### 9. Summary & Exercises

**Summary**

- A **sum type** says a value is exactly one of N shapes, each of which
  may carry its own data. `type Shape = Circle(Float) | Rect(Float,
  Float) | Point`.
- Variants are **namespaced**: `Shape.Circle(2.0)` in full, `.Circle(2.0)`
  where the expected type is known. Bare names are **pattern-only**.
- Payloads may be **positional** (`Rect(Float, Float)`) or **named**
  (`NotFound{ id: Int }`). Use named once there are more than two, or
  once the types repeat.
- Using a sum type means `match`, and `match` tests and extracts in one
  step.
- **Exhaustiveness is the payoff.** Adding a variant should break every
  match that does not handle it, producing a work list. `_ =>` spends
  that guarantee.
- Sum types buy three things: illegal states become unrepresentable;
  data is attached to the state that owns it; adding a case produces a
  compile-time work list.
- **A C-style enum is the degenerate case** — variants with no
  payloads — not a separate feature. No implicit integer conversion in
  either direction.
- **Flags are not enums.** "One of" is a sum type; "set of" is a `Set`
  or `BitSet`.
- `Option` and `Result` are ordinary two-variant sum types that happen
  to be built in.
- ○: `derive Enum` (giving `all()`, `name()`, `from_name()`), explicit
  discriminants for wire stability, and niche-optimised representation.
- `Option` is boxed as of M4c, so `Option<Option<T>>` is
  representable; spell it `Option<Int?>`, since `T??` cannot lex.

**Exercises**

1. **Count states.** Take a struct from your current codebase with
   three or more booleans, or with several nullable fields. Write down
   2^n (or the product of the option-ness). Then write down how many
   states are actually meaningful. Model it as a sum type and confirm
   the count matches. Every state in the gap was a bug waiting for a
   sufficiently unusual input.

2. **Add a variant and watch the fallout.** Write a sum type with three
   variants and four functions that match on it — put a `_ =>` arm in
   two of them. Add a fourth variant. Note which functions the compiler
   would flag and which would silently do the wrong thing. This is the
   most direct demonstration of why `_` is expensive.

3. **Model a protocol.** Pick a real protocol you have implemented —
   HTTP status handling, a WebSocket frame, a database driver's message
   types, an event stream. Write it as a sum type. Then find the place
   in your existing implementation where an invalid combination is
   possible and confirm the sum type makes it unwritable. If it does
   not, the model is not finished — keep splitting until it does.
