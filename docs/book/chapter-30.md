# Chapter 30: Files, Processes, and CLI Programs

This is the shortest chapter in the book, because the surface it
describes is deliberately tiny. In the interpreter era every stdlib
module is a Go host shim behind a Glide-shaped interface, and
`stdlib.md` states the policy plainly: **surfaces are minimal and grow
when a program needs them — the dogfood rule.**

What exists today is four functions. What the shape teaches — argument
handling, exit codes, stream discipline, and error propagation out of
`main` — is the whole chapter.

---

### 1. Basic Usage

#### The `os` module

| Function | Signature |
|---|---|
| `os.args()` | `() -> List<String>` — program name first, then arguments |
| `os.exit(code)` | `(Int) -> !` — immediate exit |

```glide
import os

fn main() {
    let args = os.args()
    println("{args:?}")
}
```

```bash
$ glide run prog.gld a b
["prog.gld", "a", "b"]
```

`os.args()[0]` is the program name, as in C and Go.

#### The `fs` module

| Function | Signature |
|---|---|
| `fs.read_string(path)` | `(String) -> Result<String, Error>` |

```glide
import fs

fn main() -> Result<(), Error> {
    let text = fs.read_string("notes.txt").context("reading notes")?
    println("{text.lines().len()} lines")
    Ok(())
}
```

That is the whole `fs` surface today. Writing, directory listing,
metadata, and streaming are ○.

#### Argument handling

There is no flag parser (○ — `flag` is on the committed stdlib list).
Argument handling is a **list pattern**:

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

Three things are doing work in those four lines:

- `[_, path]` matches **exactly two** elements, so it rejects both
  no-arguments and too-many-arguments. That exactness is the whole
  reason it is a complete check (Chapter 10).
- `eprintln` sends the usage message to **stderr**, so it cannot
  contaminate the program's output in a pipeline.
- `os.exit(2)` **diverges**, which satisfies the `let … else` rule.
  `2` is the Unix convention for misuse.

For an optional second argument, match both shapes:

```glide
let args = os.args()
let (path, n) = match args {
    [_, p]       => (p, 10)
    [_, p, raw]  => (p, raw.parse_int() ?? 10)
    _            => {
        eprintln("usage: head <file> [lines]")
        os.exit(2)
    }
}
```

#### Exit codes

Three ways a program ends, and they mean different things:

| How | Exit code | Runs `defer`? |
|---|---|---|
| `main` returns `()` or `Ok(())` | 0 | ✓ |
| `main` returns `Err(e)` | 1, `e` printed to stderr | ✓ |
| `os.exit(n)` | `n` | **✗ skipped** |
| A panic | 1, message to stderr | ✓ |

`os.exit` skipping defers is deliberate — it is an *immediate* exit,
and that is what it is for. If you need cleanup, return an `Err`
instead.

#### Stream discipline

```glide
println(data)         // stdout — the program's output
eprintln(diagnostic)  // stderr — everything else
```

Usage messages, warnings, progress, and errors go to stderr. The
program's actual result goes to stdout. That is what makes a program
composable:

```bash
$ wordfreq notes.txt | sort -rn | head
```

gets word counts in the pipe and complaints on the terminal.

Both are unbuffered (Chapter 6), so a prompt appears before the read
and a debug print survives a crash on the next line.

#### The designed surface ○

`STDLIB-GOALS.md` and `DESIGN.md` commit to: full `fs` (write, create,
remove, rename, directory walking, metadata), `process` (spawn, pipes,
exit status), `flag` for CLI parsing, `embed` for build-time file
embedding, and environment variable access.

Two designed details worth knowing now:

**`embed` is declarative grammar, not Go's magic comment:**

```glide
embed "static/**" as assets      // ○
```

The build system provides the bytes; comptime never does IO, so
hermeticity survives. Embedded trees serve through the same `fs`
interface as disk, so dev-from-disk versus prod-from-binary is one
constructor swap.

