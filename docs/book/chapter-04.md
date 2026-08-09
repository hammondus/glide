# Chapter 4: Bindings, Mutability, and Shadowing

Three rules govern every name in a Glide program, and between them they
delete an entire family of bugs that other languages spend static
analysers hunting:

1. `let` always declares. `=` always assigns. Assigning to an
   undeclared name is an error.
2. Bindings are immutable unless marked `mut`.
3. One live binding per name, always — redeclare in sequence freely,
   never in nesting.

None of the three is novel. The third is the interesting one, because
it is two features that most languages conflate under the word
"shadowing", separated and given opposite verdicts.

Everything in this chapter is ✓ except where marked.

---

### 1. Basic Usage

#### Declaring

```glide
let name = "glide"
let port = 8080              // Int, inferred
let debug: Bool = false      // annotation optional
```

`let` is the only declaration form. There is no `:=`, no `var`, no
implicit declaration on first assignment. Type annotations are allowed
anywhere and are usually omitted inside function bodies, because
inference is local and good at this.

Assigning to a name that was never declared is an error, not a
declaration:

```glide
fn main() {
    count = 1        // error — no `count` exists
}
```

#### Immutability by default

A `let` binding cannot be reassigned:

```glide
fn main() {
    let x = 1
    x = 2
}
```

```
error: line 3: cannot mutate through immutable binding "x" (declare it with `let mut`)
```

A variable that genuinely varies says so at its declaration:

```glide
let mut count = 0
count += 1
count += 1
println(count)        // 2
```

`mut` is the single most useful audit mark in the language. Skim any
function and you know exactly which locals can change. That property is
only worth anything if `mut` is *rare*, which is why the chapter's Best
Practices section spends time on how to avoid it.

There is no `++`. `count += 1` is the only increment. The compound
assignment operators — `+=`, `-=`, `*=`, `/=`, `%=` — all exist and all
require a `mut` path.

#### Assignment is a statement

```glide
let a = b = c              // does not parse
for x = next() { … }       // does not parse
```

Assignment does not produce a value, so an entire family of C
cleverness is unspellable. The classic `if (x = 1)` typo is doubly
impossible: assignment is not an expression, *and* conditions require a
`Bool` (there is no truthiness — Chapter 5).

#### Redeclaration in the same scope is idiomatic

This is the rule that surprises people, and it is deliberate:

```glide-run
fn main() {
    let input = "  8080  "
    let input = input.trim()             // new binding, same name
    let input = input.parse_int() ?? 0   // type changed: String → Int
    println(input)
}
```

```
8080
```

This is a **refinement pipeline**. Each `let` ends the previous
binding's life and starts a fresh, immutable one. After each line
exactly one `input` is alive, so there is never an "other" variable you
might have meant.

Compare the two alternatives you would otherwise be forced into:

```glide
// Bad — the naming dance
let input_raw = read_line()
let input_trimmed = input_raw.trim()
let input_parsed = input_trimmed.parse_int() ?? 0
```

Three names for one concept, and the reader has to track which stage
each is at. Worse, `input_raw` stays in scope and stays usable, which
is exactly the bug the naming was trying to prevent.

```glide
// Bad — and also impossible here
let mut input = read_line()
input = input.trim()
input = input.parse_int() ?? 0    // error: the type changed
```

`mut` reassignment cannot change a binding's type. Redeclaration can,
which is why the refinement pipeline needs it: `String` → `Int` is the
whole point.

#### Shadowing across scopes is banned

Redeclaring a name from an *enclosing* block inside a nested block is a
compile error:

```glide
fn main() {
    let count = 0
    for x in [1, 2] {
        let count = count + 1
        println(count)
    }
}
```

```
error: line 4: cannot shadow "count" from an enclosing block
       (redeclaring in the same scope is fine; nested shadowing is not)
```

This is the rule that kills Go's classic bug — an inner `:=` silently
declaring a new variable while the outer one stays unchanged, and the
change evaporating at the end of the block. It is not detected; it is
*unwritable*.

The scope of the ban is function-local: parameters and enclosing
locals. Module-level names stay shadowable, because protecting an
entire module namespace is why Go's `vet` shadow checker is too noisy
to enable by default.

Note also that a closure body is a new *function*, not a nested block,
so the shadow rule resets there. A closure may reuse an outer name.

