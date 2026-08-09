# Chapter 12: Structs

Structs are the boring part of Glide's data model and the chapter is
short for that reason — with one exception that is not boring at all:
**there are no zero values.** Every struct literal must account for
every field, and `Config{}` is not a thing you can write.

That single rule kills a bug class that Go programmers meet weekly, and
it is the same decision as "there is no null", applied to composite
data.

Everything here is ✓ except `derive` and the `Default` trait, which are
flagged.

---

### 1. Basic Usage

#### Declaring and constructing

```glide
type Config = struct {
    pub host: String
    pub port: Int
    timeout: Int
}
```

One keyword — `type` — declares every shape of type in Glide. A struct
is `type Name = struct { … }`. There is no separate `struct` keyword at
the declaration level; `struct` is a body marker.

Fields are **private by default**. `pub` marks a field visible outside
the module. A public type with private fields is therefore the natural
encapsulation shape, with no getter ceremony and no convention to
follow.

Construction names every field:

```glide
let c = Config{ host: "localhost", port: 8080, timeout: 30 }
```

#### Mandatory initialisation

Miss a field and it does not compile:

```glide
type P = struct { x: Int, y: Int }

fn main() {
    let p = P{ x: 1 }
}
```

```
error: line 2: missing field "y" in P literal (no zero values)
```

Name a field that does not exist and you get told:

```
error: line 2: P has no field "z"
```

There is no `P{}`. There is no partially-initialised struct. A `Config`
value that exists is a `Config` value that is complete.

#### Field access and mutation

```glide
println(c.host)

let mut m = Config.new()
m.port = 9090
```

Mutation requires a `mut` path, and mutability is **transitive**:
`a.b.c = v` is legal only if `a` is `mut`.

#### Struct update

```glide
let base = Config{ host: "localhost", port: 8080, timeout: 30 }
let prod = Config{ host: "prod.example", ..base }
```

`..base` fills in every field not explicitly given. The base is
untouched — this is copy-with-changes, not mutation:

```
localhost:8080 t=30       // base.describe()
prod.example:8080 t=30    // prod.describe()
localhost:8080 t=30       // base.describe() again — unchanged
```

Rules: `..base` comes last, and there is exactly one of them. It
composes with mandatory initialisation — the literal accounts for every
field, and `..base` accounts for the unchanged ones.

In an immutable-by-default language this is *the* way data evolves, not
a convenience. Chapter 10's recursive tree insert builds each modified
node this way while untouched subtrees are shared as-is.

#### Methods live in `impl` blocks

```glide
impl Config {
    fn new() -> Config {
        Config{ host: "localhost", port: 8080, timeout: 30 }
    }

    fn describe(self) -> String {
        "{self.host}:{self.port} t={self.timeout}"
    }
}
```

`Config.new()` — no `self` parameter — is an **associated function**,
called on the type itself. That is the constructor idiom, and it is
where a type's invariants get enforced, since the fields may be
private.

Methods take `self`. Chapter 16 covers receivers, mutability, and
associated functions properly.

#### Struct patterns

Construction run backwards, as always:

```glide
let Config{ host, port, .. } = c
```

The `..` is required for a partial match (Chapter 10). Field shorthand
binds; `field: pattern` nests:

```glide
match account {
    Account{ role: Admin, name, .. } => "admin {name}"
    Account{ age: 0..18, name, .. }  => "minor {name}"
    Account{ name, .. }              => "user {name}"
}
```

#### `derive` ○

```glide
type Note = struct {
    pub id: NoteId
    pub title: String
    pub created: Instant
} derive(Json, Row, Debug)
```

`derive` asks the compiler to generate implementations by walking the
type's structure at compile time — JSON encoding, database row mapping,
debug printing. It is ○, comptime-era work (Chapter 36).

Today, structural Debug output works without deriving anything, because
the interpreter renders values structurally:

```glide
println("{c:?}")     // Config{ host: "localhost", port: 8080, timeout: 30 }
```

#### Anonymous struct literals

Brace-delimited, identifier-keyed literals exist as a distinct form
from map literals — they are for log fields and named bundles:

```glide
log.info("user logged in", { user_id: id, ip: addr })     // ○ log
```

Note the split: `["a": 1]` is a `Map` (bracket family, arbitrary keys),
`{ a: 1 }` is an anonymous struct (brace family, identifier keys).
Different things, different syntax.