**Cross-compilation is a flag**, not a project:

```bash
glide build -target linux/arm64      # ○
```

Any host to any target, no sysroots, static by default,
`FROM scratch`-ready. This stays trivial *because* C FFI was exiled to
the margins — cgo is what collapses Go's cross-compilation story.

---

### 2. Under the Hood

#### Host shims

Every module in the interpreter era is Go code behind a Glide-shaped
interface. `fs.read_string` calls `os.ReadFile`; `os.args()` reads
`os.Args`.

That has a designed consequence worth knowing: **stdlib shims are
injectable per host** (○). Because they are already Go code behind an
interface, making the provided set *chosen by the embedder* buys
capability-style sandboxing for free — an untrusted script embedded in
a Go program is simply never handed `fs` or `net`.

`DESIGN.md` calls this "the one embedding requirement worth honouring
while building the interpreter, because it is painful to retrofit."

#### `os.exit` is a sentinel panic

The interpreter implements `os.exit` by panicking with a sentinel that
becomes an `ExitError`, recovered in `Run`. That is why tests can
intercept exits instead of the test process dying — a `test` block
whose code calls `os.exit` reports a failure rather than killing the
runner.

It is also why `os.exit` skips defers: the unwind is caught at the top
rather than running the normal cleanup path.

#### Errors carry the OS message

`fs.read_string` returns the underlying error text:

```
error: open /nonexistent: no such file or directory
```

`.context("reading input")` prepends a breadcrumb (Chapter 19):

```
error: reading input: open /nonexistent: no such file or directory
```

#### IO is a cancellation point

File and network operations are cancellation points (Chapter 26), so a
read inside a dying scope unwinds rather than completing.

---

### 3. Why This Design?

#### Why the surface is so small

The **dogfood rule**: a stdlib function exists because a program needed
it, not because it might be useful.

This is not minimalism for its own sake — the designed standard library
is explicitly **batteries-included** (HTTP, TLS, crypto, JSON, time,
regex, structured logging, templating, compression, a `database/sql`-
style interface). `DESIGN.md`'s three-way argument:

- **Rust's minimal std** → 300 transitive dependencies per web service,
  each a supply-chain surface. The opposite of auditable.
- **Python froze its batteries and they died on the shelf.** `urllib`
  is stdlib; everyone installs `requests`. PEP 594 hauled out corpses.
- **Go proved the upside** — stdlib `net/http` made `Handler` the
  ecosystem's shared currency — but v1 chains it to fossils.

The synthesis: **stdlib versions with the language**, `glide fix`
migrates callers mechanically, and wrong modules get fixed rather than
embalmed beside their replacements. Python's disease was not batteries;
it was batteries plus immortality.

So the small surface today is a *sequencing* decision, not a
philosophy. The philosophy is that what ships must be right, and what
ships gets built when a real program forces the question.

#### Why argument parsing is a pattern, not a library

Because the list pattern is *already* a complete check.

`let [_, path] = os.args() else { usage() }` validates arity in both
directions, binds the argument, and handles the failure — in one line,
with no dependency and no framework. Compare Go:

```go
if len(os.Args) != 2 {
    fmt.Fprintln(os.Stderr, "usage: linecount <file>")
    os.Exit(2)
}
path := os.Args[1]
```

Five lines, and the index and the length check can disagree.

A real flag parser (`--verbose`, `-o file`, subcommands) is genuinely
a library and is on the committed list. Positional arguments are not.

#### Why `os.exit` skips defers

Because it is the "get out now" primitive, and a primitive that
sometimes runs arbitrary user code is not that.

The consequence is a rule: **use `os.exit` only where there is nothing
to clean up.** Argument validation at the top of `main`, before
anything is opened, is the canonical place. Everywhere else, return an
`Err` — which unwinds normally, runs defers, prints the error, and
exits 1.