#### The two rules compose: build, then seal

Because a loop body is a nested block, loop accumulators genuinely need
`mut`. The idiom is to be mutable during construction and immutable
after:

```glide-run
fn main() {
    let mut acc = []
    for i in 0..3 {
        acc.push(i * i)
    }
    let acc = acc          // sealed: same scope, so this is legal
    println("{acc:?}")
}
```

```
[0, 1, 4]
```

`let acc = acc` is a same-scope redeclaration, so it is allowed, and
from that line on the name is immutable. The freeze idiom falls out of
the two rules for free — it is not a special feature.

#### Blocks are expressions

A `{ … }` anywhere an expression can go is a scope whose tail
expression is its value:

```glide-run
fn main() {
    let size = {
        let n = 42
        if n > 50 { "big" } else { "small" }
    }
    println(size)          // small
    // n does not exist here
}
```

This is the containment mechanism. Where Go grew an init clause on `if`
(`if num := rand.IntN(100); num > 50`) specifically to keep a helper
variable from outliving the statement, Glide gets the same result from
one general rule: wrap a block around exactly the code that should see
the name.

A freestanding block also scopes a region, exactly as in Go:

```glide
{
    let scratch = expensive_setup()
    use(scratch)
}
// scratch is gone
```

#### Discarding, explicitly

```glide
_ = db.close()
```

`_` on the left of `=` evaluates the right side and throws the value
away, *visibly*. This is the same rule as the tail-value rule from
Chapter 3, and it applies inside `defer` blocks too. Silent discards do
not exist.

`_` is also the wildcard pattern, so `let (_, port) = addr` and
`let [_, path] = args` discard positionally.

#### Reserved names

The free builtins — `print`, `println`, `eprint`, `eprintln`, `expect`,
`channel`, `Ok`, `Err`, `Some` — cannot be bound:

```glide
let println = 5          // error at the declaration
```

`None` is a literal, not a binding. Keywords need no rule: they are
token kinds, so `let for = 5` simply does not parse.

Imports are *not* reserved. A local named `sql` in a file that imports
`sql` is legal, because both are named after the domain and the
collision is usually harmless. What is an error (○, checker era) is
using the module *through* a live shadow: binding `sql` and then
calling `sql.open()` in the same function produces a diagnostic naming
both parties.

---

### 2. Under the Hood

#### What a binding is at runtime

In the interpreter, a scope is a map from name to a binding *cell*. A
`let` inserts a new cell. A same-scope redeclaration inserts a new cell
under the same name, replacing the map entry — the old cell still
exists on the heap if anything references it, which is exactly what
makes closure capture behave correctly (below).

Mutability is a flag on the cell, checked at assignment time. For an
assignment through a path — `a.b.c = v`, `counts[k] = v` — the check
happens at the **root binding** of the path. `counts[word] = 1`
requires `mut counts`; whether the `Map` object itself is shared with
some other binding is not consulted.

That last sentence is the honest statement of a recorded sacrifice, and
it is important enough to state twice.

#### `mut` is a path property, not an object guarantee

Collections are reference types. Two bindings can refer to one object,
and the object can change under a `let` binding via the other path:

```glide-run
fn main() {
    let mut a = [1, 2, 3]
    let b = a              // b and a refer to the SAME list
    a.push(4)
    println("{b:?}")       // [1, 2, 3, 4]
}
```

`let b` means "no mutation *through this name*". It does not mean "this
object is frozen."

This is a real limitation and `DESIGN.md` records it as a deliberate
sacrifice. Freezing guarantees would need either a borrow checker
(declined — see Chapter 1) or immutable data structures. The designed
answer is the latter: stdlib persistent collections (`PList`, `PMap`,
Clojure-style structural sharing, ○) for code that needs a genuine
freeze, rather than pretending `mut` is Rust's `&mut`.

Note that the interpreter currently does not enforce receiver-mut on
*builtin* methods — `xs.push(3)` works through a `let` binding today.
That is a checker-era gap, noted in `glide/DESIGN-DECISIONS.md` so
nobody mistakes it for a semantic decision.

#### Closures capture bindings, not names

This is the subtlest consequence of "redeclaration creates a new
binding", and it is worth seeing run:

