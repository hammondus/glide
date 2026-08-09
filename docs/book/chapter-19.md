# Chapter 19: The Type Checker

Every chapter before this one has been describing a language. This one
describes the thing that reads what you wrote and decides whether it
means anything.

Glide's checker is not a linter, not an optional pass, and not a
compiler-only stage. **Every program is type-checked before it runs, in
every tier, and there is no flag that turns it off.** `glide run`
checks. `glide test` checks. There is no `--no-check` and there is not
going to be one.

Everything in this chapter is ✓ — it is the state of the interpreter as
shipped. The two places where the checker is deliberately *silent* are
marked as such, because knowing where a checker stops is as important
as knowing where it starts.

This chapter is placed here, at the end of Part IV, because you now
know every construct it has an opinion about: types, structs, sum
types, traits, generics, `mut`, patterns. Read it any earlier and half
the diagnostics would be about features you had not met.

---

### 1. Basic Usage

#### The three commands

```bash
glide check app.gld      # report and stop
glide run   app.gld      # check, then execute
glide test  app.gld      # check, then run test blocks
```

`glide check` is the only one that exists purely for the checker, and
it is a convenience in the shape of `go vet`: report everything, change
nothing, exit non-zero if anything was found. The other two run the
identical check first. A program that `glide check` accepts is a
program `glide run` will not reject at load time, and vice versa.

#### Reading a diagnostic

```glide
fn greet(name: String) -> String {
    "hello, {name}"
}

fn main() {
    println(greet(42))
}
```

```
app.gld:6:19: expected String, found Int
 6 |     println(greet(42))
   |                   ^^
```

Four parts, in the order you need them: **where** (`file:line:col`,
the format every editor already knows how to jump to), **what**
(`expected String, found Int`), **the line**, and **the span** — the
caret sits under `42`, the thing that is wrong, not under the call or
the function.

That last part is a design commitment, not an accident. A diagnostic
that points at a whole statement makes you re-read the statement; one
that points at the sub-expression tells you what to change.

#### Every error, not the first one

```glide
type Level = Debug | Info | Warn | Error

type Entry = struct {
    pub level: Level
    pub msg: String
}

fn label(l: Level) -> String {
    match l {
        Debug => "DBG"
        Info  => "INF"
        Warn  => "WRN"
    }
}

fn render(e: Entry) -> String {
    "{label(e.level)} {e.text}"
}

fn parse(line: String) -> Entry {
    let parts = line.split(" ")
    Entry{ msg: parts[1] }
}

fn main() {
    let e = Entry{ level: .Info, msg: 7 }
    println(render(e))
    let n = if e.msg == "" { 0 } else { "many" }
    println(n)
}
```

Five separate mistakes, five diagnostics, one run:

```
app.gld:9:5: match is not exhaustive: Error not handled
 9 |     match l {
   |     ^^^^^
app.gld:17:25: Entry has no field "text"
 17 |     "{label(e.level)} {e.text}"
    |                         ^
app.gld:22:5: missing field "level" in Entry literal (no zero values)
 22 |     Entry{ msg: parts[1] }
    |     ^^^^^
app.gld:26:39: expected String, found Int
 26 |     let e = Entry{ level: .Info, msg: 7 }
    |                                       ^
app.gld:28:13: these branches produce different types: Int and String
 28 |     let n = if e.msg == "" { 0 } else { "many" }
    |             ^^
```

The checker does not stop at the first failure, and it does not
*cascade* either: a `match` whose exhaustiveness failed does not then
produce a second error about the type of its arms. One mistake, one
diagnostic. Fixing a real bug should never reveal four phantom ones.

The lexer and the parser *do* stop at the first error, because a
program that does not parse has no reliable structure to keep reading.
That difference is invisible to you in practice — the program did not
compile, and here is why — but it is why a syntax error arrives alone.

#### What is annotated and what is inferred

The rule is one line long: **signatures are always written; bodies are
always inferred.**

```glide
fn totals(rows: List<(String, Int)>) -> Map<String, Int> {
    let mut acc: Map<String, Int> = [:]      // annotated: nothing else could say
    for (name, n) in rows {                  // inferred from `rows`
        let seen = acc[name] ?? 0            // inferred: Int
        acc[name] = seen + n
    }
    acc
}
```