This is also why `log.Fatal` is banned in the designed logging module:
it is a hidden `os.Exit` inside a logging call — control flow disguised
as observability, skipping defers. Log, then exit, in two honest lines.

#### Why no build scripts, restated here

Chapter 2 covered it; it belongs in a chapter about files because that
is where the temptation lives. `glide build` executes no user code,
reads nothing outside the module tree and `vendor/`, and touches no
network. Code generation is a step you run, visibly, with output
committed.

Cargo's `build.rs` and npm's lifecycle scripts mean compiling someone's
code runs their program on your machine. Most supply-chain attacks in
both ecosystems ride exactly that.

#### Why `embed` is grammar

Go's `//go:embed` is a magic comment — invisible to the formatter,
invisible to `grep` for anything but the literal string, and dependent
on an import of a package you never call.

`embed "static/**" as assets` is a declaration. The build system reads
the files (comptime never does IO, so hermeticity survives), and the
result serves through the same `fs` interface as disk. Content hashes
at build time give the immutable-URL caching story for free.

---

### 4. Competing Approaches

**Go.** `os.Args`, `flag`, `os`, `io/fs`, `os/exec`, `//go:embed`. The
model for most of the designed surface. Go's `flag` package is
deliberately limited (no short/long pairs, no subcommands), which is
why every serious Go CLI uses `cobra` or `urfave/cli` — a case where
the stdlib's minimalism lost.

**Rust.** `std::fs`, `std::env::args`, `std::process`, and `clap` for
CLI parsing as a near-universal dependency. Rust's `Path`/`PathBuf` vs
`OsString` vs `String` split is the ceremony Glide declines by treating
strings as UTF-8 bytes (Chapter 6).

**Python.** `sys.argv`, `argparse`, `pathlib`, `subprocess`,
`os`/`shutil`. Batteries-included and showing its age — three
overlapping filesystem APIs, and `subprocess` with a security-relevant
`shell=True` footgun.

**Zig.** `std.fs`, `std.process`, explicit allocators everywhere, and
the best cross-compilation in the industry (it ships a C compiler and
libc for every target). Glide's cross-compilation is trivial by
*avoidance* — no C FFI — rather than by Zig's heroics.

**C.** `argc`/`argv`, `open`/`read`/`write`, `errno`. The baseline, and
the source of the exit-code conventions everything else inherits.

**Node.** `process.argv` (with two leading entries, not one),
`fs` with three parallel APIs (callback, promise, sync), and npm
lifecycle scripts as the supply-chain door.

---

### 5. Common Mistakes

**Forgetting that `os.args()[0]` is the program name.** A one-argument
program matches `[_, path]`, not `[path]`.

**Indexing instead of pattern matching.**

```glide
// Bad — panics with no arguments
let path = os.args()[1]

// Good
let [_, path] = os.args() else {
    eprintln("usage: prog <file>")
    os.exit(2)
}
```

**Using `os.exit` where cleanup matters.**

```glide
// Bad — the defer never runs
fn main() {
    let db = sql.open(dsn) ?? { os.exit(1) }
    defer { _ = db.close() }
    if bad_config() {
        os.exit(1)              // db is never closed
    }
}

// Good
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    defer { _ = db.close() }
    if bad_config() {
        return Err(.BadConfig)  // unwinds; the defer runs
    }
    Ok(())
}
```

**Sending diagnostics to stdout.** It breaks pipelines. `eprintln` for
anything that is not the program's output.

**Expecting a flag parser.** There is none yet. Positional arguments
are patterns; flags are ○.

**Expecting `fs` to do more than read.** One function today. Writing,
listing, and metadata are ○.

**Reading a whole file when you want to stream it.**
`fs.read_string` loads the entire file into memory. For a
multi-gigabyte log that is a problem, and streaming IO is ○. Until then
this is a real limitation, not a style choice.

**Forgetting `.context()` on file errors.** `open /tmp/x: no such file`
tells you what failed; `loading config: open /tmp/x: no such file`
tells you why you cared.