```glide-run
fn main() {
    let mut total = 0
    let add = |n| { total += n }
    add(40)
    add(2)
    println(total)         // 42 — the capture is live

    let name = "first"
    let who = || name
    let name = "second"    // new binding, same spelling
    println(who())         // "first" — the closure kept its binding
    println(name)          // "second"
}
```

```
42
first
second
```

Two rules interact here. **Capture is by reference**, so mutation
through a captured `mut` binding is shared and visible — `total` really
is 42. And **capture is of the binding, not the name**, so a later
same-scope redeclaration cannot retarget a closure that captured the
earlier binding.

The interpreter implements this by having a closure capture a flattened
snapshot of the binding *cells* at creation time. Sharing the cells
keeps mutation visible; snapshotting keeps a later redeclare from
retargeting. `glide/DESIGN-DECISIONS.md` records that the first
implementation got this wrong — it looked names up in a live
environment map at call time — and the bug was invisible until a
closure straddled a redeclare.

#### Why the shadow ban is still checked dynamically

The nested-shadow ban is a static property: it depends only on scope
structure, not on values. It is nevertheless one of the few rules the
**evaluator** still owns, so it fires when the shadowing `let`
actually runs:

```glide
fn main() {
    let count = 1
    if false {
        let count = 2       // never reported: this branch never runs
        println(count)
    }
    println(count)
}
```

`glide check` accepts that program. Change `false` to `true` and the
error appears. It is a gap of the safe kind — under-approximating,
never wrong — and it is listed with the others in Appendix D.

---

### 3. Why This Design?

#### Why `let` and not `:=`

Go's `:=` was the right call for Go in 2009 — it escaped
`FooFactory foo = new FooFactory()` — and it is the wrong purchase
here, for four reasons of escalating importance.

1. Go ends up with five declaration spellings (`var x T`, `var x = e`,
   `var x T = e`, `x := e`, and the grouped `var ( … )` form), and
   `:=` cannot even be used at package level.
2. `x, err := f()` *declares* `x` while *assigning* `err` — unless a
   new scope makes it declare both, silently. One operator, three
   behaviours, resolved by non-local context.
3. One character of ink carrying declaration semantics is a typo trap:
   `=` and `:=` differ by one keystroke and mean opposite things.
4. **Decisive: `:=` has no slot for mutability.** There is nowhere to
   put `mut`. This is precisely why Go has no immutable locals at all —
   not because the Go team thought immutability was a bad idea, but
   because the syntax they picked had no room for it.

`let` costs four characters and buys the highest-value auditability
feature in the language. Multi-value declaration is handled by fiat:
`let a, b = f()` declares both, `a, b = f()` assigns both, and the
mixed case is simply unwritable.

#### Why sequential redeclaration is allowed

Because the danger in shadowing comes entirely from having *two live
bindings*, and sequential redeclaration produces one.

Consider what can go wrong. With two live bindings, you can write to
the wrong one, read the wrong one, or have your change evaporate when a
scope ends. With one live binding, none of those failure modes exists —
there is no "other" variable. The old binding is unreachable by name
from that line onward.

What it buys is significant: a refinement pipeline keeps one honest
name through a series of transformations, each stage immutable, stages
free to change type. Without it you get either the `input_raw` /
`input_trimmed` naming dance or a `mut` variable that cannot change
type. Rust has had this for a decade and it is one of the quiet wins
people cite when they miss Rust in other languages.

#### Why nested shadowing is banned

Because it is the *other* feature, and it has the opposite risk
profile.

The Go bug shape needs two live bindings: `result, err := bar()` inside
an `if` block silently declares new variables, the outer ones stay
unchanged, and the change evaporates at the block's closing brace. That
requires an inner binding coexisting with a live outer one of the same
name.

C# and Java both ban exactly this, and nobody lists it among their
regrets. The cost is close to zero: in the rare case where you
genuinely want a differently-scoped variable with the same conceptual
meaning, either use a different name or wrap the region in a bare block
so the outer name is not live.

Note that Glide treats the root cause separately as well. Go's trap
needed `:=`'s dual personality; here `let` always declares and `=`
always assigns, so "meant assign, got declare" is unspellable
regardless of scoping.

#### Why immutable by default

