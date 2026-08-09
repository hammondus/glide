# Chapter 15: Distinct Types

`DESIGN.md` calls `distinct` "the cheapest safety-per-character in the
document". Ten characters of declaration kill an entire class of bugs:
the ones where you pass a `UserId` where an `OrderId` was expected, or
metres where seconds belong, and everything type-checks and quietly
corrupts data.

```glide
type NoteId = distinct Int
```

That is the feature. This is a short chapter because there is not much
to it — the interesting part is knowing when to reach for it, and
understanding one deliberately harsh rule: **no inherited operators**.

Everything here is ✓, except that a distinct value cannot yet be a map
key.

---

### 1. Basic Usage

#### Declaring and constructing

```glide
type NoteId = distinct Int
type OrderId = distinct Int
```

Both are stored as an `Int`. Neither is an `Int`, and neither is the
other.

Construction is **explicit** and checks the base type:

```glide
let n = NoteId(7)
println(n.value())      // 7
```

```glide
let n = NoteId("x")
```

```
error: line 2: NoteId wraps Int, got String (no implicit conversion)
```

`.value()` unwraps back to the base type. The pattern form
destructures:

```glide
let NoteId(raw) = n
println(raw)            // 7
```

#### No implicit conversion, in either direction

```glide
fn get_note(id: NoteId) -> Note? { … }

get_note(7)             // expected NoteId, found Int
get_note(NoteId(7))     // right
get_note(order_id)      // expected NoteId, found OrderId
```

Both of those are compile errors, reported before the program runs:

```
app.gld:9:22: expected NoteId, found Int
 9 |     println(get_note(7))
   |                      ^
app.gld:10:22: expected NoteId, found OrderId
 10 |     println(get_note(order_id))
    |                      ^^^^^^^^
```

The second is the one that pays for the feature. `NoteId` and `OrderId`
are both `Int` underneath, and every language without `distinct` will
let you pass one where the other belongs.

#### No inherited operators

This is the rule that surprises people:

```glide
println(NoteId(1) + 1)
```

```
error: line 2: operator + not defined for NoteId and Int
```

Not "no implicit conversion so convert first". The operator **does not
exist** for the type at all. `NoteId(1) + NoteId(2)` is equally an
error.

