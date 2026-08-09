# Chapter 16: Methods and `impl`

Methods in Glide live in `impl` blocks, separate from the type
declaration. There are no classes, no constructors-as-a-language-
feature, and no inheritance. What there *is* — and what is worth the
chapter — is one axis that Go conflates and Rust spells with reference
machinery: **declared receiver mutability**.

Everything here is ✓ except method values (`x.method` unapplied) and
one enforcement gap, both flagged.

---

### 1. Basic Usage

#### `impl` blocks

```glide
type Counter = struct {
    n: Int
}

impl Counter {
    fn new() -> Counter {
        Counter{ n: 0 }
    }

    fn value(self) -> Int {
        self.n
    }

    fn bump(mut self) {
        self.n += 1
    }
}
```

Three things to notice:

- The `impl` block is **separate** from the `type` declaration. A type
  may have several `impl` blocks, and they may live in different files
  of the same module.
- `Counter.new()` takes **no `self`** — it is an **associated
  function**, called on the type itself. This is the constructor idiom.
- `value` takes `self`; `bump` takes `mut self`.

#### Calling

```glide
fn main() {
    let mut c = Counter.new()
    c.bump()
    c.bump()
    println(c.value())      // 2
}
```

Associated functions are called with a dot on the *type*
(`Counter.new()`); methods with a dot on the *value* (`c.bump()`).

#### Receiver mutability is declared

`fn value(self)` is read-only. `fn bump(mut self)` may mutate, and is
callable only through a `mut` path:

```glide
fn main() {
    let c = Counter.new()
    c.bump()
}
```

```
error: line 8: cannot mutate through immutable binding "c" (declare it with `let mut`)
```

This is enforced today — dynamically, like the other rules the checker
will eventually make static. It closes the loophole that would otherwise make immutability a lie.
Without it, `let` would mean "you cannot reassign this name, but anyone
can gut the object through a method" — which is what `final` means in
Java and `const` means for a JavaScript object.

Mutability is **transitive through paths**: `a.b.c` is assignable only
if `a` is `mut`.

**Enforcement gap:** the interpreter does not currently enforce
receiver-mut on *builtin* methods, so `xs.push(3)` works through a
`let` binding today. `glide/DESIGN-DECISIONS.md` records this as the
checker's job, noted so nobody mistakes it for a semantic decision.
User-defined `mut self` methods *are* checked.

#### Methods on any type

`impl` is not limited to structs:

```glide
type NoteId = distinct Int

impl NoteId {
    fn describe(self) -> String { "note #{self.value()}" }
}
```

```glide
type Shape = Circle(Float) | Rect(Float, Float)

impl Shape {
    fn area(self) -> Float {
        match self {
            Circle(r)  => 3.14159 * r * r
            Rect(w, h) => w * h
        }
    }
}
```

A sum type with an `impl` block is the ordinary shape for "a closed set
of cases with shared behaviour" — and note that the method body is a
`match`, which the compiler checks for exhaustiveness. Adding a variant
breaks the method, which is what you want.

#### Free functions versus methods

There is no rule forcing behaviour into methods. A method is
appropriate when the function is *about* the type and belongs to its
API; a free function is appropriate when it relates several types or
belongs to the caller's domain.

```glide
impl Note {
    fn word_count(self) -> Int { … }        // about a Note
}

fn merge(a: Note, b: Note) -> Note { … }    // about two Notes
fn render_html(n: Note) -> String { … }     // about presentation
```

#### Method values ○

```glide
let f = counter.bump        // ○ not implemented
```

Taking an unapplied method as a closure over its receiver is designed
and not built. Write `|| counter.bump()` today.

---

### 2. Under the Hood

#### Dispatch

Method calls on a concrete type are **statically dispatched** in the
designed compiler — the target is known at compile time and the call
can be inlined. There is no vtable involved unless you are going
through a trait object (`any Shape`, Chapter 17), which is spelled
differently precisely so the cost is visible.

In the interpreter, dispatch is on the value's *runtime* type: the
evaluator looks up the method name in a table registered by the `impl`
block. Anything not found is a runtime error ("X has no method …").

