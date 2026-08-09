# Chapter 9: Control Flow

Glide has **one loop keyword**, **two branching constructs**, and **no
`goto`**. It also has something the C lineage mostly lacks: control
flow that produces *values*. `if` is an expression. `match` is an
expression. A bare block is an expression.

That single property removes the ternary operator, removes most
assign-in-every-branch mutable variables, and is why the `let status =
if ok { … } else { … }` shape shows up constantly in idiomatic code.

This chapter covers `if`, `for`, blocks, labels, and divergence.
`match` gets its own chapter (Chapter 10) because pattern matching
deserves the space.

Everything here is ✓.

---

### 1. Basic Usage

#### `if` is an expression

```glide
let status = if ok { "active" } else { "disabled" }
```

Both arms must produce the same type, and in value position `else` is
required — an `if` with no `else` has no value when the condition is
false.

The statement form is the ordinary one:

```glide
if n < 0 {
    eprintln("negative")
} else if n == 0 {
    eprintln("zero")
} else {
    eprintln("positive")
}
```

Braces are mandatory. Conditions must be `Bool` — there is no
truthiness (Chapter 5), so `if xs` is an error and `if !xs.is_empty()`
is the spelling.

`else` sits on the same line as the `}` before it. The canonical
formatter guarantees this, so it is not something you think about.

#### There is one loop keyword

```glide
for { … }                    // forever
for cond { … }               // while
for x in xs { … }            // over anything iterable
for (k, v) in m { … }        // patterns work in the header
for i in 0..10_000 { … }     // over a range
```

That is the complete list. No `while`, no `do`/`while`, no `loop`, no
`repeat`, no C-style three-clause form.

```glide
fn main() {
    let mut i = 0
    for {
        i += 1
        if i >= 3 { break }
    }
    println(i)          // 3

    let mut j = 0
    for j < 3 { j += 1 }
    println(j)          // 3

    for (k, v) in ["a": 1, "b": 2] {
        print("{k}={v} ")
    }
    println("")         // a=1 b=2
}
```

Iterables today: `List`, `Map` (yields `(k, v)` tuples in insertion
order), `Range`, any `Iterator`, a channel `Receiver`, and any value
with an `iter()` method — which is also what makes a user type
`for`-able (Chapter 17).

**Loop variables are a fresh binding per iteration**, which is what
makes closures created in a loop behave correctly (Chapter 8).

#### `break` and `continue`

Both work as expected and are a parse error outside a loop:

```glide
fn main() { break }
```

```
error: line 1: break outside a loop
```

A closure body is its own function, so an enclosing loop is out of
reach from inside one.

#### Labeled break and continue

```glide
fn main() {
    search: for x in 0..5 {
        for y in 0..5 {
            if x * y > 6 {
                println("found {x},{y}")
                break search
            }
        }
    }

    outer: for x in 0..3 {
        for y in 0..3 {
            if y == 1 { continue outer }
            print("{x}{y} ")
        }
    }
    println("")
}
```

```
found 2,4
00 10 20
```

Labels name **loops only**. A label must name an enclosing loop —
including through closure and `defer` boundaries, where it is a parse
error rather than a surprise. Duplicate active labels are an error.
Unlabeled `break`/`continue` still target the nearest enclosing loop.

#### Blocks are expressions

```glide
let v = {
    let a = 1
    let b = 2
    a + b
}
println(v)          // 3
```

The block's tail expression is its value, and its locals die at the
closing brace. A freestanding block scopes a region, exactly as in Go.

This one rule replaces Go's `if` init clause. Where Go writes
`if num := rand.IntN(100); num > 50 { … }` to keep `num` from
outliving the statement, Glide wraps a block around exactly the code
that should see the name.

#### Struct literals are banned in control-flow headers

```glide
type P = struct { x: Int }

fn main() {
    let p = P{ x: 0 }
    if p == P{ x: 0 } { println("eq") }
}
```

```
error: line 4: expected an expression, found ':'
```

The `{` after `P` is claimed by the `if` body. Parenthesise to force
the literal reading:

```glide
if p == (P{ x: 0 }) { println("eq") }
```

Rust has the same rule for the same reason, and Glide hits it harder
because variants are capitalised too, so case cannot disambiguate. It
applies to `if`, `for`, and `match` scrutinees.

#### Divergence

Some expressions never produce a value: `return`, `break`, `continue`,
`os.exit`, and a panic. The type system (○) calls this the never type;
today the interpreter simply knows these do not fall through.

Divergence is *required* in two places:

```glide
let [_, path] = os.args() else {
    eprintln("usage: prog <file>")
    os.exit(2)                 // must diverge
}
```

