# Chapter 8: Closures

A closure is an anonymous function that carries part of its
surroundings with it. If you have only used languages where functions
are top-level declarations that see nothing but their arguments, this
chapter is the one to read slowly — closures are the mechanism behind
callbacks, iterator adapters, sorting comparators, HTTP handlers,
`spawn`, and half the standard library's shape.

Glide's closures are deliberately boring: **one function type**, no
`move` keyword, no `Fn`/`FnMut`/`FnOnce` hierarchy, no `Box<dyn Fn>`.
That boringness is the single largest dividend of choosing a garbage
collector over a borrow checker, and this chapter explains why.

Everything here is ✓ except the *spelling* of the function type, marked
where it appears.

---

### 1. Basic Usage

#### What a closure is

Start with a named function:

```glide
fn double(x: Int) -> Int { x * 2 }
```

The same thing, written as a value with no name:

```glide
let double = |x| x * 2
```

Parameters go between pipes, then the body. That is the whole syntax.
Three forms:

```glide
let double = |x| x * 2                // expression body
let log    = |msg| { println("[log] {msg}") }   // block body
let now    = || "tick"                // no parameters
```

Parameter types are **inferred from context** — a closure passed to
`xs.map(…)` gets its parameter type from the list's element type.

Annotations on closure parameters are ○: no grammar for them exists
today, so `|x: Int| -> Int { x * 2 }` is a parse error, not merely
noise. Whether the grammar arrives with M4 is an open question in
`DESIGN.md`, and it turns on a case this chapter's own first example
raises: `let double = |x| x * 2` has no context to infer `x` from. A
language that requires a signature on every `fn` has a hard time
justifying a closure with no inferable parameter type — so either that
binding needs an annotation, or it needs to be a `fn`.

So far this is just a function you did not name. The interesting part
is the word *closure*.

#### The closing-over part

A closure can read and write bindings from the scope where it was
created — and it keeps them alive after that scope has gone:

```glide
fn make_counter() -> Counter {
    let mut n = 0
    || {
        n += 1
        n
    }
}

fn main() {
    let c = make_counter()
    println(c())      // 1
    println(c())      // 2

    let c2 = make_counter()
    println(c2())     // 1  — a fresh n
}
```

```
1
2
1
```

`make_counter` returns a closure. By the time you call `c()`,
`make_counter` has already returned and its stack frame is gone — but
`n` is still there, because the closure captured it. That is what
"closing over" means: the function *closes over* the bindings it
references, and they travel with it.

Note also that each call to `make_counter` produces a *separate* `n`.
`c` and `c2` do not share state. This is the mechanism behind almost
every "object with one method" you will ever need.

(The return type is written as a placeholder name above because the
function-type spelling is ○ — see Under the Hood.)

#### Passing closures to functions

The everyday use:

```glide
fn main() {
    let mut entries = [("a", 3), ("b", 1), ("c", 2)]
    entries.sort_by(|a, b| a.1.cmp(b.1))
    println("{entries:?}")
}
```

```
[("b", 1), ("c", 2), ("a", 3)]
```

`sort_by` takes a **three-way comparator**: a function returning a
negative number, zero, or a positive number. Swap `a` and `b` to sort
descending:

```glide
entries.sort_by(|a, b| b.1.cmp(a.1))     // descending
```

Iterator adapters are the other big consumer:

```glide
fn main() {
    let nums = [1, 2, 3, 4, 5, 6]
    let evens = nums.iter().filter(|n| n % 2 == 0).collect()
    let doubled = nums.iter().map(|n| n * 2).collect()
    println("{evens:?}")
    println("{doubled:?}")
}
```

```
[2, 4, 6]
[2, 4, 6, 8, 10, 12]
```

#### Capture is by reference, and it is live

```glide
fn main() {
    let mut total = 0
    let add = |n| { total += n }
    add(40)
    add(2)
    println(total)      // 42
}
```

