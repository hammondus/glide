# The Glide Programming Language — Chapter 1: Introduction

*For experienced programmers meeting Glide for the first time. The
chapter opens by building one small program up from hello-world, a
step at a time; the rest covers, systematically, only the language
features used in the three example programs in `GRAMMAR.md`. There is
more language than this, but not much more — smallness is a feature.*

Glide is a compiled, statically typed language in the Go tradition:
garbage collected, green-threaded, one binary, boring on purpose. It
differs from Go in that it took the type system of the ML family — sum
types, pattern matching, no null — and the ergonomics lessons of the
decade since Go shipped. If you know Go, most of your instincts
transfer; where they don't, this chapter says so explicitly.

## 1.1 One program, one step at a time

We'll build `wordfreq` — read a file, count word frequencies, print
the top 20 — starting from the classic first program. Every step is a
complete, runnable program, and each introduces a handful of symbols
that the rest of the chapter then treats properly.

### Step 1 — hello, world

```glide
fn main() {
    println("hello, world")
}
```

Small as it is, everything here is load-bearing:

- `fn` declares a function. `main` is the entry point, and *nothing*
  runs before it — no init functions, no import side effects. Line
  one of `main` is line one of your program.
- The empty `()` is the parameter list: `main` takes no arguments.
- No return type is written, because `main` returns nothing. How
  return types are spelled shows up in step 3.
- `println` writes a line to stdout. Printing is built in; no import.

No semicolons. Braces mandatory, even around one statement.

### Step 2 — variables

```glide
fn main() {
    let name = "world"
    println("hello, {name}")
}
```

`let` declares a binding, and the type (`String` here) is inferred.
Bindings are **immutable**: adding `name = "moon"` on the next line
is a compile error, not a reassignment. A variable that actually
varies must say so at its declaration:

```glide
let mut count = 0
count += 1
```

Skim any function and `mut` shows you exactly which locals can
change — the audit mark the whole language leans on.

The `{name}` inside the string is interpolation: any expression
between braces, checked at compile time. There is no `printf` and no
format verbs.

### Step 3 — a second function

```glide
fn greet(name: String) -> String {
    "hello, {name}"
}

fn main() {
    println(greet("world"))
}
```

Two new things:

- Signatures are always written out in full — parameter types, then
  `->`, then the return type. The arrow reads "returns"; no arrow (as
  on `main`) means "returns nothing". Inference does the work inside
  bodies; boundaries are documentation.
- The body of `greet` has no `return`. A function's last expression
  *is* its value. `return` exists, but only for exiting early.

### Step 4 — accepting an argument

`wordfreq` takes a filename on the command line — and a user might
not supply one. Glide makes the might-not part impossible to skip:

```glide
import os

fn main() {
    let [_, path] = os.args() else {
        eprintln("usage: wordfreq <file>")
        os.exit(2)
    }
    println("will read {path}")
}
```

- `import os` brings in a module, used qualified: `os.args`,
  `os.exit`. Importing executes no code.
- `os.args()` returns a `List<String>` — the program name, then the
  arguments. `[_, path]` is a *pattern*: match a list of exactly two
  elements, discard the first (`_` always means "discard"), bind the
  second to `path`.
- A pattern that might not match needs a plan B, and `else` is it:
  wrong number of arguments and the else-block runs. The block must
  exit or return — it can't fall through, because `path` would have
  no value.
