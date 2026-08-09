# Chapter 14: Option — There Is No Null

Tony Hoare called null references his billion-dollar mistake. Every
mainstream C-lineage language has one: a value that lies about its
type. Any reference *might* secretly be it, the compiler cannot tell
you which, and you find out in production.

Glide removes the lie by moving "might be absent" out of the value
domain and into the **type**. A plain `T` is always a real `T`. A value
that might be absent is a `T?`, and the compiler will not let you use a
`T?` where a `T` is required.

The point is not the extra question mark. It is that **presence becomes
visible in signatures.**

Everything here is ✓, with one significant interpreter caveat — the
unboxing wart — flagged in Under the Hood.

---

### 1. Basic Usage

#### The type

```glide
fn find_user(id: Int) -> User?      // absence is possible
fn owner() -> User                  // absence is not
```

`T?` is sugar for `Option<T>`, an ordinary two-variant sum type
(Chapter 13):

```glide
type Option<T> = Some(T) | None     // conceptually
```

`Some(v)` wraps a value. `None` is the absent case, and it is a
literal — not a call.

You cannot use a `T?` where a `T` is expected. You must handle the
empty case first, and there are exactly three ways to do it.

#### Way 1: `??` — supply a default

```glide
let n = counts[word] ?? 0
let host = config["host"] ?? "localhost"
```

The **option-coalescing operator**, read as "or else". The right side
is evaluated only when the left is absent.

This is C#, JavaScript, and Swift's null-coalescing operator,
retargeted at `Option` because there is no null to coalesce. That
retargeting matters: JavaScript's `||` breaks on `0` and `""` because
it tests *falsiness*; `??` tests *absence* and nothing else.

`??` binds **loosest of all binary operators**, which is almost always
what you want and occasionally a trap:

```glide
counts[word] = (counts[word] ?? 0) + 1     // parentheses required
```

Without them, `counts[word] ?? (0 + 1)`.

`??` also works on a `Result`, unwrapping `Ok` and taking the default
on `Err` — deliberately discarding the error (Chapter 19).

#### Way 2: `if let` — unwrap into a scope

```glide
if let user = find_user(id) {
    println(user.name)          // user is a User here, not a User?
} else {
    println("no such user")     // user does not exist here
}
```

Note there is **no `Some(...)` wrapper in the source**. The pattern
binds the inner value directly. Swift users will recognise this
exactly; it is deliberate ergonomics — Option handling should not
require ceremony at every site.

Chains work in both directions:

```glide
if let cached = cache[key] {
    serve(cached)
} else if let stored = db_lookup(key) {
    serve(stored)
} else {
    serve(compute(key))
}
```

#### Way 3: `let … else` — unwrap or bail

```glide
let config = load_config(path) else {
    eprintln("no config at {path}")
    os.exit(1)
}
// config is a real Config from here on
```

The else block must **diverge** — return, exit, break, continue, or
panic — because past that line the binding has to exist. The compiler
enforces the divergence, and that enforcement is what makes the flat
style *safe* rather than optimistic.

```glide
let Some(v) = m["z"] else {
    println("missing")
}
```

```
error: line 3: the else block of `let … else` must diverge (return or exit),
       but it ran off the end
```

This is Swift's `guard let`, and it is the construct that keeps the
happy path unindented.

#### Choosing between them

| Situation | Use |
|---|---|
| A default value is meaningful | `x ?? default` |
| Both cases have real code | `if let … else` |
| Absence aborts the function | `let … else` |
| More than two outcomes | `match` |

#### `match` on an Option

When you want to be explicit, or when the arms need guards:

```glide
match find_user(id) {
    Some(u) if u.active => greet(u)
    Some(u)             => reactivate(u)
    None                => signup()
}
```

#### Where Options come from

The two you meet constantly:

```glide
let v = m[key]                  // Map indexing: V?
let n = s.parse_int()           // Parsing: Int?
```

