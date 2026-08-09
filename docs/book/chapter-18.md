# Chapter 18: Generics

**Generics** let you write one piece of code that works for many types
without giving up type safety. A `List<Int>` and a `List<String>` share
one implementation; a `largest` function works on anything orderable;
a `Stack<T>` is written once.

If you have used Java or C# generics, or C++ templates, you know the
idea and can skim section 1. If your background is Go before 1.18, or C
with `void*`, the alternatives you have lived with are: write the code
N times, or throw the types away and cast. Generics are the third
option.

Glide's generics are **monomorphised**, **inferred at call sites**, and
constrained by **inline trait bounds**. There is no turbofish.

This is the chapter with the largest gap between design and
implementation. Generic *syntax* parses and generic code runs, but
nothing is checked and nothing is specialised — everything is dynamic
today. Read the ✓ examples as "this runs"; read the type-safety claims
as ○.

---

### 1. Basic Usage

#### Generic functions

```glide
fn largest<T: Ord>(xs: List<T>) -> T? {
    if xs.len() == 0 { return None }
    let mut best = xs[0]
    for x in xs {
        if x > best { best = x }
    }
    Some(best)
}
```

`<T: Ord>` after the function name introduces a **type parameter** `T`,
**bounded** by the `Ord` trait. Read it as: "for any type `T` that can
be ordered".

The bound is not decoration. Inside the body you may use only what
`Ord` provides — that is what makes `x > best` legal. Without the
bound, `T` could be anything, including types with no ordering, and the
comparison would be rejected at the *declaration* (○) rather than at
some distant call site.

Calling requires no type arguments:

```glide
println("{largest([3, 1, 4, 1, 5]):?}")     // 5

let ls = largest(["b", "a"])
println("{ls:?}")                            // "b"
```

`T` is inferred from the argument. **There is no turbofish** — you
never write `largest::<Int>(xs)`.

Explicit type arguments are legal in the rare case inference cannot
work:

```glide
let cfg = parse<Config>(text)     // ○
```

#### Generic types

```glide
type Pair<A, B> = struct {
    pub first: A
    pub second: B
}

impl Pair<A, B> {
    fn new(a: A, b: B) -> Pair<A, B> {
        Pair{ first: a, second: b }
    }

    fn swap(self) -> Pair<B, A> {
        Pair{ first: self.second, second: self.first }
    }
}
```

```glide
fn main() {
    let p = Pair.new(1, "one")
    println("{p:?}")            // Pair{ first: 1, second: "one" }
    println("{p.swap():?}")     // Pair{ first: "one", second: 1 }
}
```

Note `swap` returns `Pair<B, A>` — the parameters are reordered, and
the type system tracks it.

#### A generic container

```glide
type Stack<T> = struct {
    items: List<T>
}

impl Stack<T> {
    fn new() -> Stack<T> { Stack{ items: [] } }

    fn push(mut self, v: T) {
        self.items.push(v)
    }

    fn pop(mut self) -> T? {
        if self.items.len() == 0 { return None }
        let last = self.items[self.items.len() - 1]
        let mut rest = []
        for i in 0..self.items.len() - 1 {
            rest.push(self.items[i])
        }
        self.items = rest
        Some(last)
    }
}
```

```glide
let mut s = Stack.new()
s.push(1)
s.push(2)
println("{s.pop():?}")      // 2
println("{s.pop():?}")      // 1
println("{s.pop():?}")      // None
```

`Stack.new()` infers `T` from what gets pushed. `pop` returns `T?`
rather than panicking on empty — Chapter 14's rule.

#### Bounds

Bounds are **inline colon bounds**, and multiple bounds combine with
`+`:

```glide
fn dedupe<T: Ord + Hash>(xs: List<T>) -> List<T> { … }      // ○ Hash
```

Unconstrained is bare `<T>`:

```glide
fn first<T>(xs: List<T>) -> T? { … }
```

Note there is no `[T any]` ceremony. Go requires it because bare `[T]`
would collide with array sizes; Glide's angle brackets have no such
collision.

**There is no `where` clause** in v0. Two ways to write bounds violates
the house rules; it will be added only if nested bounds make signatures
genuinely unreadable.

