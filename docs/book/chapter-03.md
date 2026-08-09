# Chapter 3: Your First Glide Program

Hello world is a bad test of a language and a good test of its
*surface*. This chapter takes the six-line version apart token by
token, then grows it until every mechanical rule about how a Glide file
reads has been introduced: statement termination, case conventions,
braces, imports, entry points, and the tail-expression rule that
surprises everyone arriving from the C lineage.

Everything in this chapter is ✓ — it runs today.

---

### 1. Basic Usage

#### The smallest complete program

```glide
fn main() {
    println("hello, world")
}
```

```bash
$ glide run hello.gld
hello, world
```

Six tokens of significance:

- **`fn`** declares a function. There is exactly one function-declaring
  keyword, and it is used at module level, nested inside another
  function's body, and inside `impl` blocks.
- **`main`** is the entry point, and *nothing runs before it*. No
  `init()` functions, no module-level `let`, no import side effects, no
  static initialisers. Line one of `main` is line one of your program.
  This is a load-bearing guarantee, not a convenience — Chapter 29
  explains what it buys.
- **`()`** is the parameter list. `main` takes none. Command-line
  arguments arrive through `os.args()`, not through a parameter.
- **No return type** is written, because `main` here returns nothing.
  Return types are spelled with `->` and appear shortly.
- **`println`** writes a line to stdout. It is a *builtin*: no import,
  and the name cannot be shadowed or rebound.
- **No semicolons.** Braces are mandatory even around a single
  statement.

#### Two functions

```glide
fn greet(name: String) -> String {
    "Hello, {name}!"
}

fn main() {
    println(greet("world"))
}
```

```
Hello, world!
```

Three new things, each of which the rest of the book leans on heavily:

**Signatures are written out in full.** Every parameter has a declared
type; the return type follows `->`. Type inference exists and is
aggressive, but it works *inside* bodies only. Boundaries are
documentation — this is a deliberate constraint, not a limitation of
the inference engine.

**The last expression is the return value.** `greet` has no `return`
statement. A function body is a block, blocks produce the value of
their final expression, and that value is what the function returns.
`return` exists, but only for early exits.

**Strings interpolate.** `"Hello, {name}!"` substitutes the value of
`name`. There is no `printf`, no `%s`, no format-string parsing at
runtime. Any expression can go in the braces, and it is checked at
compile time in the designed language.

#### Declaration order does not matter

```glide
fn main() {
    println(greet("world"))
}

fn greet(name: String) -> String {
    "Hello, {name}!"
}
```

Identical behaviour. **Declarations are a set; statements are a
sequence.** Module-level functions, types, `impl` blocks, and `const`
bindings are all order-independent, so file order becomes *narrative*
order: the important function first, helpers after. The formatter
deliberately does not reorder declarations, because sequence is the
author's storytelling channel.

Statements inside a body remain a sequence, obviously. A `let` exists
only downstream of its line.

#### Imports

```glide
import os

fn main() {
    let args = os.args()
    println("I was called with {args.len()} arguments")
}
```

```bash
$ glide run args.gld a b c
I was called with 4 arguments
```

(Four, not three — `os.args()` puts the program name first.)

Imports go at the top of the file, one per line, and modules are used
**qualified**: `os.args`, not a bare `args`. There is no `from x import
y`, no wildcard import, and no aliasing of individual names.

The critical property: **importing executes nothing.** There is no
registration magic, no driver self-installation, no
`import _ "lib/pq"`. A module you import is inert until you call it.

Stdlib modules are bare names; external modules are quoted URLs with an
alias (○, since there is no package manager yet):

```glide
import http                          // stdlib
import "github.com/x/y" as y         // external
```

#### `main` has two legal signatures

The plain one you have seen, and one that can fail:

```glide
import fs

fn main() -> Result<(), Error> {
    let text = fs.read_string("config.txt").context("reading config")?
    println(text)
    Ok(())
}
```

Run it without the file present:

```
$ glide run config.gld
error: reading input: open /nonexistent: no such file or directory
$ echo $?
1
```

Chapter 19 covers `Result` and `?` properly. For now, three
observations about the *shape*:

- `main`'s caller is the runtime. It turns `Ok(())` into exit code 0,
  and an `Err` into the error printed on stderr plus exit code 1.
