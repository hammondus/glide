# The Glide Programming Language — Chapter 1: Introduction

*A complete tour of the language. It opens by building one small
program up from hello-world, a step at a time; the rest describes
every feature in detail, assuming you know programming but have never
met the feature — delete what you already know. There is more
language than this chapter, but not much more: smallness is a
feature.*

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
happens to be built in, so the whole ecosystem agrees on one.
"Variant" gets its proper treatment in §1.6; for now: one value,
exactly one of two shapes, and it remembers which.)

If you come from Go, this is the `(string, error)` return pair fused
into one value that cannot hold both. Go hands you both slots and
trusts you to check `err` before touching the string — nothing stops
the code that forgets. A `Result` is the either-or made physical:
there is no string to touch until you've gone through the check. `?`
is that check.

The `?` at the end of the line unwraps it. Success: the expression
produces the inner `String`, and `text` is a plain `String` from then
on. Failure: the function **returns immediately**, handing the error
to its caller. It is Go's `if err != nil { return err }` in one
character — the early exit is still visible, on the line where it
happens, but it no longer dominates the page.

`.context("reading input")` decorates an error on its way out
("reading input: no such file") and passes successes through
untouched.

The whole line, in the Go it replaces:

```go
text, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("reading input: %w", err)
}
```

Because `?` can now return an error out of `main`, `main` must
declare that: `-> Result<(), Error>` reads "either succeeds with
nothing, or fails with an `Error`". The `()` is the **unit type** —
the way "nothing" is spelled where the grammar requires a type.

Which explains the odd-looking last line. `Ok(...)` constructs the
success variant of a `Result`; what this one carries is `()`, the
unit *value*. Outer parens: a call. Inner parens: the nothing being
handed back. If `main` returned `Result<Int, Error>`, the line would
be `Ok(42)`, and the symmetry is obvious. `Ok`'s twin is `Err(...)`,
which constructs the failure — you'll build one yourself in §1.5.

Both signatures of `main` are legal — step 1's plain `fn main()`, or
this one. And returned to whom? `main`'s caller is the runtime: it
turns `Ok(())` into exit code 0, and an `Err` into the error printed
on stderr and exit code 1. Without this, `?` would be unusable in
`main`, and every CLI would open with a ceremonial `run()` wrapper
existing only to give errors somewhere to go — Go's four-line
`if err := run(); err != nil` ritual, deleted.

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
  `1`. `??` is the **option-coalescing operator** — Glide's variation
  of what C#, JavaScript, and Swift call null-coalescing, retargeted
  at `Option` because there is no null to coalesce. The right side is
  evaluated only when the left is absent, and only *you* get to say
  what absence means here — the language never guesses. (Unrelated to
  `?` from step 5, despite the shared glyph: one question mark hands
  the problem to your caller, two means you've answered it yourself.)
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
  types inferred from context. `cmp` is a three-way comparison —
  negative, zero, or positive, like Go's `strings.Compare` — and
  `sort_by` wants exactly that, not Go's boolean `less`. Comparing
  `b` to `a` — rather than `a` to `b` — is what makes the sort
  descending.
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

The rest of this chapter covers every feature properly — including
several the walkthrough had no reason to touch.

## 1.2 Reading Glide: lines, case, braces

Three mechanical rules govern how any Glide file reads. They're
usually absorbed unconsciously; here they are consciously.

**Lines end statements.** There are no semicolons. A newline ends
the statement when the last token on the line can end an expression —
an identifier, a literal, `)`, `]`, `}`, or `?`. When it can't, the
statement continues:

```glide
let total = price +
    shipping            // fine: a line can't end with +

let x = f(a, b)
    .context("...")     // NOT fine: the first line ended at )
```

So a method chain keeps the dot at the end of the continuing line's
predecessor — or, in practice, you don't think about it, because the
canonical formatter (there is exactly one layout; no configuration)
arranges line breaks for you. The one rule worth internalising:
`else` sits on the same line as the `}` before it. This is Go's
newline rule, and it has the same consequence: code pasted from
anywhere formats to the same bytes.

**Case is meaningful, and the compiler enforces it.** Capitalised
names are types, variants, and constructors: `Tree`, `Circle`,
`Some`, `None`. Lowercase names are values: bindings, functions,
fields, modules. This is not a style suggestion — pattern matching
(§1.8) *depends* on it: in `match shape { Circle(r) => …, point => … }`,
the only way to know that `Circle` tests and `point` binds is the
case of the first letter. One consequence: `pub` marks visibility
(§1.13) rather than Go's capitalisation trick, because the case axis
is already spent.

**Braces are mandatory** — every `if`, `for`, and `fn` body takes a
block, even one line. Comments are `//` to end of line — there is no
`/* */` form, on purpose: block comments either don't nest (C, Go —
commenting out code that contains one leaves live code behind) or
break on a stray `*/` in prose (Rust), and their real job, deadening
a region while debugging, is your editor's toggle-comment key. Source
files end in `.gld`. Number literals take underscores for readability:
`10_000_000`, `0..1_000_000`.