#### The built-in generic types

You have been using generics since Chapter 11 without the word:

```glide
List<T>
Map<K, V>
Option<T>        // spelled T?
Result<T, E>
Iterator<T>
```

They are ordinary generic types that happen to be built in.

#### Const generics ○

`Matrix<T, const N>` — deferred. Real machinery for numeric-library
needs Glide does not have. A sized array `[N]T` (○) needs only a
comptime-constant `N`, which comptime provides.

---

### 2. Under the Hood

#### Monomorphisation ○

The designed compiler **monomorphises**: for each concrete type a
generic is used with, it generates a separate specialised copy.
`largest([1, 2, 3])` and `largest(["a", "b"])` compile to two distinct
functions, each with the comparison inlined for its type.

The consequences:

- **Zero abstraction cost.** A `Stack<Int>` is exactly as fast as a
  hand-written integer stack. No boxing, no dynamic dispatch, no type
  tags at runtime.
- **Larger binaries and slower compiles.** N instantiations means N
  copies of the code.

This is Rust's and C++'s model. It is *not* Java's, which erases type
parameters and boxes everything, and it is not Go's, which uses a
hybrid (GC-shape stenciling with dictionaries) that gets neither
extreme.

#### What actually happens today

Type parameters and bounds are **parsed into the AST** as of M4a — up
to that point the parser skipped the `<…>` list to find its closing
bracket and threw the contents away, so nothing could have checked a
bound because nothing recorded one.

There is still no specialisation — the interpreter runs generics
type-erased — but there is now checking.

M4c made the bounds real. They are verified **at the declaration** — a
generic function's body is checked once against its bounds, so callers
get the error at the call site instead of from deep inside the callee:

```
error: line 3: Blob does not implement Ord, required by T
```

And a bound is the **complete** method set. On a `T: Ord`, `cmp`
resolves through the bound and anything else is a compile error:

```
error: line 1: T has no method "frobnicate": it is bounded by Ord,
       which does not declare one
```

That is what a bound is *for*. Without it the annotation is
decoration, and a typo inside a generic body waits until runtime.

An **unbounded** `<T>` is the deliberate opposite: fully opaque, no
diagnostics, because the checker genuinely knows nothing about it and
anything it said would be a guess. So the two spellings mean different
things — `<T>` says "any type, and I will not touch it"; `<T: Ord>`
says "any ordered type, and here is exactly what I may do".

One piece is still missing, and it is worth knowing which: **operators**
on a bounded `T`. `a < b` where `T: Ord` passes unchecked, because that
needs operator traits, which are designed and not yet built. Methods
are checked; operators wait.

Note the *interpreter* needs no monomorphisation for any of this: it
runs generics type-erased and still enforces every rule statically.
Specialisation is the compiled tier's concern, and the two tiers accept
exactly the same programs regardless.

#### Parsing `<` without a turbofish

The classic problem: is `f<T>(x)` a generic call, or is it
`(f < T) > (x)`?

Rust solved it by requiring `f::<T>(x)` in expression position — the
turbofish, universally regarded as a wart. Glide takes C# and
TypeScript's twenty-year-old solution instead: after an identifier
followed by `<`, tentatively parse a type-argument list, and commit
based on what follows the closing `>`. The lexer splits `>>` in type
context (the C++11 fix).

The parser pays a small, bounded cost once. Go's choice of square
brackets makes every *reader* pay forever.

And the ambiguity barely arises in practice: declarations are never
ambiguous (`<` follows the declared name), and with local inference,
explicit type arguments in expressions are rare.

#### Bounds are checked at the declaration ○

This is the important difference from C++ templates and Zig's comptime.

With **declaration-site checking**, a generic function's body is
verified once against its bounds. If you use an operation the bound
does not provide, the error is at the declaration, pointing at your
code. Callers get a simple "your `T` does not implement `Ord`" at the
call site.

With **use-site checking** (C++ templates before concepts, Zig
comptime), the body is re-checked per instantiation, so an error
surfaces deep inside the callee with a stack of template expansions.
This is the source of C++'s legendary error messages.