- `Result<(), Error>` reads "either succeeds with nothing, or fails
  with an `Error`". The `()` is the **unit type** — how "nothing" is
  spelled where the grammar demands a type.
- `Ok(())` is therefore a call to `Ok` whose argument is the unit
  *value*. If `main` returned `Result<Int, Error>` the line would be
  `Ok(42)` and the symmetry would be obvious.

Without this second signature, `?` would be unusable in `main`, and
every CLI would open with a ceremonial `run()` wrapper existing only to
give errors somewhere to go — Go's four-line
`if err := run(); err != nil { … }` ritual, deleted.

#### A first complete program

Putting the pieces together — a program that reads a file named on the
command line and prints its line count:

```glide
import fs
import os

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: linecount <file>")
        os.exit(2)
    }

    let text = fs.read_string(path).context("reading input")?
    println("{text.lines().len()} lines")
    Ok(())
}
```

```bash
$ glide run linecount.gld testdata/sample.txt
1 lines
$ glide run linecount.gld
usage: linecount <file>
$ echo $?
2
```

Four things are new, and each gets a full chapter later:

- `let [_, path] = …` is a **pattern**: match a list of exactly two
  elements, discard the first, bind the second. Chapter 10.
- `else { … }` is the plan for when the pattern does not match. The
  block must *diverge* — return, exit, or otherwise not fall
  through — because past it, `path` has to exist. `os.exit` diverges,
  so this compiles.
- `eprintln` is `println` aimed at **stderr**. Usage messages go there
  so they cannot contaminate the program's real output: someone running
  `linecount notes.txt | wc` gets the count in the pipe and complaints
  on the terminal.
- `.context("reading input")` decorates an error on its way out and
  passes successes through untouched.

---

### 2. Under the Hood

#### What "hello world" costs

`glide run hello.gld` lexes the file, parses it into an AST, collects
the module-level declarations into an environment, then evaluates a
call to `main`. There is no compilation, no linking, and no
type-checking pass. Total work: microseconds of parsing and one
function call.

`println` formats its single argument into a string in memory and
issues **one write syscall**. It is unbuffered, and there is no
`flush()` in the language. Chapter 6 defends that decision at length;
the short version is that the two classic footguns — a no-newline
prompt that does not appear before the read, and a debug print that
vanishes because the process died on the next line — cannot exist if
there is nothing to flush. The recorded cost is that a naive
million-line print loop is syscall-bound.

#### The newline rule

Glide has no semicolons, so something has to decide where a statement
ends. The rule is Go's:

> A newline ends the statement when the token before it **can end an
> expression** — an identifier, a literal, `)`, `]`, `}`, or `?`.

If the last token cannot end an expression, the statement continues:

```glide
let total = price +
    shipping                  // fine: a line cannot end with `+`
```

There is one addition to Go's rule, and it exists because of iterator
chains. A line that *begins* with `.` also continues the previous
statement:

```glide
let top = entries
    .iter()
    .filter(|e| e.1 > 1)
    .take(20)
    .collect()
```

Go does not allow this (you must leave the dot at the end of the
previous line), and it is a well-known annoyance. Glide takes
Swift/Kotlin's rule instead, because Rust and JavaScript muscle memory
puts the dot at the start of the line and the trailing-dot-only rule
"survived exactly as long as the first real adapter chain" (a direct
quote from the design document — this rule changed because the
implementer hit it).

Two exceptions keep the leading-dot rule from swallowing things it
should not, and both are resolved by **case**:

- `..` at line start is a range token, not a continuation.
- `.Red` — capitalised after the dot — is the variant shorthand
  starting a new statement, not a method call continuing the previous
  one.

Since methods and fields are lowercase and variants are capitalised,
those two meanings of `.` can never collide. That is not a lucky
accident; it is the case rule earning its keep.

One more mechanical rule: **`else` sits on the same line as the `}`
before it.** The canonical formatter guarantees this, so in practice
the rule never bites.

#### Case is grammar, not style

Capitalised names are types, variants, and constructors: `Tree`,
`Circle`, `Some`, `None`, `String`. Lowercase names are values:
bindings, functions, fields, modules.

This is enforced, and it is enforced because **pattern matching depends
on it**. In this arm:

```glide
match shape {
    Circle(r) => …
    point     => …
}
```

the only thing that tells you `Circle` *tests* the variant while
`point` *binds* the whole value is the case of the first letter. Go
never hit this conflict because Go has no pattern matching, which is
exactly why Go could afford to spend the case axis on visibility
instead.

Since the case axis is spent, visibility needs its own keyword — `pub`.
Chapter 29 argues that this is an improvement anyway: a visibility
change becomes a one-line diff that *says* "this became public,"
reviewable in a way that a whole-codebase capitalisation rename is not.

#### Braces, comments, and files

**Braces are mandatory** on every block, even a one-line body. There is
no single-statement `if`, which means dangling-else cannot exist and
the shape of Apple's `goto fail` bug is unwritable.

**Comments are `//` to end of line. That is the entire comment
grammar** — there is no `/* */`.

This looks like an arbitrary restriction and is not. Block comments are
a trap whichever way they are specified. C and Go's do not nest, so
commenting out a region that already contains one ends the comment
early and leaves live code behind. Rust's do nest, so a stray `*/`
inside a prose comment breaks the file. Their real job — deadening a
region while debugging — belongs to the editor's toggle-comment key,
which produces lines that are individually, greppably dead in a diff.
Documentation comments are ordinary `//` above the declaration.

**Source files end in `.gld`** and are UTF-8.

#### The tail-value rule, enforced

Because there are no semicolons, Glide cannot use Rust's trick of
distinguishing "expression" from "statement" by the presence of a
trailing `;`. The distinction hangs on the **signature** instead:

- Arrow declared → the tail expression must have that type.
- No arrow → the function returns nothing, and a tail expression with a
  meaningful value is an **error**, not a silent discard.

```glide
fn add(a: Int, b: Int) -> Int {
    a + b
}

fn noret() {
    add(1, 2)          // error
}
```

```
error: line 4: noret declares no return value but its body ends with a Int;
       discard it with `_ = …` or declare `-> Int`
```

The fix is to say what you mean: either `-> Int` and return it, or
`_ = add(1, 2)` to discard it visibly. Silent discards do not exist
anywhere in the language.

This also protects editing. Append a statement after the old tail
expression and the types stop lining up, so the compiler notices that
the function's meaning changed.

---

### 3. Why This Design?

#### Why the last expression is the return value

This is the rule that reads oddly for about a week if you come from C,
Java, or Go, and then becomes invisible.

It is not a separate feature. It is the consistent consequence of
expression-orientation. Once `let status = if ok { "active" } else {
"disabled" }` is legal — and Glide needs it legal, because refusing the
ternary operator without providing a replacement is Go's mistake — then
blocks must yield their final expression's value. Carving out function
bodies as the one kind of block that *does not* do this makes the same
braces mean different things depending on position. Kotlin took that
halfway position (expression-bodied functions get `=`, block-bodied
ones need `return`) and it is a wart.

The failure mode people worry about — accidentally returning a leftover
internal value — is a CoffeeScript problem, and it requires dynamic
typing. Mandatory signatures are the guardrail: the tail must match the
declared return type, and no-arrow means no tail value at all.

Lineage: Lisp, the ML family, Ruby, Rust.

#### Why imports execute nothing

Go's `init()` functions and import-for-side-effect
(`import _ "github.com/lib/pq"`, which runs hidden driver
registration) exist because Go's `const` can only hold scalars.
Everything structured had to go into `var`, which meant mutable globals
built at runtime, which needed somewhere to build them, which is
`init()`. All of it is downstream pressure from one limitation.

Glide's `const` is comptime-evaluated and can hold anything, so the
pressure never arises:

```glide
const table = make_crc_table()       // ○ — runs at compile time
```

Same function, either phase; the result lands in read-only data with
zero startup cost. With that in place, module level can be `const`
only, imports can be inert, and there is no initialisation-order
fiasco — no C++ static-init order problem, no Go init graph. `main` is
line one of runtime, full stop.

The cost: runtime state (a database handle, a logger, a clock) is
created in `main` and passed down. That is the design's grain
everywhere, and it is why the HTTP and SQL chapters pass `db` into
handlers explicitly rather than reaching for a global.

#### Why `main` may return a `Result`

