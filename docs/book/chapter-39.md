# Chapter 39: Common Anti-Patterns

Every language acquires anti-patterns from the languages its users came
from. Glide is unusual in that its likely users come from *three*
directions — Go, Rust, and the C-lineage mainstream — and each brings a
different set of habits that are locally reasonable and globally wrong
here.

This chapter is organised by origin, because that is how you will
recognise them in your own code.

---

### 1. Basic Usage: The Catalogue

#### Go-in-Glide

**The functional-options pattern.** Thirty lines of ceremony to express
what a signature should have said (Chapter 7).

```glide
// Bad
type Option = fn(Config) -> Config
fn with_timeout(d: Duration) -> Option { |c| Config{ timeout: d, ..c } }
fn with_tls(b: Bool) -> Option { |c| Config{ tls: b, ..c } }
fn connect(host: String, opts: List<Option>) -> Conn { … }

connect("db.local", [with_tls(false)])

// Good
fn connect(host: String, port: Int = 5432, tls: Bool = true) -> Conn
connect("db.local", tls: false)
```

The pattern exists in Go because Go has no defaults and no named
arguments. Transplanting it costs a closure and a list allocation per
call and produces a worse call site.

**The `kind` field.** A struct with a discriminator string and
mostly-unused pointers (Chapter 13).

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

Tells: a `kind`/`type`/`tag` field; several `Option` fields where only
certain combinations are valid; a comment explaining which fields apply
when.

**Boolean flag soup.** Two booleans is four states; if only three are
meaningful, it is a three-variant sum type (Chapter 12).

**Sentinel returns.** `-1` for not-found, `""` for absent, `0` for
unset. The Option exists (Chapter 14).

**The `ctx` parameter.** Cancellation is ambient (Chapter 27). A
`timeout: Duration` parameter spreading through your call graph is
`ctx` rebuilt by hand — and it is also *wrong*, because passing the
same timeout to two sequential calls doubles the budget.

**Shutdown ceremony.** A stop channel, a `done` flag, a `shutting_down`
boolean, a signal handler. The scope does all of it (Chapter 26).

```glide
// Bad
let (stop_tx, stop_rx) = channel()
_ = s.spawn(|| sweeper(db, stop_rx))
…
stop_tx.close()

// Good
_ = s.spawn(|| sweeper(db))
```

**Reaching for a global.** There is no module-level `let`, and the
reflex to want one is the tell (Chapter 30).

**`interface{}`-shaped thinking.** There is no top type. A
`Map<String, Any>` is a struct that has not been written.

#### Rust-in-Glide

**Combinator chains.** Rust's `Option` and `Result` have a large
combinator surface, and Glide deliberately does not.

```glide
// Rust-in-Glide (and most of these do not exist)
config.get("port").and_then(|s| s.parse_int()).unwrap_or(8080)

// Glide
let port = (config["port"] ?? "8080").parse_int() ?? 8080
```

`DESIGN.md` prefers three readable constructs (`??`, `if let`,
`let … else`) plus `match` over twenty methods.

**Excessive `distinct` wrapping.** Rust's newtype pattern is cheap and
idiomatic, and over-applied here it produces `.value()` noise —
especially since operator traits are ○ (Chapter 15). Wrap identifiers;
do not wrap counts, indexes, and lengths that the domain freely mixes.

**Fighting for value semantics.** Collections are references and `let`
does not freeze (Chapter 4). Writing defensive copies everywhere is
paying for a guarantee the language did not promise. Either do not hand
out the reference, or wait for persistent collections (○).

**Expecting `?` on `Option`.** Not adopted (Chapter 14).

**Trait objects everywhere.** `any Trait` (○) boxes and dispatches
dynamically, and it is spelled differently *so that the cost is
visible*. Generic bounds are the default.

**Premature lifetimes-thinking.** There is a GC. A closure can capture
whatever it likes and return it; a struct can hold a reference to
anything; nothing needs to be `'static`.

#### Java/C#-in-Glide

**Interface-per-class.** A trait with one implementation is a type with
extra steps (Chapter 17). Introduce the seam when a second
implementation or a test substitution actually exists.

**Getter/setter ceremony.**

```glide
// Bad
type User = struct { name: String }
impl User {
    pub fn get_name(self) -> String { self.name }
    pub fn set_name(mut self, n: String) { self.name = n }
}

// Good
type User = struct { pub name: String }
```