---

### 2. Under the Hood

#### Layout ○

In the designed compiler, a struct is a flat record with
compiler-chosen field ordering (to minimise padding) unless the type is
marked for a fixed representation at an FFI or wire boundary. Small
structs pass in registers.

In the interpreter, a struct is a Go map from field name to value plus
the type name — which is why field access has a hash lookup cost today
that it will not have when compiled.

#### Reference or value?

Structs are values, but the *interpreter* stores them behind pointers,
which means a struct assigned to two bindings is currently shared:

```glide
let mut a = Config.new()
let b = a
a.port = 9999
// b.port is also 9999 in the interpreter
```

This matches the recorded "collections are Go pointers" decision and
the "`mut` is a path property" sacrifice. The designed language gives
structs value semantics — assignment copies — with the compiler
choosing whether to copy eagerly or share behind the scenes.

Until the checker era, **use struct update rather than mutation** when
you care about the distinction. `Config{ port: 9999, ..a }` is
unambiguous at both tiers.

#### Why mandatory init has no runtime cost

It is purely a compile-time (today: construction-time) check. The
generated code initialises the fields it was given, in order. There is
no hidden zeroing pass, and there is no "is this initialised?" flag.

Contrast Go, which zeroes every allocation. That is fast — a `memclr`
— but it is not free, and more importantly it is the mechanism that
makes `User{}` legal.

#### The `Default` trait ○

Mandatory initialisation would be intolerable for types like `Mutex`,
`Builder`, or an empty collection, so types can opt into a default:

```glide
impl Default for Counter {
    fn default() -> Counter { Counter{ n: 0 } }
}
```

The important word is **opt in**. `Mutex` and `Builder` still construct
bare because their authors decided a default is meaningful. A `User`
gets no fake instance for free, because nobody wrote one.

That is the whole difference from Go: Go gives every type a default
whether or not the default means anything.

---

### 3. Why This Design?

#### Why no zero values

This is one of the two or three most consequential decisions in the
language, so it is worth the space.

Go's zero values are `null` by another name. Consider:

```go
type User struct {
    ID    string
    Name  string
    Email string
}

func process(u User) { … }

var u User          // legal
process(u)          // legal — and u.ID is ""
```

An "empty" user with a blank ID type-checks and flows. It gets passed
down three layers, gets written to a database, and something breaks a
long way from where the empty value was created. The compiler had every
opportunity to notice and was told not to.

The Go defence is that zero values are often *useful*: a zero `sync.
Mutex` is an unlocked mutex, a zero `bytes.Buffer` is an empty buffer,
a zero `time.Duration` is zero nanoseconds. All true — and all cases
where the type's author would have written a `Default` implementation.

The problem is that Go extends the courtesy to every type
automatically, including the ones where "all fields zero" is a
meaningless state. Glide's rule is: **defaults must be chosen, not
inherited.**

Notice how this composes with the rest of the design:

- No zero values + `Option` means "absent" is spelled `T?` and nothing
  else can impersonate it.
- No zero values + sum types means an enum has no accidental first
  variant. Go's `iota` enums make the zero value silently mean the
  first case, which is why `Color(0)` is `Red` whether or not anyone
  chose red.
- No zero values + mandatory struct patterns (Chapter 10) means adding
  a field breaks exactly the places that claimed to account for
  everything.
- No zero values + JSON decoding (Chapter 33) means "field absent from
  input" is a decode error rather than a silent zero — Go's
  `encoding/json` disease, cured upstream.

One decision, four bug classes.

#### Why field-level `pub` rather than getters

Because a public type with private fields should be the *default
shape*, not a pattern you assemble.

In Java and C#, encapsulation means writing a private field and a
public getter for each. In Go, it means capitalising the field or not —
which works, but couples visibility to naming so that changing
visibility is a rename touching every use site.

Glide's `pub` per field means: declare what is public, and the rest is
module-private. A visibility change is a one-line diff that says "this
became public". No getter ceremony, and no convention to follow.

#### Why struct update instead of mutation

In an immutable-by-default language, "copy with changes" is the primary
way data evolves. `Config{ timeout: 5.s, ..base }` says exactly what
happened and produces a new value; `base` is provably unchanged.