The closure did not copy `total`; it captured the *binding*. Mutating
through it is visible outside.

Note the rule this implies: **mutating a captured binding requires that
binding to be `mut`.** A closure cannot launder immutability.

```glide
let total = 0
let add = |n| { total += n }     // error: total is not mut
```

#### Capture is of bindings, not names

This is the subtle one, and it follows from Chapter 4's rule that
redeclaration creates a *new* binding:

```glide
fn main() {
    let name = "first"
    let who = || name
    let name = "second"      // a NEW binding, same spelling
    println(who())           // first
    println(name)            // second
}
```

The closure holds the binding it captured at creation. A later
same-scope redeclaration cannot retarget it, because the redeclaration
made a different binding.

#### Loop variables are fresh per iteration

```glide
fn main() {
    let mut fs = []
    for i in 0..3 {
        fs.push(|| i)
    }
    for f in fs {
        print("{f()} ")
    }
    println("")
}
```

```
0 1 2
```

Each iteration binds a fresh `i`, so each closure captures its own.

This is worth pausing on if you have written Go. Before Go 1.22 the
loop variable was reused across iterations, so all three closures
captured the same memory and printed `3 3 3` (or `2 2 2`, depending on
the loop shape). It was Go's single most expensive capture bug — it
produced wrong results in production goroutine spawns for thirteen
years, and fixing it in 1.22 required a *semantic break* to the
language. Glide has the correct behaviour from day one.

#### Closure parameters may shadow outer names

A closure body is a new *function* boundary, not a nested block, so the
shadow ban from Chapter 4 does not apply:

```glide
fn main() {
    let n = 100
    let g = |n| n + 1
    println(g(1))      // 2
    println(n)         // 100
}
```

This is legal and occasionally useful. It is also occasionally
confusing, so the Best Practices section has a view on it.

#### A closure and a named function have the same type

```glide
fn triple(x: Int) -> Int { x * 3 }

fn apply_twice(f: F, x: Int) -> Int { f(f(x)) }

fn main() {
    println(apply_twice(triple, 3))            // 27
    println(apply_twice(|x| x * 3, 3))         // 27
}
```

`apply_twice` accepts either. There is exactly **one function type**,
written `fn(Int) -> Int`, covering named functions, closures, and
(eventually) method values alike.

Whether a closure captured anything is *representation*, not type. That
sentence is the whole design, and section 3 explains what it buys.

#### `break` and `continue` do not cross a closure

```glide
for x in xs {
    run(|| {
        break        // parse error: not inside a loop
    })
}
```

A closure body is its own function, so an enclosing loop is out of
reach. This is the same reason `return` inside a closure returns from
the *closure*, not from the enclosing function.

---

### 2. Under the Hood

#### What a closure is at runtime

A closure is two things: code, and a record of the bindings it
captured.

In the interpreter, creating a closure snapshots a flattened map of the
binding **cells** it references. Sharing the cells is what makes
mutation visible from outside; snapshotting *which* cells is what stops
a later redeclaration from retargeting it.

`glide/DESIGN-DECISIONS.md` records that the first implementation got
this wrong. It kept a reference to the live environment map and looked
names up at *call* time. Everything worked until a closure straddled a
redeclaration — at which point `who()` above would have printed
`"second"`. The bug was invisible in every test that did not have a
redeclare between creation and call.

In the designed compiler, a closure is a function pointer plus a
capture record, heap-allocated only if the closure escapes the frame
that made it. Escape analysis decides. This is Go's model exactly.

#### The function type ○

The type is spelled `fn(Int) -> Int`, and today the parser does not
accept it in a type position:

```glide
fn apply(f: fn(Int) -> Int, x: Int) -> Int { f(x) }
```

```
error: line 1: expected a type, found 'fn'
```