`DESIGN.md` makes this a hard fence: **comptime is not a second
generics system.** No user-written functions that take or return types.
`List<T>` comes from trait-bounded generics, never from a comptime
function. Zig had no choice — comptime is all they have. Glide does,
and without the fence, hard generic bounds get "solved" by escaping to
comptime and the ecosystem ends up duck-typed and undiagnosable.

---

### 3. Why This Design?

#### Why angle brackets and not Go's square brackets

Go chose `[T]` for parser convenience. Glide chooses `<T>` for the
human, and the argument is the visibility pillar.

Square brackets already mean indexing, and the collision hits
**readers**, not just parsers. In Go, `a[T]` is element access *or*
type instantiation depending on what `a` is — invisible at a glance.
Glide has spent whole sections making kinds and costs visible at the
use site; type application looking identical to indexing runs the wrong
way.

`<T>` is unambiguously "types here" to every programmer alive.

The parser cost is real, small, paid in exactly one place, and proven
for two decades in C# and TypeScript.

#### Why monomorphisation

Three models exist:

**Erasure** (Java): one implementation, type parameters erased at
runtime, everything boxed. `List<Integer>` boxes every int. You cannot
ask a `List<T>` what `T` is. Cheap compiles, permanent runtime tax.

**Monomorphisation** (C++, Rust, Glide): specialised code per
instantiation. Zero runtime cost, larger binaries, slower compiles.

**Hybrid** (Go 1.18+): GC-shape stenciling — one copy per "shape" of
type, with a runtime dictionary for the differences. It avoids code
bloat and reintroduces indirect calls, so Go generics are frequently
*slower* than the equivalent interface-based or hand-written code, which
surprises people.

Glide picks monomorphisation because the "human-friendly and performant
are both hard requirements" principle says abstraction should not cost
anything at runtime. The compile-time cost is real and is why "compile
speed is a feature" is also a principle — the dev-tier backend can
generate unoptimised instantiations quickly.

#### Why no turbofish

Because it is a wart, and because there is a twenty-year-old fix.

Rust's `::<>` exists to disambiguate `<` in expression position. It
appears in real code often enough to be a recognised annoyance and
often enough that "turbofish" is a word. Glide's `DESIGN.md` says
plainly: **no turbofish, ever** — the angle-bracket section exists to
avoid it.

#### Why inline bounds and no `where` clause

`T: Ord + Hash` reads well and sits where the parameter is introduced.
A `where` clause is a second place bounds can live, which violates "one
way to do it".

The counter-argument is that complex bounds make signatures
unreadable — which is true in Rust, where lifetimes and associated-type
constraints pile up. Glide has no lifetimes and defers associated
types, so the pressure is much lower. `DESIGN.md` records: add `where`
only if nested bounds make signatures genuinely unreadable.

#### Why generics and not comptime-as-generics

Zig generates types with comptime functions: `fn List(comptime T: type)
type`. It is elegant, and it is why Zig's error messages for a
misused generic look like C++'s — the failure happens deep inside the
callee body during instantiation.

The fence — no user functions taking or returning types — is what buys
declaration-site checking and therefore good error messages. It costs
expressiveness that Glide is content to lose.

#### Why generics *and* traits *and* sum types

They solve three different problems, and confusing them is the most
common design mistake in this area:

| You have | You want | Use |
|---|---|---|
| Many types, same code, known at compile time | Zero-cost reuse | **Generics** |
| Many types, shared capability, open set | Extension by third parties | **Traits** |
| Few types, closed set, known now | Exhaustiveness checking | **Sum types** |

Generics answer "the same algorithm for any type". Traits answer "what
can I do with this type". Sum types answer "which of these things is
it". A `Stack<T>` is generic; `Ord` is a trait; `Shape = Circle | Rect`
is a sum type. They compose: `fn largest<T: Ord>` is a generic function
with a trait bound returning a sum type (`Option<T>`).

---

### 4. Competing Approaches

**Go (1.18+).** `[T any]`, hybrid GC-shape stenciling, no methods with
extra type parameters, no operator constraints beyond the built-in
`comparable` and `constraints` package. Go resisted generics for
thirteen years specifically to avoid compile-time and complexity costs,
and the result is deliberately constrained. The square brackets and the
mandatory `any` are the two things Glide changes.