- Exact means exact, in both directions: `[_, path]` rejects *three*
  elements as surely as one. Patterns match precisely what they
  spell; "and whatever else" is never implied. A program that wanted
  to tolerate extra arguments would say so with a rest binding —
  `[_, path, ..rest]` matches two-or-more and binds the extras as a
  list (`.._` if you don't care about them). The absence of `..` *is*
  the exactness.
- `eprintln` is `println` aimed at **stderr**. The usage message goes
  there so it can't contaminate the program's real output: someone
  running `wordfreq notes.txt | sort` gets word counts in the pipe
  and complaints on the terminal, not usage text sorted in among
  their data.

### Step 5 — reading the file, or failing to

This step carries the most new weight; slow down here.

```glide
import fs
import os

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: wordfreq <file>")
        os.exit(2)
    }
    let text = fs.read_string(path).context("reading input")?
    println(text)
    Ok(())
}
```

`fs.read_string(path)` does not return a `String`. It returns a
`Result<String, Error>`: a value that is *either* a success carrying
a `String` *or* a failure carrying an `Error`. (`Result` is not a
keyword and not magic — it's an ordinary two-variant type that
happens to be built in, so the whole ecosystem agrees on one.)

The `?` at the end of the line unwraps it. Success: the expression
produces the inner `String`, and `text` is a plain `String` from then
on. Failure: the function **returns immediately**, handing the error
to its caller. It is Go's `if err != nil { return err }` in one
character — the early exit is still visible, on the line where it
happens, but it no longer dominates the page.

`.context("reading input")` decorates an error on its way out
("reading input: no such file") and passes successes through
untouched.

Because `?` can now return an error out of `main`, `main` must
declare that: `-> Result<(), Error>` reads "either succeeds with
nothing, or fails with an `Error`". The `()` is the **unit type** —
the way "nothing" is spelled where the grammar requires a type.

Both signatures of `main` are legal — step 1's plain `fn main()`, or
this one. And returned to whom? `main`'s caller is the runtime: it
turns `Ok(())` into exit code 0, and an `Err` into the error printed
on stderr and exit code 1. Without this, `?` would be unusable in
`main`, and every CLI would open with a ceremonial `run()` wrapper
existing only to give errors somewhere to go — Go's four-line
`if err := run(); err != nil` ritual, deleted.

Which explains the odd-looking last line. `Ok(...)` constructs the
success variant of a `Result`; what this one carries is `()`, the
unit *value*. Outer parens: a call. Inner parens: the nothing being
handed back. If `main` returned `Result<Int, Error>`, the line would
be `Ok(42)`, and the symmetry is obvious.

### Step 6 — counting words

```glide
let mut counts: Map<String, Int> = [:]
for word in text.split_whitespace() {
    counts[word] = (counts[word] ?? 0) + 1
}
```

- `[:]` is the empty map literal. The annotation is required here
  only because an empty literal gives inference nothing to go on.
- `for x in …` iterates anything iterable, and `for` is the only
  loop keyword in the language (`for { }` loops forever,
  `for cond { }` is a while).
- Reading `counts[word]` returns `Int?` — "an `Int` or nothing" —
  because the key may be absent. Glide has no null and no zero-value
  fallback; the map can't hand you a fake `0` and hope, so it hands
  you a maybe. `?? 0` supplies the default: first sighting of a word,
  the read yields nothing, `?? 0` makes it `0`, and the count becomes
  `1`.
- Assigning through an index (`counts[word] = …`) inserts or updates.

### Step 7 — sort, take twenty, print

```glide
let mut entries = counts.entries()          // List<(String, Int)>
entries.sort_by(|a, b| b.1.cmp(a.1))

for (word, n) in entries.iter().take(20) {
    println("{n:6}  {word}")
}
```

- `counts.entries()` returns a list of *tuples* — `(String, Int)`
  pairs. Tuple fields are positional: `.0`, `.1`.
- `|a, b| b.1.cmp(a.1)` is a closure: parameters between pipes,
  types inferred from context. Comparing `b` to `a` — rather than
  `a` to `b` — is what makes the sort descending.
- `.iter().take(20)` — iterators are lazy and compose; take the
  first twenty, stop.
- `{n:6}` is interpolation with a width: right-aligned in six
  columns.

### The whole program

Assembled, this is `wordfreq` — program 1 of the three in
`GRAMMAR.md` — and every symbol in it has now been introduced:

```glide
// wordfreq counts word frequencies in a file.
import fs
import os

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: wordfreq <file>")
        os.exit(2)
    }

    let text = fs.read_string(path).context("reading input")?

    let mut counts: Map<String, Int> = [:]
    for word in text.split_whitespace() {
        counts[word] = (counts[word] ?? 0) + 1
    }

    let mut entries = counts.entries()          // List<(String, Int)>
    entries.sort_by(|a, b| b.1.cmp(a.1))

    for (word, n) in entries.iter().take(20) {
        println("{n:6}  {word}")
    }
    Ok(())
}
```

The rest of this chapter goes back over these features more
carefully, then covers what a thirty-line CLI had no reason to use:
sum types, pattern matching, traits, generators, `defer`, structured
concurrency, and built-in tests.

## 1.2 Values and bindings

Variables are declared with `let` and are **immutable by default**:

```glide
let name = "glide"
let port = 8080          // Int, inferred
let debug: Bool = false  // annotation optional, inference is local
```

If you need to reassign, say so at the declaration:

```glide
let mut count = 0
count += 1
```

That `mut` is the single most useful audit mark in the language: skim
any function and you know exactly which locals can change. There is no
`:=`, no `var`, and no `++` — `count += 1` is the only increment.
Assignment is a statement, not an expression, so `a = b = c` and
`while x = next()` do not parse.

You may redeclare a name in the *same* scope — this is idiomatic for
refinement pipelines:

```glide
let input = read_line()?
let input = input.trim()          // fine: same scope, new binding
```

But shadowing an outer variable from a *nested* block is a compile
error. The Go bug where an inner `:=` silently creates a new variable
cannot be written.

## 1.3 There is no null — `T?` and friends

Glide has no null, no nil, no zero-value fallback. A value that might
be absent has an Option type, written `T?`:

```glide
let found: Note? = db.first(id)
```

You cannot use a `T?` as a `T`; the compiler makes you handle the
empty case, and the language gives you three ergonomic ways:

```glide
// 1. `??` — provide a default
let n = counts[word] ?? 0

// 2. `if let` — bind and unwrap in one move (Swift-style, no `Some` ceremony)
if let root = tree.root {
    // root is a Node here, not a Node?
}

// 3. `let … else` — unwrap or bail out early
let [_, path] = os.args() else {
    eprintln("usage: wordfreq <file>")
    os.exit(2)                     // the else-block must exit/return
}
```

Note what `??` did in that first line: map indexing `counts[word]`
returns an `Int?` (the key might be absent). Go returns a zero value
and hopes; Glide returns an Option and asks.

## 1.4 Errors are values — `Result` and `?`

Functions that can fail return `Result<T, E>`. The `?` operator
unwraps a success or returns the failure to the caller:

```glide
fn main() -> Result<(), Error> {
    let text = fs.read_string(path).context("reading input")?
    // ... text is a String here
    Ok(())
}
```

This is Go's errors-as-values philosophy without the `if err != nil`
skyline: the error path is still visible (every `?` is a possible
early return) but costs one character. `.context("...")` wraps the
error with a breadcrumb on the way up. `main` may return a `Result`;
an error prints and exits nonzero.

Error *types* are ordinary sum types (next section), so a function's
signature tells you exactly what can go wrong.

## 1.5 Declaring types

One keyword declares every shape of type:

```glide
type NoteId = distinct i64            // a distinct type

type Note = struct {                  // a struct
    pub id: NoteId
    pub title: String
    pub created: Time
} derive(Json, Row, Debug)

type ApiError =                       // a sum type
    NotFound(id: NoteId)
    | BadInput(msg: String)
    | Db(cause: Error)
```

Three things deserve attention:

**`distinct`** creates a new type with the same representation but no
implicit conversion. A `NoteId` is stored as an `i64` but you cannot
pass a plain `i64` (or an `OrderId`) where a `NoteId` is expected —
`NoteId(raw)` converts explicitly. Ten characters of declaration, and
an entire class of mixed-up-identifiers bugs is gone.

**Sum types** say "a value is exactly one of these shapes", and each
variant can carry data. Go has no equivalent — this is the feature
you'd miss most going back. Where the expected type is known you can
abbreviate a variant with a leading dot: `Err(.NotFound(id))`.

**`derive(...)`** generates implementations at compile time — JSON
encoding, database row mapping, debug printing — by walking the
struct's fields. It looks like magic; it is ordinary code generation,
at compile time, with no runtime reflection anywhere in the language.

Structs must be fully initialised at construction — there are no zero
values. `pub` marks what's visible outside the module (a directory of
source files, as in Go); everything else is private. Note `pub` on
fields: a public type with private fields is the normal encapsulation
pattern.

