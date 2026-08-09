# Chapter 17: Traits

A **trait** is a named set of capabilities that a type can declare it
provides. If you have used Java or C# interfaces, Go interfaces, Swift
protocols, or Haskell type classes, you have met the idea. If you have
not, this chapter builds it from nothing.

Glide's traits are a synthesis rather than a copy: **conformance is
declared, satisfaction is structural**, and traits may carry **default
methods**. That combination is Swift's, and it fixes the two things
Go's interfaces get wrong without adopting Rust's forwarding
boilerplate.

The chapter is mostly ✓ — traits, default methods, and `impl Trait for
Type` all run. Trait *checking* (verifying that a type actually
provides what it claims), the orphan rule, and `any Trait` boxing are
○.

---

### 1. Basic Usage

#### The problem traits solve

Suppose you have several types that can each report an area:

```glide
type Circle = struct { r: Float }
type Square = struct { w: Float }
```

You want a function that takes "anything with an area". Without traits
you have two options, both bad: write one function per type, or make a
sum type containing every possible shape — which means the *caller*
cannot add a new shape.

A trait names the capability:

```glide
trait Shape {
    fn area(self) -> Float
}
```

That declares: a `Shape` is anything that has an `area(self) -> Float`
method. The method has no body, so it is a **requirement**.

#### Declaring conformance

```glide
impl Shape for Circle {
    fn area(self) -> Float { 3.14159 * self.r * self.r }
}

impl Shape for Square {
    fn area(self) -> Float { self.w * self.w }
}
```

`impl Trait for Type` is the conformance declaration. It is one line of
intent, and it is greppable: "who implements `Shape`?" is a text
search.

#### Default methods

A trait method *with* a body is a **default**, inherited by any type
that declares conformance:

```glide
trait Shape {
    fn area(self) -> Float                       // required
    fn name(self) -> String { "shape" }          // default
    fn describe(self) -> String {                // default, uses both
        "{self.name()} with area {self.area()}"
    }
}
```

Now a conforming type gets `name` and `describe` for free, and may
override either:

```glide
impl Shape for Circle {
    fn area(self) -> Float { 3.14159 * self.r * self.r }
    fn name(self) -> String { "circle" }         // override
}

impl Shape for Square {
    fn area(self) -> Float { self.w * self.w }   // takes both defaults
}
```

```glide
fn main() {
    println(Circle{ r: 1.0 }.describe())
    println(Square{ w: 3.0 }.describe())
}
```

```
circle with area 3.14159
shape with area 9
```

`describe` was written once, in the trait, and calls `self.name()` and
`self.area()` — which resolve to whatever the concrete type provides.
That is polymorphism, and it is the mechanism that replaces a base
class with implemented methods.

All trait methods take `self`.

#### Conformance is declared; satisfaction is structural

If the type already has the methods, the `impl` block can be empty:

```glide
impl Reader for TcpConn {}      // the existing read() satisfies it
```

That is the whole conformance. You do not restate methods that already
match — bodies are written only for what is missing.

This is the synthesis: Go's model asks nothing (accidental conformance)
and Rust's asks for forwarding boilerplate when shapes already match.
Swift got it right first, and Glide takes Swift's answer.

#### Making a type iterable

The most common trait interaction you will hit early: a type with an
`iter()` method works in `for … in`:

```glide
type Bag = struct { items: List<Int> }

impl Bag {
    fn iter(self) -> Iterator<Int> {
        for x in self.items { yield x }
    }
}

fn main() {
    let b = Bag{ items: [1, 2, 3] }
    for x in b {
        print("{x} ")
    }
    println("")

    let doubled = b.iter().map(|x| x * 2).collect()
    println("{doubled:?}")
}
```

```
1 2 3
[2, 4, 6]
```

`for x in b` calls `b.iter()`. Chapter 23 covers the protocol; the
point here is that providing one method is what makes a user type
participate in the language's iteration machinery.

#### Trait composition ○

```glide
trait ReadWriter: Reader + Writer {}     // ○
```

Declared bounds, not embedding-aggregation. A type conforming to
`ReadWriter` must conform to both.

#### Generic bounds

Traits are how generics get constrained (Chapter 18):

```glide
fn largest<T: Ord>(xs: List<T>) -> T? { … }
```

`T: Ord` reads "any `T` that can be ordered". This is **static
dispatch** — the compiler generates a specialised version per concrete
type.