```glide
let Some(user) = find(id) else {
    return Err(.NotFound)      // must diverge
}
```

The `else` block of a `let … else` must not fall through, because past
it the binding has to exist. Chapter 10 covers this properly.

---

### 2. Under the Hood

#### `for … in` desugars to the iterator protocol

```glide
for x in xs { body }
```

becomes, conceptually:

```glide
let it = xs.iter()
for {
    let Some(x) = it.next() else { break }
    body
}
```

The protocol is one method — `fn next(mut self) -> T?` — returning
`None` when exhausted. One method with an `Option` return means no
invalid states, no `hasNext`/`next` desynchronisation, and no
`(value, ok)` tuple. Chapter 23 covers iterators in full.

A `List` is not an iterator; it is *iterable*. `xs.iter()` produces a
fresh iterator each time, which is why you can loop over the same list
twice. Collapsing the two concepts bites every language that tries.

#### Fresh loop bindings

Each iteration creates a new binding cell for the loop variable rather
than reusing one. In the interpreter this is a fresh entry in the
iteration's child environment. In the designed compiler it is a fresh
stack slot per iteration, which the optimiser collapses back to one
slot whenever no closure captures it — so the guarantee costs nothing
in the common case.

#### Labels

Labels are resolved at parse time. `break search` compiles to an unwind
to a known enclosing loop, and naming a label that is not an enclosing
loop is a parse error rather than a runtime surprise. In the
interpreter, labeled break and continue are signal values that
propagate up through the evaluator until the matching loop consumes
them — the same mechanism as `return`.

#### Why the struct-literal ban is a parse-level rule

The parser, on reaching a control-flow header, parses an expression in
a mode where `{` terminates the expression and opens the body. There is
no lookahead heuristic and no ambiguity resolution — the rule is
mechanical, and parentheses are the documented escape.

`DESIGN.md` marks this as "forced by the interpreter; ratified",
meaning it was discovered while writing the parser and then accepted as
the design rather than worked around.

#### Blocks in the interpreter

A block allocates a child environment. That is a small map allocation
per block *entry*, so a bare block inside a tight loop has a real cost
today that it will not have when compiled (where a block is purely
lexical). Do not restructure code to avoid blocks based on interpreter
timings.

---

### 3. Why This Design?

#### Why one loop keyword

Go got this exactly right, and Glide copies it without modification
except for the `in` spelling.

The argument is that `while`, `do`/`while`, `loop`, `repeat`, and
`for(;;)` are five spellings of one concept, and every one of them is a
decision the author has to make and the reader has to decode. With one
keyword, every loop in every codebase starts with the same token, and
the shape of the loop is carried by what follows it.

The one thing Glide changes is dropping the C-style
`for i := 0; i < n; i++` form. It has no customers left: counting loops
are `for i in 0..n`, and the three-clause form's remaining uses
(multiple loop variables, unusual step) are rare enough to write as a
`for cond` with explicit updates.

`in` replaces Go's `range` keyword — same semantics, reads more
naturally, and frees `range` as an ordinary identifier.

#### Why `if` is an expression, and why there is no ternary

Go is half right and C is half right.

Go is right that `?:` nests unreadably, that its precedence breeds
bugs, and that a second cryptic branching syntax is a cost. C is right
that conditionals *are* expressions.

Go refused the operator without providing the compensation. The result
is a permanent FAQ entry and four lines plus a mutable variable for the
innocent case:

```go
var status string
if ok {
    status = "active"
} else {
    status = "disabled"
}
```

Glide's `let status = if ok { "active" } else { "disabled" }` is the
same thing with no mutable variable and no repetition of the name.

Three pieces make it a genuine replacement rather than an
approximation:

- The formatter keeps one-liners that fit on a line, so the common case
  stays compact.
- Value-position `if` requires `else`, checked by the type checker (○),
  so you cannot accidentally write a conditional with a missing branch.
- The two commonest ternaries are covered better by other constructs:
  `x != nil ? x : y` is `x ?? y`, and nested ternary chains are
  subjectless `match`, which is what they were always trying to be.

#### Why labeled break survives but `goto` does not

Go kept `goto` (function-scoped, used a couple of dozen times in the
standard library, mostly in scanners and state machines) because Go
lacked the replacements. Its three surviving legitimate uses are each
covered here:

1. **Cleanup on error** — the `goto fail` C kernel idiom, goto's
   strongest modern case. Killed twice over: `defer`/`errdefer` do
   structural cleanup (Chapter 21), and `?` does the early exit
   (Chapter 19).