Field-level `pub` is the encapsulation mechanism (Chapter 12). Make a
field public or do not; a getter that returns the field unchanged is
noise.

**Reaching for inheritance.** There is none, and there is no embedding
either (Chapter 16). Composition holds data; traits hold behaviour.

**Exception-shaped error handling.** Wanting a `try`/`catch` at the top
of `main` that handles everything. Errors are values with types; handle
them where the decision belongs.

**Looking for `recover`.** Permanently absent (Chapter 21). If you want
a containment boundary, that is a scope.

**Layer cake architecture.** `NoteController` → `NoteService` →
`NoteRepository` → `NoteEntity`, where each layer is a thin pass-through
and the interfaces exist for a dependency-injection framework that does
not exist here. Chapter 40 covers this properly.

#### Python/JavaScript-in-Glide

**Stringly-typed everything.** A `Map<String, String>` config that
every consumer re-parses (Chapter 11).

**Truthiness habits.** `if xs` does not compile. That is the point
(Chapter 5).

**Dynamic dispatch on a string.**

```glide
// Bad
fn handle(kind: String, body: String) -> Result<(), Error> {
    if kind == "created" { … } else if kind == "deleted" { … } else { … }
}

// Good
type Event = Created{ id: Int } | Deleted{ id: Int }
fn handle(e: Event) -> Result<(), Error> {
    match e {
        Created{ id } => …
        Deleted{ id } => …
    }
}
```

**Side effects in comprehension-shaped code.** An adapter chain used
for effects does nothing (it is lazy) or is a loop in disguise
(Chapter 24).

#### Glide-native anti-patterns

These are not imported; they are ways to misuse features this language
actually has.

**`_ =>` as a habit.** The single most damaging one. Every `_ =>` on a
closed type converts a future compile-time work list into a silent
default. Write it only when "anything else" is genuinely the meaning —
an HTTP method string, an integer.

**`??` as an error silencer.**

```glide
// Bad — three failures vanish and the function returns a plausible number
fn sync(db: Db) -> Int {
    let a = db.exec("delete from stale") ?? 0
    let b = db.exec("vacuum") ?? 0
    _ = db.close()
    a + b
}
```

Each discard is *visible*, which is the design working — and a reader
can see three places where a failure disappears.

**`mut` creep.** Six mutable bindings where one is needed devalues the
marker everywhere else (Chapter 4).

**Sum-type explosion.** Beyond seven or eight variants, ask whether two
dimensions have been flattened into one. `HttpGet | HttpPost | GrpcUnary
| GrpcStream` is probably `Protocol` × `Method`.

**Generator abuse.** A generator whose body has side effects runs them
at unpredictable times, or never (Chapter 25). Generators produce
values.

**Scope-per-call.** A scope with one child that you immediately join is
a function call with extra steps (Chapter 26).

**Channel-for-one-value.** `join()` returns what the closure returned;
channels are for streams (Chapter 28).

**Interpolation in a query string.** SQL injection, and the one place
in the book where nothing structural stops you (Chapter 35).

**Interpolated log messages** (○). The one place interpolation is an
antipattern: an infinite-cardinality message cannot be grouped,
counted, or alerted on.

```glide
// Bad
log.info("user {id} logged in from {ip}")

// Good
log.info("user logged in", { user_id: id, ip: ip })
```

---

### 2. Under the Hood: Why These Persist

**Anti-patterns are usually correct code from a different language.**
None of the above is stupid. The functional-options pattern is
excellent Go. The newtype-everything reflex is excellent Rust.
Interface-per-class is defensible Java. They fail here because the
constraint that motivated them is gone.

That makes them hard to see: the code looks *good*, by a standard you
learned honestly.

**The reliable detection method is to ask what constraint the pattern
solves, and whether that constraint exists here.**

| Pattern | Constraint it solves | Present in Glide? |
|---|---|---|
| Functional options | No default parameters | No |
| `kind` field + type switch | No sum types | No |
| `sql.NullString` | No `Option` | No |
| `ctx` threading | No structured cancellation | No |
| Stop channels | No scoped task lifetime | No |
| `recover` in handlers | Panics kill the process | No |
| Getters | No field-level visibility | No |
| Interface-per-class | DI framework needs a type | No |
| Combinator chains | No `let … else` | No |
| Defensive copying | Borrow checker enforces value semantics | **Yes** — this one is real |