---

### 6. Performance Considerations

**`fs.read_string` reads the whole file into memory** in one
allocation. Fine for configuration files and source code; not fine for
arbitrary user input of unknown size.

**Printing is one syscall per call** and unbuffered (Chapter 6). For
bulk output — a million lines — that is seconds rather than
milliseconds, and the designed answer is an explicit buffered writer
(○) whose `flush`/`close` is visible via `defer`.

Until it lands, the mitigation is to build the output and print once:

```glide
// Bad — a million syscalls
for line in lines { println(line) }

// Better today — one syscall
println(lines.join("\n"))
```

**IO is a cancellation point**, so a read inside a `scope(timeout:)`
can be aborted. That costs a context per call in the interpreter.

**`os.args()` allocates a list per call.** Call it once.

**Startup is nothing** (Chapter 29) — no init graph, no static
constructors.

---

### 7. Best Practices

**Validate arguments with a pattern, at the top of `main`, before
anything is open.**

```glide
fn main() -> Result<(), Error> {
    let [_, src, dst] = os.args() else {
        eprintln("usage: copy <src> <dst>")
        os.exit(2)
    }
    // nothing is open yet, so os.exit above is safe
    …
}
```

**Return `Err` from `main` for anything that happens after a resource
is open.** It prints the error, exits 1, and runs your defers.

**Use `os.exit(2)` for usage errors** and let everything else be an
`Err`. That is the Unix convention (`2` = misuse) and it distinguishes
"you called me wrong" from "something went wrong".

**Add context to every file error.**

```glide
// Good
let text = fs.read_string(path).context("reading {path}")?
```

**Keep the happy path unindented with `let … else`.**

```glide
fn main() -> Result<(), Error> {
    let [_, path] = os.args() else { usage() }
    let text = fs.read_string(path).context("reading {path}")?
    let Some(config) = parse(text) else {
        return Err(.BadConfig{ path: path })
    }
    run(config)
}
```

Three possible failures, zero nesting.

**Write to stdout only what a downstream program should consume.** If
you would not want it in a `| sort`, it goes to stderr.

**Do not build a flag parser.** Wait for `flag`, or use positional
arguments — which for a tool with one or two inputs is better anyway.

---

### 8. Examples

**`wc`, complete:**

```glide
// wc.gld — count lines, words, and bytes.
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
$ glide run wc.gld testdata/sample.txt
       1       7      33 testdata/sample.txt
$ glide run wc.gld
usage: wc <file>
$ echo $?
2
$ glide run wc.gld /nonexistent
error: reading input: open /nonexistent: no such file or directory
$ echo $?
1
```

Three exit paths, three exit codes, and each failure message says
something useful. Note the contrast between exit 2 (you called me
wrong) and exit 1 (something went wrong).

**`wordfreq` — the repository's own example:**

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

    let mut entries = counts.entries()
    entries.sort_by(|a, b| b.1.cmp(a.1))

    for (word, n) in entries.iter().take(20) {
        println("{n:6}  {word}")
    }
    Ok(())
}
```

```bash
$ glide run examples/wordfreq.gld testdata/sample.txt
     3  the
     2  quick
     1  lazy
     1  dog
```

Twenty lines, no dependencies, and every failure path handled. This is
the program `GRAMMAR.md` used to find the language's ugly corners, and
it is worth noting how much of the book it exercises: list patterns,
`let … else`, divergence, `?`, `.context`, map indexing returning an
Option, `??`, a comparator closure, lazy `take`, and a format spec.

**Optional arguments, without a flag parser:**

```glide
// head.gld — print the first N lines (default 10).
import fs
import os