The argument is not "mutation is bad." It is that **the default should
be the case that needs no explanation**, and most locals in most
programs are assigned once.

Making immutability the default and mutation the marked case means the
marks are informative. If 90% of your locals are `let`, then `mut` on a
binding is a signal. If everything is mutable by default — as in Go,
Java, C, Python — then the absence of a marker tells you nothing, and
you have to read the whole function to know whether a variable changes.

This is the auditability pillar in its purest form, and the same
reasoning appears again in Chapter 16 (receiver mutability is declared)
and Chapter 7 (free functions that mutate a parameter repeat the marker
at the call site).

#### Why blocks are expressions

One rule replaces several carve-outs. Go has an init clause on `if` and
on `switch` specifically so that a helper variable can be scoped to a
statement. Glide instead says: any block is an expression, its tail is
its value, and its locals die at the closing brace. Wrap a block around
exactly what should see the name.

This is also what makes the tail-expression rule for functions
consistent rather than special — a function body *is* a block, and it
behaves like every other block.

---

### 4. Competing Approaches

**Go.** `var` and `:=`; everything mutable; shadowing allowed
everywhere and a `vet` check too noisy to enable. Go's model is the
direct source of the bug Glide's nested-shadow ban exists to kill. Go
also has no way to express "this local never changes", which means
readers cannot skim for mutation.

**Rust.** `let` and `let mut`, sequential shadowing allowed *and* nested
shadowing allowed. Glide takes the first and rejects the second. Rust
gets away with nested shadowing partly because its borrow checker
catches many of the resulting mistakes and partly because the
community has absorbed the idiom. Rust's `let mut` and Glide's are
otherwise the same idea; the difference is that Rust's immutability
interacts with the borrow checker to give genuine freezing guarantees,
where Glide's is an auditability marker over reference semantics.

**Swift.** `let` and `var`, with `let` meaning immutable — the closest
analogue, and a good demonstration that the ergonomics work in a
GC'd language. Swift allows nested shadowing.

**JavaScript.** `var`, `let`, and `const`, with `var`'s function
scoping and hoisting being the historical disaster that `let` exists to
fix. `const` in JS means the *binding* cannot be reassigned while the
object stays mutable — exactly Glide's "path property, not object
guarantee" caveat, and a good source of intuition for it.

**Python.** No declaration at all; assignment declares, and scope is
function-level with `global`/`nonlocal` escape hatches. The
"assignment inside a function shadows a global you meant to modify"
bug is the same shape as Go's, arrived at from the opposite direction.

**C / C++.** Declaration and assignment are distinct, `const` exists
and is under-used, and nested shadowing is allowed with a warning most
projects disable. C++'s `const` propagation through methods is the
ancestor of Glide's receiver-mutability rule.

---

### 5. Common Mistakes

**Reaching for `mut` when a redeclaration would do.** The most common
Go-habit mistake:

```glide
// Bad
let mut s = read_line()
s = s.trim()
```

```glide
// Good
let s = read_line()
let s = s.trim()
```

The second version is immutable throughout, works when the type
changes, and does not spend a `mut` marker.

**Expecting `let` to freeze an object.** Covered above; worth repeating
because it catches people who arrive from Rust:

```glide
let xs = [1, 2, 3]
let mut ys = xs
ys.push(4)
println("{xs:?}")      // [1, 2, 3, 4] — same object
```

If you need a genuine copy, copy explicitly. If you need a genuine
freeze, that is what persistent collections (○) are for.

**Trying to shadow in a loop body.** A loop body is a nested block:

```glide
let total = 0
for x in xs {
    let total = total + x    // error: cannot shadow enclosing "total"
}
```

The fix is `let mut total = 0` and `total += x`, then seal with
`let total = total` after the loop if you want immutability downstream.

**Forgetting that a closure body resets the shadow rule.** This is
legal, and occasionally surprising:

```glide
let n = 10
let f = |n| n * 2       // fine — a closure is a new function boundary
println(f(21))          // 42
```

**Assuming redeclaration retargets closures.** It does not, and the
behaviour is deliberate. If you want a closure to see a later value,
capture a `mut` binding and mutate it, rather than redeclaring.

**Using `_ = x` to silence an unused variable.** Do not. Glide has a
different mechanism: prefix the *declaration* with an underscore.