There is still no *written* spelling for a function type: the parser
takes a type name, and `fn(Int) -> Int` does not parse. The checker
has the type internally — a closure passed to `filter` or `sort_by` is
checked against the parameter's signature, and a wrong return type is
a compile error — but you cannot yet write one down, which is why the
examples above use a placeholder name. The spelling is designed; only
the grammar is missing.

The important part is not the spelling; it is that there is exactly
**one** function type. No distinction between "closure that captures by
value", "closure that captures by mutable reference", and "closure that
consumes its captures".

#### Why Rust needs three and Glide needs one

Rust's `Fn` / `FnMut` / `FnOnce` hierarchy is not gratuitous. It exists
because Rust must know, statically, what a closure does to its captured
data:

- `Fn` — captures by shared reference; callable many times.
- `FnMut` — captures by mutable reference; callable many times, but
  the borrow checker must ensure no aliasing.
- `FnOnce` — consumes its captures; callable once.

Plus `move` to force by-value capture, plus `Box<dyn Fn>` to store one,
plus lifetime annotations when a captured reference outlives the frame.

Every one of those exists to answer a question a garbage collector
answers at runtime: *is this captured value still alive, and who may
write to it?* With a GC, the answer is always "it is alive as long as
anything references it", and the `mut` rule handles the writing
question at a granularity that is adequate rather than perfect.

`DESIGN.md` calls this "the GC dividend", and it is the clearest
example of one in the language. Returning and storing closures is
boring, as it should be.

#### What capture costs

One pointer per captured binding, in the capture record. A closure that
captures nothing is a bare function pointer.

In the interpreter, a closure allocates a small map at creation. In a
tight loop that creates a closure per iteration, that is measurable
today and largely disappears when compiled — the compiler can stack-
allocate a non-escaping closure.

#### Task boundaries have an extra rule ○

There is one compile-time restriction, designed and not yet enforced:

> **Closures crossing task boundaries must not capture `mut` bindings.**

```glide
let mut counter = 0
s.spawn(|| { counter += 1 })     // ○ will be a compile error
```

This is the data-race archetype, and it is statically visible:
mut-ness is known and `spawn` is a known boundary. The escape hatches
are to freeze (`let snapshot = counter`) or to clone. Cheap rule, whole
race class turned into a compile error; the race detector backstops
what escapes via reference-typed immutable captures. Chapter 25 returns
to this.

---

### 3. Why This Design?

#### Why pipes instead of `func(x int) int`

Go taxes closures. Doubling a number costs `func(x int) int { return x
* 2 }` — forty characters, of which four are the actual computation.
`DESIGN.md` states plainly that this ceremony "is half of why Go never
grew a map/filter culture", and the claim is easy to check: look at how
often Go code uses `sort.Slice` (a closure, unavoidable) versus how
often it uses a functional pipeline (almost never).

Cost shapes culture. `|x| x * 2` is eight characters, so the pipeline
style becomes available.

Why pipes specifically, and not `=>`? Because `=>` is the match-arm
glyph, and the house rule is **one glyph, one meaning**. Rust's pipes
were available and unambiguous.

#### Why no trailing closures

Swift and Kotlin let you move a final closure argument outside the
parentheses:

```
// Swift
items.forEach { print($0) }
```

It reads beautifully and it is the gateway to builder DSLs — code that
looks like new syntax but is method calls, which the formatter and the
parser both have to special-case. Glide declines it: **closures are
arguments, and they sit in the parens.**

```glide
items.iter().for_each(|x| println(x))    // ○ for_each
```

#### Why no `$0` or `it`

Swift's `$0` and Kotlin's `it` save four characters and cost the reader
a name. In a chain of adapters, `|note| note.title` tells you what is
flowing through; `$0.title` does not. Naming the parameter is
documentation.

#### Why capture by reference

The alternative is capture by value, which means a closure gets a copy
and mutation is invisible outside. That makes counters, accumulators,
and callbacks-that-record impossible without an explicit box.