2. **Escaping nested loops** — this is the one that survives, as
   labeled `break`/`continue`. The alternatives are a flag checked at
   every level, or extracting a function purely so you can `return`.
   Both are worse. Go and Rust both have labels; they are used rarely
   and gratefully.
3. **Threaded dispatch** — computed `goto` in C interpreters.
   `for { match state { … } }` *is* the state machine; compiling it to
   threaded dispatch is a release-backend optimisation obligation, and
   it is on the backend requirements list.

What remains of `goto` — backward jumps, jumps into blocks — is the
part nobody defends.

Note the constraint on labels: they name *loops*, and `break` exits
outward through enclosing loops only. There are no arbitrary jumps.

#### Why blocks are expressions

One general rule beats several carve-outs. Go grew an init clause on
`if` and on `switch` for exactly one purpose: scoping a helper variable
to a statement. Glide says any block is an expression whose tail is its
value and whose locals die at the brace, and gets the same containment
for free — plus computed-and-hidden values, which Go's init clause
cannot express.

It is also what makes the function-body tail rule consistent rather
than special-cased. A function body *is* a block.

#### Why no `if` init clause

`if v, ok := m[k]; ok { … }` existed to scope `v` to the branch. Two
things replace it:

- The dominant case is Option-shaped, and `if let v = m[k] { … }`
  covers it better — map indexing returns `V?`, so the comma-ok dance
  is gone entirely (Chapter 14).
- Everything else declares on the line above. With the shadowing trap
  unspellable (Chapter 4), a variable living one line longer is a
  non-cost.

---

### 4. Competing Approaches

**Go.** One loop keyword (the model), `switch` with implicit break,
labeled break/continue, `goto`, statement-only `if` with an init
clause. Glide keeps the loop, replaces `switch` with `match`, keeps
labels, drops `goto` and the init clause, and makes `if` an expression.

**Rust.** `loop`/`while`/`for` — three keywords where Glide has one —
plus `while let`, labeled loops with `'label:` syntax, and expression-
oriented everything. Rust's `loop` can also *return* a value via
`break value`, which Glide does not have; the use case (retry loops
that produce a result) is covered by a mutable accumulator or a
recursive function.

**C / C++.** `for`, `while`, `do`/`while`, `goto`, `switch` with
fallthrough by default. The fallthrough default is the famous mistake —
a missing `break` is a silent bug, which is why every C linter checks
for it and why Go inverted the default.

**Python.** `for`/`while` with the genuinely unusual `for … else`
construct (the `else` runs if the loop completed without `break`),
which is widely considered a wart because almost nobody remembers what
it means. Python has no labeled break, so escaping nested loops needs a
flag or an exception — a real gap.

**JavaScript.** `for`, `for…in`, `for…of`, `while`, `do…while`, labeled
statements, and `switch` with fallthrough. `for…in` iterating keys and
`for…of` iterating values, with `for…in` also walking the prototype
chain, is a genuine trap. Glide's `for (k, v) in map` avoids the whole
question by yielding pairs.

**Zig.** `while` with a continue expression, `for` over slices, labeled
blocks that can *break with a value* (`blk: { break :blk x; }`) — an
elegant feature Glide does not need, because blocks already produce
their tail expression.

---

### 5. Common Mistakes

**Writing `if xs` for a collection.** No truthiness:

```glide
// Bad
if items { … }

// Good
if !items.is_empty() { … }
```

**Forgetting `else` in value position.** An `if` used as a value must
have an `else`, because the false branch needs to produce something
too. (Today the interpreter is lenient here — `let x = if true { 1 }`
runs — but the checker will reject it, so write the `else`.)

**Hitting the struct-literal ban and not recognising it.** The error is
`expected an expression, found ':'`, which does not obviously say
"parenthesise your struct literal". When you see it in an `if`, `for`,
or `match` header, that is what happened.

**Using a label where a function extraction is clearer.** Labels are
for escaping nested loops. If the inner loop has its own meaning, a
function with an early `return` usually reads better:

```glide
// Fine
search: for row in rows {
    for cell in row {
        if cell == target { break search }
    }
}

// Often better
fn find_in(rows: List<List<Int>>, target: Int) -> Bool {
    for row in rows {
        for cell in row {
            if cell == target { return true }
        }
    }
    false
}
```

**Trying to `break` out of a closure.** Parse error. A closure is a
function boundary.

**Reaching for a C-style `for` loop.** There is no three-clause form.
Counting is `for i in 0..n`; anything else is `for cond` with the
update in the body.