Nothing crosses a function boundary. The checker never looks inside
`totals` to work out what its caller should pass, and never looks at
callers to work out what `rows` is. That restriction is what makes the
checker small, and it is why a type error points at the line you wrote
rather than at a function three modules away that "constrained" it.

The places you must annotate are exactly the places where the program
has not said enough:

```glide
let mut acc: Map<String, Int> = [:]     // an empty map has no element type
let ids: List<Int> = []                 // nor an empty list
let b: Box<Int> = Box.new()             // nor a constructor taking no arguments
let f = |x: Int| x + 1                  // nor a closure nothing else constrains
```

#### A partial catalogue

Twenty-odd rules, grouped by what they protect. Every one of these is
✓, and every one has a case in `glide/testdata/conformance/`:

**Calls and signatures** — argument types, arity, named arguments,
defaults, return type against the tail expression, `?` used on
something that is not a `Result`.

**Data** — struct literals (every field present, no extras, no zero
values), variant literals, field existence, method existence,
associated function existence, list and map element homogeneity.

**Types that must line up** — both arms of an `if` used as a value,
every arm of a `match`, `==` between types that can never be equal,
operator operands (including `Duration`/`Instant` arithmetic and
`distinct`'s refusal to inherit any operator at all).

**The type system's own rules** — generic bounds, trait conformance,
`Ord` behind `<`, `Self` inside traits and impls, undetermined type
parameters, generator yield types against the declared `Iterator<T>`.

**Everything else that has a right answer** — `mut` paths, match
exhaustiveness, unreachable match arms, integer literal range against
a sized type, `.Shorthand` against the expected type, the
spawn-captures-`mut` ban, and every name being defined.

```glide
type Blob = struct { n: Int }

fn biggest<T: Ord>(a: T, b: T) -> T {
    if a.cmp(b) > 0 { a } else { b }
}

fn main() {
    let x = biggest(Blob{ n: 1 }, Blob{ n: 2 })
    println(x.n)
}
```

```
app.gld:8:20: Blob does not implement Ord, required by T
 8 |     let x = biggest(Blob{ n: 1 }, Blob{ n: 2 })
   |                    ^
```

And in the other direction, inside the generic body:

```glide
fn biggest<T: Ord>(a: T, b: T) -> T {
    if a.len() > 0 { a } else { b }
}
```

```
app.gld:2:13: T has no method "len": it is bounded by Ord, which does not declare one
 2 |     if a.len() > 0 { a } else { b }
   |             ^
```

Two diagnostics from one rule: **a bound is the complete method set**.
Inside the body, `T` can do exactly what `Ord` declares. At the call,
the argument must be a type that declared `impl Ord`. Neither side can
lie to the other.

#### Diagnostics that carry the fix

Where there is one obvious repair, the message says it:

```
a spawned closure cannot capture the mutable binding "total" — the
parent may still be writing it. Freeze it first (`let total_now =
total`) or send it over a channel

cannot mutate through immutable binding "xs" (declare it with `let mut`)

variants are namespaced: write .Bad or A.Bad (bare variant names are
pattern-only)

cannot tell what T is in Box<T> here — annotate the binding

NotFound cannot be matched against an Error: it is the dynamic error
type, so the concrete error is inside it — recover it with
e.find(PortErr)

300 does not fit in u8
```

This is a deliberate style. A diagnostic that only names the violated
rule makes the reader translate it; one that names the edit does not.
Where there is *no* single obvious fix, the message says nothing about
repair rather than guessing — a wrong suggestion costs more than no
suggestion.

---

### 2. Under the Hood

#### Bidirectional checking

The checker has exactly two modes, and every expression goes through
one of them.

**`infer`** — "what type is this?" Applied where the program has not
said what it wants. `1 + 2` synthesises `Int`; `xs.len()` synthesises
`Int`; `User{ … }` synthesises `User`.

**`check`** — "is this a T?" Applied where the program *has* said what
it wants, and the expectation is pushed *inward*. Checking
`[1, 2, 3]` against `List<Float>` checks each element against `Float`
rather than inferring `List<Int>` and then complaining.

That inward push is what makes untyped literals work without a
constraint solver:

```glide
let x: Float = 5           // 5 is checked against Float — it is 5.0
let y: u8 = 200            // checked against u8 — fits
let z: u8 = 300            // checked against u8 — 300 does not fit in u8
```

and it is what makes `.Shorthand` resolve:

```glide-run
type Color = Red | Green | Blue

fn paint(c: Color) { println("{c}") }

fn main() {
    paint(.Red)        // `.Red` resolved against the expected type Color
}
```

This is Pierce and Turner's *local type inference* (2000), the same
scheme Rust, Swift, Kotlin and TypeScript all use for expressions. Its
defining property is the one Glide wants: inference is local, so an
error is reported where it happened.

#### Why mandatory signatures buy a cheap checker

The alternative is Hindley–Milner: infer everything, everywhere,
including function signatures, by generating constraints across the
whole program and solving them. That is what ML and Haskell do, and it
is why a Haskell type error can point at a line you did not write.

Glide's requirement that every signature be written removes the need
for all of it. There is:

- no unification across function boundaries — a call checks arguments
  against a signature that is *already known*;
- no constraint store, no occurs check, no let-generalisation;
- no need to run the checker to a fixed point.

The checker is a single pass over the AST with a scope stack. That is
the payoff for the discipline, and it is the entire reason a type
checker was tractable for one person to write.

#### The `Unknown` type, and reporting only when certain

Every checker faces the same question: what do you do about the parts
of the program you cannot model yet? There are two answers, and only
one of them is safe.

Glide's answer is **`Unknown`**: an internal type that is compatible
with everything and reports nothing. When the checker cannot determine
what something is, it becomes `Unknown`, and every rule that touches
an `Unknown` operand goes quiet.

```glide
fn main() {
    let f = |x| x + 1          // nothing constrains x — the closure is Unknown
    println(f("hello"))        // no diagnostic; fails at runtime instead
}
```

That looks like a weakness. It is the property the whole design rests
on: **the checker under-approximates, so widening its coverage can
never break a working program.** Any program that runs correctly today
will still be accepted tomorrow when a new rule lands, because the new
rule can only reject programs that were already wrong.

The opposite policy — reject anything you cannot prove safe — is what
makes a type system a negotiation. Every new rule breaks working code,
so every new rule needs an escape hatch, and the escape hatches are
what people write when they are in a hurry.

The concrete cost is on the previous page: `|x| x + 1` is unchecked.
The concrete benefit is that there is no `--no-check`, no `any`, no
`@ts-ignore`, and no cast, because none of them were ever needed to
get out of the checker's way.

#### The side tables: what the checker hands the evaluator

The checker does not only say yes or no. It returns an `Info` record —
four side tables keyed by AST node — and the evaluator reads it. This
is `go/types.Info`'s shape, and it is what makes the checker
*load-bearing* rather than advisory.

| Table | What it records | What the evaluator does with it |
|---|---|---|
| `Types` | the type of every expression | debugging, and the source of the other three |
| `Shorthand` | which type each `.Variant` resolved to | constructs the right variant |
| `Wrap` | where a `T` becomes a `T?` | boxes into `Some(v)` |
| `IntoError` | where a value becomes an `Error` | boxes into an `Error` |

`Wrap` is why `Option` is a real box — `Option<Option<T>>` is
representable and `Some(None) != None` — without every value carrying a
tag it does not need. The checker knows the one place the coercion
happens; the evaluator boxes exactly there.

`IntoError` is the same trick for the dynamic `Error` type
(Chapter 20), with one deliberate difference: the boxing is
**idempotent**, so an already-boxed `Error` passes through untouched.
That makes `IntoError` a *hint* rather than an instruction, which is
what allows it to be recorded even where the source type is `Unknown`.
Double-boxing an `Option` merely gives a different value; double-boxing
an `Error` would give one whose message is another error's rendering —
almost right, and very hard to spot.

#### The conformance corpus

`glide/testdata/conformance/` holds around fifty whole programs. A file
with no `// error:` comments must be **accepted**; a `// error: <text>`
comment on a line asserts exactly one diagnostic there, containing that
text.

```glide
fn main() {
    let xs = [1, 2, 3]
    xs.push(4)              // error: cannot mutate through immutable binding
}
```

Two things make this more than a test suite.

First, it is the **contract every future frontend must meet**. The
checker will be rewritten in Glide (Chapter 37); this corpus is what
proves the rewrite is faithful rather than merely plausible.

Second, coverage is **measured from the checker's own source**. A test
extracts every diagnostic format string out of the Go source and
asserts that some program in the corpus triggers it. Adding a
diagnostic without a case fails the build. A hand-maintained list would
rot exactly the way stale documentation rots; reading the source cannot.

That test has already earned its keep: writing a case for a `yield`
diagnostic revealed that the parser makes the condition impossible, and
the dead branch was deleted rather than covered.

#### Where the checker still stops

Three gaps are worth knowing, because all three are silent.

**A bound is not enforced when the type parameter appears only inside a
constructor.** `fn top<T: Ord>(xs: List<T>)` accepts a
`List<Blob>` even where `Blob` has no `Ord`; the failure arrives at
runtime as "Blob has no method cmp". Passing a bare `T` is checked;
passing a `List<T>` or a `T?` is not.

**The tail-value rule is enforced by the evaluator, not the checker.**
A no-arrow function whose body ends in a value is an error — but the
error arrives when that function is *called*, so `glide check` does not
report it.

**The nested-shadow ban is also the evaluator's.** It depends only on
scope structure, so it *could* be static; today a shadowing `let` on a
branch that never runs is never reported.

All three are `Unknown`-shaped holes: they under-approximate, so
closing them cannot break a program that works. They are listed here
rather than hidden because a book that describes a checker's ambitions
and not its edges teaches you to trust the wrong things.

---

### 3. Why This Design?

#### Why the checker is mandatory in the interpreter

This is the decision that reversed an earlier one, and the reasoning is
worth reading even if you never write an interpreter.

The original plan was to let the interpreter run unchecked and put the
checker in the compiler, where it "belongs". The problem is what that
makes true of the *language*. An interpreter that runs unchecked Glide
means unchecked Glide is the real scripting dialect — the one people
actually type. Annotations in that dialect are comments. They rot,
because nothing reads them, and by the time you compile a script that
has grown into a program, every annotation in it is a lie you now have
to audit.

So: one frontend, both tiers, no skip. The interpreter and the compiler
differ in how they *execute* a checked program. They never differ in
what they accept, and never in what it means. Drift between tiers
becomes unreachable by construction rather than something a test suite
hopes to notice — and "runs interpreted, fails compiled" is the failure
mode that has discredited every other dual-tier language.

`glide check` exists because report-and-stop is genuinely useful in an
editor and a Makefile. A *skip* does not exist, and adding one is not
an open question.

#### Why local inference and not full inference

Full inference is strictly more powerful and strictly worse to use.

- **Errors move.** With no signature to anchor it, a type error is
  reported wherever the solver's constraints finally collide — often
  in a caller, often in code you did not write.
- **Signatures stop being documentation.** An inferred signature can
  change meaning because someone edited a body two modules away.
- **The checker gets big.** Constraint generation, a solver, an occurs
  check, generalisation rules, and a set of well-known corner cases.

Glide already requires signatures for a reason that has nothing to do
with the checker: they are the documentation, and a reader should be
able to know what a function does from its first line. Given that
requirement, full inference would buy nothing and cost all of the
above.

#### Why under-approximation, always

The alternative framing: a checker can have false positives (rejecting
good programs) or false negatives (accepting bad ones). Glide accepts
only false negatives.

The reason is that the two failures have different costs *over time*.
A false negative is a bug you find at runtime — which is where you
would have found it anyway with no checker at all, so it costs
nothing relative to the baseline. A false positive is a correct program
you cannot run, and the fix is a cast, an escape hatch, or a
restructure. Ship enough of those and your users spend their day
arguing with the type system instead of using it.

The version of this rule you can hold in your head: **the checker is
allowed to be ignorant, never wrong.**

#### Why the checker feeds the evaluator instead of only judging it

A checker that only says yes or no is a checker you can delete and
still have a working program. That is a tempting property — it makes
the checker easy to skip — and it is the wrong one.

`Option` boxing is the concrete case. Making `Some(None)` differ from
`None` at runtime requires knowing *where* a `T` becomes a `T?`. The
evaluator cannot know that; only the checker does. So the checker
records the coercion sites and the evaluator boxes there — and the
checker is now load-bearing, which means it cannot be skipped, which is
exactly the property the first decision wanted.

---

### 4. Competing Approaches

| Language | Inference | Optional? | What goes wrong |
|---|---|---|---|
| **Go** | inside bodies only (`:=`) | no | almost nothing; Glide's model is Go's plus generics-with-bounds |
| **Rust** | local + trait resolution | no | powerful, and the error messages are the price |
| **Haskell / ML** | whole-program HM | no | errors report far from their cause |
| **TypeScript** | local, structural | yes — `any`, `@ts-ignore` | the escape hatches are load-bearing in real code |
| **Python + mypy** | gradual, opt-in | yes | unannotated code is unchecked forever; annotations rot |
| **Zig** | comptime-driven | no | errors surface at instantiation, C++-template-style |

The two rows that shaped Glide are **TypeScript** and **mypy**, both
for the same reason: an optional checker is a checker whose coverage
never reaches 100%, because the escape hatch is always cheaper than the
fix at the moment you meet it. Glide has no escape hatch, and it can
afford not to have one *only* because the checker under-approximates —
there is nothing to escape from.

The row that shaped the *diagnostics* is Rust. Rust's error messages
are the best in the industry, and the lesson taken here is the cheap
half: point at the sub-expression, name the fix where there is one, and
report every independent error in one pass without cascading.

---

### 5. Common Mistakes

**Expecting inference across a function boundary.**

```glide
fn double(n) -> Int { n * 2 }        // parse error: parameters are annotated
```

Every parameter and every return type is written. This is not a
limitation the checker is working around; it is the requirement that
makes the checker what it is.

**Assuming an unannotated closure is checked.**

```glide
let f = |x| x + 1
println(f("hello"))     // no diagnostic; runtime error
```

A closure passed to a typed slot takes its parameter types from that
slot and is fully checked. A closure bound to a `let` with nothing
constraining it has no types to check against, so it is `Unknown`. If
you are keeping a closure in a binding, annotate it:

```glide
let f = |x: Int| x + 1
println(f("hello"))     // expected Int, found String
```

**Expecting exhaustiveness where there is nothing to enumerate.**

```glide
fn describe(n: Int) -> String {
    match n {
        0 => "zero"
        1 => "one"
    }
}
```

Exhaustiveness is checked over sum types, `Option`, `Result` and
`Bool`. `Int`, `String` and structs have too many values to enumerate,
so they need a `_` arm, and forgetting it is a runtime fall-through
rather than a compile error.

**Expecting a bound to be checked through a container.** Covered above
under *Where the checker still stops*. Write the bound anyway — it
documents the requirement and it will be enforced when the gap closes,
and closing it cannot break code that was already correct.

**Thinking `glide check` catches strictly more than `glide run`.** It
catches the same set, minus the one runtime-enforced rule noted above.
It is faster and it does not execute anything; it is not a stricter
mode.

**Reaching for a cast.** There isn't one. If the checker is wrong about
your program, that is a bug in the checker — the design says it may be
ignorant but not wrong — and the fix belongs in the checker, not in
your source.

---

### 6. Performance Considerations

**Checking is one pass and costs almost nothing.** No constraint store,
no solver, no fixed point. For scripts, the check is lost in process
startup; you will not notice it in `glide run`.

**Type erasure at runtime.** The interpreter runs generics erased — it
does not specialise `Stack<Int>` — which is possible precisely
*because* the checker already enforced every rule. Checking and
specialising are separable, and only the compiled tier needs the
second.

**The side tables cost memory proportional to the AST.** They are
keyed by expression node and live as long as the program does. For a
script this is invisible; it is one of the several reasons the compiled
tier will do its work at build time and emit code, not tables.

**Diagnostics are built lazily.** The source line and caret are
rendered when an error is printed, not when it is recorded, so an
accepted program never pays for the formatting machinery.

---

### 7. Best Practices

**Put `glide check` in your Makefile and your editor.** It is the fast
feedback loop; `glide run` is for when you want the program to
actually happen.

```make
check:
	glide check src/*.gld
```

**Annotate any closure you store.** A closure passed straight to
`sort_by` or `map` is checked against the parameter. One that lands in
a `let` first is not, unless you say what it takes.

```glide
// Unchecked — nothing constrains x
let key = |row| row.1

// Checked
let key = |row: (String, Int)| row.1
```

**Annotate empty collections at their declaration, not at their first
use.**

```glide
// Bad — the annotation is doing nothing here; the empty literal
// already had to guess
let mut seen = [:]

// Good
let mut seen: Map<String, Bool> = [:]
```

**Write bounds even where they are not yet enforced.** `<T: Ord>` is a
statement about what the function needs. It documents the requirement,
it is checked in the direct-argument case today, and it will be checked
everywhere later — and because the checker under-approximates,
"later" can never break you.

**Do not restructure code to please the checker's silence.** If a
construct is unchecked, that is not permission to rely on it; it is a
gap. Write the program you meant, and the checker will catch up.

**Read the caret, not the line.** The span is the answer. In
`expected String, found Int`, the caret is under the argument — which
tells you whether to change the call or the signature.

---

### 8. Examples

#### The five-error program, repaired

The program from section 1, with every diagnostic addressed. Nothing
here is a workaround: each fix is what the code was trying to say.

```glide-run
type Level = Debug | Info | Warn | Error

type Entry = struct {
    pub level: Level
    pub msg: String
}

fn label(l: Level) -> String {
    match l {
        Debug => "DBG"
        Info  => "INF"
        Warn  => "WRN"
        Error => "ERR"
    }
}

fn render(e: Entry) -> String {
    "{label(e.level)} {e.msg}"
}

fn parse(line: String) -> Entry {
    let parts = line.split(" ")
    let level = match parts[0] {
        "DBG" => Level.Debug
        "INF" => Level.Info
        "WRN" => Level.Warn
        _     => Level.Error
    }
    Entry{ level: level, msg: parts.slice(1, parts.len()).join(" ") }
}

fn main() {
    for line in ["INF started", "WRN disk 91%", "ERR gave up"] {
        println(render(parse(line)))
    }
}
```

```
INF started
WRN disk 91%
ERR gave up
```

Worth noting what changed and what did not. The exhaustiveness fix
added a real case — `Error` was a level the program genuinely had to
handle, and the checker found the omission rather than inventing a
requirement. The `e.text` fix was a typo. The `Entry{ msg: … }` fix
supplied a field that had to come from somewhere, and working out where
is what turned three lines of `parse` into six that actually parse.

That is the pattern to expect: **exhaustiveness and mandatory
initialisation find missing logic, not missing syntax.**

#### Adding a variant, and letting the checker do the survey

```glide-run
type Shape = Circle(Float) | Square(Float)

fn area(s: Shape) -> Float {
    match s {
        Circle(r) => 3.141592653589793 * r * r
        Square(w) => w * w
    }
}

fn name(s: Shape) -> String {
    match s {
        Circle(_) => "circle"
        Square(_) => "square"
    }
}

fn main() {
    let shapes = [Shape.Circle(1.0), Shape.Square(2.0)]
    for s in shapes {
        println("{name(s)}: {area(s):.2}")
    }
}
```

```
circle: 3.14
square: 4.00
```

Now add a variant — one line — and ask the checker where the work is:

```glide
type Shape = Circle(Float) | Square(Float) | Rect(Float, Float)
```

```
app.gld:4:5: match is not exhaustive: Rect not handled
 4 |     match s {
   |     ^^^^^
app.gld:11:5: match is not exhaustive: Rect not handled
 11 |     match s {
    |     ^^^^^
```

Both sites, named, in one pass. This is the single highest-value
interaction in the language: **the checker converts "add a case" from
a code review problem into a compile error**. In Go the equivalent
change compiles, and the missing branch is a `default` that returns
zero.

#### Watching the checker stay quiet

Three programs the checker accepts, each for a different reason, and
each fails at runtime. Read them as a map of where the edges currently
are.

```glide
fn main() {
    let f = |x| x + 1               // Unknown: nothing constrains x
    println(f("hello"))
}
```

```
app.gld:2:19: operator + not defined for String and Int
 2 |     let f = |x| x + 1
   |                   ^
```

Note *where* that lands: inside the closure body, at run time, one call
after the mistake. A checked closure would have reported it at the call
site instead.

```glide-run
type Blob = struct { n: Int }

fn top<T: Ord>(xs: List<T>) -> T { xs[0] }

fn main() {
    println(top([Blob{ n: 1 }]).n)   // the bound is not checked through List<T>
}
```

```
1
```

— accepted, and it even *runs*, because this particular body never
calls `cmp`. Add one and it fails at runtime rather than at the call.

```glide
fn main() {
    let xs = [1, 2, 3]
    println(xs[7])                  // index bounds are a runtime question
}
```

```
app.gld:3:15: list index 7 out of range (len 3)
 3 |     println(xs[7])
   |               ^
```

The third is not a gap at all — it is a value, not a type, and no
checker without dependent types could answer it. Telling the three
apart is the skill this section is for.

#### A checked script, end to end

```glide-run
import fs
import os

type Row = struct {
    pub name: String
    pub size: Int
}

fn measure(dir: String) -> Result<List<Row>, Error> {
    let mut rows: List<Row> = []
    for name in fs.list_dir(dir)? {
        let path = fs.join([dir, name])
        if fs.is_dir(path) { continue }
        let body = fs.read_string(path) ?? ""
        rows.push(Row{ name: name, size: body.len() })
    }
    rows.sort_by(|a, b| b.size.cmp(a.size))
    Ok(rows)
}

fn main() -> Result<(), Error> {
    let dir = os.cwd()?
    let rows = measure(dir)?
    println("{rows.len()} file(s) in {dir}")
    for r in rows.slice(0, if rows.len() < 3 { rows.len() } else { 3 }) {
        println("  {r.size:8}  {r.name}")
    }
    Ok(())
}
```

Every annotation here earns its place, and each one is doing a
different job:

- `List<Row>` on `rows` — the empty literal has no element type.
- `Result<List<Row>, Error>` on `measure` — this is what makes `?`
  legal inside it, and what makes `?` legal on it in `main`.
- `|a, b|` in `sort_by` needs *no* annotation, because the parameter's
  declared type `fn(T, T) -> Int` supplies both — and `b.size.cmp(…)`
  is checked as a result.
- `Row{ name: …, size: … }` must mention both fields, because there
  are no zero values.

Delete any one of the two annotations and the checker asks for it back.
Get any of the four types wrong and it says so, at the sub-expression.

---

### 9. Summary & Exercises

**Summary**

- **Every program is checked, in every tier, with no way to skip it.**
  `glide check` reports and stops; `glide run` and `glide test` do the
  identical check first.
- **Signatures are written, bodies are inferred.** Nothing crosses a
  function boundary, so errors are reported where they happened.
- **Bidirectional checking**: `infer` synthesises a type, `check`
  pushes an expected type inward. That inward push is what makes
  untyped literals, `.Shorthand` and empty collections work with no
  constraint solver.
- **Mandatory signatures are what make the checker cheap** — no
  unification across functions, no constraint store, one pass.
- **`Unknown` is the safety valve.** Anything the checker cannot model
  passes in silence. The checker may be ignorant; it may not be wrong.
- **Under-approximation means new rules cannot break working code**,
  which is why Glide needs no cast, no `any`, and no `--no-check`.
- **The checker feeds the evaluator** through side tables — variant
  shorthand, `Option` boxing, `Error` boxing — which is what makes it
  load-bearing rather than advisory.
- **Diagnostics point at the sub-expression, name the fix where there
  is one, and do not cascade.** Every independent error in one pass.
- **The conformance corpus is the contract** for every future frontend,
  and its coverage is measured from the checker's own source.
- Three known silences: bounds through a container, the tail-value
  rule, and the nested-shadow ban. All under-approximate; all are safe
  to close later.

**Exercises**

1. **Break something on purpose, five ways.** Take a program you have
   written and plant one instance each of: a wrong argument type, a
   missing struct field, a non-exhaustive match, a mutation through a
   `let`, and a variant used without its namespace. Run `glide check`
   once. Did you get five diagnostics, and did each caret land on the
   thing you actually broke?

2. **Find the boundary of `Unknown`.** Write a closure with an
   annotated parameter and an unannotated one (`|a: Int, b|`) and use
   both wrongly. Which mistake is reported? Now pass the same closure
   directly to `sort_by` instead of binding it, and answer again.

3. **Add a variant to a real program.** Take any sum type in a codebase
   you know — in Glide, or the equivalent `iota` enum in Go — and add a
   case. In Glide, count the diagnostics. In Go, count the places you
   had to *find*. That difference is the argument for exhaustiveness,
   and it is worth measuring once rather than believing.

4. **Argue the other side.** The checker deliberately accepts
   `|x| x + 1`. Write down the strongest case for rejecting it
   instead — then work out what escape hatch that rejection would force
   into the language, and who would use it. (This is the actual reason
   Glide has no `any`.)