#### Trait objects ○

For a heterogeneous collection, you need dynamic dispatch, and it is
spelled explicitly:

```glide
let shapes: List<any Shape> = [Circle{ r: 1.0 }, Square{ w: 2.0 }]   // ○
```

`any Shape` is Swift's spelling and it is honest about the cost: a box
and a vtable. Go boxes every interface value invisibly; Glide makes you
write the word.

#### The orphan rule ○

You may implement:

- **your trait** for **any type** (including foreign ones), or
- **any trait** for **your type**,

but never a foreign trait for a foreign type. This is Rust's rule, and
it guarantees coherence: exactly one implementation per (trait, type)
pair, so behaviour can never depend on link order.

#### No `any` top type

There is no bare `any` / `Object` / `interface{}`. Sum types, generics,
and `any Trait` cover the need. A top type can be added deliberately
later; it can never be removed.

---

### 2. Under the Hood

#### What "declared but not verified" means today

The interpreter registers an `impl Trait for Type` block's methods
against the type and records the conformance. It does **not** verify
that the type actually provides every required method — conformance is
asserted, not checked, until the checker era.

The practical consequence: a missing method surfaces as a runtime
"X has no method …" error at the call, rather than a compile error at
the `impl`. Write the methods.

What *does* work today, and is the useful half: declaring `impl Trait
for Type` **inherits the trait's default methods**. That is why
`Square` gets `name` and `describe` above without writing them.

#### Default-method conflicts

If two traits supply the same unoverridden default method and a type
conforms to both, that is an error **at the call**, naming both
traits. There is no arbitrary precedence, and there is no diamond
problem to resolve, because the resolution is "you must say which".

#### Dispatch: static by default, dynamic when you ask

**Static** (generics, `T: Shape`): the compiler monomorphises —
generates a separate specialised function per concrete type. The call
is direct and inlinable. Zero abstraction cost, larger binary.

**Dynamic** (`any Shape`): the value is boxed with a pointer to a
vtable of function pointers. One indirect call per method, no inlining,
one allocation per box.

The important design property is that **you choose, visibly**. In Go,
every interface value boxes and dispatches dynamically, and the source
does not say so: `func draw(s Shape)` and `func draw[T Shape](s T)`
look almost identical and cost very differently. Glide's `any` keyword
is the price tag.

#### Why the typed-nil trap cannot exist

Go's most-asked confusion:

```go
var p *MyError = nil
var err error = p
fmt.Println(err == nil)     // false!
```

An interface value holds (type, data). A nil `*MyError` stored in an
`error` interface makes the interface non-nil, because its type word is
set.

Glide has no nil to smuggle. Absence of a trait object is
`(any Reader)?`, an ordinary Option, and there is no second nil-ness to
disagree with.

---

### 3. Why This Design?

#### Why declared conformance rather than Go's implicit

Go's structural typing is friendlier at small scale and produces three
problems at large scale.

**Accidental conformance.** Any type with a `Close() error` method "is"
your `Closer`, whether or not its author intended it. That is
occasionally convenient and occasionally means a type satisfies an
interface whose contract it does not honour — the method name matched
and the semantics did not.

**Undiscoverability.** "Who implements `io.Reader`?" is not a text
search in Go; it requires whole-program analysis. With `impl Reader for
X`, it is `grep`.

**Interfaces can never grow.** This is the decisive one. In Go, adding
a method to an interface breaks every implementor, everywhere,
including in code you do not control. The consequence is visible in
Go's own standard library: its interfaces are fossils, and new
capabilities arrive as *new interfaces*
(`io.ReaderFrom`, `io.WriterTo`, `http.Flusher`, `http.Hijacker`) that
consumers must type-assert for.

Default methods fix evolution completely: adding a method **with a
default** breaks nobody. `DESIGN.md` says this alone justifies the
model, and it is hard to disagree — it is the difference between a
library that can improve and one that cannot.

Plus the diagnostics improve. "Tree is missing method `iter` of
`Iterable`" beats Go's error, which reports the mismatch at the
*assignment site*, listing the method set.

#### What Go gets right, and how each survives

`DESIGN.md` is careful here: Go's implicit model gets three things
right, and each survives explicitness.

**Consumer-defined interfaces** — "accept interfaces, return structs",
Go's best architectural idea. The pattern is about *who owns the
abstraction*: you define the narrow interface you need where you use
it, rather than importing a broad one from the dependency. This works
identically here: define the trait in your module, and `impl` it for
the dependency's type. The pattern was never about implicitness.