```glide
// Bad — Go's habit; silences at a distance
let conn = open()
_ = conn

// Good — declared as deliberately unused, visible in review
let _conn = open()
```

`_ = expr` means "evaluate this and discard the result". `_name` means
"this binding is deliberately unused". Two different intentions, two
different spellings.

---

### 6. Performance Considerations

**Immutability is free.** `let` versus `let mut` is a compile-time
property with no runtime representation. It affects what the optimiser
may assume (○, release tier) and nothing else.

**Redeclaration is free.** A new binding is a new slot in a stack frame
that the compiler was going to allocate anyway; the old slot becomes
dead and its value is eligible for collection at its last use. In the
interpreter it is one map insert.

**Blocks-as-expressions are free.** A block introduces a scope, which
in a compiled tier is a lexical concept with no runtime cost. In the
interpreter it is one child environment map — small, but not zero, so a
block inside a tight loop has measurable cost today that it will not
have when compiled.

**The freeze idiom is free.** `let acc = acc` re-binds an existing
value; no copy happens. This matters: the idiom would be useless if
sealing meant copying a large list.

**Closure capture costs one pointer per captured binding.** In the
interpreter, a closure snapshots the binding cells it references — a
small map. In the designed compiler, a closure is a function pointer
plus a capture record, heap-allocated only if the closure escapes. This
is the same model as Go's, and Chapter 8 covers it properly.

**The reference-semantics decision has a real cost model.** Because
collections are references, passing a big list to a function is a
pointer copy, not a deep copy. That is fast and it is why `let` cannot
freeze. If you want value semantics for a collection, you copy it
yourself, and the copy is visible in the source — the pricing pillar
again.

---

### 7. Best Practices

**Default to `let`. Earn every `mut`.**

The measure of a healthy Glide function is that `mut` appears where
something genuinely accumulates and nowhere else. When you find
yourself writing `mut`, ask whether one of these covers it instead:

| Instead of `mut` | Use |
|---|---|
| Assign once, in two branches | `let x = if cond { a } else { b }` |
| Assign once, in many branches | `let x = match … { … }` |
| Transform through stages | Sequential redeclaration |
| Build a list, then read it | `let mut` inside the loop, `let xs = xs` after |
| Compute with helpers you want hidden | A block expression |
| Provide a default for an absent value | `let x = maybe ?? default` |

**Seal after building.**

```glide
// Good
let mut routes = http.router()
routes.get(`/notes/{id}`, get_note)
routes.post(`/notes`, create_note)
let routes = routes            // no more registration after this line
```

The seal is documentation: it marks the end of the construction phase.

**Keep the refinement pipeline honest.** Redeclaration is for stages of
*the same value*, not for reusing a convenient name:

```glide
// Good — stages of one thing
let body = req.body()
let body = body.trim()
let body = json.decode(body)?

// Bad — two unrelated things sharing a name
let result = compute_total(xs)
let result = format_report(users)     // these are not the same value
```

The second version compiles and is confusing. The name should describe
the value, and if the value changed identity, so should the name.

**Use bare blocks to shrink scopes rather than extracting functions
prematurely.** A block costs nothing and keeps the code where it is
read. Extract a function when the code has a *name* worth giving it,
not merely to control scope.

**Prefer `_name` over `_ = name`.** Deliberate-unused is a property of
the declaration; declare it there.

**Do not fight the shadow ban with creative naming.** If you hit
"cannot shadow enclosing X", the usual right answer is that the inner
thing is a different concept and deserves a different name. The
second-best answer is a bare block around the region so the outer name
is not live. The worst answer is `x2`.

---

### 8. Examples

**A parser stage using every rule in the chapter:**

```glide-run
// Parse "KEY=value" lines into a map, skipping blanks and comments.
fn parse_env(text: String) -> Map<String, String> {
    let mut out: Map<String, String> = [:]

    for line in text.lines() {
        let line = line.trim()                    // refinement
        if line == "" || line.starts_with("#") {
            continue
        }
        let parts = line.split("=")
        if parts.len() != 2 {
            continue
        }
        out[parts[0].trim()] = parts[1].trim()    // mut path required
    }

    out
}

fn main() {
    let text = `
# a comment

HOST=db.local
PORT = 5432
`
    let env = parse_env(text)
    println("{env:?}")
}
```