Note the last row. Not every imported instinct is wrong: Glide *does*
have reference semantics without a borrow checker, so caring about
aliasing is legitimate. The habit to keep is "know who can mutate
this"; the habit to drop is "copy everywhere just in case".

---

### 3. Why This Design?

#### Why the language cannot prevent most of these

It prevents some. `interface{}`, `nil`, zero values, `recover`,
inheritance, embedding, and `init()` are all unwritable. A `kind` field
compiles but the sum type is easier. `ctx` threading compiles but the
scope is shorter.

What the language *does* is make the good version cheaper than the bad
one, which is the only lever that works at scale. Nobody writes thirty
lines of options pattern when one signature does it.

`DESIGN.md`'s recurring phrase is **culture follows cost**. Go never
grew a map/filter culture because closures cost forty characters.
Property testing never went mainstream because setup cost an
afternoon. Small interfaces are Go culture because they cost nothing.

#### Why `_ =>` is singled out

Because it is the one anti-pattern that *removes a guarantee the
language was built to provide*, and it does so silently.

Exhaustiveness is the reason sum types are worth having (Chapter 13).
The value is not "I can express one-of-N" — it is "when I add a sixth
variant, the compiler hands me every place that needs updating". A
`_ =>` arm opts that site out, permanently, and nothing ever tells you.

`DESIGN.md`: a `_ =>` arm is legal but **spends the guarantee**.

#### Why over-abstraction is worse here than in Java

Because Glide's cheap abstractions are *very* cheap and its expensive
ones are visibly expensive.

A `distinct` type is ten characters and zero runtime cost. A sum type
is one line. A nested `fn` is free. These do not need justification.

A trait with one implementation, a generic parameter that never varies,
a four-layer architecture — these cost indirection, binary size,
compile time, and reader effort, and they buy flexibility you have not
yet needed. In Java the ceremony is unavoidable so you stop noticing
it; here it stands out.

#### Why the "Go-in-Glide" section is the longest

Because Glide is a Go-tradition language and Go instincts mostly
transfer — which makes the ones that do not transfer harder to spot.

A Rust programmer writing `.and_then().unwrap_or()` gets a compile
error, because the methods do not exist. A Go programmer writing a
functional-options pattern gets working code that a reviewer might
approve.

---

### 4. Competing Approaches

Every ecosystem has this chapter, and the interesting thing is what
each one's list is *about*:

**Go's** anti-patterns are mostly about **over-abstraction** —
interface pollution, premature generics, Java-style layering. Go's
sparseness makes adding structure the temptation.

**Rust's** are mostly about **fighting the borrow checker** —
`clone()` everywhere, `Rc<RefCell<T>>` as a habit, `unsafe` to avoid
learning lifetimes. Rust's strictness makes escaping it the
temptation.

**Java's** are mostly about **ceremony** — anaemic domain models,
`AbstractSingletonProxyFactoryBean`, getters on everything. Java's
verbosity makes patterns-as-substitutes-for-features the temptation.

**Python's** are mostly about **dynamism** — monkey-patching,
`**kwargs` soup, mutable default arguments. Python's flexibility makes
cleverness the temptation.

**Glide's** list is mostly about **imported habits**, which is what a
young language's list looks like. The native ones — `_ =>`, `??` as a
silencer, `mut` creep — are all about *spending guarantees the language
provides*, which is probably what a mature Glide list will look like
too.

---

### 5. Common Mistakes (About Anti-Patterns)

**Treating this list as a style guide.** Every item here has a
legitimate use. `_ =>` is right for genuinely open sets. `??` is right
when the error truly does not matter. A trait with one implementation
is right when the second is arriving next week. The anti-pattern is
doing it *by reflex*.

**Rewriting working code to remove them.** A functional-options
pattern that works is not an emergency. Change it when you touch it.

**Cargo-culting the fixes.** Applying `distinct` to every integer
produces `.value()` noise; applying sum types to open sets makes them
unextensible; applying "parse, don't validate" to values with no
invariant is a wrapper around nothing.

**Missing that some instincts are still right.** Reference semantics
without a borrow checker means aliasing is real (Chapter 4). Caring
about who can mutate a shared collection is correct here.

---

### 6. Performance Considerations

Most anti-patterns in this chapter cost clarity rather than speed. Four
cost both:

**Functional options** allocate a closure per option and a list per
call. A defaulted parameter allocates nothing.

**Trait objects by reflex** (`any Trait`, ○) box and dispatch
dynamically where a generic bound would monomorphise. This is the
biggest one — Go pays it invisibly on every interface value, and
Glide's `any` keyword exists so you can see the bill.

**Adapter chains used for effects** either do nothing (lazy) or add
per-element closure dispatch where a loop would not.

**Defensive copying** copies. If the copy is not buying a guarantee you
need, it is pure cost.

And one that costs *only* clarity, and is worth calling out for that
reason: **`_ =>`** has zero runtime cost and removes a compile-time
guarantee. It is the cheapest possible way to make a codebase worse.

---

### 7. Best Practices

**Ask what constraint the pattern solves.** The detection method from
section 2, applied continuously. If the constraint is gone, so is the
pattern.

**Watch for these in review, in this order:**

1. A new `_ =>` on a closed type.
2. A new `mut` that is assigned in every branch.
3. A `??` swallowing an error someone would want.
4. A `kind`/`type`/`tag` field.
5. A trait with one implementation.
6. A parameter that is really ambient (`timeout`, `logger`) or really
   a global (`db` threaded through five layers that do not use it).
7. Interpolation inside a query.

**Prefer the cheap abstractions and be suspicious of the expensive
ones.**

| Cheap — use freely | Expensive — justify |
|---|---|
| `distinct` types | Traits with one implementation |
| Sum types | Generic parameters that never vary |
| Nested `fn`s | Architectural layers |
| Block expressions | Trait objects (`any T`) |
| Small structs | Modules per type |

**When you catch yourself writing ceremony, look for the feature.**
Almost every ceremonial pattern in this chapter has a one-line
replacement, because the language was designed by cataloguing exactly
these patterns and asking what feature would delete each one.

That is, in a sense, what `DESIGN.md` *is*.

---

### 8. Examples

**The full Go-in-Glide translation, and its fix.**

A faithful port of a Go service, using every Go pattern:

```glide
// Bad — everything here is idiomatic Go and wrong here
type ServerOption = fn(ServerConfig) -> ServerConfig

fn with_timeout(d: Int) -> ServerOption { |c| ServerConfig{ timeout: d, ..c } }
fn with_tls(b: Bool) -> ServerOption { |c| ServerConfig{ tls: b, ..c } }

type Job = struct {
    kind: String            // "email" | "sms" | "push"
    recipient: String
    subject: String?        // only for email
    body: String
    sent: Bool
    failed: Bool
    error: String
}

fn process(db: Db, j: Job, timeout: Int, verbose: Bool) -> Int {
    let mut result = 0
    if j.kind == "email" {
        if j.subject == None {
            result = 400
        } else {
            _ = send_email(j.recipient, j.subject ?? "", j.body) ?? false
            result = 200
        }
    } else if j.kind == "sms" {
        _ = send_sms(j.recipient, j.body) ?? false
        result = 200
    } else {
        result = 400
    }
    result
}
```

Six anti-patterns, all imported, all individually defensible in Go:

1. **Functional options** — three declarations and a closure per call.
2. **`kind` string** with fields that apply to some kinds only.
3. **`sent`/`failed`/`error`** — flag soup, eight states, three
   meaningful.
4. **`timeout: Int`** — a `ctx` parameter in disguise, and unitless.
5. **`verbose: Bool`** positional — a boolean trap, and unused.
6. **`Int` return** encoding HTTP statuses, plus a `mut` return slot,
   plus two `?? false` silencing failures.

The fix, feature by feature:

```glide
// Good
type Recipient = distinct String

type Delivery =
    Email{ to: Recipient, subject: String, body: String }
    | Sms{ to: Recipient, body: String }
    | Push{ device: String, body: String }

type Outcome =
    Pending
    | Sent{ at: Instant }
    | Failed{ reason: String }

type Job = struct {
    pub id: JobId
    pub delivery: Delivery
    pub outcome: Outcome
}

type SendError =
    Transport{ cause: Error }
    | Rejected{ reason: String }

impl SendError {
    fn from(e: Error) -> SendError { Transport{ cause: e } }
}

fn process(db: Db, job: Job) -> Result<(), SendError> {
    match job.delivery {
        Email{ to, subject, body } => send_email(to, subject, body)?
        Sms{ to, body }            => send_sms(to, body)?
        Push{ device, body }       => send_push(device, body)?
    }
    Ok(())
}
```