Capture by reference plus the `mut` rule gets both: mutation works when
you asked for it (`let mut`), and is a compile error when you did not.
The one place by-value capture is genuinely needed — crossing a task
boundary — gets the explicit rule above rather than a `move` keyword
applied everywhere.

#### Why loop variables are fresh per iteration

Because the alternative was Go's most expensive bug and Go paid a
semantic break to fix it.

The failure mode: you spawn a goroutine per item in a loop, capturing
the loop variable, and every goroutine sees the *last* item because
they all captured the same slot. It produces wrong answers rather than
crashes, which means it survives testing and appears in production.

There is no upside to reuse. Glide has it right from day one, and
records that Go's 1.22 change is the evidence.

#### Why closures may shadow but nested blocks may not

Chapter 4's shadow ban targets a specific bug: an inner binding
coexisting with a live outer one, where you might write to the wrong
one. Inside a closure, the outer binding is not *live in the same
sense* — you have crossed a function boundary, and the parameter list
is a declaration site the reader is already inspecting.

Banning it would also break the common and harmless case of a generic
callback whose parameter has an obvious name (`|err| …`, `|req| …`,
`|n| …`) that happens to collide with something in an outer scope
thirty lines up.

#### Why big closures should become named functions

`DESIGN.md` notes that "the formatter nudges by refusing to make big
lambdas pretty." This is a deliberate ergonomics lever: when a closure
grows past a few lines, promoting it to a named `fn` is *cut and
paste*, because a closure and a named function have the same type. No
signature surgery, no wrapper.

That only works because there is one function type. In Rust, promoting
a closure to a named function means deciding whether callers want
`impl Fn`, `&dyn Fn`, or a generic parameter — a real refactor.

---

### 4. Competing Approaches

**Go.** `func(x int) int { return x * 2 }`, capture by reference,
per-iteration loop variables since 1.22. Structurally the same model as
Glide, with four times the syntax and no `mut` distinction. Go also has
no nested named functions, so every helper is either a closure assigned
to a variable (with the recursive-closure two-step) or promoted to
package scope.

**Rust.** `|x| x * 2` — the syntax Glide takes — and the `Fn`/`FnMut`/
`FnOnce` hierarchy, `move`, `Box<dyn Fn>`, and lifetime annotations
that Glide does not need. Rust's model is strictly more expressive and
strictly more expensive; the expressiveness buys compile-time race
freedom, which Glide explicitly sacrificed.

**Swift.** Closure expressions with `{ (x: Int) -> Int in … }`,
trailing-closure syntax, `$0` shorthand, and explicit capture lists
(`[weak self]`) that exist because of reference counting. Glide
declines trailing closures and `$0`; capture lists have no analogue
because tracing GC has no retain cycles to break.

**Kotlin.** `{ x -> x * 2 }`, `it` for single parameters, trailing
closures. Kotlin's `it` is the single most contested piece of its
syntax in code review — it is lovely in one-liners and unreadable in
chains.

**JavaScript.** `x => x * 2`, capture by reference, and — until `let`
arrived — the same loop-variable bug Go had, for the same reason.
JavaScript's closures are the most-used in the world and its `this`
binding rules are the cautionary tale for what happens when a language
has two kinds of function with different capture semantics.

**Python.** `lambda x: x * 2`, restricted to a single expression, with
statements requiring a nested `def`. Python's late-binding closures
capture the *name*, not the binding — so a closure created in a loop
sees the final value of the loop variable, which is the same bug again
from a third direction. Glide's binding-not-name rule is precisely the
fix.

**Java.** `x -> x * 2`, but captured variables must be effectively
final — Java's answer to the same aliasing question, solved by
forbidding mutation entirely. That makes counters impossible without a
mutable box (`AtomicInteger`, or a one-element array), which is a
well-known Java annoyance.

---

### 5. Common Mistakes

**Expecting `return` inside a closure to exit the enclosing function.**