#### Where `impl` blocks live

Registered at load time, per type name, order-independent. Several
`impl` blocks for the same type merge. A trait `impl` block
(`impl Shape for Circle`) registers its methods the same way and
additionally records the conformance.

#### How `mut self` is checked

At the call site, not inside the method. The evaluator walks the
assignment path back to its **root binding** and requires that binding
to be `mut`. So `a.b.c.bump()` requires `a` to be `mut` — mutability is
a property of the path you reached the object through, not of the
object.

That is the honest statement of Chapter 4's recorded sacrifice, at the
method level. Two bindings can alias one object; `let` prevents
mutation through *that name*.

#### No hidden receiver copying

Go's value receivers copy the receiver on every call, which is the
source of the classic footgun: a "mutating" method with a value
receiver mutates a copy and the caller sees nothing.

Glide has no value/pointer receiver distinction, so that bug shape does
not exist. Representation — whether the receiver is passed by value or
by reference — is the compiler's business.

---

### 3. Why This Design?

#### Why one mutability axis instead of Go's two

Go's receivers conflate two questions:

```go
func (c Counter) Value() int    // value receiver
func (c *Counter) Bump()        // pointer receiver
```

The `*` answers *how is it passed?* and only implies *may it mutate?*.
Worse, Go enforces neither:

- A value receiver can "mutate" and the change silently vanishes.
- A pointer receiver need not mutate, so `*` tells you nothing
  definite.
- A method set gotcha follows: a value type does not satisfy an
  interface requiring pointer-receiver methods, which is a rule people
  learn by hitting it.

Glide separates the questions and keeps only the one that matters to
the reader. **Mutability is the declared, checked axis. Representation
is the compiler's business.**

The cost: you cannot control passing convention. That is deliberate —
it is a performance concern, and the language's position is that
performance concerns get explicit opt-ins (arenas, `wrapping_*`) rather
than being smuggled into a semantic annotation.

#### Why methods take no call-site `mut` marker

This is asymmetric with free functions, deliberately.

A free function that mutates a parameter declares it *and* the call
site repeats the marker (○):

```glide
fn sort_desc(mut xs: List<Int>)
sort_desc(mut xs)
```

A method does not:

```glide
xs.push(3)          // not `mut xs.push(3)`
```

The reasoning in `DESIGN.md`: method names carry intent. `push`,
`clear`, `insert`, and `bump` all announce mutation by being verbs on
the receiver. Marking every receiver would be noise, and noise trains
people to ignore markers — which destroys the value of the markers that
matter.

Rust made exactly the same compromise (`&mut self` on the declaration,
nothing at the call site) and a decade of use says it is right.

#### Why associated functions instead of constructors

`Counter.new()` is just a function in the `impl` block with no `self`.
It is not a language feature, has no special rules, and can be named
anything:

```glide
impl Config {
    fn new() -> Config { … }
    fn from_file(path: String) -> Result<Config, Error> { … }
    fn for_testing() -> Config { … }
}
```

Compare a language with constructors: they are overloaded (banned
here), they cannot fail without exceptions (banned here), they cannot
return a subtype or a cached instance, and they have a special name you
cannot choose.

Three named associated functions with honest names beat three
overloaded constructors, and one of them can return a `Result`.

#### Why methods are separate from the type declaration

Two reasons.

**Retroactive extension.** Because `impl` is separate, you can add
methods to a type in a different `impl` block — and, with the orphan
rule (Chapter 17), to a type from another module by implementing your
own trait for it. A type declaration that had to contain all its
methods could not do that.

**Data and behaviour read separately.** A `type` declaration is a
data-shape statement you can take in at a glance. Interleaving twenty
methods with it makes the shape hard to find. This is the same instinct
as Go's separate method declarations.

#### Why no inheritance

`DESIGN.md` is blunt: inheritance is the feature every ecosystem
regrets by year five. Fragile base classes, diamond problems, "is-a"
taxonomies that stop fitting the domain after the third requirement
change.

Its legitimate uses split cleanly in two:

- **Sharing data** → composition. A struct containing a struct.
- **Sharing behaviour / polymorphism** → traits (Chapter 17), including
  default methods, which cover the "base class with some implemented
  methods" case.

There is no third thing inheritance does that is not covered.

Note that Glide also declines Go's **embedding**, which is
inheritance's method-promotion half without the type hierarchy.
Embedding makes the promoted methods invisible at the use site, and
that invisibility is the cost the whole language is trying to avoid.

---

### 4. Competing Approaches

**Go.** Methods declared at package level with a receiver; value versus
pointer receivers conflating mutability and passing; no constructors
(the `NewX` convention); embedding for method promotion; interfaces
satisfied implicitly. Glide keeps methods-outside-the-type and rejects
the receiver conflation, the embedding, and the implicit interfaces.

**Rust.** `impl` blocks, `self`/`&self`/`&mut self`, associated
functions, `Self` type, no inheritance. Glide is Rust's model with the
reference machinery removed: where Rust distinguishes `self` (consumes)
from `&self` (borrows) from `&mut self` (mutably borrows), Glide has
`self` and `mut self`, because the GC makes ownership a non-question.

**Swift.** Methods inside the type declaration, `mutating func` for
value types — the direct ancestor of `mut self`. Swift also has
classes with inheritance alongside structs, and the community guidance
is "prefer structs", which is Glide's position made mandatory.

**Java / C#.** Classes with methods inside, constructors as a language
feature, inheritance, `this` implicit. The `final`/`readonly` keywords
prevent reassignment but not mutation through methods, which is exactly
the loophole `mut self` closes.

**Python.** Methods inside the class, explicit `self`, no mutability
distinction at all, and multiple inheritance with an MRO algorithm most
users cannot recite.

**C++.** `const` member functions are the closest analogue to
`fn value(self)` — and `const` correctness in C++ is widely regarded as
valuable and painful. Glide's version is the same idea inverted:
immutable is the default and `mut` is marked, so the annotation appears
on the rare case rather than the common one.

---

### 5. Common Mistakes

**Forgetting `mut` on the binding.**

```glide
// Bad — error: cannot mutate through immutable binding "c"
let c = Counter.new()
c.bump()

// Good
let mut c = Counter.new()
c.bump()
```

**Declaring `mut self` on a method that does not mutate.** It works and
it lies: every caller now needs a `mut` binding for no reason. `mut
self` is part of the API contract.

**Expecting a value-receiver copy.** There is no such thing. A method
that takes `self` cannot mutate; a method that takes `mut self`
mutates the actual object. There is no third case where you get a copy
to scribble on — if you want that, take `self` and return a new value:

```glide
impl Config {
    fn with_port(self, port: Int) -> Config {
        Config{ port: port, ..self }
    }
}
```

**Reaching for inheritance.** There is none. If two types share
behaviour, that is a trait with a default method. If two types share
data, that is composition — a field.

**Reaching for embedding.** Also none. Write the delegating method:

```glide
// There is no embedding. Delegate explicitly.
type Server = struct {
    logger: Logger
    port: Int
}

impl Server {
    fn log(self, msg: String) { self.logger.log(msg) }
}
```

Three extra lines, and the reader can see where `log` goes.

**Putting everything in methods.** A function that relates two values
of a type, or that belongs to the caller's concern rather than the
type's, is a free function. Method-ifying everything produces the
`OrderService.processOrderForUser(order, user)` shape that object
orientation is criticised for.

**Taking a method value.** `let f = c.bump` is ○. Use `|| c.bump()`.

**Assuming `impl` on a foreign type works.** You may add inherent
methods to types your module declares. Adding methods to another
module's type requires a trait and is subject to the orphan rule
(Chapter 17).

---

### 6. Performance Considerations

**Static dispatch, always, for concrete types** (○). A method call on a
known type is a direct call and is inlinable. Dynamic dispatch happens
only through `any Trait`, which is spelled differently so the cost is
visible at the use site — unlike Go, where every interface value boxes
and dispatches invisibly.

**No receiver copying.** Go's value receivers copy the receiver on
every call, which for a large struct in a hot loop is a real cost that
looks free. Glide's compiler chooses.