**Iterating a map and expecting a hash-random order.** Glide's maps are
**insertion-ordered**, deliberately, which makes programs and golden
tests deterministic. Do not write code that depends on *not* having an
order; equally, do not be surprised that the order is stable. (Go
randomises map iteration specifically to stop people depending on
order; Glide instead makes the order well-defined.)

**Putting a bare block inside a hot loop for scoping, in the
interpreter.** It costs an environment allocation today. Harmless in
compiled code; measurable now.

---

### 6. Performance Considerations

**`for x in xs` is a method call per element** in the general case:
`next()` returning an `Option`. In the designed compiler this inlines
and, for a `List`, degenerates to an index-and-bounds-check loop — the
same code you would write by hand. In the interpreter it is a real
dynamic dispatch per element, which is one of the main reasons
tree-walked loops are slow.

**Ranges allocate nothing.** `for i in 0..1_000_000` yields lazily.
`(0..n).iter().collect()` is the spelling that actually materialises a
list, and its extra length is deliberate.

**Fresh loop bindings cost nothing** when nothing captures them — the
compiler collapses them back to one slot. They cost one allocation per
iteration when a closure *does* capture, which is exactly when you need
it.

**Labeled break costs nothing** at the compiled tier; it is a jump. In
the interpreter it is a signal propagating up through the evaluator,
which is a handful of returns.

**Blocks cost nothing** at the compiled tier and one map allocation per
entry in the interpreter.

**`if` as an expression costs nothing** relative to the statement form
— it is the same branch, with the result in a register rather than
stored to a variable. If anything, the expression form is cheaper,
because it avoids the write-then-read of a mutable slot.

---

### 7. Best Practices

**Use expression-`if` for assign-once values.** This is the single
biggest readability lever in the chapter:

```glide
// Bad
let mut level = ""
if errors > 0 {
    level = "error"
} else if warnings > 0 {
    level = "warn"
} else {
    level = "ok"
}

// Good
let level = if errors > 0 {
    "error"
} else if warnings > 0 {
    "warn"
} else {
    "ok"
}

// Better still, for a chain this shape
let level = match {
    errors > 0   => "error"
    warnings > 0 => "warn"
    _            => "ok"
}
```

The third form is what nested ternaries were always trying to be, and
`match` gets exhaustiveness checking that a chain of `if`s does not
(Chapter 10).

**Prefer guard clauses to nesting.**

```glide
// Bad
fn handle(req: Request) -> Result<Response, ApiError> {
    if req.method() == "POST" {
        if req.body().len() > 0 {
            if authorized(req) {
                …
            } else {
                Err(.Forbidden)
            }
        } else {
            Err(.BadInput{ msg: "empty body" })
        }
    } else {
        Err(.MethodNotAllowed)
    }
}

// Good
fn handle(req: Request) -> Result<Response, ApiError> {
    if req.method() != "POST" {
        return Err(.MethodNotAllowed)
    }
    if req.body().len() == 0 {
        return Err(.BadInput{ msg: "empty body" })
    }
    if !authorized(req) {
        return Err(.Forbidden)
    }
    …
}
```

Same logic, one level of indentation, and each precondition reads as a
statement of fact.

**Iterate the thing, not the index.**

```glide
// Bad
for i in 0..xs.len() {
    process(xs[i])
}

// Good
for x in xs {
    process(x)
}

// When you genuinely need the index
for (i, x) in xs.iter().enumerate() {
    process(i, x)
}
```

The bad version pays a bounds check per element and can go out of range
if the list is mutated in the body.

**Use a bare block to shrink a scope before extracting a function.** A
block is free and keeps the code where it is read. Extract a function
when the code deserves a *name*, not merely to control scope.

**Do not use a loop where an adapter says it better** — and do not use
an adapter where a loop says it better:

```glide
// Good — a transformation
let titles = notes.iter().filter(|n| n.published).map(|n| n.title).collect()

// Good — an effect
for note in notes {
    publish(note)?
}

// Bad — an effect dressed as a transformation
notes.iter().for_each(|n| publish(n))    // and you cannot `?` inside
```

The `?` point is decisive: error propagation works in a loop and does
not work inside a closure, because `?` returns from the *closure*.

**Reserve labels for the case they exist for.** If you have more than
one label in a function, or a label crossing more than two levels, the
function wants splitting.

---

### 8. Examples

**A small state machine, showing `for` + `match` as the replacement for
threaded dispatch:**