The alternative — mutate a `mut` copy — needs the copy to be explicit
(reference semantics!) and does not read as a transformation.

Note what Glide *declines* here: JavaScript's object-merge spread
(`{...a, ...b}`, silent right-wins). There is exactly one `..base` per
literal, so there is no collision question to answer. Map merging is a
method with a stated collision strategy.

#### Why one `type` keyword for everything

```glide
type NoteId = distinct Int
type Note = struct { … }
type ApiError = NotFound | BadInput(String)
type Handler = fn(Request) -> Response        // ○
```

Four shapes, one keyword. The alternative — separate `struct`, `enum`,
`newtype`, and `typedef` keywords — makes the *declaration form* carry
information that the right-hand side already carries, and it means
changing a type's shape changes its keyword.

That last point matters more than it sounds: a type that starts as
`distinct Int` and later grows into a struct keeps the same declaration
form here, and the diff shows only what actually changed.

---

### 4. Competing Approaches

**Go.** Structs with zero values, capitalisation-based visibility,
struct tags as stringly-typed metadata, embedding for pseudo-
inheritance. Glide keeps the flat-record data model and rejects all
four of those: mandatory init, `pub`, comptime `derive` options instead
of tags, and composition without embedding's method promotion.

**Rust.** Structs with mandatory initialisation (the same rule),
`..base` update syntax (the same syntax), `#[derive(…)]` (the same
idea, implemented with proc macros rather than comptime), field-level
`pub`. Glide's struct chapter is essentially Rust's, minus lifetimes.
Rust also has tuple structs (`struct Meters(f64)`), which Glide covers
with `distinct` (Chapter 15).

**Swift.** Structs with value semantics and memberwise initialisers,
which are synthesised and *do* require every field unless a default is
declared in the type. Very close to Glide's model. Swift's
`let`/`var` on fields is a per-field mutability declaration that Glide
declines — `DESIGN.md` calls per-field mutability "a `Cell`-shaped
rabbit hole".