Map indexing returns an Option because the key might be absent, and
there is no zero value to fall back on. `parse_int` returns an Option
because "not a number" is an ordinary outcome, not an error worth a
`Result`.

And from your own signatures, which is the important case:

```glide
fn Email.parse(s: String) -> Email?
fn first_admin(users: List<User>) -> User?
fn env(name: String) -> String?
```

#### Options in structs

```glide
type User = struct {
    pub name: String
    pub email: String?          // may genuinely have no email
}
```

Combined with mandatory initialisation (Chapter 12), this is precise:
you must supply `email`, and supplying `None` is a *statement* rather
than an oversight.

```glide
let u = User{ name: "ada", email: None }
```

Contrast Go, where the field is `""` by default and there is no way to
tell "no email" from "empty string" from "nobody filled this in".

#### `?` does not work on Options

```glide
let name = find_user(id)?.name      // not adopted
```

The `?` operator propagates a `Result`'s `Err` (Chapter 19). It is
**not** overloaded onto `Option`. Use `??`, `if let`, or `let … else`.

---

### 2. Under the Hood

#### Representation ○

In the designed compiler, `Option<T>` is a two-variant sum type with
**niche optimisation**: when `T` is a reference type, the `None` case
uses a null pointer as its tag, so `T?` is exactly as large as `T` and
costs nothing.

For value types like `Int`, `Option<Int>` needs a tag word, so it is
larger than `Int`.

The key thing this buys: the *representation* is the same nullable
pointer that Go and Java use — the difference is entirely in what the
type system lets you do with it. You get null's efficiency and none of
its danger.

#### The unboxing wart

The interpreter represents `T?` **unboxed**: `Some` is the identity
function and `None` is a distinct sentinel. `glide/DESIGN-DECISIONS.md`
records the reason — without static types, the interpreter cannot see
where `T -> T?` coercion should happen, so it makes the coercion a
no-op.

Three consequences today:

- `Some(p)` patterns match any non-`None` value.
- **`Option<Option<T>>` is unrepresentable.** `Some(None)` collapses
  to `None`.
- A channel receive returns `Option<T>` with `None` meaning
  closed-and-drained, so a *sent* `None` reads as end-of-stream
  (Chapter 27).

The checker era must box. Do not build anything on the current
behaviour, and if you find yourself needing a three-state value, model
it explicitly:

```glide
type Field<T> = Missing | Null | Present(T)
```

#### Why `if let` panics on a non-`None` mismatch

`if let Some(x) = e` means "unwrap `e`; if absent, take the else
branch". It does not mean "if `e` happens to have this shape".

So a scrutinee that is neither `None` nor matchable by the pattern is a
*bug*, and it panics. Only a `None` routes to `else`.

The rule keeps `if let` from quietly becoming a second, weaker `match`
with no exhaustiveness story. Variant dispatch is `match`'s job.

#### `??` is lazy

The right-hand side is evaluated only when the left is absent:

```glide
let cfg = cache[key] ?? expensive_default()     // called only on a miss
```

This matters for the `Result` case too: `risky() ?? fallback()` does
not compute `fallback()` on success.

---

### 3. Why This Design?

#### What null actually costs

It is worth being precise about the failure, because "null is bad" is
too vague to design against.

The problem is not that absence exists — absence is a real thing that
programs must model. The problem is that in a language with null,
**every reference type is secretly a sum type with an extra variant
that the type system does not mention.** `String` in Java means "a
string, or null". `*User` in Go means "a user, or nil". The type
promises one thing and delivers two.

Three consequences follow:

1. **You cannot express "definitely present".** There is no way to
   write a Java method signature that means "this will never be null"
   — only a convention, a comment, or an annotation processor.
2. **You cannot express "possibly absent" either.** Since everything is
   possibly-null, marking one thing as possibly-null says nothing.
3. **The check is not enforced.** The compiler knows a value might be
   null and lets you dereference it anyway.