```glide
type State = Scanning | InWord

fn count_words(s: String) -> Int {
    let chars = s.runes().collect()
    let mut state = State.Scanning
    let mut count = 0

    for c in chars {
        let space = c == ' '
        state = match state {
            Scanning => {
                if space {
                    .Scanning
                } else {
                    count += 1
                    .InWord
                }
            }
            InWord => {
                if space { .Scanning } else { .InWord }
            }
        }
    }
    count
}

fn main() {
    println(count_words("the quick  brown fox"))
}
```

```
4
```

Two structural points. A hand-rolled state machine in a language with
`match` is a `for` loop over a sum type, and it needs no `goto` and no
computed jump — the design document lists compiling this shape to
threaded dispatch as a *backend* obligation, not a syntax one.

And note the arm bodies: a `match` arm is a **single expression**, so a
multi-statement arm needs a block. `{ count += 1  .InWord }` is a block
whose tail expression is the new state. Getting this wrong — writing an
un-braced multi-line `if`/`else` chain as an arm body — is a parse
error, and it is the most common `match` formatting mistake.

(In real code you would write `s.split_whitespace().len()`. The example
exists to show the shape.)

**Nested loop escape, three ways:**

```glide
fn main() {
    let grid = [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
    let target = 5

    // 1. Labeled break — direct, fine for a small search.
    let mut found = (-1, -1)
    search: for (r, row) in grid.iter().enumerate() {
        for (c, cell) in row.iter().enumerate() {
            if cell == target {
                found = (r, c)
                break search
            }
        }
    }
    println("{found:?}")

    // 2. Extracted function with return — usually clearer.
    println("{locate(grid, target):?}")
}

fn locate(grid: List<List<Int>>, target: Int) -> (Int, Int) {
    for (r, row) in grid.iter().enumerate() {
        for (c, cell) in row.iter().enumerate() {
            if cell == target {
                return (r, c)
            }
        }
    }
    (-1, -1)
}
```

```
(1, 1)
(1, 1)
```

The second version is better: it has a name, it is testable on its own,
and its `(-1, -1)` sentinel is visible in one place. (A real version
would return `(Int, Int)?` — Chapter 14.)

**The block expression earning its keep:**

```glide
fn main() {
    // The working variables of the computation are hidden.
    let summary = {
        let raw = [3, 1, 4, 1, 5, 9, 2, 6]
        let sorted = raw.sorted()
        let lo = sorted[0]
        let hi = sorted[sorted.len() - 1]
        "range {lo}..{hi} over {raw.len()} values"
    }
    println(summary)
    // raw, sorted, lo, hi do not exist here.
}
```

```
range 1..9 over 8 values
```

Without blocks-as-expressions, either those four names leak into the
enclosing scope or you extract a function that exists only to hide
them.

---

### 9. Summary & Exercises

**Summary**

- **One loop keyword.** `for {}` is forever, `for cond {}` is while,
  `for pat in iterable {}` is iteration. No `while`, no `do`, no
  three-clause C form.
- Loop variables are a fresh binding per iteration, which is what makes
  closures created in loops correct.
- `break` and `continue` target the nearest loop; **labeled** forms
  target a named enclosing loop. Labels name loops only; there are no
  arbitrary jumps and there is no `goto`.
- **`if` is an expression.** Value-position `if` requires `else`. This
  replaces the ternary operator and eliminates assign-in-every-branch
  mutable variables.
- **Blocks are expressions**: the tail is the value, locals die at the
  brace. This replaces Go's `if` init clause with one general rule.
- Conditions must be `Bool`; there is no truthiness.
- Struct literals are banned in control-flow headers — parenthesise to
  force one.
- Some expressions diverge (`return`, `break`, `continue`, `os.exit`,
  panic), and divergence is *required* in a `let … else` block.
- Maps iterate in **insertion order**, deterministically.

**Exercises**

1. **Count your ternaries.** In a codebase you know, find ten uses of
   `?:` (or ten four-line assign-in-both-branches `if` statements in
   Go). Rewrite each as expression-`if` and note which ones would be
   better still as a subjectless `match`. The ratio tells you how much
   Go's refusal actually costs.

2. **Escape without a label.** Take the nested-loop search from the
   Examples section and rewrite it three ways: with a label, with an
   extracted function, and with a boolean flag checked at each level.
   Rank them, then argue why Glide provides the first two and no
   language provides good support for the third.

3. **Build the loop you miss.** Pick a loop shape from another language
   that Glide's single `for` does not obviously cover — Python's
   `for…else`, Rust's `loop { break value }`, a C-style loop with two
   counters, or `do…while`. Write the Glide equivalent. For each,
   decide whether the extra keyword would have earned its place, and
   note which ones become natural once `match` and iterators are in
   hand.