And the caller sets the budget rather than passing it:

```glide
scope(timeout: 5.s) {
    process(db, job)?
}
```

What changed:

| Anti-pattern | Feature that deleted it |
|---|---|
| Functional options | Default parameters (7) |
| `kind` string | Sum type (13) |
| Fields that apply to some kinds | Payloads on variants (13) |
| Flag soup | `Outcome` sum type (13) |
| `timeout` parameter | `scope(timeout:)` (26) |
| Positional bool | Named arguments (7) |
| `Int` status return | `Result<(), SendError>` (19) |
| `?? false` | `?` with conversion (19) |
| `mut result` | `match` as an expression (10) |
| Untyped `String` recipient | `distinct` (15) |

Ten anti-patterns, ten features, and the second version is shorter.

**The native anti-pattern, in isolation.** This one is worth its own
example because it is the one you will commit after you are fluent:

```glide
// Bad — added six months in, when a fourth variant appeared and
// this match would not compile
fn describe(s: Shape) -> String {
    match s {
        Circle(r)  => "circle r={r}"
        Rect(w, h) => "rect {w}x{h}"
        _          => "shape"
    }
}
```

The `_` was added to make an error go away. The error was the compiler
telling you that a new variant needed a description here, and now it
never will again — this function will silently print `"shape"` for
every future variant.

```glide
// Good
fn describe(s: Shape) -> String {
    match s {
        Circle(r)  => "circle r={r}"
        Rect(w, h) => "rect {w}x{h}"
        Point      => "point"
        Line(a, b) => "line {a}..{b}"
    }
}
```

Four arms, and adding a fifth variant breaks this function — which is
the entire reason the type is a sum type.

---

### 9. Summary & Exercises

**Summary**

- Most Glide anti-patterns are **correct code from another language**,
  which is what makes them hard to see: they look good by a standard
  you learned honestly.
- **The detection method:** ask what constraint the pattern solves, and
  whether that constraint exists here. Functional options solve "no
  default parameters"; `kind` fields solve "no sum types"; `ctx`
  threading solves "no structured cancellation". None of those
  constraints exists.
- **Go-in-Glide** is the longest list, because Go instincts mostly
  transfer and the exceptions are therefore camouflaged: functional
  options, `kind` fields, flag soup, sentinel returns, `ctx`
  parameters, shutdown ceremony, globals.
- **Rust-in-Glide:** combinator chains, over-wrapping with `distinct`,
  defensive copying, expecting `?` on `Option`, trait objects by
  reflex, lifetimes-thinking.
- **Java-in-Glide:** interface-per-class, getter ceremony, inheritance
  reflexes, exception-shaped handling, layer cake.
- **Native anti-patterns** — the ones you will commit once fluent —
  are all about **spending guarantees**: `_ =>` on closed types, `??`
  as an error silencer, `mut` creep, generator side effects,
  scope-per-call, channels for single values.
- **`_ =>` is the worst**, because it silently removes the guarantee
  that makes sum types worth having, and it has zero runtime cost. It
  is the cheapest possible way to make a codebase worse.
- **Not every imported instinct is wrong.** Reference semantics without
  a borrow checker means aliasing is real, so caring about who can
  mutate a shared collection is correct here.
- The language cannot prevent most of these; it makes the good version
  cheaper. **Culture follows cost.**

**Exercises**

1. **Audit an imported pattern.** Take a design pattern you use
   reflexively — options, builders, repositories, factories, DI
   containers — and write down the language constraint it exists to
   work around. Then check whether Glide has that constraint. Roughly
   half of the Gang of Four catalogue is missing-language-feature
   compensation, and it is instructive to work out which half.

2. **Find your `_ =>` arms.** In a codebase with sum types, tagged
   unions, or sealed classes, find every default/wildcard branch. For
   each, decide whether "anything else" is genuinely the meaning or
   whether it was added to silence an error. Then add a variant and see
   which ones silently do the wrong thing.

3. **Grade the ten-anti-pattern example.** Take the "Bad" version from
   section 8 and, without looking at the fix, list every problem you
   can find and the feature that solves it. Then compare with the
   table. Anything you missed is a habit you still have; anything you
   found that is not in the table is worth arguing about.