## 1.3 Values and bindings

`let` declares; `=` assigns; the two never blur. Assigning to a name
that hasn't been declared is an error (there is no Go `:=`, no
implicit declaration), and declaring is always visible on its line.

```glide
let name = "glide"
let port = 8080          // Int, inferred
let debug: Bool = false  // annotation optional; inference is local
```

**Immutable by default.** A `let` binding cannot be reassigned. If a
variable's value genuinely evolves, mark it at the declaration:

```glide
let mut count = 0
count += 1
```

`mut` is the single most useful audit mark in the language: skim any
function and you know exactly which locals can change. There is no
`++` — `count += 1` is the only increment. Assignment is a statement,
not an expression, so `a = b = c` and `while x = next()` don't parse;
an entire family of C cleverness is unspellable.

**Redeclaration is not mutation.** You may redeclare a name in the
*same* scope, and it's idiomatic — this is a *refinement pipeline*:

```glide
let input = read_line()?
let input = input.trim()           // new binding, same name
let input = parse<Config>(input)?  // the type changed: String → Config
```

Each `let` ends the old binding's life and starts a fresh, immutable
one; nothing ever changes behind anyone's back, and after each line
exactly one `input` is alive. This is why the example needs no `mut`
and no `input_raw`/`input_trimmed` naming dance. The distinction has
teeth: a closure that captured the old binding keeps it — capture
binds to the *binding*, not the name (§1.10) — and redeclaration is
same-scope only, which is why loop accumulators genuinely need `mut`
(a loop body is a nested block; see next paragraph).

**Shadowing across scopes is banned.** Redeclaring a name from an
*enclosing* block inside a nested block is a compile error:

```glide
let count = 0
for x in xs {
    let count = count + 1   // error: cannot shadow enclosing `count`
}
```

This kills Go's classic bug — an inner `:=` silently declaring a new
variable while the outer one stays unchanged — by making the shape
unwritable. The two rules compose into an idiom: build with `mut`,
then seal — `let mut acc = …; …; let acc = acc` — mutable during
construction, immutable after.

Two Go shadowing bites are handled separately. The free builtins
(`print`, `println`, `eprint`, `eprintln`, `expect`) are reserved —
`let println = 5` is an error at the declaration, since no program
legitimately wants that local. Imports are *not* reserved: a variable
named `sql` in a file that imports `sql` is fine — both are named
after the domain, and the collision is usually harmless — but using
the module *through* a live shadow (binding `sql`, then calling
`sql.open()` in the same function) is a compile error that names both
parties, instead of Go's diagnosis fog.

**Blocks are expressions, and bare blocks scope.** A `{ … }` anywhere
an expression can go is a scope whose tail expression is its value —
so a computation's working variables can be hidden inside it:

```glide
let size = {
    let num = rand.int_n(100)
    if num > 50 { "big" } else { "small" }
}                       // num is gone; size is "big" or "small"
```

A freestanding block scopes a region, as in Go. Where Go grew an init
clause on `if` (`if num := rand.IntN(100); num > 50`) to keep a
helper from outliving the statement, Glide gets the same containment
from this one rule: wrap a block around exactly the code that should
see the name.