Making absence a type fixes all three simultaneously. `fn find(id:
UserId) -> User?` says absence is possible; `fn owner() -> User` says
it is not, and you do not defensively check. That second half is the
part people underestimate: **the value of Option is as much in the `T`
signatures as in the `T?` ones.**

#### Why `??` and not truthiness

JavaScript's `||` is the cautionary tale:

```javascript
const port = config.port || 8080;      // 0 becomes 8080. Bug.
const name = user.name || "anonymous"; // "" becomes "anonymous". Maybe a bug.
```

`||` tests falsiness, so it conflates "absent" with "zero" and "empty".
JavaScript eventually added `??` for exactly this reason, and Glide
starts there.

Note that this is the same argument as the no-truthiness decision in
Chapter 5, arriving from a different direction. Truthiness conflates
absent with empty; `Option` exists to distinguish them; so a language
with `Option` cannot afford truthiness.

#### Why three constructs rather than one

Because the three cases genuinely differ in what should happen, and a
single construct would make two of them awkward.

`??` is for when a default is meaningful — the absent case has a
sensible value and no code. Forcing that through `match` would be four
lines for one decision.

`if let` is for when both cases have code. Forcing that through `??`
would require the default branch to be an expression, which is often
not natural.

`let … else` is for when absence aborts. Forcing that through `if let`
means indenting the entire rest of the function inside the `if`, which
is exactly the pyramid that guard clauses exist to prevent.

They are three shapes of one idea, and each is the shortest correct
spelling of its case. That is not "three ways to do it" — it is one way
to do each of three things.

#### Why `?` was not overloaded onto Option

Rust allows `?` on `Option`, propagating `None` out of a function
returning `Option`. Glide declines it.