```glide
// Bad — this returns from the closure, not from find_admin
fn find_admin(users: List<User>) -> User? {
    users.iter().for_each(|u| {
        if u.role == .Admin { return Some(u) }    // returns from the closure
    })
    None
}

// Good
fn find_admin(users: List<User>) -> User? {
    for u in users {
        if u.role == .Admin { return Some(u) }
    }
    None
}
```

A closure is a function. Its `return` is its own. This is also why the
`or |e| { … }` construct was declined in the error-handling design
(Chapter 19): a block that *looks* like a closure but whose `return`
exits the enclosing function is impersonating something it is not.

**Trying to `break` out of a loop from inside a closure.** Parse error.
Restructure as a real loop, or use an iterator adapter that stops
(`take_while`, ○).

**Mutating a captured binding that is not `mut`.** The error is clear;
the fix is to declare `let mut`. If you find yourself doing this a lot,
check whether the accumulate-then-seal idiom (Chapter 4) fits better.

**Assuming a redeclaration updates an existing closure.**

```glide
// Bad — surprising if you expect name-based lookup
let config = load_defaults()
let render = || render_with(config)
let config = load_overrides()      // render still uses the defaults!
println(render())
```

If you want the closure to see later values, capture a `mut` binding
and mutate it, or pass the value as a parameter.

**Creating a closure per loop iteration in a hot path.** Each creation
allocates a capture record. In the interpreter this is a map. Hoist the
closure out of the loop if it does not depend on the loop variable:

```glide
// Bad
for row in rows {
    process(row, |x| x * factor)      // new closure every iteration
}

// Good
let scale = |x| x * factor
for row in rows {
    process(row, scale)
}
```

**Reaching for `$0`-style brevity by naming parameters `a`, `b`, `x`.**
In a comparator `|a, b|` is fine and conventional. In a domain
pipeline, `|n| n.title` beats `|x| x.title`.

**Annotating closure parameters out of habit.** `|x: Int| x * 2` is
legal and adds nothing. The types come from context; that is the point.

**Forgetting that a closure keeps its captures alive.** This is
correct behaviour and occasionally a memory issue: a long-lived closure
capturing a large structure keeps that structure reachable. If a
callback outlives the data it was created near, capture only what it
needs.

---

### 6. Performance Considerations

**Creation** costs one capture record: roughly one pointer per captured
binding, plus the allocation if the closure escapes. A closure that
captures nothing is a bare function pointer and costs nothing.

**Calling** a closure through a value is an indirect call — it cannot
be inlined unless the compiler can prove which closure it is. In
practice, the optimiser inlines closures passed directly to a function
that is itself inlined (the common `xs.iter().map(|x| …)` shape), and
does not inline closures stored in a struct field and called later.

**In the interpreter**, closure creation allocates a Go map for the
captured cells, and every call allocates a child environment. Both are
significant tree-walker costs. A benchmark comparing a closure-heavy
pipeline against an explicit loop will look much worse today than it
will when compiled — do not tune against the interpreter.

**Escape analysis** decides stack versus heap in the designed compiler,
exactly as in Go. A closure passed downward into a function that does
not store it stays on the stack. A closure returned from a function, or
stored in a struct, or handed to `spawn`, escapes.

**The one-function-type decision has a cost**: every closure call is an
indirect call through a uniform representation, where Rust's
monomorphised `impl Fn` parameters can be specialised per call site.
This is a real performance difference in tight functional pipelines and
is part of the "roughly the last 20% of performance" that `DESIGN.md`
lists as a deliberate sacrifice.

**Adapters are lazy** (Chapter 23), so `xs.iter().map(f).take(3)`
calls `f` three times, not `xs.len()` times. The closure cost scales
with what you consume, not with what you have.

---

### 7. Best Practices

**Name the parameter after what it is.**

```glide
// Bad
notes.iter().filter(|x| x.published)

// Good
notes.iter().filter(|note| note.published)
```

The exception is genuinely generic positions: `|a, b|` in a comparator,
`|_|` when the value is ignored.