**Discarding.** `_` on the left of `=` evaluates and discards the
right side, visibly: `_ = db.close()`. A function with no declared
return type whose body ends in a meaningful value is an error — you
either return it, or discard it explicitly. Silent discards don't
exist.

## 1.4 There is no null — `T?`

Every mainstream C-lineage language has a value that lies about its
type: `null`, `nil`, `None`-at-runtime. Any reference might secretly
be it; the compiler can't tell you which; you find out in production.
Glide removes the lie by moving "might be absent" into the type. A
plain `T` is *always* a real `T`. A value that might be absent is a
`T?` (an *Option*), and the compiler will not let you use a `T?`
where a `T` is required — you must handle the empty case first.

The point is not the extra `?` — it's that *presence becomes visible
in signatures*. `fn find(id: UserId) -> User?` tells you at the
boundary that absence is possible; `fn owner() -> User` tells you it
isn't, and you don't defensively check. Three ways to handle a `T?`:

**`??` — supply a default.** The option-coalescing operator, reads
"or else":

```glide
let n = counts[word] ?? 0
let host = config["host"] ?? "localhost"
```

Map indexing returns `V?` — the key might be absent — which is
exactly the shape `??` was built for. Go returns a zero value and
hopes; Glide returns an Option and asks.

**`if let` — unwrap into a scope.** Checks and binds in one move:

```glide
if let user = find_user(id) {
    // user is a User here, not a User?
} else {
    // absent; `user` doesn't exist here
}
```

No `Some(...)` wrapper appears in the source — the pattern binds the
inner value directly. (Swift users will recognise this exactly.)

**`let … else` — unwrap or bail.** For guard clauses; keeps the
happy path unindented:

```glide
let config = load_config(path) else {
    eprintln("no config at {path}")
    os.exit(1)
}
// config is real from here on
```

The else-block must *diverge* — return, exit, or otherwise not fall
through — because past it, the binding must exist. The compiler
enforces the divergence, which is what makes the flat style safe
rather than optimistic.

Choosing between them: `??` when a default is meaningful, `if let`
when both cases have code, `let … else` when absence aborts. Together
they cover what null-checking does in other languages, except the
compiler verifies you did it.

## 1.5 Errors are values — `Result`, `?`, and `or`

Glide's error philosophy is Go's — errors are ordinary values,
visible in signatures, no invisible control flow — with the
boilerplate removed and the failure modes made enumerable.

**`Result<T, E>`** is a two-variant type: `Ok(T)` carrying a success,
or `Err(E)` carrying a failure. A function that can fail *says so in
its return type*:

```glide
fn read_config(path: String) -> Result<Config, Error>
```

There are no exceptions. Nothing propagates invisibly; every failure
path is in a signature. Two operators then make handling cheap
enough that you never resent writing it:

**`?` propagates.** Unwrap the success, or return the failure to the
caller — one character, placed exactly where the risk is:

```glide
let text = fs.read_string(path).context("reading input")?
```

A skim of any function shows every line that can exit early: look
for `?`. The `.context("…")` call wraps an error with a breadcrumb
as it passes ("reading input: open notes.txt: no such file"), so a
deep failure surfaces with its route attached.

**`or` handles in place.** The sibling of `?` for when you *don't*
want to propagate — handle the error right here:

```glide
let input = req.json<Note>() or |e| { return Err(.BadInput("{e}")) }
let cfg = load_config() or |e| { default_config() }
```

Read it as "or, given the error e, do this instead". The block
receives the error and must either diverge (return/exit) or produce
a fallback value of the success type — the same must-not-fall-through
rule as `let … else`, and for the same reason: past that line, the
binding must hold a real value.

> **Unratified.** `or |e|` is the one construct in Glide with no
> direct precedent in Rust, Swift, or Go — GRAMMAR.md flags it as
> "the biggest thing to fight about in this sketch". Rust spells
> these cases `.unwrap_or_else(...)` / `match`; Swift spells them
> `do/catch`. Whether `or` earns its place is an open fight.