The reason is that `?` in Glide has a specific meaning — *propagate the
error to my caller* — and it carries error-type conversion machinery
with it (Chapter 19). Making the same glyph mean "propagate absence"
gives one operator two behaviours resolved by the operand's type, which
is exactly the kind of context-dependent meaning the language avoids
elsewhere (see: Go's `:=`).

The cost is that Option-heavy chains are slightly longer here than in
Rust. `DESIGN.md` records the decision as "not adopted" rather than
"declined forever".

#### Why absence is not an error

A recurring modelling question: should a lookup that finds nothing
return `T?` or `Result<T, NotFound>`?

The rule of thumb: **`Option` when absence is an ordinary outcome,
`Result` when it is a failure the caller might want to report.**

`map[key]` returns an Option, because a missing key is normal.
`fs.read_string(path)` returns a Result, because a missing file is a
failure with a cause worth reporting ("permission denied" versus "no
such file"). `s.parse_int()` returns an Option, because "not a number"
has exactly one cause and no detail worth carrying.

The test: does the absent case have information in it? If yes, that
information wants a `Result`.

---

### 4. Competing Approaches

**Go.** `nil` for pointers, maps, slices, channels, funcs, and
interfaces, with different behaviour for each (reading a nil map is
fine, writing panics; a nil slice appends fine; a nil interface holding
a nil pointer is *not* nil — the famous typed-nil trap). Zero values
substitute for Option in many places, which is why a `map[string]int`
read cannot distinguish absent from zero. Go's `comma-ok` form
(`v, ok := m[k]`) is Option in disguise, without composability.

**Rust.** `Option<T>` with niche optimisation, `?` on Option,
`unwrap_or`, `map`, `and_then`, and a large combinator surface. Glide
takes the type and the ergonomics and declines the combinator zoo — the
three constructs plus `match` cover it, and `DESIGN.md` prefers three
readable spellings over twenty methods.

**Swift.** `T?`, `if let`, `guard let`, `??`, optional chaining
(`a?.b?.c`). Glide takes the first four directly, including the
no-`Some`-wrapper ergonomic. Optional chaining is not adopted; it is
convenient and it hides how many things could be absent in one
expression.

**Kotlin.** `T?` with smart casts (after `if (x != null)`, `x` is
non-null in that branch), `?:` elvis, `?.` chaining, and `!!` to
assert. `!!` is the escape hatch, and its existence is instructive:
retrofitting nullability onto a language with a null-having runtime
means you need a way to lie. Glide has no runtime null, so no `!!`.

**Java.** `Optional<T>` as a library type added in Java 8, which
cannot be used for fields idiomatically, boxes, and coexists with null
— so a `Optional<String>` can itself be null. This is what happens when
you add the type without removing the value.

**Haskell / ML.** `Maybe a` / `option`, the origin. Monadic
composition via `>>=` and `do` notation, which is more powerful and
requires understanding monads. Glide's three constructs are the
pragmatic subset.

**C#.** Nullable reference types (C# 8) — an opt-in static analysis
retrofitted onto a null-having runtime, with `?` annotations and a
`!` suppression operator. Same shape as Kotlin's answer, same
conclusion: it helps a lot and it cannot be sound.

---

### 5. Common Mistakes

**Forgetting the parentheses with `??`.**

```glide
// Bad — counts[word] ?? (0 + 1)
counts[word] = counts[word] ?? 0 + 1

// Good
counts[word] = (counts[word] ?? 0) + 1
```

**Using `??` to paper over a real error.**

```glide
// Bad — the error is discarded silently
let cfg = load_config(path) ?? Config.default()

// Better, if you meant it
let cfg = match load_config(path) {
    Ok(c)  => c
    Err(e) => {
        eprintln("config failed ({e}); using defaults")
        Config.default()
    }
}
```

`??` on a `Result` discards the error *deliberately*, which is fine
when you genuinely do not care. If you would want to know, do not use
`??`.

**Falling off the end of a `let … else` block.** The block must
diverge. If you have a value to supply, you wanted `??` or `if let`.

**Using `if let` for variant dispatch.** It unwraps. A non-`None`
value that fails the pattern panics. Use `match`.

**Nesting `if let` three deep.**

```glide
// Bad
if let a = find_a() {
    if let b = find_b(a) {
        if let c = find_c(b) {
            use(c)
        }
    }
}

// Good
let Some(a) = find_a() else { return }
let Some(b) = find_b(a) else { return }
let Some(c) = find_c(b) else { return }
use(c)
```

`let … else` is the flattening tool. This is the single most common
readability improvement in Option-heavy code.

**Making everything optional out of caution.**

```glide
// Bad — now every caller must handle absence that cannot happen
type Order = struct {
    pub id: OrderId?
    pub items: List<Item>?
    pub total: Int?
}

// Good
type Order = struct {
    pub id: OrderId
    pub items: List<Item>
    pub total: Int
    pub note: String?          // genuinely optional
}
```

Every `?` you add is work for every caller, forever. Add them where
absence is real.

**Confusing "empty" with "absent".**

```glide
// These mean different things — pick deliberately
pub tags: List<String>      // may be empty; always present
pub tags: List<String>?     // may be absent entirely
```

For a list, "absent" and "empty" are usually the same thing and the
`?` is noise. For a `String`, they often differ (no middle name versus
a middle name that is the empty string).

**Relying on `Option<Option<T>>`.** It does not exist at this tier.

**Reaching for `?` on an Option.** Not adopted. The error will be
confusing because `?` will try to treat it as a `Result`.

---

### 6. Performance Considerations

**`T?` for reference types is free** (○, niche optimisation) — the same
nullable pointer as Go's `*T`, with the type system doing the work at
compile time. There is no wrapper object and no allocation.

**`T?` for value types costs a tag word.** `Option<Int>` is larger than
`Int`. In a large array of optional integers this is measurable, and
the fix is the usual one: if the array is hot and a sentinel is
genuinely available, use the sentinel and document it.

**`??` is lazy** — the right side is not evaluated on the present path,
so an expensive default costs nothing when it is not needed.

**`if let` and `let … else` compile to a tag test and a branch.** No
allocation, no dynamic dispatch.

**Compared to Go's zero-value approach**, Option is free-to-slightly-
larger and strictly safer. Compared to Go's comma-ok, it is the same
machinery with better composition.

**In the interpreter**, `Option` is unboxed, so `Some(x)` is a no-op
and `None` is a sentinel comparison. This is actually the *fastest*
possible representation and is the one silver lining of the wart.

---

### 7. Best Practices

**Let the signature carry the information.**

```glide
// Bad — the caller cannot tell, and must defensively check
fn find_user(id: UserId) -> User

// Good
fn find_user(id: UserId) -> User?
```

And equally:

```glide
// Bad — now every caller writes `?? panic()` or an if-let they do not need
fn current_user(session: Session) -> User?

// Good, if a session always has a user
fn current_user(session: Session) -> User
```

**Use `let … else` to flatten.** Guard clauses at the top, happy path
unindented. This is the idiom that makes Option-heavy code readable.

**Prefer `??` for genuine defaults, not for silencing.** The question
to ask: if this value is absent, does the program have a *correct*
thing to do that needs no explanation? If yes, `??`. If the answer is
"well, it probably won't be absent", you want `let … else` and an
error.

**Push Options to the boundary.** Validate once, then carry proof:

```glide
// Bad — every function re-checks
fn send(to: String?, body: String) {
    let Some(addr) = to else { return }
    …
}

// Good — the caller resolves absence once
fn send(to: Email, body: String) { … }
```

This is Chapter 12's parse-don't-validate, and `Option` is half of the
mechanism.

**Do not model a state machine with Options.**

```glide
// Bad
type Conn = struct {
    socket: Socket?
    error: String?
    retry_at: Instant?
}

// Good
type Conn =
    Disconnected
    | Connecting{ retry_at: Instant }
    | Connected{ socket: Socket }
    | Failed{ error: String }
```

Three Options is eight states. A sum type is exactly as many as you
declared. When you find several Options in one struct that are
correlated, you have found a sum type.

**Return `Option` for ordinary absence, `Result` for reportable
failure.** The test: does the absent case carry information?

---

### 8. Examples

**The three constructs, side by side on one problem:**

```glide
type User = struct {
    pub id: Int
    pub name: String
    pub email: String?
}

fn find(users: List<User>, id: Int) -> User? {
    for u in users {
        if u.id == id { return Some(u) }
    }
    None
}

// 1. `??` — a default is meaningful.
fn display_name(users: List<User>, id: Int) -> String {
    let u = find(users, id)
    match u {
        Some(user) => user.name
        None       => "unknown"
    }
}

// 2. `if let` — both cases have code.
fn notify(users: List<User>, id: Int) -> String {
    if let u = find(users, id) {
        if let addr = u.email {
            "emailing {addr}"
        } else {
            "no email for {u.name}"
        }
    } else {
        "no such user"
    }
}

// 3. `let … else` — absence aborts.
fn promote(users: List<User>, id: Int) -> Result<String, String> {
    let Some(u) = find(users, id) else {
        return Err("no user {id}")
    }
    let Some(addr) = u.email else {
        return Err("user {u.name} has no email")
    }
    Ok("promoted {u.name} <{addr}>")
}

fn main() {
    let users = [
        User{ id: 1, name: "ada",   email: Some("ada@example.com") },
        User{ id: 2, name: "grace", email: None },
    ]
    println(display_name(users, 1))
    println(display_name(users, 9))
    println(notify(users, 1))
    println(notify(users, 2))
    println(notify(users, 9))
    println("{promote(users, 1):?}")
    println("{promote(users, 2):?}")
    println("{promote(users, 9):?}")
}
```

```
ada
unknown
emailing ada@example.com
no email for grace
no such user
Ok("promoted ada <ada@example.com>")
Err("user grace has no email")
Err("no user 9")
```

Compare `notify` and `promote`. `notify` genuinely has something to say
in every case, so nested `if let` is right. `promote` aborts on
absence, so `let … else` flattens it — and notice that `promote` has no
indentation at all despite handling two possible absences.

**Flattening a nested lookup chain:**

```glide
type Config = struct { pub sections: Map<String, Map<String, String>> }

// Bad — the pyramid
fn get_bad(c: Config, section: String, key: String) -> String {
    if let s = c.sections[section] {
        if let v = s[key] {
            v
        } else {
            "unset"
        }
    } else {
        "unset"
    }
}

// Good — a default all the way down
fn get(c: Config, section: String, key: String) -> String {
    let s = c.sections[section] ?? [:]
    s[key] ?? "unset"
}

fn main() {
    let c = Config{ sections: ["db": ["host": "localhost"]] }
    println(get(c, "db", "host"))
    println(get(c, "db", "port"))
    println(get(c, "web", "host"))
}
```

```
localhost
unset
unset
```

**Bad versus good: the sentinel return**

```glide
// Bad — -1 is a magic value the caller must know about
fn index_of(xs: List<String>, target: String) -> Int {
    for (i, x) in xs.iter().enumerate() {
        if x == target { return i }
    }
    -1
}

fn main() {
    let xs = ["a", "b", "c"]
    let i = index_of(xs, "z")
    println(xs[i])            // panics — and the type system permitted it
}
```

The sentinel version compiles, and the bug is a runtime panic at the
*use* site, far from the missing check.

```glide
// Good
fn index_of(xs: List<String>, target: String) -> Int? {
    for (i, x) in xs.iter().enumerate() {
        if x == target { return Some(i) }
    }
    None
}

fn main() {
    let xs = ["a", "b", "c"]
    let Some(i) = index_of(xs, "z") else {
        println("not found")
        return
    }
    println(xs[i])
}
```

```
not found
```

The caller *cannot* index without handling the absent case. The check
is not a discipline; it is the only way the code compiles.

---

### 9. Summary & Exercises

**Summary**

- **There is no null.** A plain `T` is always a real `T`; `T?` is
  sugar for `Option<T>`, an ordinary two-variant sum type.
- The value is as much in the `T` signatures as the `T?` ones:
  `fn owner() -> User` tells you not to defensively check.
- Three constructs, one per case: **`??`** for a meaningful default,
  **`if let`** when both branches have code, **`let … else`** when
  absence aborts. `match` when there are more than two outcomes.
- `??` binds loosest of all binary operators and is lazily evaluated.
  It tests **absence**, not falsiness — unlike JavaScript's `||`.
- `if let` binds the inner value with no `Some(…)` wrapper in the
  source. Only a `None` routes to `else`; a non-`None` mismatch panics,
  because `if let` unwraps rather than dispatching.
- `let … else` requires its block to **diverge**, and that enforcement
  is what makes the flat guard-clause style safe.
- `?` is **not** overloaded onto Option — it is the `Result`
  propagation operator and carries error conversion with it.
- Use `Option` when absence is an ordinary outcome; use `Result` when
  the absent case carries information worth reporting.
- Several correlated Options in one struct is a sum type wearing a
  disguise.
- ○: niche optimisation makes `T?` free for reference types.
  Interpreter caveat: `Option` is unboxed, so `Option<Option<T>>` is
  unrepresentable and `Some(x)` patterns match any non-`None` value.

**Exercises**

1. **Find the nil that cannot happen.** In a Go or Java codebase, find
   a function that checks a parameter for nil at the top. Determine
   whether any caller could actually pass nil. Most such checks are
   defensive against a case the code structure already prevents — and
   each one is a line that exists because the type could not say
   "definitely present". Count them in one file.

2. **Flatten a pyramid.** Take a function with three or more nested
   null checks and rewrite it with `let … else`. Then measure the
   maximum indentation before and after. The difference is the whole
   argument for guard-let, and it is why Swift added it.

3. **Decide Option versus Result, five times.** For each of these,
   decide which return type is right and write down the deciding
   factor: looking up an environment variable; parsing a port number
   from a string; finding a user by ID in a database; getting the first
   element of a list; reading a file. There is no universally correct
   answer for all five — the point is that the *reason* should be
   "does the absent case carry information", not habit.