fn main() -> Result<(), Error> {
    let args = os.args()
    let (path, n) = match args {
        [_, p]      => (p, 10)
        [_, p, raw] => (p, raw.parse_int() ?? 10)
        _           => {
            eprintln("usage: head <file> [lines]")
            os.exit(2)
        }
    }

    let text = fs.read_string(path).context("reading {path}")?
    for line in text.lines().iter().take(n) {
        println(line)
    }
    Ok(())
}
```

The `match` over `os.args()` handles all three shapes exhaustively, and
the `_` arm diverges so the `match` type-checks.

**Bad versus good: the exit that loses data**

```glide
// Bad
fn main() {
    let f = fs.create("out.txt") ?? { os.exit(1) }     // ○ fs.create
    defer { _ = f.close() }                             // never runs
    write_report(f)
    if verify_failed() {
        eprintln("verification failed")
        os.exit(1)          // the buffered write is lost
    }
}
```

`os.exit` skips defers, so `f.close()` — which is where a buffered
write is flushed — never happens. The file is truncated and the program
exits 1, looking like a clean failure.

```glide
// Good
type AppError = VerificationFailed

fn main() -> Result<(), AppError> {
    let f = fs.create("out.txt")?
    defer { _ = f.close() }
    write_report(f)
    if verify_failed() {
        return Err(.VerificationFailed)     // unwinds; the defer runs
    }
    Ok(())
}
```

The rule in one line: **`os.exit` before you open anything; `Err` after.**

---

### 9. Summary & Exercises

**Summary**

- The surface is deliberately tiny today: `os.args()`, `os.exit(code)`,
  and `fs.read_string(path)`. Everything else is ○ and arrives under
  the **dogfood rule** — a stdlib function exists because a program
  needed it.
- The *designed* standard library is batteries-included; the small
  surface is a sequencing decision. `DESIGN.md`'s synthesis: stdlib
  versions with the language and `glide fix` migrates callers, so
  wrong modules get fixed rather than embalmed. Python's disease was
  not batteries; it was batteries plus immortality.
- **`os.args()[0]` is the program name.** Argument handling is a **list
  pattern** — `let [_, path] = os.args() else { … }` validates arity
  in both directions, binds, and handles failure in one line.
- **Exit codes:** `Ok(())` → 0, `Err(e)` → 1 with the error on stderr,
  `os.exit(n)` → n, panic → 1. **`os.exit` skips `defer`s.**
- The rule: **`os.exit` before you open anything; `Err` after.** Same
  reasoning bans `log.Fatal` in the designed logging module — a hidden
  exit inside a logging call, skipping defers.
- **Stream discipline:** stdout is the program's output, stderr is
  everything else. Both unbuffered.
- ○: full `fs`, `process`, `flag`, environment variables, streaming IO,
  a buffered writer, `embed` as declarative grammar, and
  `glide build -target` cross-compilation that stays trivial because C
  FFI was exiled.
- Host shims are **injectable per embedder** (○), which buys
  capability-style sandboxing for free — an untrusted embedded script
  is simply never handed `fs`.

**Exercises**

1. **Write `cat` with three failure modes.** Handle no arguments (exit
   2, usage on stderr), a missing file (exit 1, error propagated from
   `main`), and success. Then add a `-n` line-numbering flag using only
   pattern matching, and note exactly where a real flag parser would
   start earning its place.

2. **Find the lost write.** In a codebase you know, find a call to
   `os.Exit`, `sys.exit`, `System.exit`, or `process.exit` that happens
   after a file or connection is opened. Trace whether the cleanup runs.
   In Go this is a known trap that `go vet` does not catch; in Glide it
   is the same trap, which is why the rule is worth internalising
   rather than relying on the language.

3. **Design the `flag` module.** Given that Go's `flag` was too limited
   and every serious Go CLI uses `cobra`, write the signature for a
   Glide `flag` API that handles short and long options, subcommands,
   and required arguments — using named parameters and sum types rather
   than a builder. Then check it against the "no DSL" principle: if
   your design has a fluent chain of `.Flag().Short().Required()`, it
   has become the thing `DESIGN.md` declines.