**Error types are ordinary sum types** (§1.6), so a function can
enumerate exactly what can go wrong — `Result<Note, ApiError>` where
`ApiError = NotFound(…) | BadInput(…) | Db(…)` — and `match` can
handle each shape. No `errors.Is` archaeology, no downcasting.

**Panics exist, for bugs only.** Out-of-bounds indexing, a broken
invariant — things a correct program never does. They are not
control flow, they are not caught in ordinary code, and APIs never
use them to report expected failures.

## 1.6 Declaring types

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

**Structs** are the familiar record: named, typed fields. Two things
differ from what you're used to. First, **there are no zero values**:
a struct literal must account for every field, or it doesn't compile.
Go's `User{}` — an "empty" user with a blank ID that type-checks and
flows until something breaks far from the cause — is unwritable.
(Types can opt in to a default via a trait, so a `Builder` or `Mutex`
still constructs bare; *domain* types get no fake instances for
free.) Second, fields are private unless marked `pub`, so a public
type with private fields is the natural encapsulation story — no
getter ceremony, no convention.

**`distinct`** creates a new type with the same representation but no
implicit conversion. A `NoteId` is stored as an `i64`, but you cannot
pass a plain `i64` — or an `OrderId` — where a `NoteId` is expected;
`NoteId(raw)` converts, explicitly. Ten characters of declaration
kill the entire class of transposed-identifier bugs, the ones that
type-check fine and corrupt data quietly.

**Sum types** are the star of the language, and if you've lived in
the C lineage they are probably genuinely new, so slowly: a sum type
says a value is **exactly one of these N shapes**, and each shape can
carry its own data. `ApiError` above is one type with three shapes —
a value of it is a `NotFound` holding an id, *or* a `BadInput`
holding a message, *or* a `Db` holding a cause. Never two of them,
never none of them, never a shape you didn't list.

If that sounds like an enum: it's an enum whose variants can carry
payloads, checked by the compiler. If it sounds like a C union: it's
a union that always knows which member is live and refuses to read
the wrong one. The C lineage approximates this constantly — an enum
tag plus a struct of mostly-unused fields, an interface plus type
switches, "check the kind field before touching the payload" — and
every approximation relies on discipline. The sum type makes the
discipline a type.

Where they change your designs: "one of N shapes" is what most
domain data *is*. A payment is card or transfer or cash. A config
value is a string or a number or a list. An AST node is one of the
node kinds. Model those as a sum type and illegal states — a payment
that's both, a node that's neither — stop being representable at
all. You'll also stop writing boolean-flag pairs (`isLoaded`,
`hasError`) that have four combinations of which two are meaningful:
`Loading | Loaded(Data) | Failed(Error)` has exactly the three.

Where the expected type is known, a variant can be abbreviated with
a leading dot — `Err(.NotFound(id))` rather than
`Err(ApiError.NotFound(id))`.

Using a sum type requires `match` (§1.8), and that pairing is where
the payoff compounds — the compiler checks you handled every shape.

**`derive(...)`** asks the compiler to generate implementations by
walking a type's structure at compile time — JSON encoding, database
row mapping, debug printing. It looks like magic; it is ordinary code
generation with no runtime reflection anywhere in the language: what
`derive(Json)` writes is what you'd have written, at zero runtime
cost.

## 1.7 Pattern matching

Patterns are the disassembly half of the language, and one principle
organises everything: **a pattern is construction run backwards.**
Whatever syntax builds a value, the same syntax takes it apart:

```glide
let pair = (host, port)              // build a tuple…
let (host, port) = parse_addr(s)?    // …and unbuild one

let user = User{ name: n, age: a }   // build a struct…
let User{ name, .. } = user          // …pull a field out

Some(5)                              // build an Option…
if let Some(n) = maybe { … }         // …take one apart
```

Anywhere a name can be bound, a pattern can stand: `let`, function
of `for` headers, match arms. `for (word, n) in entries` destructures
each element as it arrives. `_` discards; `..` in a struct or list
pattern means "and the rest, deliberately" — unmentioned parts are
an error without it, because patterns match exactly what they spell.