**Java / C#.** Classes with constructors, getters, setters, and
`null`-initialised fields. Records (Java 16, C# 9) are the recognition
that most classes are structs, and both languages' record syntax
generates the constructor, equality, and printing that Glide's `derive`
will generate.

**Python.** Dictionaries as records, then `namedtuple`, then
`dataclass`, then `pydantic`. The progression is the whole argument for
"maps at the boundary, structs inside" (Chapter 11).

**C.** Structs with designated initialisers (C99) and everything else
zero — the ancestor of Go's model, including the
partially-initialised-and-flowing bug.

---

### 5. Common Mistakes

**Reaching for `Config{}`.** It does not exist. If you want a
"default", write a constructor:

```glide
// Bad — the Go habit
let c = Config{}

// Good
impl Config {
    fn new() -> Config {
        Config{ host: "localhost", port: 8080, timeout: 30 }
    }
}
let c = Config.new()
```

The constructor is better anyway: it has a name, it is a place to
enforce invariants, and the defaults live in one place instead of
being implicit in the field types.

**Forgetting `..` in a struct pattern.** Chapter 10's rule. The error
message tells you exactly what to do.

**Assuming struct assignment copies, today.** In the interpreter it
does not. Use struct update when you mean a copy:

```glide
// Ambiguous across tiers
let mut b = a
b.port = 9999

// Unambiguous
let b = Config{ port: 9999, ..a }
```

**Using a struct where a sum type belongs.** The tell is a `kind`
field, or a cluster of `Option` fields where only some combinations are
valid:

```glide
// Bad — 2^3 representable states, 3 meaningful
type Response = struct {
    loading: Bool
    data: String?
    error: String?
}

// Good — exactly 3 states
type Response = Loading | Loaded(String) | Failed(String)
```

Chapter 13 develops this.

**Making every field `pub` reflexively.** The default is private for a
reason. Make a field public when external code has a legitimate need to
read it, not because it is easier than writing an accessor — and
notice that a field you did not make public is a field you can change
the representation of later.

**Nesting structs deeply and then mutating through the path.**
`a.b.c.d = v` requires `a` to be `mut`, and it means four levels of
your data are reachable for mutation from one binding. That usually
signals the inner type should own a method instead.

---

### 6. Performance Considerations

**Field access is a fixed offset** in the designed compiler — no
lookup, no indirection. In the interpreter it is a hash-map lookup by
field name, which is one of the tree-walker's larger constant costs.

**Struct update copies the whole struct.** `Config{ port: 9, ..base }`
allocates a new `Config` and copies every field. For a small struct
that is a register shuffle. For a struct with fifty fields in a hot
loop, it is not, and that is the case where a `mut` binding is
genuinely the right tool.

Note what struct update does *not* copy: fields that are references
(lists, maps, other structs at the current tier) are shared, not deep
copied. That is what makes the recursive-tree pattern efficient —
untouched subtrees are shared.

**Mandatory initialisation costs nothing.** There is no hidden zeroing
pass and no initialisation flag. Compare Go, which zeroes every
allocation.

**Compiler-chosen field ordering** (○) minimises padding. A struct of
`Bool, Int, Bool` occupies 24 bytes in declaration order under natural
alignment and 16 when reordered. Go does not reorder; Rust does.

**Small structs pass in registers** (○), so returning a
`(Int, Int)`-shaped struct is free. This is why tuples and small
structs have the same cost model.

---

### 7. Best Practices

**Give every type a constructor that enforces its invariants.**

```glide
type Port = struct { n: Int }        // private field

impl Port {
    pub fn new(n: Int) -> Result<Port, String> {
        if n < 1 || n > 65535 {
            return Err("port out of range: {n}")
        }
        Ok(Port{ n: n })
    }
    pub fn value(self) -> Int { self.n }
}
```

Because the field is private, the *only* way to obtain a `Port` is
through `new`, so every `Port` in the program is valid. This is
"parse, don't validate" and it is what private fields plus mandatory
initialisation buy you together.

**Keep fields private unless there is a reason.**

```glide
// Bad — everything public by reflex
type Connection = struct {
    pub socket: Socket
    pub buffer: List<Int>
    pub state: Int
    pub retries: Int
}

// Good — the API is the methods
type Connection = struct {
    socket: Socket
    buffer: List<Int>
    state: State
    retries: Int
}
```

**Prefer struct update to mutation for value-like types.**

```glide
// Good
let updated = Note{ title: new_title, ..note }

// Reserve this for genuine accumulators
let mut builder = Builder.new()
builder.add(x)
```

**Do not model absence with a sentinel field.**

```glide
// Bad
type User = struct {
    pub name: String
    pub email: String        // "" means no email
}

// Good
type User = struct {
    pub name: String
    pub email: String?       // absence is in the type
}
```

**Flatten aggressively; nest only where the nesting is real.** A struct
that exists solely to group three fields that are always used together
with their parent is noise. A struct that has its own invariants,
methods, or lifetime is real.

**Do not put a `kind`, `type`, or `tag` field in a struct.** That is a
sum type in a struct costume, and the next chapter is about why.

---

### 8. Examples

**A configuration type with a constructor and update:**

```glide-run
type Config = struct {
    pub host: String
    pub port: Int
    timeout: Int
}

impl Config {
    fn new() -> Config {
        Config{ host: "localhost", port: 8080, timeout: 30 }
    }

    fn describe(self) -> String {
        "{self.host}:{self.port} t={self.timeout}"
    }
}

fn main() {
    let base = Config.new()
    println(base.describe())

    let prod = Config{ host: "prod.example", ..base }
    println(prod.describe())

    println(base.describe())     // base is untouched
}
```

```
localhost:8080 t=30
prod.example:8080 t=30
localhost:8080 t=30
```

Note `timeout` is private, so it cannot be set from outside the
module — but `..base` still carries it, because struct update happens
where the literal is written. Inside the module, that works; outside
it, `Config{ host: …, ..base }` would be an error on the private field.
That is the encapsulation story working.

**Parse, don't validate — the pattern private fields exist for:**

```glide-run
type Email = struct { raw: String }

impl Email {
    pub fn parse(s: String) -> Email? {
        let s = s.trim()
        if !s.contains("@") || s.starts_with("@") || s.ends_with("@") {
            return None
        }
        Some(Email{ raw: s })
    }

    pub fn value(self) -> String { self.raw }

    pub fn domain(self) -> String {
        let parts = self.raw.split("@")
        parts[parts.len() - 1]
    }
}

fn send(to: Email, body: String) {
    println("-> {to.value()} ({to.domain()}): {body}")
}

fn main() {
    let Some(addr) = Email.parse("  ada@example.com ") else {
        eprintln("bad address")
        return
    }
    send(addr, "hello")

    match Email.parse("not-an-email") {
        Some(e) => println("parsed {e.value()}")
        None    => println("rejected")
    }
}
```

```
-> ada@example.com (example.com): hello
rejected
```

The key property: `send` takes an `Email`, not a `String`. It does not
validate, because it *cannot* receive an invalid one — the only
constructor is `parse`, and `parse` returns an `Option`. Validation
happened once, at the boundary, and the type carries the proof
forward.

This is the single most valuable pattern in the chapter and it needs
three things Glide has together: private fields, mandatory
initialisation, and `Option`.

**Bad versus good: the flag soup**

```glide
// Bad — four booleans, sixteen states, four meaningful
type Job = struct {
    pub id: Int
    pub queued: Bool
    pub running: Bool
    pub done: Bool
    pub failed: Bool
    pub error: String
}

fn status(j: Job) -> String {
    if j.failed { "failed: {j.error}" }
    else if j.done { "done" }
    else if j.running { "running" }
    else { "queued" }
}
```

What is a `Job` with `running: true` and `done: true`? What is one with
`failed: false` and a non-empty `error`? The type permits both, so
somewhere in the codebase something will produce one.

```glide
// Good
type JobState =
    Queued
    | Running{ since: Instant }
    | Done{ took: Duration }
    | Failed{ reason: String }

type Job = struct {
    pub id: Int
    pub state: JobState
}

fn status(j: Job) -> String {
    match j.state {
        Queued          => "queued"
        Running{ .. }   => "running"
        Done{ took }    => "done in {took}"
        Failed{ reason } => "failed: {reason}"
    }
}
```

Four states, exactly. Each carries precisely the data that state needs
and no other — `took` exists only for `Done`, `reason` only for
`Failed`. And the `match` is exhaustive, so adding a `Cancelled` state
produces a compile error here rather than a silent fall into `"queued"`.

Chapter 13 is entirely about this transformation.

---

### 9. Summary & Exercises

**Summary**

- `type Name = struct { … }` — one `type` keyword declares every kind
  of type in Glide.
- **There are no zero values.** Every struct literal accounts for every
  field. `Config{}` is unwritable, and `missing field "y" (no zero
  values)` is the error.
- Fields are **private by default**; `pub` marks them visible outside
  the module. A public type with private fields is the natural
  encapsulation shape.
- **Struct update** — `Config{ port: 9, ..base }` — is copy-with-
  changes. `..base` comes last, exactly one per literal, and the base
  is untouched. This is how data evolves in an immutable-first
  language.
- Methods live in `impl` blocks, separate from the declaration.
  `Type.new()` (no `self`) is an associated function and the
  constructor idiom.
- Struct patterns need `..` for a partial match, and can nest patterns
  into fields.
- Types opt into a default via the `Default` trait (○), so `Mutex` and
  `Builder` still construct bare while domain types get no fake
  instances.
- `derive(Json, Row, Debug)` is ○ and is Chapter 36. Structural Debug
  output works today without it.
- Interpreter caveat: structs are shared behind pointers, so assignment
  does not copy yet. Prefer struct update where the distinction
  matters.

**Exercises**

1. **Hunt the empty struct.** In a Go codebase, grep for `T{}` and for
   `var x T` where `T` is a domain type (not a `Mutex`, `Buffer`, or
   `WaitGroup`). For each, decide whether the zero value is meaningful
   or merely tolerated. Then decide which of those types would need a
   `Default` implementation in Glide and which would not — the ratio is
   the value of the decision.

2. **Apply parse-don't-validate.** Pick a `String` that flows through
   your current codebase carrying an implicit format — a user ID, a
   currency code, a slug, an ISO date. Write the Glide type with a
   private field and a `parse` returning `T?`. Then count how many
   validation checks disappear downstream. That count is the payoff.

3. **Kill a flag soup.** Find a struct with three or more independent
   booleans, or with several `Option` fields where only some
   combinations are valid. Write down the number of representable
   states and the number of meaningful ones. Then model it as a sum
   type and check the numbers match. If they do not, you have found
   either a state you forgot or a state that was never real.
