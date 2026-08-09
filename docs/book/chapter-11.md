# Chapter 11: Lists, Maps, and Tuples

Glide has three built-in collection shapes and each has exactly one
literal syntax. There is no array-versus-slice distinction, no
capacity to reason about, no `make` versus `new`, and no nil
collection.

The two things worth reading slowly in this chapter are **reference
semantics** (which explains why `let` does not freeze a list) and **map
indexing returning an Option** (which is how "there is no null" reaches
everyday code).

Everything here is ✓. `Set`, `PList`/`PMap`, and most adapter methods
are ○ and flagged.

---

### 1. Basic Usage

#### Lists

```glide
let xs = [1, 2, 3]
println(xs.len())      // 3
println(xs[0])         // 1
```

A `List<T>` is a growable ordered sequence. There is no fixed-size
array type at the language level — the sized-array type `[N]T` is ○ and
is for layout-sensitive code, not for everyday collections.

```glide
let mut xs = [3, 1, 2]
xs.push(4)
println("{xs:?}")            // [3, 1, 2, 4]
xs.sort_by(|a, b| a.cmp(b))
println("{xs:?}")            // [1, 2, 3, 4]
println("{xs.sorted():?}")   // [1, 2, 3, 4] — a copy, original untouched
```

Mutating methods (`push`, `sort_by`, index assignment) require a `mut`
path. `sorted()` returns a new list.

Indexing out of range **panics** — it is bug territory, not an error
value:

```
error: line 3: list index 7 out of range (len 3)
```

The full method surface today:

| Method | Returns | Notes |
|---|---|---|
| `len()` | `Int` | |
| `push(v)` | `()` | append; needs `mut` |
| `sorted()` | `List<T>` | copy, ascending (Int/Float/String) |
| `sort_by(cmp)` | `()` | in place, **stable**; needs `mut`; three-way comparator |
| `repeat(k)` | `List<T>` | **shallow** — see the trap below |
| `join(sep)` | `String` | elements must all be Strings |
| `iter()` | `Iterator<T>` | |

#### Spread in list literals

```glide
let xs = [1, 2, 3]
let spread = [0, ..xs, 99]
println("{spread:?}")           // [0, 1, 2, 3, 99]

let from_range = [..(0..3)]
println("{from_range:?}")       // [0, 1, 2]
```

`..` splices any iterable — a list, a range, an iterator — at any
position. It is honestly a copy; nothing is shared.

This is the construction side of the `[first, ..rest]` pattern from
Chapter 10. Patterns are construction run backwards.

#### The `repeat` trap

```glide
let mut grid = [[0, 0]].repeat(2)
grid[0][0] = 9
println("{grid:?}")
```

```
[[9, 0], [9, 0]]
```