## 1.6 Pattern matching

`match` is the multi-way branch, and on sum types it is *exhaustive*:
cover every variant or the compiler objects. Add a variant next year,
and every match site that misses it becomes a compile error — the
language tells you what the change touched.

```glide
match found {
    Some(n) => Ok(http.json(n))
    None    => Err(.NotFound(id))
}
```

Patterns nest arbitrarily and bind as they match. Guards add
conditions; struct patterns pull out fields; `..` means "and the rest,
deliberately":

```glide
match at {
    None => Node{ value: value, left: None, right: None }
    Some(n) if value < n.value =>
        Node{ left: insert_node(n.left, value), ..n }
    Some(n) =>
        Node{ right: insert_node(n.right, value), ..n }
}
```

That `Node{ left: ..., ..n }` is a **struct update**: a copy of `n`
with one field replaced. In an immutable-first language this is the
standard way data evolves.

`match` and `if` are expressions — they produce values — which is why
Glide has no ternary operator:

```glide
let status = if ok { "active" } else { "disabled" }
```

Destructuring works anywhere a binding does — including tuples, which
Glide has and Go does not:

```glide
let (host, port) = parse_addr(s)?
for (word, n) in entries.iter().take(20) { ... }
```

## 1.7 Strings

Strings interpolate, always, with full expressions in the braces —
and it's checked at compile time:

```glide
println("{n:6}  {word}")            // width spec: right-align in 6
log.info("swept", { count: n })
return Err(.BadInput("{e}"))
```

There is no `printf`, no format verbs, no runtime format parsing. A
type that can't be displayed in a string is a compile error, not
`%!v(MISSING)` in production output.