## 1.8 `match`

`match` is the only multi-way branch in the language (`if`/`else`
covers two-way). It tests a value against patterns, top to bottom,
and runs the first arm that fits:

```glide
match shape {
    Circle(r)      => 3.14 * r * r
    Square(w)      => w * w
    Rect(w, h)     => w * h
}
```

Three properties make it more than a switch:

**Arms bind as they match.** `Circle(r)` doesn't just test the
variant — it extracts the radius into `r` for that arm's body. Test
and disassembly are one step; there is no separate cast-and-hope.
Case tells you which is which (§1.2): capitalised `Circle` tests,
lowercase `r` binds. Nesting works: `Ok(User{ name, .. })` reaches
two levels down in one pattern.

**Exhaustiveness is checked.** Match a sum type and miss a variant:
compile error. The payoff arrives later, when you *add* a variant —
every `match` in the codebase that doesn't handle it becomes a
compile error, and the compiler hands you the complete list of
places the change touches. In the C lineage, adding a case means
grepping and praying; here it means fixing what the compiler lists.
A `_ =>` arm is legal but spends this guarantee — write it only when
"anything else" is genuinely the meaning.

**Arms take guards.** `Some(n) if n.value < limit => …` — an extra
condition after the pattern. A guard that fails falls through to the
next arm. (Guards are opaque to the exhaustiveness checker: it
assumes any guard can fail, so a guarded arm never completes
coverage — the compiler demands the unguarded case rather than
trusting your predicate.)

Patterns also match literals and ranges — `1..10 =>`, `'a'..'z' =>`,
`"GET" =>` (equality only; no regexes in patterns).

**`match` and `if` are expressions** — they produce values — which
is why Glide has no ternary operator and no naked-mutable-then-assign
dance:

```glide
let status = if ok { "active" } else { "disabled" }

let label = match n {
    0 => "none"
    1 => "one"
    _ => "{n} items"
}
```

**Struct update** is construction's answer to immutability: build a
copy with some fields changed, sharing the rest —

```glide
let renamed = Note{ title: new_title, ..note }
```

In an immutable-first language this is *the* way data evolves; the
recursive tree insert in GRAMMAR.md program 3 builds each modified
node this way while untouched subtrees are shared as-is.