Because `?` has to work somewhere.

If `main` could only return nothing, then every program that wants
error propagation opens with a wrapper whose only job is to give errors
a place to go. Go programs do this constantly:

```go
func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run() error { … }
```

That is four lines of pure ceremony in every CLI ever written in Go.
Allowing `fn main() -> Result<(), Error>` and having the runtime do the
printing and the exit code deletes it. Rust made the same call for the
same reason.

#### Why no semicolons but also no significant whitespace

Significant whitespace breaks code generation and copy-paste — paste a
block into a differently-indented context and the meaning changes. That
rules out Python's model.

Semicolons are pure ceremony in a language where newlines already
delimit statements 99% of the time. So: braces for structure, newlines
for termination, with explicit continuation rules for the 1%. This is
Go's answer and it has been thoroughly validated.

The important part is that "semicolons or not" is then *not a style
question*. It is the grammar. `DESIGN.md` calls this "style baked into
the grammar" and treats it as a strategy: the strongest possible "one
way to do it" is when the alternative does not parse. Braces on the
same line, mandatory block braces, and no semicolons are all instances.

---

### 4. Competing Approaches

**Go.** The closest relative. Same newline rule (extended for leading
dots), same directory-is-a-package model, same order-independent
declarations, same `//` comments plus block comments Glide drops.
Differences: Go's `func` versus `fn`; Go's capitalisation-as-visibility
versus `pub`; Go's `return` requirement versus tail expressions; Go's
`init()` versus nothing-before-main.

**Rust.** Tail expressions, `fn`, `let`, `->`, and `//` comments are all
Rust's, and a Rust programmer will find Glide's surface immediately
familiar. The visible differences at hello-world scale: no `::` path
separator (Glide uses `.` uniformly), no `!` on macros because there
are no macros, `println` takes one already-interpolated string rather
than a format string plus arguments, and the semicolon-as-statement-
terminator distinction is replaced by the signature rule.

**Zig.** `pub fn main() !void`, explicit allocators, and `test` blocks
in the language. Glide takes the `test` blocks and the `//`-only
comments; it declines the manual memory management and the
errors-on-unused-variables strictness.

**Python.** No braces, significant whitespace, `if __name__ ==
"__main__"` as the entry-point idiom, and imports that execute the
module body. Every one of those is a decision Glide goes the other way
on, and the import one is the most consequential: a Python import can
do anything, which is why Python programs have startup-order bugs.

**Java / C#.** A class wrapper around `main`, a `String[] args`
parameter, `System.out.println`. The class wrapper is pure ceremony for
a program with no objects in it; Glide has no classes at all
(Chapter 17). Argument arrival through `os.args()` rather than a
parameter is a small thing that matters: it means `main`'s signature
does not vary.

---

### 5. Common Mistakes

**Putting the dot at the end of the line out of Go habit.** Both work
today, but the idiom — and what the formatter will produce — is the dot
at the *start* of the continuing line:

Bad (works, but is not what the formatter emits):

```glide
let top = entries.iter().
    take(20).
    collect()
```

Good:

```glide
let top = entries
    .iter()
    .take(20)
    .collect()
```

**Forgetting that a bare tail value is an error.** The most common
version is calling a function for its side effect when that function
happens to return something:

```glide
fn setup() {
    db.exec("create table …")     // error: tail value not discarded
}
```

Fix with `_ =`:

```glide
fn setup() {
    _ = db.exec("create table …")
}
```

Yes, this is more typing than Go. It is also the reason Glide can
promise there are no silent discards anywhere — the same rule applies
inside `defer` blocks, which is how Go's worst `defer f.Close()`
data-loss bug is prevented (Chapter 21).

**Expecting `os.args()` to exclude the program name.** It does not.
`os.args()[0]` is the program name, exactly as in C and Go. A
one-argument program matches `[_, path]`, a two-argument program
matches `[_, src, dst]`.

**Writing a bare variant name in an expression.** This is the mistake
you will make in your first hour with sum types:

```glide
type Shape = Circle(Float) | Rect(Float, Float)

fn main() {
    let s = Circle(2.0)        // error
}
```

```
error: line 4: variants are namespaced: write .Circle or Shape.Circle
       (bare variant names are pattern-only)
```