**Retroactive conformance** — making someone else's type satisfy your
abstraction. Preserved, via the orphan rule, and with a coherence
guarantee Go cannot offer: exactly one impl per pair.

**Small-interface culture** — `io.Reader` is one method, and that is
why it is the most-used abstraction in Go. Culture follows cost, and
one `impl` line is cheap. The standard library seeds the culture
Go-style, with one-method `Reader`/`Writer` at the bottom.

#### Why default methods matter more than they look

They are the feature that turns a trait from "a contract" into "a
reusable behaviour", and they replace the single legitimate use of
inheritance.

An abstract base class in Java does two things: declares abstract
methods (a contract) and provides concrete methods built on them (reuse
). A trait with required methods plus default methods does both, and
does not drag along a type hierarchy, a fragile base class, or a
diamond problem.

The `describe` method above is the demonstration: written once, in
terms of `self.name()` and `self.area()`, and every conforming type
gets it.

#### Why dispatch is chosen visibly

Because it is a real cost and the source should say so — the pricing
pillar from Chapter 1.

Static dispatch is free at runtime and costs binary size. Dynamic
dispatch costs an allocation, an indirect call, and lost inlining, and
saves binary size and lets you build heterogeneous collections.

Neither is universally right, so the language refuses to pick silently.
`fn area<T: Shape>(s: T)` is static; `fn area(s: any Shape)` is
dynamic. Go hides the choice by having only one, and the hidden one is
the expensive one.

#### Why no top type

`any` / `Object` / `interface{}` is the escape hatch that reflection
crawls through. In Go, `interface{}` plus `reflect` is how JSON
encoding, ORMs, and dependency-injection frameworks work — and Glide
has banned runtime reflection (Chapter 34), so a top type would be a
door to nothing useful and a way to lose type information.

The needs it serves are covered: heterogeneous-but-known is a sum type;
heterogeneous-with-shared-capability is `any Trait`; generic-over-
anything is `<T>`.

And the asymmetry matters: a top type can be added deliberately later
and can never be removed.

#### Why no inheritance, restated

Chapter 16 said it; here is the trait-shaped half. Inheritance bundles
three things: subtyping, implementation reuse, and data layout reuse.
Traits provide the first two, cleanly and separately, and composition
provides the third. Nothing is lost, and fragile base classes, diamond
problems, and taxonomies that stop fitting go with it.

---

### 4. Competing Approaches

**Go.** Implicit structural interfaces. Small, elegant, and the source
of accidental conformance, undiscoverability, un-growable interfaces,
and the typed-nil trap. Glide keeps consumer-defined interfaces,
retroactive conformance, and small-interface culture, and adds
declaration, default methods, and coherence.

**Rust.** Traits with explicit `impl`, the orphan rule, default
methods, associated types, generic bounds, and `dyn Trait` for dynamic
dispatch. Glide's model *is* Rust's, minus lifetimes and minus the
requirement to write forwarding bodies when a shape already matches.
Rust's `Fn` traits and blanket impls are more powerful and add
complexity Glide declines.

**Swift.** Protocols with declared conformance and structural
satisfaction (`extension TcpConn: Reader {}`), protocol extensions
(default methods), and `any Protocol` / `some Protocol` for the
dispatch choice. This is the closest relative and the direct source of
Glide's synthesis, including the `any` spelling.

**Java / C#.** Interfaces with declared implementation and, since Java
8, `default` methods — added for exactly the evolution reason described
above (they needed to add `forEach` to `Collection` without breaking
every implementor). Java validates the design; its verbosity and the
class hierarchy alongside it are what Glide declines.

**Haskell.** Type classes, the origin of the whole idea, with coherence
enforced globally and no orphan instances permitted in the same way.
Haskell's classes are more powerful (multi-parameter, functional
dependencies, higher-kinded) and correspondingly harder.

**C++.** Templates plus duck typing (compile-time structural), which is
Go's model at compile time with worse error messages. Concepts (C++20)
are the belated addition of declared constraints — the same journey
Glide starts from.

**Python.** Duck typing plus `abc.ABC` plus `typing.Protocol`
(structural, opt-in, checked by a separate tool). The progression
Python has taken is the argument for declaring things.

---

### 5. Common Mistakes