## 1.8 Functions, closures, named arguments

Signatures are always explicit — they're documentation. Inside
bodies, inference does the work.

```glide
fn get_note(db: sql.Db, req: http.Request) -> Result<http.Response, ApiError>
```

Arguments can be named at the call site, and parameters can have
defaults; both together replace Go's functional-options ceremony and
most constructor overloads:

```glide
http.serve(addr: ":8080", handler: r)
```

Closures use pipes and infer their parameter types:

```glide
entries.sort_by(|a, b| b.1.cmp(a.1))
r.get("/notes/{id}", |req| get_note(db, req))
```

(`b.1` is tuple field access: `.0`, `.1`.) A closure and a named
function have the same type — `fn(A) -> B` — so refactoring one into
the other is cut and paste.

## 1.9 Methods and traits

Methods live in `impl` blocks. A method that mutates its receiver
must say `mut self` — and can then only be called through a `mut`
binding:

```glide
impl Tree<T> {
    pub fn new() -> Tree<T> { Tree{ root: None } }
    pub fn insert(mut self, value: T) { ... }
}

let mut t = Tree.new()      // mut required...
t.insert(3)                  // ...because insert mutates
```

Interfaces are **traits**, and conformance is declared — one line —
while satisfaction is structural (existing methods count):

```glide
impl Iterable<T> for Tree<T> {
    fn iter(self) -> Iterator<T> { ... }
}
```

Generics use angle brackets with trait bounds: `Tree<T: Ord>` reads
"a Tree of any T that is orderable". If you know Go's newer generics
or Rust's, nothing here will surprise you except the absence of
ceremony.

## 1.10 Loops, iterators, generators

There is one loop:

```glide
for { ... }                       // forever
for cond { ... }                  // while
for x in xs { ... }               // over anything iterable
for x in 0..10_000 { ... }        // over a range
```

Anything with an `iter()` method works in `for … in`. Iterators are
lazy and compose: `entries.iter().take(20)`.

Writing your own iterator is where Glide gets a feature Go and Rust
struggle with — **generators**. A function containing `yield` *is* an
iterator; the compiler builds the state machine:

```glide
fn walk<T>(n: Node<T>) -> Iterator<T> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

An in-order tree traversal, written as the traversal it is. Try
hand-writing this as a `next()` method with an explicit stack — that's
the code you're not writing.

## 1.11 Cleanup: `defer`

`defer` schedules a block to run when the enclosing scope exits —
success, error, or panic:

```glide
let db = sql.open("sqlite:notes.db")?
defer { _ = db.close() }
```

It is block-scoped (a defer in a loop body runs each iteration — the
Go fd-exhaustion bug is not reproducible), takes a block rather than
a call (no argument-evaluation puzzle), and discarding an error
inside one is visible (`_ =`).

## 1.12 Concurrency in one paragraph

Glide is green-threaded like Go — no async, no await, no function
coloring — but tasks are **structured**: they belong to a scope, and
the scope's end waits for them; a failing child cancels its siblings;
nothing leaks.

```glide
scope s {
    s.spawn(|| sweeper(db))       // dies with the scope
    http.serve(addr: ":8080", handler: r)
}
```

If `serve` returns with an error, the sweeper is cancelled — no
`context.Context` threading, no orphaned goroutine. Cancellation is
ambient: blocking calls inside a cancelled scope (like the sweeper's
`time.sleep`) simply stop waiting.

## 1.13 Tests live with the code

Tests are a language construct, in any source file:

```glide
test "in-order traversal is sorted" (xs: List<Int>) {
    let mut t = Tree.new()
    for x in xs { t.insert(x) }
    expect(t.iter().collect() == xs.sorted())
}
```

Two things to notice: `expect(a == b)` needs no assertion library —
on failure the compiler-instrumented expression reports both sides.
And this test takes a *parameter*: the runner generates hundreds of
input lists, and on failure shrinks to the smallest one that breaks —
property-based testing, built in. `bench "..." { }` blocks are
benchmarks; the runner owns the timing loop.

`glide test` runs it all — and also enforces formatting, unused-code
hygiene, and doc-link validity. The compiler stays quiet while you
edit; the test boundary is where the standards apply.

## 1.14 Where the rest lives

What this chapter skipped (because the examples did): channels and
`select`, `errdefer`, comptime, labeled loops, the unsafe boundary,
embedding. The design rationale for everything — including every
deliberate rejection — is in `DESIGN.md`, which is unusually honest
about costs and is the recommended second read.

The shortest useful summary of Glide for a Go programmer might be:
*your runtime, your tooling philosophy, your deployment story — the
ML family's types, the decade's ergonomics, and nothing you didn't
ask for.*