**Promote a closure to a named `fn` when it grows past a few lines or
gets a second caller.** It is cut and paste — same type, no signature
surgery. The moment a closure has a *name* worth giving it, give it
one.

```glide
// Bad — a 15-line closure inline in a route registration
r.get(`/notes/{id}`, |req| {
    // …fifteen lines of handler…
})

// Good
r.get(`/notes/{id}`, |req| get_note(db, req))

fn get_note(db: Db, req: Request) -> Result<Response, ApiError> {
    // …fifteen lines, testable on its own…
}
```

Notice the good version still needs a closure — a one-line adapter that
supplies `db`. That is the idiomatic shape for dependency injection in
Glide, and it appears throughout Chapter 32.

**Prefer a nested `fn` when the helper does not need to capture.**
Chapter 7's rule. If the body only uses its parameters, `fn` states
that fact; a closure leaves the reader to verify it.

**Capture narrowly.** A closure that needs one field should capture
that field, not the whole struct:

```glide
// Bad — keeps the entire request alive for the lifetime of the callback
let cb = || audit(req)

// Good
let user_id = req.user_id
let cb = || audit(user_id)
```

**Do not use a closure to fake a loop.**

```glide
// Bad
(0..n).iter().for_each(|i| { work(i) })

// Good
for i in 0..n {
    work(i)
}
```

Adapters are for *transforming* sequences. When you are just doing
something n times, the loop is clearer and cheaper, and it lets you
`break`.

**Freeze before spawning.** Even though the compile-time rule is ○,
adopt the idiom now:

```glide
// Bad — will be a compile error when the rule lands
let mut count = 0
s.spawn(|| { count += 1 })

// Good
let snapshot = count
s.spawn(|| { use(snapshot) })
```

---

### 8. Examples

**Building a small pipeline, one step at a time:**

```glide
type Note = struct {
    pub title: String
    pub words: Int
    pub published: Bool
}

fn main() {
    let notes = [
        Note{ title: "alpha",   words: 120, published: true },
        Note{ title: "beta",    words: 40,  published: false },
        Note{ title: "gamma",   words: 800, published: true },
        Note{ title: "delta",   words: 300, published: true },
    ]

    // Step 1: a predicate as a closure.
    let is_long = |note| note.words > 100

    // Step 2: filter with it.
    let long_notes = notes.iter().filter(is_long).collect()
    println("{long_notes.len()} long notes")

    // Step 3: chain, mapping to titles.
    let titles = notes
        .iter()
        .filter(|note| note.published)
        .filter(is_long)
        .map(|note| note.title)
        .collect()
    println("{titles:?}")

    // Step 4: sort with a comparator closure.
    let mut by_length = notes.iter().map(|n| (n.title, n.words)).collect()
    by_length.sort_by(|a, b| b.1.cmp(a.1))
    println("{by_length:?}")
}
```

```
3 long notes
["alpha", "gamma", "delta"]
[("gamma", 800), ("delta", 300), ("alpha", 120), ("beta", 40)]
```

Note that `is_long` is defined once and used twice, in two different
positions — a closure is an ordinary value.

**A closure as a configuration point:**

```glide
fn retry_until(max: Int, done: Check) -> Int {
    let mut n = 0
    for i in 1..=max {
        n = i
        if done(i) { return n }
    }
    n
}

fn main() {
    println(retry_until(10, |i| i * i > 20))     // 5
    println(retry_until(3, |i| false))           // 3
}
```

```
5
3
```

`retry_until` knows nothing about the condition. That is the whole
value of first-class functions: the *policy* is a parameter.

**Counters, and the closure-as-object pattern:**

```glide
fn make_accumulator() -> Acc {
    let mut total = 0
    |n| {
        total += n
        total
    }
}

fn main() {
    let acc = make_accumulator()
    println(acc(10))     // 10
    println(acc(5))      // 15
    println(acc(1))      // 16

    let other = make_accumulator()
    println(other(100))  // 100 — independent state
}
```