**Expecting conformance to be verified.** It is not, yet. A missing
required method is a runtime "no method" error at the call, not a
compile error at the `impl`. Write the methods.

**Forgetting that `impl Trait for Type` is what grants defaults.** A
type with matching methods but no `impl` block does *not* get the
trait's defaults:

```glide
// Bad — has area(), but no defaults, because nothing was declared
type Triangle = struct { b: Float, h: Float }
impl Triangle {
    fn area(self) -> Float { self.b * self.h / 2.0 }
}
// Triangle{…}.describe()  →  no method "describe"

// Good
impl Shape for Triangle {}      // area() already satisfies it
```

That empty `impl` block is the whole point of "declared conformance,
structural satisfaction".

**Making traits too big.** A trait with twelve required methods is a
trait almost nothing can implement. `io.Reader` is one method and it is
the most-used abstraction in Go — that is not a coincidence.

```glide
// Bad
trait Storage {
    fn get(self, k: String) -> String?
    fn put(mut self, k: String, v: String)
    fn delete(mut self, k: String)
    fn list(self) -> List<String>
    fn compact(mut self)
    fn stats(self) -> Stats
    fn backup(self, to: String) -> Result<(), Error>
}

// Good — split by capability; most callers need one of these
trait Get { fn get(self, k: String) -> String? }
trait Put { fn put(mut self, k: String, v: String) }
```

**Defining a trait with one implementation.** That is a type with extra
steps. Add the trait when the second implementation exists, or when a
test genuinely needs a substitute — not speculatively.

**Reaching for a trait when a sum type fits.** The discriminator is
**open versus closed**:

```glide
// Closed set, known at compile time, want exhaustiveness → sum type
type Shape = Circle(Float) | Rect(Float, Float)

// Open set, callers may add cases → trait
trait Shape { fn area(self) -> Float }
```

If you want the compiler to tell you every place that needs updating
when a case is added, you want a sum type. If you want third parties to
add cases without touching your code, you want a trait. Choosing wrong
in the sum-type direction means you cannot extend; choosing wrong in
the trait direction means you lose exhaustiveness.

**Assuming two traits' defaults will silently resolve.** They will not.
Two unoverridden defaults with the same name is an error at the call,
naming both. Override explicitly.

**Expecting `any Trait` to work.** It is ○.

---

### 6. Performance Considerations

**Static dispatch through generic bounds is free** (○,
monomorphisation). `fn area<T: Shape>(s: T) -> Float` compiles to a
separate specialised function per concrete `T`, so the call is direct
and inlinable, exactly as if you had written it by hand.

The cost is binary size and compile time: N instantiations means N
copies. This is Rust's and C++'s model, and it is the reason those
languages produce large binaries.

**Dynamic dispatch through `any Trait`** (○) costs one allocation to
box, one pointer indirection to reach the vtable, and one indirect call
per method, with no inlining across it. This is Java's and Go's model
for every interface value.

**Default methods are ordinary calls.** A default that calls
`self.area()` dispatches to the concrete implementation — statically in
a generic context, dynamically through a trait object.

**In the interpreter**, everything is dynamic and everything is a name
lookup, so trait dispatch has the same cost as any other method call.
No conclusions about the compiled tier can be drawn from that.

**The design property worth remembering:** Glide never boxes without
you writing `any`. Go's `func Print(v interface{})` allocates on every
call with a non-pointer argument, invisibly, and that allocation shows
up in profiles of code that looks allocation-free.

---

### 7. Best Practices

**Keep traits small.** One to three methods. The Go standard library's
one-method `Reader` and `Writer` are the model, and Glide's standard
library seeds the same culture deliberately.

**Define the trait where it is used, not where the type lives.** This
is Go's "accept interfaces, return structs", and it survives here:

```glide
// In your module — the abstraction you need
trait NoteStore {
    fn get(self, id: NoteId) -> Result<Note?, Error>
    fn put(mut self, n: Note) -> Result<(), Error>
}

// Also in your module — conformance for the dependency's type
impl NoteStore for sql.Db { … }
```

The dependency does not know your trait exists. You own the
abstraction, and swapping the dependency means writing one `impl`
block.

**Accept the trait; return the concrete type.**

```glide
// Good
fn sync<S: NoteStore>(store: S) -> Result<Int, Error>
fn open_store(path: String) -> Result<SqlStore, Error>
```