**Rust.** `<T: Trait>`, monomorphisation, `where` clauses, associated
types, blanket impls, the turbofish. Glide takes the model and declines
the turbofish, `where`, and (for now) associated types.

**C++.** Templates — the most powerful and least diagnosable. Duck
typing at compile time, instantiation-time errors, and template
metaprogramming as an accidental Turing-complete sub-language. Concepts
(C++20) are the belated addition of declaration-site constraints,
arriving at where Glide starts.

**Java.** Erasure, so `List<String>` and `List<Integer>` are the same
class at runtime, everything is boxed, and you cannot write `new T[]`.
The trade bought backward compatibility with pre-generics code and cost
performance permanently. Project Valhalla is the multi-decade attempt
to undo it.

**C#.** Reified generics with runtime type information — you *can* ask
what `T` is. Value types are specialised (fast); reference types share
one implementation (compact). Arguably the best-engineered mainstream
compromise, and it costs a runtime that carries generic metadata, which
Glide's no-reflection stance rules out.

**Zig.** Comptime functions taking and returning types. Maximally
expressive, no separate generics system, and C++-tier error messages.
The thing Glide's fence exists to avoid.

**Python / JavaScript.** Duck typing — no generics needed, no checking
either. `typing.Generic` and TypeScript's generics are the retrofits,
and TypeScript's are genuinely excellent (structural, inference-heavy,
erased).

---

### 5. Common Mistakes

**Expecting the bounds to be checked today.** They are not. A generic
function will happily do anything to its `T` at the current tier.
Write the bounds anyway.

**Reaching for generics when a trait is the answer.**

```glide
// Questionable — T appears once, so nothing is being related
fn draw<T: Shape>(s: T) { … }

// Same thing, dynamically dispatched, when you want a collection
fn draw(s: any Shape) { … }        // ○
```

A type parameter earns its place when it appears **more than once** in
the signature — tying an argument to a return type, or two arguments to
each other. A generic that mentions `T` exactly once is usually a trait
bound in disguise, and the honest question is whether you want static
or dynamic dispatch.

**Reaching for generics when a sum type is the answer.** If the set of
types is closed and you want exhaustiveness, generics give you neither.

**Over-parameterising.**

```glide
// Bad — three parameters, and the caller must satisfy all of them
fn process<T: Ord, U: Display, V: Hash>(a: T, b: U, c: V) -> String

// Usually better
fn process(a: SortKey, b: Label, c: CacheKey) -> String
```

Every type parameter is a decision the caller must make and a
constraint they must satisfy. Add one when the code genuinely works for
many types, not because it might one day.

**Writing a generic container that duplicates `List`.** `List<T>` and
`Map<K, V>` already exist and are optimised. A `Stack<T>` wrapping a
`List<T>` is fine when the *API* is the point (push/pop, no indexing);
it is waste when you are re-implementing a list.

**Assuming binary size does not matter.** Monomorphisation means a
generic function used with twenty types produces twenty copies. For a
large generic function this is real. The mitigation is the standard
one: keep the generic *shell* small and delegate the bulk to a
non-generic inner function.

**Expecting a turbofish.** There is none. If inference genuinely
cannot determine `T`, use an annotation on the binding or an explicit
type argument in the rare designed form (`parse<Config>(s)`).

---

### 6. Performance Considerations

**Monomorphised generics are free at runtime** (○). A `Stack<Int>` has
the same code as a hand-written integer stack: no boxing, no type tags,
no dynamic dispatch, and full inlining.

This is the single biggest difference from Java, where every
`List<Integer>` boxes each element into a heap object, and from Go's
generics, which insert dictionary lookups for non-pointer-shaped types.

**The costs are compile time and binary size.** N instantiations means
N copies of the generated code. Two mitigations:

```glide
// The generic shell is tiny; the work is monomorphisation-free
fn sort_by_key<T, K: Ord>(xs: List<T>, key: fn(T) -> K) {
    sort_impl(xs, |a, b| key(a).cmp(key(b)))    // one shared implementation
}
```