**`mut` costs nothing at runtime.** It is a compile-time property.

**Struct-update methods allocate.** `fn with_port(self, port: Int) ->
Config` copies the whole struct. For small structs that is a register
shuffle; for large ones in a loop it is not, and that is where a `mut
self` method is the right answer.

**In the interpreter**, method dispatch is a name lookup in a
per-type table, and field access inside the method is a map lookup.
Both are tree-walker costs.

---

### 7. Best Practices

**Give every type a named constructor, and make the fields private.**

```glide
type Port = struct { n: Int }

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

Private fields plus a validating associated function means every `Port`
in the program is valid. Chapter 12 called this parse-don't-validate;
the constructor is where it lives.

**Default to `self`; use `mut self` only for genuine state changes.**

```glide
// Good
impl Cart {
    fn total(self) -> Cents { … }              // read
    fn add(mut self, item: Item) { … }         // genuine mutation
    fn with_coupon(self, c: Coupon) -> Cart { … }  // value-style update
}
```

The three shapes are: read (`self`, returns something), mutate
(`mut self`, returns nothing), and transform (`self`, returns a new
value). Mixing them — a `mut self` method that also returns a value —
is usually a sign that two operations are tangled.

**Name mutating methods as verbs, reading methods as nouns.**

```glide
// Good
fn len(self) -> Int
fn is_empty(self) -> Bool
fn push(mut self, v: T)
fn clear(mut self)

// Bad — reads like a query, mutates
fn size(mut self) -> Int
```

Because there is no call-site marker, the method name is the only
signal a reader has. That makes naming load-bearing rather than
stylistic.

**Prefer returning a new value for small types, mutating for
accumulators.**

```glide
// Good — Config is small and value-like
let prod = Config{ host: "prod", ..base }

// Good — a builder accumulates
let mut sb = StringBuilder.new()      // ○
sb.push("a")
```

**Put the `match` inside the method for sum types.**

```glide
// Good
impl Shape {
    fn area(self) -> Float {
        match self {
            Circle(r)  => 3.14159 * r * r
            Rect(w, h) => w * h
        }
    }
}
```

The alternative — a free function taking a `Shape` — works identically,
but the method form means the exhaustiveness break lands next to the
type when a variant is added.

**Keep `impl` blocks near their type, and split them by concern.**
Several `impl` blocks are legal. Grouping "construction", "queries",
and "mutation" into three blocks with a comment each is a real
readability win for a large type.

---

### 8. Examples

**A complete small type, showing all three method shapes:**

```glide
type Stack = struct {
    items: List<Int>
}

impl Stack {
    // Construction
    fn new() -> Stack { Stack{ items: [] } }

    fn of(items: List<Int>) -> Stack { Stack{ items: items } }

    // Queries — `self`
    fn len(self) -> Int { self.items.len() }

    fn is_empty(self) -> Bool { self.items.len() == 0 }

    fn peek(self) -> Int? {
        if self.items.len() == 0 {
            None
        } else {
            Some(self.items[self.items.len() - 1])
        }
    }

    // Mutation — `mut self`
    fn push(mut self, v: Int) {
        self.items.push(v)
    }

    // Transformation — `self`, returns a new value
    fn reversed(self) -> Stack {
        let mut out = []
        for i in 0..self.items.len() {
            out.push(self.items[self.items.len() - 1 - i])
        }
        Stack{ items: out }
    }
}

fn main() {
    let mut s = Stack.new()
    s.push(1)
    s.push(2)
    s.push(3)
    println(s.len())              // 3
    println("{s.peek():?}")       // 3
    println("{s.reversed().peek():?}")   // 1
    println("{s.peek():?}")       // 3 — reversed() did not mutate
    println(Stack.new().is_empty())      // true
}
```

```
3
3
1
3
true
```

Note `peek` returning `Int?` rather than panicking on an empty stack —
Chapter 14's rule, applied. And note that `reversed()` leaves `s`
alone, which the reader can tell from the signature alone: it takes
`self`, not `mut self`.

**A sum type with behaviour:**

```glide
type Temp = Celsius(Float) | Fahrenheit(Float) | Kelvin(Float)