Returning an abstraction hides information the caller might need and
forces boxing for no reason.

**Use default methods to make traits growable.** When you add a
capability, add it with a default:

```glide
trait NoteStore {
    fn get(self, id: NoteId) -> Result<Note?, Error>
    fn put(mut self, n: Note) -> Result<(), Error>

    // Added later. Breaks nobody; implementors may override for speed.
    fn get_many(self, ids: List<NoteId>) -> Result<List<Note>, Error> {
        let mut out = []
        for id in ids {
            if let n = self.get(id)? {
                out.push(n)
            }
        }
        Ok(out)
    }
}
```

That is the pattern Go cannot express, and it is the reason Go's
standard-library interfaces are frozen.

**Prefer a sum type for closed sets.** Ask: will third parties add
cases? If no, you want exhaustiveness, which means a sum type.

**Do not create a trait for testing alone until you need it.** "Every
dependency behind an interface" is a Java habit that produces one-line
interfaces with one implementation each. Introduce the seam when a test
actually needs to substitute, and make the trait the shape the *test*
needs — which is usually much smaller than the real type's API.

**Name traits for capability, types for identity.** `Reader`,
`Writer`, `Ord`, `Iterable`, `Display` — a trait says what you can *do*
with a value. `SqlStore`, `Circle`, `Config` — a type says what a value
*is*.

---

### 8. Examples

**Building up the shape example, one step at a time:**

```glide
// Step 1 — a trait with one requirement.
trait Shape {
    fn area(self) -> Float
}

type Circle = struct { r: Float }
type Square = struct { w: Float }

impl Shape for Circle {
    fn area(self) -> Float { 3.14159 * self.r * self.r }
}
impl Shape for Square {
    fn area(self) -> Float { self.w * self.w }
}
```

```glide
// Step 2 — add defaults. Existing implementors keep working.
trait Shape {
    fn area(self) -> Float
    fn name(self) -> String { "shape" }
    fn describe(self) -> String {
        "{self.name()} with area {self.area()}"
    }
}

impl Shape for Circle {
    fn area(self) -> Float { 3.14159 * self.r * self.r }
    fn name(self) -> String { "circle" }        // override the default
}

impl Shape for Square {
    fn area(self) -> Float { self.w * self.w }  // takes both defaults
}

fn main() {
    println(Circle{ r: 1.0 }.describe())
    println(Square{ w: 3.0 }.describe())
}
```

```
circle with area 3.14159
shape with area 9
```

The important move is between steps 1 and 2: two methods were added to
the trait and **no implementor changed**. In Go this is impossible.

**A user type that participates in iteration:**

```glide
type Bag = struct { items: List<Int> }

impl Bag {
    fn new() -> Bag { Bag{ items: [] } }
    fn add(mut self, x: Int) { self.items.push(x) }

    // One method, and Bag becomes for-able and adapter-able.
    fn iter(self) -> Iterator<Int> {
        for x in self.items { yield x }
    }
}

fn main() {
    let mut b = Bag.new()
    b.add(1)
    b.add(2)
    b.add(3)

    for x in b {
        print("{x} ")
    }
    println("")

    let doubled = b.iter().map(|x| x * 2).collect()
    println("{doubled:?}")

    println(b.iter().filter(|x| x % 2 == 1).count())
}
```

```
1 2 3
[2, 4, 6]
2
```

`iter()` is a generator (Chapter 24) — the body contains `yield`, so
calling it returns an `Iterator`. One method makes `Bag` a
first-class participant in the whole iteration ecosystem.

**Consumer-defined traits, the architectural pattern:**

```glide
// --- Your module defines the abstraction it needs ---
type Note = struct { pub id: Int, pub title: String }

trait NoteStore {
    fn get(self, id: Int) -> Note?
    fn all(self) -> List<Note>

    // A default built from the requirements.
    fn count(self) -> Int { self.all().len() }
    fn titles(self) -> List<String> {
        self.all().iter().map(|n| n.title).collect()
    }
}

// --- One implementation: in memory, for tests ---
type MemStore = struct { notes: List<Note> }

impl NoteStore for MemStore {
    fn get(self, id: Int) -> Note? {
        for n in self.notes {
            if n.id == id { return Some(n) }
        }
        None
    }
    fn all(self) -> List<Note> { self.notes }
}

// --- Code that works against the abstraction ---
fn summary(s: MemStore) -> String {
    let ts = s.titles()
    let joined = ts.join(", ")
    "{s.count()} notes: {joined}"
}

fn main() {
    let store = MemStore{ notes: [
        Note{ id: 1, title: "alpha" },
        Note{ id: 2, title: "beta" },
    ]}
    println(summary(store))
    println("{store.get(1):?}")
    println("{store.get(9):?}")
}
```