One parser rule to know: struct literals are banned in control-flow
headers — `if c == Red { … }` gives the brace to the body, so
`if x == Point{ x: 0, y: 0 }` needs parens around the literal. (Same
rule and same reason as Rust: variants are capitalised too, so case
can't disambiguate.)

## 1.9 Strings

Strings interpolate, always, with full expressions in the braces,
checked at compile time:

```glide
println("{n:6}  {word}")             // width spec: right-align in 6
log.info("swept", { count: n })
return Err(.BadInput("{e}"))
```

There is no `printf`, no format verbs, no runtime format-string
parsing. A type that can't be displayed is a compile error, not
`%!v(MISSING)` in production logs. After the colon comes an optional
format spec (`{n:6}` — width 6). Escapes are the backslash family:
`\n`, `\t`, `\"`, `\\`, and `\{` for a literal brace.

Strings are immutable; building one incrementally is a named type
(`StringBuilder`), not a loop of concatenations. Comparison is byte
equality — locale-aware collation is a library concern, not an
operator.

## 1.10 Functions and closures

Signatures are always explicit — they're documentation, and they're
what inference works *between*. Inside bodies, inference does the
work.

```glide
fn get_note(db: sql.Db, req: http.Request) -> Result<http.Response, ApiError>
```

**The last expression is the return value.** Function bodies are
blocks; blocks produce their final expression's value; `return`
exists only for early exits. Small functions read as what they
compute — `fn double(n: Int) -> Int { n * 2 }` — and since `if` and
`match` are expressions, most "compute then return" shapes collapse
into a tail expression. (Lineage: Lisp and the ML family, via Rust
and Ruby. The C lineage never had it, so it reads oddly for about a
week.) With no semicolons, the declared return type is the check: no
arrow means a meaningful tail value is an error — discard with
`_ =` if you mean to drop it.

**Closures** are anonymous functions, written with pipes:

```glide
entries.sort_by(|a, b| b.1.cmp(a.1))
r.get("/notes/{id}", |req| get_note(db, req))
let tick = || count += 1     // zero parameters; block body for statements
```

Parameter types are inferred from context (annotations allowed,
rarely needed). A closure and a named function have the same type —
`fn(A) -> B`, one function type for named functions, closures, and
method values alike — so promoting a grown closure to a named
function is cut-and-paste, no signature surgery. There is no
`$0`/`it` implicit parameter: naming the parameter is documentation.

**Closures capture by reference, and they capture *bindings*, not
names.** Two consequences:

```glide
let mut total = 0
let add = |n| { total += n }
add(40); add(2)                 // total is 42 — the capture is live

let name = "first"
let who = || name
let name = "second"             // new binding, same spelling
who()                           // "first" — the closure kept its binding
```

Mutation through a captured `mut` binding is shared and visible;
redeclaring a name afterwards cannot retarget a closure that
captured the old binding, because redeclaration creates a *new*
binding (§1.3) and the closure holds the old one.

**Loop variables are fresh bindings per iteration.** Closures created
in a loop each capture that iteration's value — Go's most expensive
capture bug (fixed there in 1.22 as a semantic break) is correct here
from day one.

**Named arguments and defaults.** Any argument can be named at the
call site, and parameters can declare defaults:

```glide
http.serve(addr: ":8080", handler: r)
fn connect(host: String, port: Int = 5432, tls: Bool = true) -> Conn
connect(host: "db.local", tls: false)
```

Together these replace Go's functional-options pattern and most
constructor overloads: optional configuration is just parameters
with defaults, and call sites say what each value means.

**Mutation is marked at free-function call sites.** A function that
mutates a parameter declares it — `fn sort(mut xs: List<Int>)` — and
the *call site* repeats the marker: `sort(mut xs)`. Skimming a
function shows every place your data can change under you. This is
Rust's `&mut x` without the reference machinery. Method receivers
are the recorded exception (§1.11): `xs.push(3)` takes no marker,
because method names carry intent and marking every receiver trains
people to ignore markers.

## 1.11 Methods, `impl`, and traits

Methods live in `impl` blocks, separate from the type declaration:

```glide
impl Tree<T> {
    pub fn new() -> Tree<T> { Tree{ root: None } }
    pub fn insert(mut self, value: T) { … }
    pub fn len(self) -> Int { … }
}
```

`Tree.new()` — no `self` parameter — is an *associated function*,
called on the type itself: the constructor idiom. Methods take
`self`, and **receiver mutability is declared**: `fn len(self)` is
read-only; `fn insert(mut self, …)` may mutate, and is callable only
through a `mut` path:

```glide
let mut t = Tree.new()     // mut required…
t.insert(3)                // …because insert declares mut self
let u = Tree.new()
u.insert(3)                // compile error: u is not mut
```

This closes the loophole that would otherwise make immutability a
lie — without it, `let` would mean "can't reassign, but anyone can
gut the object through a method". Mutability is transitive through
paths: `a.b.c` is assignable only if `a` is `mut`.

**Traits** are Glide's interfaces, with one deliberate difference
from Go: conformance is *declared*, one line, rather than inferred
from method sets:

```glide
impl Iterable<T> for Tree<T> {
    fn iter(self) -> Iterator<T> { … }
}
```

Go's structural typing is friendlier at small scale but produces
accidental conformance at large scale — types satisfying interfaces
nobody intended, discovered by grep. Explicit `impl` gives
findability ("who implements Iterable?" is answerable) and precise
diagnostics ("Tree is missing method iter of Iterable").

**There is no inheritance.** No base classes, no `extends`, no
virtual dispatch hierarchies. Composition holds the data (a struct
containing a struct); traits describe the capabilities. Inheritance
is the feature every ecosystem regrets by year five — fragile base
classes, diamond problems, "is-a" taxonomies that stop fitting — and
its legitimate uses are covered by the other two mechanisms.

**Generics** use angle brackets with trait bounds, and bounds are
spelled inline: `Tree<T: Ord>` reads "a Tree of any T that can be
ordered". Monomorphized (each instantiation compiles to concrete
code, like Rust and unlike Java's erasure), inferred at call sites
(you write `max(a, b)`, never `max<Int>(a, b)` — there is no
turbofish), and unconstrained parameters are bare `<T>`.

## 1.12 Loops, iterators, generators

There is one loop:

```glide
for { … }                       // forever
for cond { … }                  // while
for x in xs { … }               // over anything iterable
for (k, v) in m { … }           // patterns work in the header
for i in 0..10_000 { … }        // over a range
```

**Iterators** are lazy sequences. `xs.iter()` doesn't walk the list —
it produces a value that yields elements on demand, and adapters
compose without materialising anything: `entries.iter().take(20)`
does no work until the loop pulls, and stops pulling after twenty.
Laziness is what makes infinite sequences ordinary values and
pipelines cheap. Anything with an `iter()` method works in
`for … in`; `.collect()` materialises an iterator back into a list
when you actually need one.

**Generators** are how you *write* an iterator, and they're the
feature Go and Rust both struggle with. The problem: to write an
iterator by hand you must turn your traversal inside-out into a
`next()` method — externalised state, explicit stack, resume logic.
For a tree, that's real code, and it looks nothing like a traversal.

In Glide, a function containing `yield` *is* an iterator:

```glide
fn walk<T>(n: Node<T>) -> Iterator<T> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}
```

`yield` hands the next value to whoever is consuming and *pauses the
function mid-flight*; the next pull resumes it exactly where it
stopped, locals intact. `yield from` delegates to a sub-iterator
(here: recursion). The compiler builds the state machine you didn't
write. In-order tree traversal — the canonical "iterators are hard"
example — reads as the three lines it conceptually is.

Generators compose with everything above: they're lazy, so
`walk(root).take(5)` visits five nodes and stops, and an infinite
generator (`for { yield n; n += 1 }`) is a perfectly good value.

## 1.13 Modules, imports, visibility

**A directory is a module.** All `.gld` files in a directory share one
namespace — no intra-module imports, no per-file declarations, no
`mod.rs` tree. Go's model, kept.

**Imports are by module name, used qualified:**

```glide
import http                          // stdlib: bare name
import "github.com/x/y" as y         // external: URL, aliased
```

Importing executes nothing — no init functions, no registration
magic, no what-runs-on-import. A module you import is inert until
you call it. (Runtime state like database drivers is passed
explicitly, created in `main` — nothing global materialises behind
your back.)

**`pub` is the visibility system, and it's two-level:** private to
the module by default, `pub` visible outside. No `pub(crate)` zoo,
no friend classes; wanting a third level usually means the module
boundary is drawn wrong. Fields take `pub` individually, so a public
type with private fields is the default encapsulation shape. And a
visibility change is a one-line diff that says "this became public" —
reviewable, unlike a capitalisation rename touching every use site.

## 1.14 Cleanup: `defer`

`defer` schedules a block to run when the enclosing scope exits —
success, error, or panic — so acquisition and cleanup sit together:

```glide
let db = sql.open("sqlite:notes.db")?
defer { _ = db.close() }
```

Differences from Go's, each fixing a known trap: it is
**block-scoped**, not function-scoped — a defer in a loop body runs
each iteration, so the open-files-pile-up-until-the-function-ends
bug is not reproducible. It takes a **block**, not a call, so
there's no argument-evaluation-time puzzle. And discarding an error
inside one is visible (`_ =`), not silent. Its sibling `errdefer`
runs only when the scope exits *with an error* — the missing
construct behind Go's awkward multi-step-initialisation cleanup.

## 1.15 Concurrency in one paragraph (for now)

Glide is green-threaded like Go — cheap tasks, no `async`/`await`,
no function colouring — but tasks are **structured**: every task
belongs to a scope, the scope's end waits for its tasks, and a
failing child cancels its siblings. Nothing leaks.

```glide
scope s {
    s.spawn(|| sweeper(db))       // dies with the scope
    http.serve(addr: ":8080", handler: r)
}
```

If `serve` returns with an error, the sweeper is cancelled — no
`context.Context` threaded through every signature, no orphaned
goroutine discovered in a heap dump. Cancellation is ambient:
blocking calls inside a cancelled scope (the sweeper's
`time.sleep(1.min)`) simply stop waiting and unwind. Channels and
`select` exist for communication; they arrive in a later chapter.

## 1.16 Tests live with the code

Tests are a language construct, legal in any source file:

```glide
test "in-order traversal is sorted" (xs: List<Int>) {
    let mut t = Tree.new()
    for x in xs { t.insert(x) }
    expect(t.iter().collect() == xs.sorted())
}
```

**`expect(…)` needs no assertion library.** The compiler instruments
the expression: on failure you see both sides — `left: [2, 1]`,
`right: [1, 2]` — not "assertion failed". The stdlib therefore ships
no `assertEqual` zoo, and the ecosystem never grows one.

**That test takes a parameter, which makes it a property test** —
and if example-based testing is all you've used, this is the feature
to sit with. You don't choose inputs; you state a *property* that
should hold for all of them ("inserting anything, in any order,
iterates back sorted"), and the runner generates hundreds of inputs
trying to falsify it. When one succeeds, the runner doesn't hand you
the 27-element monster it found first — it **shrinks**, re-running
with structurally smaller variants until the failure is minimal:
you're told the property, and the smallest input that breaks it
(`xs = [0, 0, 0, 0, 0]` tells you "exactly five elements" *is* the
bug). Properties catch the cases you'd never think to write —
empties, duplicates, negatives, orderings — because nobody writes
them; the generator finds them.

`bench "…" { }` blocks are benchmarks; the runner owns the timing
loop, so the measure-around-the-loop boilerplate doesn't exist.

`glide test` runs it all — and also enforces formatting, unused-code
hygiene, and doc-link validity. The compiler stays quiet while you
edit; the test boundary is where the standards apply.

## 1.17 Odds and ends

- **Printing is four builtins**: `print`/`println` to stdout,
  `eprint`/`eprintln` to stderr — `e` marks the stream, `ln` appends
  the newline; that's the whole family, since formatting lives in
  interpolation. All four are unbuffered: a `print("continue? ")`
  prompt shows before the read, and a debug print lands even if the
  program dies on the next line. There is no `flush()`.
- **Duration and date literals are method calls**: `1.min`, `30.d`,
  `500.ms` — stdlib-defined methods on numbers, no language magic, no
  bare-int timeouts (`sleep(1.min)` cannot be confused with
  `sleep(60)` of unknown unit).
- **Integer overflow**: traps in dev builds, wraps in release.
  Constant arithmetic is exact at compile time (`1 << 100` is fine in
  a constant; `let x: u8 = 300` is a compile error).
- **Const names are `snake_case`** like any binding — there is no
  SCREAMING_CASE convention; an earlier evaluation time is not a
  siren.
- **Flags are not enums**: "one of" is a sum type; "set of" is a
  `Set<Color>` (or `BitSet` where performance demands). An enum
  that's secretly a bitfield is the int costume again.

## 1.18 Where the rest lives

Skipped here and covered later: channels and `select`, `supervise`
scopes, comptime, labeled loops, the unsafe boundary, embedding. The
design rationale for everything above — including every deliberate
rejection and its cost — is in `DESIGN.md`, which is unusually honest
and is the recommended second read. (Implementation status, if you're
following along with the interpreter: `glide/DESIGN-DECISIONS.md`
tracks what runs today versus what's designed-but-pending — `defer`,
concurrency, named arguments, and `or |e|` are in the latter set.)

The shortest useful summary of Glide for a Go programmer might be:
*your runtime, your tooling philosophy, your deployment story — the
ML family's types, the decade's ergonomics, and nothing you didn't
ask for.*