In *patterns*, `Circle(r)` is right. In *expressions*, you need
`Shape.Circle(2.0)` or the dot shorthand `.Circle(2.0)`. Chapter 13
explains why the asymmetry exists.

**Assuming a file without `main` is broken.** `glide run` needs an
entry point; a library file with only types, functions, and tests is
run with `glide test`.

**Reaching for a block comment.** `/* … */` is not a syntax error in a
confusing way — it is a lex error immediately. Use your editor's
toggle-comment key.

---

### 6. Performance Considerations

At this scale there is almost nothing to say, which is itself worth
recording.

**Startup.** Nothing runs before `main`: no init graph to walk, no
static constructors, no module bodies to execute. A Glide binary's
startup cost is the runtime's own initialisation and nothing else. Go
programs with a deep dependency tree can spend real milliseconds in
`init()` before reaching `main`; that class of cost is unrepresentable
here.

**Printing.** One syscall per print call, always, on every stream type.
No tty detection, so there is no "works in my terminal, silent under
`| tee`" behaviour difference. The trade is explicit: correctness and
predictability for print, and an opt-in buffered writer (○) for bulk
output, whose `flush`/`close` is visible at the use site via `defer`.

One nice side effect of one-write-per-call: **per-call atomicity under
green threads.** Two tasks printing concurrently cannot interleave
half-lines.

**Interpolation.** In the designed language, interpolation desugars at
compile time into builder/writer calls through traits — zero runtime
machinery, no format-string parsing. In the interpreter it is done at
runtime, which is fine for a tree-walker but is not the shipping cost
model. Compare Go, which parses the format string at runtime on *every*
`Printf` call and then walks the arguments with reflection.

**Declaration order.** Order-independence at module level costs a
resolution pass, which the interpreter does when it collects
declarations. It is not a per-call cost.

---

### 7. Best Practices

**Write the important function first.** Order-independence exists so
that file order can be narrative. Put `main`, or the module's headline
function, at the top; helpers below it. The formatter will not reorder
your declarations, precisely so that this channel stays yours.

Bad — bottom-up, C-style, because the compiler used to require it:

```glide
fn parse_line(s: String) -> Result<Entry, Error> { … }
fn parse_file(s: String) -> Result<List<Entry>, Error> { … }
fn main() -> Result<(), Error> { … }
```

Good — the reader meets the purpose before the machinery:

```glide
fn main() -> Result<(), Error> { … }
fn parse_file(s: String) -> Result<List<Entry>, Error> { … }
fn parse_line(s: String) -> Result<Entry, Error> { … }
```

**Send diagnostics to stderr, data to stdout.** `eprintln` for usage
messages, warnings, and progress; `println` for the program's actual
output. This is what makes a program composable in a pipeline, and the
`e` prefix makes it a two-character decision rather than a
`fmt.Fprintln(os.Stderr, …)` decision.

**Prefer `fn main() -> Result<(), Error>` for anything that touches the
outside world.** The moment a program reads a file, opens a socket, or
parses input, `?` earns its keep. Starting with the fallible signature
costs one line (`Ok(())`) and saves restructuring later.

**Use exit codes deliberately.** `os.exit(2)` for usage errors is the
Unix convention (`2` = misuse); returning an `Err` from `main` gives
you `1`. Do not use `os.exit(1)` where an `Err` would do — the `Err`
path prints the error for you and, in the designed language, runs
`defer` blocks that `os.exit` skips.

**Do not import a module you only might need.** Imports are inert, so
an unused import costs nothing at runtime — but it is a warning in dev
builds and an error at `glide test`, and in the designed toolchain the
formatter maintains the import block for you anyway. Humans do not
curate imports.

---

### 8. Examples

**A program with no imports, demonstrating the tail rule and
interpolation:**

```glide
fn fizzbuzz(n: Int) -> String {
    match {
        n % 15 == 0 => "FizzBuzz"
        n % 3 == 0  => "Fizz"
        n % 5 == 0  => "Buzz"
        _           => "{n}"
    }
}

fn main() {
    for i in 1..=15 {
        println(fizzbuzz(i))
    }
}
```

```
1
2
Fizz
4
Buzz
Fizz
7
8
Fizz
Buzz
11
Fizz
13
14
FizzBuzz
```