and simply not making things generic that do not need to be.

**Trait bounds cost nothing** in a generic context — the call is
static and inlinable. The cost appears only with `any Trait`
(Chapter 17), which is spelled differently for exactly that reason.

**In the interpreter**, generics have no cost and no benefit: nothing
is specialised, everything is dynamic. Benchmarks here say nothing
about the compiled tier.

**Compare the three models on `List<Int>` holding a million integers:**

| Model | Storage |
|---|---|
| Java (erasure) | 1M boxed `Integer` objects + 1M pointers |
| Go (hybrid) | 1M ints, dictionary indirection on generic operations |
| Glide / Rust (mono) | 1M ints, direct code |

That table is why the decision was made.

---

### 7. Best Practices

**Use a type parameter only when it appears more than once.**

```glide
// Good — T ties input to output
fn first<T>(xs: List<T>) -> T?

// Good — T ties two arguments together
fn max<T: Ord>(a: T, b: T) -> T

// Suspicious — T appears once; this is a trait bound
fn log<T: Display>(v: T)
```

The last one is fine, and it is worth *noticing* that what you wanted
was "anything displayable", which is a statement about capability.

**Bound only what you use.** Every bound is a requirement on callers.
If the body only compares, bound `Ord` and not `Ord + Hash + Display`.

**Name type parameters meaningfully once there is more than one.**
`T` is fine alone. `<K, V>` beats `<T, U>` for a map. `<T, E>` beats
`<A, B>` for a result-shaped type. Single capitals are the convention;
meaningless single capitals are not.

**Keep generic functions small; delegate the bulk.** This limits code
bloat and compile time, and it usually improves the design anyway.

**Prefer generic *functions* over generic *types* where you can.** A
generic function is used at one call site and disappears. A generic
type propagates through every signature that mentions it, and changing
its parameters is a breaking change everywhere.

**Do not build a generics-heavy abstraction speculatively.** The
cheapest thing in this language is to write the concrete version twice
and generalise on the third. `DESIGN.md`'s whole posture — add a
feature when a program needs it — applies to your own code too.

**Write the bounds now even though nothing checks them.** They are the
specification, they are documentation, and the checker will read them.
A codebase full of unbounded `<T>` will be miserable to bring under the
checker.

---

### 8. Examples

**A generic pair, showing type-parameter tracking:**

```glide
type Pair<A, B> = struct {
    pub first: A
    pub second: B
}

impl Pair<A, B> {
    fn new(a: A, b: B) -> Pair<A, B> {
        Pair{ first: a, second: b }
    }

    fn swap(self) -> Pair<B, A> {
        Pair{ first: self.second, second: self.first }
    }
}

fn main() {
    let p = Pair.new(1, "one")
    println("{p:?}")
    println("{p.swap():?}")
}
```

```
Pair{ first: 1, second: "one" }
Pair{ first: "one", second: 1 }
```

`swap` returns `Pair<B, A>`, not `Pair<A, B>`. That is the sort of
thing a type system tracks for free and a comment does not.

**A bounded generic function:**

```glide
fn largest<T: Ord>(xs: List<T>) -> T? {
    if xs.len() == 0 { return None }
    let mut best = xs[0]
    for x in xs {
        if x > best { best = x }
    }
    Some(best)
}

fn main() {
    println("{largest([3, 1, 4, 1, 5]):?}")

    let ls = largest(["b", "a", "c"])
    println("{ls:?}")

    let empty: List<Int> = []
    println("{largest(empty):?}")
}
```

```
5
"c"
None
```

Three call sites, three inferred `T`s, no type arguments written. Note
the empty case needs an annotation — an empty literal gives inference
nothing to work with, exactly as with `[:]` in Chapter 11.

**A generic container with a real API:**