`repeat` is **shallow**. It repeats the *value*, and the value here is
a reference to one inner list — so both slots point at the same list.
`[0].repeat(n)` is the correct fill constructor for scalars (it is Go
1.23's `slices.Repeat`), but for nested structures you need fresh
values:

```glide
let fresh = (0..2).iter().map(|_| []).collect()
let mut f = fresh
f[0].push(1)
println("{f:?}")                // [[1], []]
```

The documentation for `repeat` says "shallow" for exactly this reason.

#### Maps

```glide
let mut m: Map<String, Int> = [:]
m["a"] = 1
m["b"] = 2

println(m.len())                 // 2
println("{m.entries():?}")       // [("a", 1), ("b", 2)]

let lit = ["x": 1, "y": 2]
println("{lit:?}")               // ["x": 1, "y": 2]
```

`[:]` is the empty map literal, and it needs an annotation because an
empty literal gives inference nothing to work with. Non-empty literals
use `["key": value, …]` — the bracket family, shared with lists,
because both are collections.

**Maps preserve insertion order.** Iteration, `entries()`, and Debug
output are all in the order keys were first inserted:

```glide
for (k, v) in m {
    print("{k}:{v} ")
}
println("")                      // a:1 b:2
```

#### Map indexing returns an Option

This is the important one:

```glide
println(m["a"] ?? -1)            // 1
println(m["z"] ?? -1)            // -1
```

`m[k]` has type `V?`, not `V`. The key might be absent, and there is no
null and no zero value, so the map cannot hand you a fake `0` and hope.

The three ways to consume it are Chapter 14's material, previewed here:

```glide
let n = m[k] ?? 0                // default

if let v = m[k] {                // both branches
    use(v)
} else {
    handle_missing()
}

let Some(v) = m[k] else {        // absence aborts
    return Err(.Missing{ key: k })
}
```

The counting idiom you will write constantly:

```glide
counts[word] = (counts[word] ?? 0) + 1
```

Note the parentheses. `??` binds loosest of all operators, so without
them this reads `counts[word] ?? (0 + 1)`.

Compare Go, where `m[k]` returns the zero value for a missing key and
you cannot distinguish "absent" from "stored zero" without the
comma-ok form. The Option removes the ambiguity and the second form.

#### Tuples

```glide
let t = (1, "two", 3.0)
println(t.0)                     // 1
println(t.1)                     // two

let (a, b, c) = t
println("{a} {b} {c}")           // 1 two 3
```

Tuples are anonymous fixed-size heterogeneous groups. Fields are
positional: `.0`, `.1`, `.2`. They need at least two elements — `(x)`
is grouping, not a one-tuple.

Tuples are how multi-return works:

```glide
fn min_max(xs: List<Int>) -> (Int, Int) {
    let s = xs.sorted()
    (s[0], s[s.len() - 1])
}

let (lo, hi) = min_max([3, 1, 4, 1, 5])
```

Unlike Go's multiple return values, a tuple is a *value*: you can store
it, put it in a list, pass it around, and pattern-match it.

**Doctrine: tuples are for transport.** Two things, briefly. Crossing
more than one API boundary, or reaching three elements, means it should
be a named struct. `DESIGN.md` marks that as a vet-tier nudge.

---

### 2. Under the Hood

#### Collections are references

`List` and `Map` are reference types. Assigning one to another binding
does not copy:

```glide
let mut a = [1, 2, 3]
let b = a
a.push(4)
println("{b:?}")                 // [1, 2, 3, 4]
```

This is the concrete form of Chapter 4's "`mut` is a path property, not
an object guarantee". `let b` means "no mutation through `b`". It does
not mean the object is frozen.

In the interpreter these are Go pointers (`*ListV`, `*MapV`), which
matches the recorded sacrifice exactly. Mutability is checked at the
**root binding** of an assignment path: `counts[k] = v` requires
`mut counts`, and does not consult whether the map is shared.

The designed escape for code that needs a genuine freeze is persistent
collections (`PList`, `PMap`, ○) with structural sharing, in the
Clojure tradition — a module rather than the default, so Clojure's
everywhere-persistent constant factor is not paid silently.

#### Map representation

The interpreter's map is a Go map plus a keys slice, which is how
insertion order is preserved. That costs one slice's worth of memory
and makes deletion O(n) (deletion is ○ anyway).

The insertion-order guarantee is marked "provisional language
semantics" in `glide/DESIGN-DECISIONS.md` — ratify or revisit when
`Map` lands properly — but the *reason* is solid: it makes programs and
golden tests deterministic.

Contrast Go, which deliberately randomises map iteration order to stop
people depending on an order that was never guaranteed. Both approaches
solve the same problem (do not let people build on accidental order);
Glide's solves it by giving a guarantee rather than by injecting
entropy.

#### Distinct values are not yet map keys

A `distinct` type (Chapter 15) cannot be used as a map key today — it
needs hashable boxing, which will be added when a program wants it.

#### Tuples

A tuple is a small fixed-layout value. In the designed compiler it
lowers to an anonymous struct and is passed in registers when it fits,
so `(Int, Int)` returns are free. There is no boxing.

Go's multiple return values are *not* values — you cannot store the
pair, put it in a slice, or pass it along. `DESIGN.md` calls that a
permanent wart, and Glide's tuples are a strict improvement with the
same performance.

**Known lexer limitation:** nested tuple access `x.0.1` lexes `0.1` as
a float. Rust special-cases this; Glide will when it matters. Today,
destructure instead: `let (inner, _) = x` then `inner.1`.

#### `sort_by` is stable

The interpreter's sort is stable and takes a three-way comparator
returning an `Int` in `cmp` order (negative, zero, positive). Whether
Glide grows a proper `Ordering` type is an open stdlib question; `Int`
is the current shim.

Stability matters more than people expect: it is what makes multi-key
sorting work by sorting repeatedly, least-significant key first.

---

### 3. Why This Design?

#### Why one list type and no array/slice split

Go's split between arrays (`[3]int`, value semantics) and slices
(`[]int`, reference semantics with length and capacity) is a constant
source of confusion, and the confusion has teeth: `append` may or may
not allocate depending on capacity you cannot see, two slices may or
may not share a backing array, and a slice of a slice keeps the whole
original alive.

Glide has one growable list with reference semantics. The
layout-sensitive case — a genuinely fixed-size array for a struct field
or an FFI boundary — is `[N]T` (○) and is not the collection you reach
for by default.

The cost: no value-semantics collection, so passing a list to a
function that mutates it mutates the caller's list. That is the same
cost Go pays for slices, and the mitigation is the same — API design.

#### Why map indexing returns an Option

Because the alternatives all lie.

Go returns a zero value: `m["missing"]` is `0` for an `int` map,
indistinguishable from a stored `0`. To disambiguate you need
`v, ok := m[k]`, which is a second form that people forget.

Python raises `KeyError`, which turns an ordinary absence into an
exception and needs `.get()` as an escape.

Java returns `null`, which is the mistake the whole language exists to
avoid.

An `Option` return is the honest answer: the operation may not find
anything, so its type says so. And it composes — `??` supplies a
default, `if let` branches, `let … else` guards — where the comma-ok
form composes with nothing.

#### Why insertion-ordered maps

Determinism. A program whose output depends on map iteration order is
either wrong or untestable, and there are only two ways to fix that:
guarantee an order, or destroy the order so nobody can accidentally
depend on it.

Go chose destruction (randomised iteration). It works, and it costs you
whenever you *do* want a stable order and have to sort the keys — which
in practice is most of the time you iterate a map for output.

Glide chose the guarantee. JSON round-trips preserve key order. Debug
output is reproducible. Golden tests work. The cost is a keys slice per
map and slower deletion.

#### Why tuples exist and multi-return does not

They are the same feature, and tuples are the version that is a value.

Go's multi-return is unstorable and unlistable: you cannot write
`[]（int, error)`, you cannot pass `f()`'s results directly into a
function taking two arguments except in one special case, and there is
no name for the thing. It is a special case in the language rather than
a type.

`let (a, b) = f()` is a destructuring `let` — the same pattern
machinery as everything else — and `(Int, Error)` is a type you can
write down.

#### Why the tuples-are-for-transport doctrine

Because positional access does not scale. `.0` and `.1` are fine when
the two components are obvious from the function name (`min_max`,
`split_at`, `entries`). At three elements, or across more than one API
boundary, nobody remembers which is which and transposition bugs
appear.

A struct costs three lines and gives every component a name that
travels with it. The vet-tier nudge exists to make the switch happen at
the right time rather than after the first bug.

#### Why `[:]` for the empty map

The bracket family owns collections: `[1, 2, 3]` is a list,
`["a": 1]` is a map, `[]` is an empty list, `[:]` is an empty map.
Swift's allocation, and it keeps braces free for structs and blocks.

The alternative — braces for maps, as in Python and JavaScript — would
collide with block expressions and struct literals in every ambiguous
position, and Glide already has enough brace disambiguation rules
(Chapter 9's control-flow header ban).

---

### 4. Competing Approaches

**Go.** Arrays versus slices with the capacity model; `map[K]V` with
zero-value reads and randomised iteration; multiple returns that are
not values; `nil` slices and maps that behave inconsistently (reading a
nil map is fine, writing panics). Glide simplifies every one of those:
one list, Option reads, ordered iteration, real tuples, no nil.

**Rust.** `Vec<T>`, `HashMap<K, V>`, `BTreeMap`, arrays and slices,
tuples. `HashMap::get` returns `Option<&V>` — the same decision Glide
makes. The borrow checker means Rust's collections have genuine value
semantics and no aliasing, which is strictly better and strictly more
expensive; Glide's reference semantics is the GC trade.

**Python.** `list`, `dict` (insertion-ordered since 3.7 — the same
choice Glide makes, and for the same reason), `tuple`, `set`. `dict[k]`
raises; `dict.get(k)` returns `None`. Python's tuples are used
structurally in ways Glide's doctrine discourages, and Python's
`namedtuple`/`dataclass` exist precisely because positional access
stops scaling.

**Java.** `List`, `Map`, `ArrayList`, `HashMap`, `LinkedHashMap`
(insertion-ordered, opt-in), no tuples at all until records. `Map.get`
returns `null`, which is the bug. `Optional` arrived in Java 8 and is
not used by `Map`.

**JavaScript.** Arrays that are also objects, `Object` used as a map
with string-coerced keys, and `Map` added later with real key
semantics. Reading a missing key gives `undefined`, which is
indistinguishable from a stored `undefined` — Go's zero-value problem
with an extra layer.

**Clojure.** Persistent collections everywhere with structural sharing;
the direct source of Glide's designed `PList`/`PMap`. Clojure pays a
constant factor on every operation for immutability by default;
`DESIGN.md` declines to pay it silently and makes it a module you
choose.

---

### 5. Common Mistakes

**Forgetting the parentheses around `??` in the counting idiom.**

```glide
// Bad — parses as counts[word] ?? (0 + 1); every count stays 1
counts[word] = counts[word] ?? 0 + 1

// Good
counts[word] = (counts[word] ?? 0) + 1
```

**Expecting `let` to freeze a collection.**

```glide
let config = load()
mutate_somehow(config)      // can still change it
```

If you need a guarantee, either do not hand out the reference, or copy
at the boundary, or wait for persistent collections. Chapter 4 covers
this; it bites most often with collections.

**Using `repeat` for nested structures.** The classic:

```glide
// Bad — every row is the same list
let mut grid = [[0, 0, 0]].repeat(3)
grid[0][0] = 1
// grid is [[1,0,0], [1,0,0], [1,0,0]]

// Good
let grid = (0..3).iter().map(|_| [0, 0, 0]).collect()
```

**Indexing a list you have not bounds-checked.** Out of range panics.
For data that might be short, pattern-match instead:

```glide
// Bad
let path = args[1]

// Good
let [_, path] = args else {
    eprintln("usage: prog <file>")
    os.exit(2)
}
```

**Assuming `sorted()` sorts in place, or `sort_by` returns a value.**
`sorted()` copies and returns; `sort_by` mutates and returns `()`.
The names follow Rust's convention — a past participle means "give me
the sorted version".

**Writing a three-or-more tuple that travels.**

```glide
// Bad
fn parse_addr(s: String) -> (String, Int, Bool, String)

// Good
type Addr = struct {
    pub host: String
    pub port: Int
    pub tls: Bool
    pub path: String
}
fn parse_addr(s: String) -> Addr
```

**Hitting the `x.0.1` lexer limitation.** Nested tuple access does not
parse. Destructure the outer tuple first.

**Expecting `join` to work on non-strings.** `[1, 2, 3].join(",")` is a
runtime error — elements must all be `String`. Map first:

```glide
let s = xs.iter().map(|n| "{n}").collect().join(",")
```

**Reaching for a `Set`.** It is ○. Today, a `Map<T, Bool>` or a
`Map<T, ()>` stands in.

---

### 6. Performance Considerations

**List indexing is O(1)** with a bounds check. `push` is amortised
O(1) with geometric growth, so appending n elements is O(n) total with
O(log n) reallocations.

**`sort_by` is O(n log n)** and stable, which means it is a merge sort
rather than an in-place quicksort — it allocates O(n) scratch space.
That is the right default; an unstable in-place sort would be a
separate method if it were ever needed.

**Comparator closures cost an indirect call per comparison.** In a
tight sort of a large list in the interpreter, this dominates. In the
compiled tier the closure usually inlines.

**Map operations are O(1) average.** The keys slice adds one pointer
per entry and makes iteration O(n) in insertion order without sorting —
which is the win, because in Go you pay an O(n log n) key sort every
time you need deterministic output.

**Map indexing allocates nothing** — `V?` is unboxed in the
interpreter, and in the designed compiler an `Option<V>` for a
reference-typed `V` is a nullable pointer with no allocation.

**Spread copies.** `[0, ..xs, 99]` allocates a new list of
`xs.len() + 2` and copies. It is honest about it — `DESIGN.md` calls
this "honestly a copy, the range-indexing doctrine". If you are
spreading in a loop, you have an O(n²).

**Tuples are free.** They lower to anonymous structs, pass in
registers when small, and never box.

**Reference semantics means passing a collection is a pointer copy.**
Handing a million-element list to a function costs nothing. That is
fast and it is exactly why `let` cannot freeze — the two properties are
the same decision.

**`join` is O(n) with one allocation** because it can compute the total
length first. Building a string by `+=` in a loop is O(n²). Chapter 6
covers this.

---

### 7. Best Practices

**Return an Option, not a sentinel.**

```glide
// Bad
fn find(xs: List<Int>, target: Int) -> Int {     // -1 means "not found"
    …
}

// Good
fn find(xs: List<Int>, target: Int) -> Int? {
    …
}
```

The caller of the bad version has to know the convention and remember
to check. The caller of the good version is *made* to handle it.

**Prefer `entries()` and iteration over index loops.**

```glide
// Bad
for i in 0..xs.len() {
    process(xs[i])
}

// Good
for x in xs {
    process(x)
}

// Good — when the index is genuinely needed
for (i, x) in xs.iter().enumerate() {
    process(i, x)
}
```

**Annotate empty collection literals; do not annotate non-empty ones.**

```glide
// Necessary
let mut counts: Map<String, Int> = [:]

// Noise — the literal says everything
let xs: List<Int> = [1, 2, 3]
```

**Promote a tuple to a struct at three fields or the second boundary.**
The rule of thumb: if you find yourself writing a comment explaining
what `.2` is, or if the same tuple shape appears in two signatures, it
is a struct.

**Build with `mut`, then seal.**

```glide
let mut index: Map<String, Int> = [:]
for (i, w) in words.iter().enumerate() {
    if index[w] == None {
        index[w] = i
    }
}
let index = index          // construction over
```

**Do not use a map as an object.** A `Map<String, Any>` is the shape a
struct should have had, and Glide has no `Any` anyway. If the keys are
known at compile time, it is a struct.

```glide
// Bad
let user = ["name": "ada", "age": "36"]    // and now age is a String

// Good
let user = User{ name: "ada", age: 36 }
```

**Decide who owns a collection at the API boundary.** Because
collections are references, a function that takes a `List` can mutate
the caller's data. Either document that it does not, make it a method
on a type that owns the data, or copy on entry. This is the same
discipline Go requires for slices, and it is the price of reference
semantics.

---

### 8. Examples

**A frequency counter, built up in stages:**

```glide
fn main() {
    let words = ["the", "quick", "the", "lazy", "the", "dog", "quick"]

    // Stage 1: count.
    let mut counts: Map<String, Int> = [:]
    for w in words {
        counts[w] = (counts[w] ?? 0) + 1
    }
    println("{counts:?}")

    // Stage 2: to a sortable list of pairs.
    let mut entries = counts.entries()
    println("{entries:?}")

    // Stage 3: sort descending by count, stable — so ties keep
    // insertion order, which is why "lazy" comes before "dog".
    entries.sort_by(|a, b| b.1.cmp(a.1))
    println("{entries:?}")

    // Stage 4: report.
    for (word, n) in entries.iter().take(3) {
        println("{n:4}  {word}")
    }
}
```

```
["the": 3, "quick": 2, "lazy": 1, "dog": 1]
[("the", 3), ("quick", 2), ("lazy", 1), ("dog", 1)]
[("the", 3), ("quick", 2), ("lazy", 1), ("dog", 1)]
   3  the
   2  quick
   1  lazy
```

Two things are load-bearing and worth noticing. The map's insertion
order means `lazy` precedes `dog` before the sort. And `sort_by` is
*stable*, so that relative order survives the sort — which is why the
output is reproducible rather than dependent on the sort's internals.

**A tiny inverted index, showing nested collections done right:**

```glide
fn build_index(docs: List<String>) -> Map<String, List<Int>> {
    let mut index: Map<String, List<Int>> = [:]

    for (doc_id, text) in docs.iter().enumerate() {
        for word in text.split_whitespace() {
            let word = word.to_lower()
            // Fresh list per key — never `repeat`.
            let mut posting = index[word] ?? []
            posting.push(doc_id)
            index[word] = posting
        }
    }
    index
}

fn main() {
    let docs = [
        "the quick brown fox",
        "the lazy dog",
        "quick dog",
    ]
    let index = build_index(docs)
    println("{index[\"quick\"] ?? []:?}")
    println("{index[\"the\"] ?? []:?}")
}
```

Note the `index[word] ?? []` idiom: read the posting list or start a
fresh one. The `??` gives us "get-or-default" without a second lookup
API.

(The two `println` lines above contain string literals inside
interpolations, which the current lexer rejects — see Chapter 6. Hoist
them:)

```glide
fn main() {
    let docs = [
        "the quick brown fox",
        "the lazy dog",
        "quick dog",
    ]
    let index = build_index(docs)
    let quick = index["quick"] ?? []
    let the = index["the"] ?? []
    println("{quick:?}")
    println("{the:?}")
}
```

```
[0, 2]
[0, 1]
```

**Bad versus good: the stringly-typed record**

```glide
// Bad — a map pretending to be a struct
fn parse_config(text: String) -> Map<String, String> {
    …
}

fn main() {
    let cfg = parse_config(text)
    let port = (cfg["port"] ?? "8080").parse_int() ?? 8080
    let host = cfg["host"] ?? "localhost"
    let debug = (cfg["debug"] ?? "false") == "true"
}
```

Every field access is a lookup that might fail, every value is a
`String` that needs parsing, every default is written at the use site
(so two call sites can disagree), and a typo in `"prot"` is a runtime
default rather than a compile error.

```glide
// Good — parse once, into a type
type Config = struct {
    pub host: String
    pub port: Int
    pub debug: Bool
}

fn parse_config(text: String) -> Result<Config, ConfigError> {
    let raw = parse_kv(text)
    Ok(Config{
        host:  raw["host"] ?? "localhost",
        port:  (raw["port"] ?? "8080").parse_int() ?? 8080,
        debug: (raw["debug"] ?? "false") == "true",
    })
}
```

The map still exists — it is the honest representation of "text file of
unknown keys" — but it lives for three lines and then becomes a type.
Every downstream user gets `cfg.port` as an `Int`, with the defaults
decided in one place.

This is a general pattern worth naming: **maps at the boundary, structs
inside.**

---

### 9. Summary & Exercises

**Summary**

- Three collection shapes: `List<T>`, `Map<K, V>`, and tuples. One
  literal syntax each: `[1, 2, 3]`, `["a": 1]` / `[:]`, `(a, b)`.
- There is no array/slice split, no capacity to reason about, and no
  nil collection.
- **Collections are references.** `let b = a` does not copy, and `let`
  does not freeze the object — only the binding. Persistent collections
  (○) are the designed answer for genuine immutability.
- **`m[k]` returns `V?`.** There is no zero-value read and no comma-ok
  form. Consume it with `??`, `if let`, or `let … else`. Remember the
  parentheses: `(m[k] ?? 0) + 1`.
- **Maps are insertion-ordered**, deterministically — the opposite
  choice from Go's randomisation, solving the same problem.
- Indexing a list out of range **panics**. It is a bug, not an error
  value.
- `repeat` is **shallow**: `[[]].repeat(2)` gives two slots sharing one
  inner list. Build fresh values with `(0..n).iter().map(|_| []).collect()`.
- `sort_by` mutates in place and is **stable**; `sorted()` returns a
  copy. The comparator is three-way (`a.cmp(b)`), not a boolean less-than.
- Spread (`[0, ..xs, 99]`) splices any iterable and is honestly a copy.
- Tuples are real values — storable, listable, pattern-matchable —
  unlike Go's multiple returns. **Doctrine: tuples are for transport.**
  Three elements or a second API boundary means a struct.

**Exercises**

1. **Find the zero-value bug.** In a Go codebase, find a
   `map[string]int` read that does not use the comma-ok form. Determine
   whether "key absent" and "value is 0" mean the same thing there. In
   most codebases at least one of these is a latent bug; in the rest,
   the code is correct only because of an invariant nobody wrote down.

2. **Break `repeat` on purpose, then fix it three ways.** Build a 3×3
   grid of zeros with `repeat` and observe the aliasing. Then produce a
   correct grid using (a) an iterator with `map`, (b) a `for` loop with
   `push`, and (c) a nested list literal. Time all three in the
   interpreter and note which is clearest versus which is fastest —
   they will not be the same one.

3. **Design the map/struct boundary.** Take a configuration file format
   you actually use (TOML, YAML, `.env`, JSON). Write the Glide types
   for it, deciding for each field whether it is required (`T`),
   optional (`T?`), or defaulted. Then write down what happens for each
   field when it is absent, when it is present but the wrong type, and
   when it is present but empty. That table is the specification, and
   Glide's type system will make you write most of it down whether you
   want to or not.