Points of interest: `match` with no subject is a clean if/else-if chain
(first true arm wins); every arm is an *expression*, so the whole
`match` is the function's tail value; `1..=15` is an inclusive range;
and `"{n}"` converts an `Int` to a `String` by interpolation rather
than by a conversion function.

**The same program written badly**, to make the contrast concrete:

```glide
fn fizzbuzz(n: Int) -> String {
    let mut out = ""
    if n % 15 == 0 {
        out = "FizzBuzz"
    } else {
        if n % 3 == 0 {
            out = "Fizz"
        } else {
            if n % 5 == 0 {
                out = "Buzz"
            } else {
                out = "{n}"
            }
        }
    }
    return out
}
```

Everything here is legal. It is bad for four separate reasons, and each
maps to a rule from this chapter:

1. `let mut out` introduces a mutable binding whose only job is to
   carry a value to the `return`. In an expression-oriented language
   that variable is pure overhead — and `mut` is supposed to be a
   meaningful audit mark, so spending it on assign-once variables
   devalues it everywhere else.
2. The nested `if`/`else` ladder is what expressionless `match` exists
   to replace.
3. `return out` at the tail is redundant; `return` is for early exits.
4. Four levels of indentation for a three-way decision.

**A program that uses everything from this chapter:**

```glide
// wc.gld — count lines, words, and bytes, like the Unix tool.
import fs
import os

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: wc <file>")
        os.exit(2)
    }

    let text = fs.read_string(path).context("reading input")?

    let lines = text.lines().len()
    let words = text.split_whitespace().len()
    let bytes = text.len()

    println("{lines:8}{words:8}{bytes:8} {path}")
    Ok(())
}
```

```bash
$ cat testdata/sample.txt
the quick the lazy the dog quick
$ glide run wc.gld testdata/sample.txt
       1       7      33 testdata/sample.txt
```

`{lines:8}` is interpolation with a width spec: right-aligned in eight
columns. That is the whole formatting story — no `%8d`, and a spec that
does not fit the value's type is an error rather than
`%!d(string=hi)`.

---

### 9. Summary & Exercises

**Summary**

- `fn main()` is the entry point and nothing runs before it. `main` may
  also be declared `-> Result<(), Error>`, in which case the runtime
  prints an `Err` to stderr and exits 1.
- Signatures are always explicit; inference works inside bodies only.
  The last expression of a body is the return value, and `return` is
  for early exits.
- A no-arrow function whose body ends in a meaningful value is an
  error. Discard explicitly with `_ =`. There are no silent discards
  anywhere in the language.
- Newlines end statements when the previous token can end an
  expression; trailing operators and *leading* dots continue a line.
  `else` sits on the same line as its `}`.
- Case is grammar: capitalised = type/variant/constructor, lowercase =
  binding/function/field/module. Pattern matching depends on it, which
  is why `pub` exists instead of Go's capitalisation trick.
- Braces are mandatory on every block. Comments are `//` only. Files
  are `.gld`.
- Declarations are order-independent at module level, so file order is
  narrative order. Statements inside a body remain sequential.
- Imports are qualified and inert: importing executes nothing.
- `println`/`eprintln` are unbuffered builtins; `e` means stderr, `ln`
  means newline. That 2×2 grid is the entire print family.

**Exercises**

1. **Break the newline rule on purpose.** Write a statement that
   continues across three lines using a trailing operator, one that
   continues using a leading dot, and one that you *expect* to continue
   but does not. Predict the error before running it. (Hint: try
   splitting after a `)` .)

2. **Write `head`.** A program taking a filename and an optional line
   count, printing the first N lines (default 10). It should use
   `let … else` for argument handling, send its usage message to
   stderr, exit 2 on misuse, and propagate read errors out of `main`.
   Then make it handle *both* `head file` and `head file 20` — which
   will make you think about list patterns before Chapter 10 gets to
   them.

3. **Argue the tail rule.** Find a function in a codebase you maintain
   whose last statement is `return someCall()` where `someCall` returns
   something you ignore, or where a called function's return value is
   dropped silently. Would Glide's rule have caught a real bug, or
   would it just have cost you a `_ =`? Write down the ratio you would
   expect across the whole codebase. Chapter 21 asks you the same
   question about `defer`.