```
2 notes: alpha, beta
Note{ id: 1, title: "alpha" }
None
```

That second line is the `Option` unboxing wart from Chapter 14 showing
through: `Some(v)` is the identity function in the interpreter, so
Debug renders the inner value with no `Some(…)` wrapper. Once the
checker boxes Options, it will print `Some(Note{ … })`.

`count` and `titles` were written once in the trait. A future
`SqlStore` implements `get` and `all` and gets both for free — and can
override `count` with a `select count(*)` if the default's "fetch
everything and measure it" is too slow.

(`summary` takes a concrete `MemStore` because generic bounds are not
checked yet; with the checker it would be
`fn summary<S: NoteStore>(s: S) -> String`.)

**Bad versus good: the interface that cannot grow**

```glide
// Bad — every method is required, so adding one breaks all implementors
trait Codec {
    fn encode(self, v: Value) -> String
    fn decode(self, s: String) -> Result<Value, Error>
    fn content_type(self) -> String
    fn supports_streaming(self) -> Bool
}
```

Adding `fn pretty(self) -> Bool` to that trait breaks every
implementation in the world.

```glide
// Good — the capabilities that have a sensible default, have one
trait Codec {
    fn encode(self, v: Value) -> String
    fn decode(self, s: String) -> Result<Value, Error>

    fn content_type(self) -> String { "application/octet-stream" }
    fn supports_streaming(self) -> Bool { false }
    fn pretty(self) -> Bool { false }          // added later, breaks nobody
}
```

Two required methods define what a codec *is*. Everything else has a
conservative default that an implementor may improve on. This trait can
be extended for years.

---

### 9. Summary & Exercises

**Summary**

- A **trait** names a set of capabilities. Body-less methods are
  requirements; methods with bodies are **defaults**, inherited by
  conforming types.
- **Conformance is declared** (`impl Trait for Type` — one greppable
  line) **and satisfaction is structural** (an empty `impl` block
  suffices when the methods already match). Swift's synthesis.
- Declaring conformance is what grants the defaults. A type with
  matching methods and no `impl` block does not get them.
- **Default methods make traits growable** — adding a method with a
  default breaks nobody. This is the single strongest argument against
  Go's model, whose standard-library interfaces are frozen fossils.
- Go's three good ideas survive: consumer-defined interfaces,
  retroactive conformance (via the orphan rule), and small-interface
  culture.
- **Dispatch is chosen visibly.** Generic bounds (`<T: Shape>`)
  monomorphise and are free; `any Shape` (○) boxes and uses a vtable.
  Go hides the choice and always picks the expensive one.
- Providing an `iter()` method makes a user type work in `for … in` and
  with all the iterator adapters.
- **There is no inheritance and no top type.** Composition holds data,
  traits hold behaviour, sum types hold closed sets, generics hold
  parametric code.
- Trait versus sum type: **open set → trait, closed set →
  sum type**. Sum types buy exhaustiveness; traits buy third-party
  extension.
- ○: trait checking (conformance is asserted, not verified), the orphan
  rule, `any Trait`, trait composition (`trait A: B + C`).

**Exercises**

1. **Grow an interface.** Take a Go interface with three or more
   implementations in a codebase you know. Add a method to it and count
   the files you have to touch. Then write the Glide version with the
   new method as a default and count again. That difference is the
   whole design argument, measured.

2. **Find the accidental conformance.** In a Go codebase, pick a
   single-method interface you defined (`Closer`, `Validator`,
   `Namer`). Search for every type in the module with a matching method
   signature. How many of them satisfy your interface without their
   author knowing? Decide whether any of them would be *wrong* to pass
   in.

3. **Pick trait or sum type, five times.** For each of these, decide
   and justify: HTTP status codes; payment methods in a checkout;
   loggers; AST node kinds; database drivers. For each, the deciding
   question is "can a third party add a case without me changing my
   code, and do I want the compiler to list every site when I add one?"
   — note that for two of the five, the answer changes depending on
   whether you are writing the application or the library.