```glide
type Stack<T> = struct {
    items: List<T>
}

impl Stack<T> {
    fn new() -> Stack<T> { Stack{ items: [] } }

    fn len(self) -> Int { self.items.len() }

    fn push(mut self, v: T) { self.items.push(v) }

    fn pop(mut self) -> T? {
        if self.items.len() == 0 { return None }
        let last = self.items[self.items.len() - 1]
        let mut rest = []
        for i in 0..self.items.len() - 1 {
            rest.push(self.items[i])
        }
        self.items = rest
        Some(last)
    }
}

fn main() {
    let mut s = Stack.new()
    s.push("a")
    s.push("b")
    println(s.len())
    println("{s.pop():?}")
    println("{s.pop():?}")
    println("{s.pop():?}")
}
```

```
2
"b"
"a"
None
```

The API is the point: push and pop, no indexing, no iteration in the
middle. That is a legitimate reason to wrap a `List`; "I want a growable
sequence" is not.

**Bad versus good: the premature abstraction**

```glide
// Bad — generic over things that are never varied
type Repository<T, K, E, C> = struct {
    conn: C
    …
}

fn find<T, K, E, C>(r: Repository<T, K, E, C>, k: K) -> Result<T?, E>
```

Four type parameters, and in the entire codebase there is one
instantiation. Every signature that mentions a `Repository` now carries
four parameters that never vary, every call site must satisfy four
constraints, and the error messages are four times longer.

```glide
// Good — concrete until there is a second case
type NoteRepository = struct {
    db: Db
}

fn find(r: NoteRepository, id: NoteId) -> Result<Note?, Error>
```

When the second entity arrives, *then* generalise — and probably to
one parameter, not four.

**Bad versus good: the generic that should be a trait**

```glide
// Bad — T appears once; this is asking for a capability
fn render_all<T: Display>(items: List<T>) -> String { … }
// …and it cannot take a mixed list at all.
```

```glide
// Good, if the list is homogeneous — the generic is right
fn render_all<T: Display>(items: List<T>) -> String { … }

// Good, if the list is mixed — you want dynamic dispatch, said out loud
fn render_all(items: List<any Display>) -> String { … }      // ○
```

The two versions differ in cost and capability, and Glide makes you
choose in the signature. Go would give you only the second, silently.

---

### 9. Summary & Exercises

**Summary**

- Generics let one implementation serve many types with full type
  safety. `fn largest<T: Ord>(xs: List<T>) -> T?`,
  `type Pair<A, B> = struct { … }`.
- Bounds are **inline colon bounds** (`T: Ord + Hash`); unconstrained
  is bare `<T>`; there is no `[T any]` ceremony and **no `where`
  clause** in v0.
- Type arguments are **inferred at call sites**. **There is no
  turbofish** — the `f<T>(x)` ambiguity is solved with C#/TypeScript's
  twenty-year-old tentative-parse rule.
- **Angle brackets, not square**, because `a[T]` in Go is indexing or
  instantiation depending on context, and that invisibility runs
  against the whole visibility pillar.
- **Monomorphisation** (○): a specialised copy per concrete type. Zero
  runtime cost, larger binaries, slower compiles. Not Java's erasure
  (boxes everything), not Go's hybrid (dictionary indirection).
- **Bounds are checked at the declaration** (○), so errors point at
  your code rather than deep inside a callee — the C++ template
  problem, avoided.
- **Comptime is not a second generics system.** No user functions
  taking or returning types. That fence is what buys the good error
  messages.
- Generics, traits, and sum types answer three different questions:
  same code for any type; what can I do with this type; which of these
  is it.
- ○ status is broad here: syntax parses and code runs, but nothing is
  checked or specialised yet. Write the bounds anyway.

**Exercises**

1. **Measure erasure.** In Java or C#, create a `List` of a million
   integers and measure the memory. Then do the same with a primitive
   array. The ratio is what erasure costs and what monomorphisation
   avoids — and it is the reason Project Valhalla exists.

2. **Find the parameter that never varies.** In a generic type from a
   codebase you know, count the distinct instantiations of each type
   parameter across the whole program. Any parameter with exactly one
   instantiation is pure overhead — remove it and see whether anything
   breaks.

3. **Decide generic, trait, or sum type — and defend it.** For each:
   a JSON value; a sorted collection; a payment processor; a retry
   policy; a database row. Two of these have defensible answers in more
   than one column; identify which, and say what would tip the
   decision. (Hint: the tipping question is almost always "who adds the
   next case, me or my caller?")