```
["HOST": "db.local", "PORT": "5432"]
```

(Map Debug output uses the map-literal spelling — brackets and colons,
not braces. Chapter 11 explains why the bracket family owns
collections.)

Notes: `out` is the only `mut` in the function, and it is a genuine
accumulator. `let line = line.trim()` inside the loop body is a
same-scope redeclaration of the *loop variable* — legal, because the
loop variable is bound in the body's scope, not an enclosing one. The
backtick string is a raw string: no escapes, no interpolation,
multiline (Chapter 6).

**The same function written badly:**

```glide
fn parse_env(text: String) -> Map<String, String> {
    let mut out: Map<String, String> = [:]
    let mut line = ""
    let mut parts = []
    let mut key = ""
    let mut value = ""

    for raw_line in text.lines() {
        line = raw_line.trim()
        if line == "" || line.starts_with("#") {
            continue
        }
        parts = line.split("=")
        if parts.len() != 2 {
            continue
        }
        key = parts[0].trim()
        value = parts[1].trim()
        out[key] = value
    }

    return out
}
```

Six `mut` bindings where one is needed. Every scratch variable is
hoisted to function scope, so a reader must check whether any of them
carries state *between* iterations — which is exactly the question the
good version answers by construction. The `mut` marker has been
devalued: in this function it means nothing, so a reader stops reading
it, and when a genuinely stateful variable appears they will miss it.

This is the practical argument for the whole design. `mut` is only an
audit mark if it is rare.

**The freeze idiom in context:**

```glide
fn build_index(words: List<String>) -> Map<String, Int> {
    let mut index: Map<String, Int> = [:]
    for (i, w) in words.iter().enumerate() {
        if index[w] == None {
            index[w] = i          // first occurrence wins
        }
    }
    let index = index             // sealed
    index
}
```

The seal on the second-to-last line is arguably redundant here — the
value is returned immediately — but in a longer function, where more
code follows, it is the marker that says "construction is over."

---

### 9. Summary & Exercises

**Summary**

- `let` declares, `=` assigns, and assigning an undeclared name is an
  error. There is no `:=` and no implicit declaration.
- Bindings are immutable unless declared `let mut`. `mut` is an audit
  mark, and it is only valuable if it is rare.
- Assignment is a statement, not an expression, so `a = b = c` and
  `for x = next()` are unwritable. There is no `++`.
- Sequential redeclaration in the same scope is legal and idiomatic —
  the refinement pipeline. It may change the binding's type.
- Nested shadowing of a live enclosing name is a compile error. This
  makes Go's evaporating-assignment bug unwritable. Closure bodies are
  new function boundaries, so the rule resets there.
- The two rules compose into "build with `mut`, then seal":
  `let mut acc = …; …; let acc = acc`.
- Blocks are expressions: `{ … }` yields its tail, and its locals die
  at the brace. This replaces Go's `if` init clause.
- Discards are explicit: `_ = expr` evaluates and throws away;
  `_name` declares a binding as deliberately unused.
- `mut` is a **path** property, not an object guarantee. Two bindings
  can alias one collection, and the object can change under a `let`
  name. This is a recorded, deliberate sacrifice.
- Closures capture by reference and capture *bindings*, not names.

**Exercises**

1. **Count your would-be `mut`s.** Take a function of 30–60 lines from
   a Go or Python codebase you maintain and translate it, using `mut`
   only where a value genuinely accumulates across iterations or
   branches. Count how many variables in the original were mutable and
   how many survive. If the ratio is not dramatic, look for the
   variables that are assigned in every branch of an `if` — those are
   expression-`if` in disguise.

2. **Break the aliasing rule.** Write a program where a `let`-bound
   list is modified through another binding, and then rewrite it so
   that cannot happen. You will find there are exactly two honest
   options: copy at the boundary, or never hand out the reference.
   Which one you pick is an API design decision, and it is the same
   decision you make in Go with slices.

3. **Design the third shadowing rule.** Glide bans nested shadowing of
   *locals* but permits shadowing *module-level* names. Find a case
   where that permission causes a real problem, then argue either for
   extending the ban or for keeping the exception. (The design
   document's reasoning is that a blanket ban is why Go's vet shadow
   check is unusable — attack or defend that.)