The reasoning is one sentence in `DESIGN.md`: *an id is not a
quantity.* Adding two note IDs is meaningless, so the language does not
provide it. If your distinct type *is* a quantity — `Metres`,
`Cents` — you implement the operator traits deliberately (○,
Chapter 36's territory).

#### Equality is within the type only

```glide
println(NoteId(7) == NoteId(7))      // true
println(NoteId(1) == OrderId(1))     // false
```

Two different distinct types over the same base are never equal, even
with the same underlying value.

#### Methods work like any user type

```glide-run
type NoteId = distinct Int

impl NoteId {
    fn describe(self) -> String { "note #{self.value()}" }
}

fn main() {
    let n = NoteId(7)
    println(n.describe())      // note #7
    println("{n:?}")           // NoteId(7)
}
```

An `impl` block on a distinct type behaves exactly as it does on a
struct. This is what turns `distinct` from "a tag" into "a real type
with behaviour".

#### Codecs unwrap at the boundary

JSON encoding and SQL parameter binding both unwrap a distinct value
transparently:

```glide
db.exec("insert into notes (id) values (:id)", ["id": NoteId(7)])
```

binds `7`. A codec's conversion is the explicit kind — you asked to
serialise, so the wrapper comes off at the wire.

---

### 2. Under the Hood

#### Representation

**Zero cost.** A `distinct Int` *is* an `Int` at runtime in the
designed compiler — same layout, same registers, same size. The
distinction exists entirely in the type checker and evaporates at code
generation. This is Nim's model, from which the feature is borrowed.

In the interpreter it is a small wrapper record (`DistinctV{Type, V}`),
so today it costs one allocation. That is a tier artifact.

#### How "no inherited operators" is implemented

Elegantly, and it is worth mentioning because it demonstrates something
about the design: the binary-operator dispatch simply does not have a
case for distinct values. **Falling through *is* the semantics.** No
code was written to forbid arithmetic on a `NoteId`; the absence of
code is the rule.

`glide/DESIGN-DECISIONS.md` records this explicitly, and it is a small
example of a general principle — the best way to prevent something is
for it to be unrepresentable rather than checked.

#### Construction is checked statically

```
app.gld:3:20: NoteId wraps Int, got String (no implicit conversion)
 3 |     println(NoteId("x"))
   |                    ^^^
```

The evaluator keeps its own base-type check as a backstop, but the
diagnostic you will actually see comes from the checker, before
anything runs.

#### Not yet a map key

A distinct value cannot be used as a `Map` key today — it needs
hashable boxing, which will be added when a program wants it. Use
`.value()` as the key for now, and accept that the map's key type is
then the base type rather than the distinct one.

---

### 3. Why This Design?

#### The bug it prevents

```glide
fn transfer(from: Int, to: Int, amount: Int) -> Result<(), Error>
```

Three `Int`s. Transposing the first two is a silent, type-correct,
money-moving bug. Named arguments (Chapter 7) help at the call site:

```glide
transfer(from: src, to: dst, amount: 500)
```

but they do not help when the values are already in variables with
plausible names, and they do not help *inside* the function.

```glide
type AccountId = distinct Int
type Cents = distinct Int

fn transfer(from: AccountId, to: AccountId, amount: Cents) -> Result<(), Error>
```

Now `amount` cannot be transposed with either account, because the
types differ. `from` and `to` are still transposable — same type — and
that is what named arguments are for. **The two features cover
different halves of the same problem**, which is why the language has
both.

The general shape: any time a function takes two or more parameters of
the same primitive type, you have a transposition hazard. Distinct
types remove the ones where the values mean genuinely different things;
named arguments handle the ones where they do not.

#### Why no inherited operators

This is the contentious choice, and the alternative is defensible: Nim
lets you opt into borrowing operators, Haskell's `newtype` plus
`deriving` does something similar.

The argument for the harsh default: **most distinct types are not
quantities.** IDs, tokens, keys, handles, usernames, paths — arithmetic
on any of them is a bug. If operators were inherited by default, the
type would prevent transposition and permit nonsense, which is half a
feature.

For the minority that *are* quantities — `Cents`, `Metres`,
`Milliseconds` — you implement `Add` and friends deliberately (○), and
that implementation is where you decide the interesting questions:
should `Metres + Metres` be `Metres`? (Yes.) Should `Metres * Metres`
be `Metres`? (No — it is `SquareMetres`, and if you care about that you
are building a units library.)

Making the quantity case opt-in means the interesting decisions get
made rather than inherited.

#### Why `distinct` and not a one-field struct

You could write:

```glide
type NoteId = struct { value: Int }
```

and get most of the same benefit. `distinct` is better for three
reasons:

1. **Zero cost is guaranteed.** A struct is a struct; the compiler may
   or may not unbox it. `distinct` is defined to have the base type's
   representation.
2. **Construction and destructuring are shorter.** `NoteId(7)` and
   `let NoteId(n) = id` versus `NoteId{ value: 7 }` and
   `let NoteId{ value } = id`.
3. **It says what it means.** `distinct Int` documents "this is an
   integer with a name"; a one-field struct documents "this is a record
   that happens to have one field", which invites someone to add a
   second.

#### Why the base type is visible in the declaration

`type NoteId = distinct Int` tells the reader the representation. That
matters at boundaries: you know a `NoteId` serialises as a number, fits
in a database integer column, and costs eight bytes.

Contrast an opaque type where the representation is hidden. That is a
reasonable thing to want, and it is what a struct with a private field
gives you (Chapter 12). `distinct` is the transparent version, and the
choice between them is "do callers need to know the representation?"

---

### 4. Competing Approaches

**Nim.** `type UserId = distinct int` — the direct source. Nim allows
borrowing operators with `{.borrow.}`, which Glide declines by default.

**Haskell.** `newtype UserId = UserId Int` — zero-cost, no inherited
instances unless you derive them. Essentially identical semantics.
Haskell's `GeneralizedNewtypeDeriving` is the opt-in operator
borrowing.

**Rust.** The "newtype pattern": `struct UserId(u64)`, a tuple struct.
Zero cost, no inherited operators (you implement `Add` if you want it),
and idiomatic. Glide's `distinct` is the same thing with a keyword
instead of a convention, which mostly matters for discoverability.

**Go.** `type UserId int64` — a *defined type*. Close, and weaker in
two ways: conversion is implicit-looking (`UserId(x)` and `int64(y)`
both work freely, so the barrier is thin), and operators **are**
inherited, so `UserId(1) + UserId(2)` compiles. Go's version prevents
accidental *assignment* between types but not accidental *arithmetic*.

**TypeScript.** Branded types — `type UserId = string & { __brand:
"UserId" }` — a community hack around a structural type system. It
works and it is ugly, which is evidence that the need is real.

**C / C++.** `typedef` is a pure alias and prevents nothing.
`enum class` and strong-typedef libraries (Boost.Units) exist because
the need is real; C++'s `std::chrono` is the best mainstream example of
a units library built on the idea.

**Java / C#.** Value classes and records, which cost an allocation
unless the runtime unboxes them. Java's Project Valhalla exists partly
to make this free.

---

### 5. Common Mistakes

**Expecting arithmetic to work.**

```glide
// Bad
let next = NoteId(1) + 1

// Good — the operation is on the base type
let next = NoteId(NoteId(1).value() + 1)
```

If you find yourself writing the second form often, the type is
probably a quantity and wants operator implementations (○) — or it
should not be distinct at all.

**Using `distinct` for a type that is genuinely arithmetic.**

```glide
// Questionable today — every operation needs .value()
type Cents = distinct Int

// Fine today
type Cents = Int          // with a comment, until operator traits land
```

Until operator traits are implementable, a heavily-arithmetic distinct
type is painful. Weigh the transposition safety against the unwrapping
noise.

**Forgetting the wrapper at a construction site.** `expected NoteId,
found Int` — a compile error. Write `NoteId(row["id"])`.

**Using it as a map key.** Not supported yet. Use `.value()`.

**Wrapping something that has no invariant and no confusion risk.**

```glide
// Bad — nothing is prevented; every use site pays
type Count = distinct Int
type Index = distinct Int
type Length = distinct Int
```

If three types are freely interchangeable in the domain, making them
distinct produces conversion noise for no safety. Reserve it for values
that genuinely must not mix.

**Expecting Debug output to hide the wrapper.** `"{n:?}"` prints
`NoteId(7)`, which is correct — Debug shows structure. Display (○)
would print `7`.

---

### 6. Performance Considerations

**Zero cost in the designed compiler.** A `distinct Int` is an `Int`:
same size, same registers, same code generated. There is no wrapper, no
allocation, and no indirection.

**One small allocation per value in the interpreter**, because the
tree-walker boxes it as a `DistinctV`. In a hot loop over millions of
IDs this is visible today and will not be later.

**Codecs unwrap without copying.** JSON encoding and SQL binding read
through the wrapper.

**No cache or layout effect.** Because the representation is
unchanged, a struct field of type `NoteId` occupies exactly what an
`Int` field would, and a `List<NoteId>` has the same layout as a
`List<Int>`.

This is the whole appeal: safety with a genuinely zero runtime cost,
which is rare. Most safety features cost something.

---

### 7. Best Practices

**Wrap identifiers, always.**

```glide
// Good — the six most valuable distinct types in a typical service
type UserId = distinct Int
type OrderId = distinct Int
type SessionToken = distinct String
type TenantId = distinct Int
type Sha256 = distinct String
type Slug = distinct String
```

Every one of those is a value that is meaningless to compute with and
disastrous to transpose. They are the sweet spot.

**Wrap units where confusion is plausible.**

```glide
type Millis = distinct Int
type Bytes = distinct Int
```

Note that Glide's `Duration` type (Chapter 27) exists precisely so you
do not hand-roll `Millis` — `500.ms` is a real `Duration`. Use the
stdlib type when there is one.

**Do not wrap where the domain freely mixes.** If your code genuinely
adds counts to indexes to lengths, distinct types will fight you.

**Put the invariant in an associated function.** A distinct type can
have a validating constructor, exactly like a struct:

```glide
type Slug = distinct String

impl Slug {
    pub fn parse(s: String) -> Slug? {
        let s = s.trim().to_lower()
        if s == "" || s.contains(" ") {
            return None
        }
        Some(Slug(s))
    }
}
```

The difference from Chapter 12's `Email` struct is that the
representation is public (`.value()` works), which is often what you
want for an identifier that has to reach a database.

**Name the type after the domain concept, not the representation.**
`UserId`, not `UserIdInt`. The `distinct Int` on the declaration line
already says the representation.

**Convert at the boundary, once.**

```glide
// Good
fn handle(req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(n) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let id = NoteId(n)          // wrapped once, here
    …
}
```

Everything downstream takes a `NoteId` and never sees a bare `Int`.

---

### 8. Examples

**The transposition bug, prevented:**

```glide-run
type AccountId = distinct Int
type Cents = distinct Int

type Ledger = struct { pub entries: List<String> }

fn transfer(l: Ledger, from: AccountId, to: AccountId, amount: Cents) -> Ledger {
    let entry = "{from.value()} -> {to.value()}: {amount.value()}c"
    let mut es = l.entries
    es.push(entry)
    Ledger{ entries: es }
}

fn main() {
    let src = AccountId(1001)
    let dst = AccountId(2002)
    let amt = Cents(500)

    let l = transfer(Ledger{ entries: [] }, src, dst, amt)
    println("{l.entries:?}")

    // A compile error, with both arguments flagged:
    //     transfer(l, src, amt, dst)
    // expected AccountId, found Cents / expected Cents, found AccountId
}
```

```
["1001 -> 2002: 500c"]
```

The commented line is the whole point. With three bare `Int`s it
compiles and moves the wrong money.

**A validated identifier:**

```glide-run
type Slug = distinct String

impl Slug {
    pub fn parse(s: String) -> Slug? {
        let s = s.trim().to_lower()
        if s == "" || s.contains(" ") || s.contains("/") {
            return None
        }
        Some(Slug(s))
    }

    pub fn url(self) -> String { "/posts/{self.value()}" }
}

fn main() {
    for raw in ["Hello-World", "  Draft  ", "not a slug", ""] {
        match Slug.parse(raw) {
            Some(s) => println("{raw:14} -> {s.url()}")
            None    => println("{raw:14} -> rejected")
        }
    }
}
```

```
   Hello-World -> /posts/hello-world
       Draft   -> /posts/draft
    not a slug -> rejected
               -> rejected
```

Note how `url()` needs no validation: a `Slug` that exists is a `Slug`
that parsed.

**Bad versus good on the same service:**

```glide
// Bad — everything is an Int and a String
fn create_note(db: Db, user: Int, tenant: Int, title: String, slug: String)
    -> Result<Int, Error>
```

Five parameters, four transposition hazards, and the return value is an
`Int` that callers will pass to `get_note(id: Int)` — which also
accepts a tenant by mistake.

```glide
// Good
type UserId = distinct Int
type TenantId = distinct Int
type NoteId = distinct Int
type Slug = distinct String

fn create_note(db: Db,
               user: UserId,
               tenant: TenantId,
               title: String,
               slug: Slug) -> Result<NoteId, Error>
```

Four declarations, one per domain concept, and every transposition in
that signature becomes a compile error. `title` remains a bare `String`
because it is genuinely free text — not everything needs wrapping, and
wrapping things that do not need it is the mistake in the other
direction.

---

### 9. Summary & Exercises

**Summary**

- `type NoteId = distinct Int` creates a new type with the base type's
  representation and **no implicit conversion** in either direction.
- Construction is explicit — `NoteId(7)` — and checks the base type.
  `.value()` unwraps; `let NoteId(n) = id` destructures.
- **No inherited operators.** `NoteId(1) + 1` is an error and so is
  `NoteId(1) + NoteId(2)`, because an id is not a quantity. Types that
  genuinely are quantities implement the operator traits deliberately
  (○).
- `==` compares within the same distinct type only. Two distinct types
  over the same base are never equal.
- `impl` blocks work exactly as on any user type, so a distinct type
  can carry methods and a validating constructor.
- Codecs (JSON, SQL) unwrap transparently at the boundary.
- **Zero cost** in the designed compiler — same layout as the base
  type. One wrapper allocation in the interpreter.
- `distinct` and named arguments cover different halves of the
  transposition problem: distinct types stop parameters of *different*
  meanings mixing; named arguments stop parameters of the *same* type
  swapping.
- Not yet a map key.

**Exercises**

1. **Count your transposition hazards.** Take the ten most-called
   functions in a service you maintain. For each, count the parameters
   that share a primitive type. Every such pair is a silent bug waiting
   for a copy-paste. Then decide, for each, whether the fix is a
   distinct type, a named argument, or a restructured signature.

2. **Build a units type.** Write `Cents` as a distinct type with a
   validating constructor and methods for the operations you actually
   need (`plus`, `times_int`, `format`). Then write the same thing as a
   bare `Int` with a comment. Use both for an hour of realistic code
   and decide honestly which you would ship *today*, given that
   operator traits are ○. Note that this answer changes when they land.

3. **Find the wrapping that does not pay.** Deliberately over-apply
   `distinct` — wrap every integer in a small program, including
   counts, indexes, and lengths. Count the `.value()` calls you had to
   write. That number is the cost side of the trade, and it tells you
   where the line is.