This is an object with one method and private state, built from nothing
but a closure. When you need two methods, use a struct with an `impl`
block (Chapter 16) — that is exactly the line where "closure as object"
stops being the right tool.

**Bad code, and why:**

```glide
// Bad — everything wrong at once
fn process(items: List<Int>) -> List<Int> {
    let mut out = []
    items.iter().for_each(|x| {
        if x % 2 == 0 {
            out.push(x * 2)
        }
    })
    out
}
```

Four problems:

1. `for_each` with a side effect is a `for` loop with extra syntax and
   no `break`.
2. Building a list by mutation inside a callback, when `filter`/`map`
   exist, throws away laziness — the whole list is materialised whether
   or not the caller consumes it.
3. `out` must be `mut` and is mutated from inside a closure, which is
   the pattern the task-boundary rule will eventually forbid; getting
   into the habit here means rewriting later.
4. `x` names nothing.

```glide
// Good
fn process(items: List<Int>) -> List<Int> {
    items
        .iter()
        .filter(|n| n % 2 == 0)
        .map(|n| n * 2)
        .collect()
}
```

Or, if you genuinely want a loop — which is fine, and often clearer:

```glide
// Also good
fn process(items: List<Int>) -> List<Int> {
    let mut out = []
    for n in items {
        if n % 2 == 0 {
            out.push(n * 2)
        }
    }
    out
}
```

The difference between "bad" and "also good" is not the loop; it is
using a callback to *simulate* a loop.

---

### 9. Summary & Exercises

**Summary**

- A closure is an anonymous function that captures bindings from its
  creating scope and keeps them alive. Syntax: `|x| expr`, `|x| { … }`,
  `||` for no parameters. Parameter types are inferred from context.
- **There is exactly one function type**, `fn(A) -> B`, shared by named
  functions, closures, and method values. Whether a closure captured
  anything is representation, not type. This is the GC dividend: no
  `Fn`/`FnMut`/`FnOnce`, no `move`, no `Box<dyn Fn>`, no lifetimes.
- Capture is **by reference** and **of bindings, not names**. Mutating
  a captured binding requires it to be `mut`. A later same-scope
  redeclaration does not retarget an existing closure.
- **Loop variables are fresh per iteration**, so closures created in a
  loop each capture their own value. Go needed a semantic break in 1.22
  to reach this.
- A closure body is a function boundary: it may shadow outer names,
  `return` exits the closure only, and `break`/`continue` cannot reach
  an enclosing loop.
- Promoting a grown closure to a named `fn` is cut and paste, because
  the types are identical.
- No trailing closures, no `$0`/`it`. Closures are arguments and sit in
  the parens; naming the parameter is documentation.
- ○: the `fn(A) -> B` type spelling is not parsed yet; the rule that
  task-crossing closures may not capture `mut` bindings is designed and
  unenforced.

**Exercises**

1. **Reproduce the Go loop bug, then explain why you cannot.** Write a
   loop that creates three closures capturing the loop variable, and
   print what each returns. Then write, in comments, what the same code
   would have printed in Go 1.21 and why. Finally, look up the Go 1.22
   release notes on the loop-variable change and note what they had to
   say about backward compatibility.

2. **Build a memoiser.** Write `memoize(f)` that returns a closure with
   the same behaviour as `f` but caching results in a captured `Map`.
   Then answer: what happens if two tasks call the memoised closure
   concurrently? (You will need Chapter 25 for the real answer, but
   predict it now — the captured map is a `mut` binding crossing a task
   boundary.)

3. **Find the closure that should be a struct.** Take a piece of code —
   yours or from a library — where a function returns a closure holding
   state. Rewrite it as a struct with an `impl` block. Then argue which
   version is better and at what number of methods the answer flips.
   The honest answer is usually "one method: closure; two or more:
   struct", and it is worth confirming that on real code.