impl Temp {
    fn to_celsius(self) -> Float {
        match self {
            Celsius(c)    => c
            Fahrenheit(f) => (f - 32.0) * 5.0 / 9.0
            Kelvin(k)     => k - 273.15
        }
    }

    fn describe(self) -> String {
        let c = self.to_celsius()
        let label = match {
            c < 0.0   => "freezing"
            c < 15.0  => "cold"
            c < 25.0  => "mild"
            _         => "hot"
        }
        "{c:.1}C ({label})"
    }
}

fn main() {
    for t in [Temp.Celsius(20.0), .Fahrenheit(100.0), .Kelvin(250.0)] {
        println(t.describe())
    }
}
```

```
20.0C (mild)
37.8C (hot)
-23.1C (freezing)
```

`to_celsius` is the single place the conversion knowledge lives, and
adding a `Rankine` variant breaks exactly that one `match`.

**Bad versus good: the mutability lie**

```glide
// Bad — `total` looks like a query and mutates
impl Cart {
    fn total(mut self) -> Cents {
        self.cached_total = self.compute()
        self.cached_total
    }
}
```

Every caller of `total()` now needs a `mut` binding, which propagates
outward: a function that only reads a cart must take it mutably, and so
must *its* caller. A read that requires write access is contagious.

```glide
// Good — separate the concerns
impl Cart {
    fn total(self) -> Cents {
        self.compute()
    }
}

// Or, if caching genuinely matters, make it explicit
impl Cart {
    fn recompute(mut self) { self.cached_total = self.compute() }
    fn total(self) -> Cents { self.cached_total }
}
```

The second version makes the caching visible in the API, which is
correct: a cache that can be stale is something the caller needs to
know about.

---

### 9. Summary & Exercises

**Summary**

- Methods live in `impl` blocks, separate from the `type` declaration.
  A type may have several, and they may span files in a module.
- **Associated functions** take no `self` and are called on the type:
  `Counter.new()`. That is the constructor idiom — no language-level
  constructors, so they can be named, can fail, and can be several.
- **Receiver mutability is declared.** `fn value(self)` is read-only;
  `fn bump(mut self)` may mutate and is callable only through a `mut`
  path. This is what stops `let` from being a lie.
- One axis, one annotation. Go's value-versus-pointer receivers
  conflate mutability with passing convention and enforce neither;
  representation here is the compiler's business.
- **Methods take no call-site marker** (`xs.push(3)`), asymmetrically
  with free `mut` parameters (○, `sort(mut xs)`), because method names
  carry intent and universal markers train people to ignore markers.
- Mutability is **transitive through paths**: `a.b.c` needs `a` to be
  `mut`. It is a path property, not an object guarantee.
- `impl` works on structs, sum types, and `distinct` types alike.
- **There is no inheritance and no embedding.** Composition holds data;
  traits (with default methods) hold shared behaviour.
- ○: method values (`x.method` unapplied). Enforcement gap: builtin
  methods do not check receiver-mut yet.

**Exercises**

1. **Find the value-receiver bug.** In a Go codebase, find a method
   with a value receiver that assigns to a field of the receiver. If
   there is none, find one with a pointer receiver that does not
   mutate, and ask what the `*` was telling the reader. Then write out
   what each would look like in Glide and which question the annotation
   answers.

2. **Split a mutating query.** Find a method in your code that both
   reads and mutates — a lazy getter, a cache-filling accessor, a
   counter-incrementing lookup. Split it. Then trace how far the `mut`
   requirement had propagated through callers before the split. That
   propagation distance is the cost of the lie.

3. **Replace an inheritance hierarchy.** Take a three-level class
   hierarchy you know and re-model it with composition plus traits. For
   each method in the base class, decide whether it is shared data (a
   field), shared behaviour (a trait default method), or per-type
   behaviour (a required trait method). Then note which parts of the
   hierarchy existed only for code reuse and which expressed a genuine
   "is-a" — the ratio is usually the argument.
