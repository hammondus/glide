# Chapter 31: Files, Processes, and the Environment

This chapter used to be the shortest in the book, because the surface it
described was four functions. It is no longer short. `fs`, `os` and
`process` grew — in one sitting, driven by one real script — and what
they grew into is enough to replace most of what people write shell
scripts for.

That growth is the **dogfood rule** working as designed: a stdlib
function exists because a program needed it, not because it might be
useful. Chapter 32 is the program that needed these.

Everything in section 1 is ✓ and was executed to produce the output
shown. The ○ material is confined to *The designed surface*, and to a
short honest list at the end of what is still missing.

---

### 1. Basic Usage

#### The `os` module

| Function | Signature | Notes |
|---|---|---|
| `os.args()` | `() -> List<String>` | program name first |
| `os.exit(code)` | `(Int) -> !` | immediate; skips `defer` |
| `os.env(name)` | `(String) -> String?` | `None` when unset |
| `os.set_env(name, value)` | `(String, String) -> Result<(), Error>` | this process and its children |
| `os.cwd()` | `() -> Result<String, Error>` | resolved, symlinks followed |
| `os.chdir(path)` | `(String) -> Result<(), Error>` | **process-global** |

```glide-run
import os

fn main() {
    println("home={os.env("HOME") ?? "?"}")
    println("nope={os.env("DEFINITELY_NOT_SET") ?? "(unset)"}")
}
```

```
home=/Users/craig
nope=(unset)
```

`os.env` returns an **Option**, not a String, and that is not
pedantry: "unset" and "set to the empty string" are different states,
and every shell script that has ever confused them has a bug in it.
Pair it with `??` when you have a default, and match it when you do not.

`os.chdir` is marked process-global as a warning. Two tasks calling it
under `spawn` interleave, and a third resolving a relative path sees
whichever won. It is fine in a single-threaded script, which is what it
is for; prefer building absolute paths with `fs.join` in anything
concurrent.

#### The `fs` module

Paths are Strings. A typed `path` module is designed (○); until it
exists, a String is what a script has, and converting at every boundary
would buy no checking.

| Function | Signature | Notes |
|---|---|---|
| `fs.read_string(path)` | `-> Result<String, Error>` | whole file |
| `fs.write_string(path, s)` | `-> Result<(), Error>` | creates or **truncates** — the shell's `>` |
| `fs.append_string(path, s)` | `-> Result<(), Error>` | the shell's `>>` |
| `fs.exists(path)` | `-> Bool` | bare Bool |
| `fs.is_dir(path)` | `-> Bool` | `false` for a missing path too |
| `fs.remove(path)` | `-> Result<(), Error>` | one file, or one *empty* directory |
| `fs.remove_all(path)` | `-> Result<(), Error>` | the whole tree |
| `fs.mkdir_all(path)` | `-> Result<(), Error>` | parents too; already-exists is `Ok` |
| `fs.rename(from, to)` | `-> Result<(), Error>` | same-filesystem move |
| `fs.list_dir(path)` | `-> Result<List<String>, Error>` | entry **names**, sorted |
| `fs.join(segments)` | `(List<String>) -> String` | cleaned, platform separator |

```glide-run
import fs
import os

fn main() -> Result<(), Error> {
    let dir = fs.join([os.env("TMPDIR") ?? "/tmp", "glide-demo"])
    fs.mkdir_all(dir)?

    let file = fs.join([dir, "a.txt"])
    fs.write_string(file, "one\n")?
    fs.append_string(file, "two\n")?

    println("exists={fs.exists(dir)} is_dir={fs.is_dir(dir)}")
    println("entries={fs.list_dir(dir)?}")
    println("body={fs.read_string(file)?.lines()}")

    fs.remove_all(dir)?
    println("gone={fs.exists(dir) == false}")
    Ok(())
}
```

```
exists=true is_dir=true
entries=["a.txt"]
body=["one", "two"]
gone=true
```

Four of these signatures are worth a sentence each.

**`fs.exists` and `fs.is_dir` return a bare `Bool`,** not a
`Result<Bool, Error>`. A Result here would be one you could only ever
unwrap: there is no useful way to fail at answering "is this there?"
that is not simply "no".

**`fs.list_dir` returns entry *names*, not paths,** and they are
**sorted**. Sorting is deliberate — an unsorted listing makes a
program's output depend on the filesystem's whim, and that is a class
of flaky test nobody should have to debug. To get a path, join:

```glide
for name in fs.list_dir(dir)? {
    let path = fs.join([dir, name])
    …
}
```

**`fs.remove_all` is named so it is never reached by accident.** It is
the one call in the module that can destroy something you meant to
keep, and `remove` will refuse a non-empty directory.

**`fs.join` takes a `List<String>`, not a variadic,** because the
language has no variadics and never will. It moves to a `path` module
when that lands.

#### The `process` module

| Surface | Signature | Notes |
|---|---|---|
| `process.run(cmd [, args])` | `(String, List<String>?) -> Result<Output, Error>` | runs to completion, capturing both streams |
| `out.status()` | `() -> Int` | exit code; `-1` if a signal killed it |
| `out.ok()` | `() -> Bool` | `status() == 0` |
| `out.stdout()` / `out.stderr()` | `-> String` | captured in full |

```glide-run
import process

fn main() -> Result<(), Error> {
    let out = process.run("echo", ["hello", "from a child"])?
    println("status={out.status()} ok={out.ok()}")
    print(out.stdout())

    let miss = process.run("grep", ["zzz", "/etc/hosts"])?
    println("grep status={miss.status()} ok={miss.ok()}")

    match process.run("no-such-binary-anywhere", []) {
        Ok(_)  => println("unreachable")
        Err(e) => println("could not run: {e}")
    }
    Ok(())
}
```

```
status=0 ok=true
hello from a child
grep status=1 ok=false
could not run: exec: "no-such-binary-anywhere": executable file not found in $PATH
```

Three properties of this surface are deliberate, and all three are the
opposite of what a shell does.

**A non-zero exit is not an error.** Look at the `grep` line: it exited
1, and that is an `Ok` with `status() == 1`. `Err` means the program
could not be run *at all* — not on `PATH`, not executable, killed by
the enclosing scope. A program that ran and exited 1 produced an
answer: that is how `grep` says "no match", `diff` says "they differ",
and `test` says "false". Folding it into `Err` would make `?` propagate
an ordinary result, and every caller would have to un-propagate it.

**There is no shell.** The command and its arguments are separate
values, and an argument containing a space stays one argument. Nothing
is word-split, so there is no quoting to get wrong and no injection to
audit — which is most of what makes shell scripts fragile. A shell is
still available, and then it is *visible at the call site*:

```glide
process.run("sh", ["-c", "a | b > c"])
```

which is the only kind of `shell=True` that should exist: one you can
grep for.

**It is a cancellation point.** `scope(timeout: 5.s)` kills the child.
Without that the scope would return and leave the process running,
which is exactly the leak structured concurrency exists to prevent
(Chapter 27).

```glide-run
import process

fn main() {
    let r = scope(timeout: 500.ms) {
        process.run("sleep", ["5"])
    }
    match r {
        Ok(_)  => println("finished")
        Err(_) => println("timed out — and the child is dead, not orphaned")
    }
}
```

```
timed out — and the child is dead, not orphaned
```

#### Argument handling

There is still no flag parser (○ — `flag` is on the committed stdlib
list). Argument handling is a **list pattern**:

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

Three things do work in those four lines:

- `[_, path]` matches **exactly two** elements, so it rejects both
  no-arguments and too-many-arguments. That exactness is what makes it
  a complete check (Chapter 10).
- `eprintln` sends the usage message to **stderr**, so it cannot
  contaminate the program's output in a pipeline.
- `os.exit(2)` **diverges**, which satisfies the `let … else` rule. `2`
  is the Unix convention for misuse.

For an optional second argument, match both shapes:

```glide
let args = os.args()
let (path, n) = match args {
    [_, p]      => (p, 10)
    [_, p, raw] => (p, raw.parse_int() ?? 10)
    _           => {
        eprintln("usage: head <file> [lines]")
        os.exit(2)
    }
}
```

#### Exit codes

| How | Exit code | Runs `defer`? |
|---|---|---|
| `main` returns `()` or `Ok(())` | 0 | ✓ |
| `main` returns `Err(e)` | 1, `e` printed to stderr | ✓ |
| `os.exit(n)` | `n` | **✗ skipped** |
| A panic | 1, message to stderr | ✓ |

`os.exit` skipping defers is deliberate — it is an *immediate* exit,
and that is what it is for. If you need cleanup, return an `Err`.

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

Both are unbuffered (Chapter 6), so a prompt appears before the read
and a debug print survives a crash on the next line.

#### The designed surface ○

`STDLIB-GOALS.md` and `DESIGN.md` commit to `flag` for CLI parsing,
`embed` for build-time file embedding, a typed `path` module, file
metadata, and streaming IO.

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
interface. `fs.read_string` calls `os.ReadFile`; `process.run` builds
an `exec.Cmd`; `os.args()` reads `os.Args`.

That has a designed consequence worth knowing: **stdlib shims are
injectable per host** (○). Because they are already Go code behind an
interface, making the provided set *chosen by the embedder* buys
capability-style sandboxing for free — an untrusted script embedded in
a Go program is simply never handed `fs` or `process`.

`DESIGN.md` calls this "the one embedding requirement worth honouring
while building the interpreter, because it is painful to retrofit."

#### How `process.run` becomes a cancellation point

The child is started with `exec.CommandContext` under the interpreter's
*host context* — the same context a `scope(timeout:)` cancels and an
`http.get` respects. The interpreter lock is released while the child
runs, so other tasks in the scope keep making progress; when the scope
dies, the context is cancelled and the child is signalled.

That is why the timeout example above kills `sleep 5` rather than
merely stopping waiting for it. "Stopped waiting" is the Go
`context.WithTimeout` + `cmd.Run` mistake, and it leaves a process
behind.

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

`.context("reading input")` prepends a breadcrumb (Chapter 20):

```
error: reading input: open /nonexistent: no such file or directory
```

Because `Error` is boxed (Chapter 20), an error you construct yourself
and one that came from the operating system are the same kind of value
and print alike.

---

### 3. Why This Design?

#### Why the surface grew when it did

It grew because someone tried to write a shell script in it. That is
the dogfood rule stated as a process rather than a slogan: the module
list did not come from a survey of what `os` modules usually contain,
it came from a program that could not be written.

Two things fell out of that sitting which no amount of design-in-the-
abstract had found: `match` arms could not be separated by commas (so a
one-line `match` was unwritable), and `Some(1) == Some(1)` panicked.
Both were found within ten minutes of typing real code, and neither was
visible from the roadmap.

The lesson is in Chapter 32, and it is the reason that chapter exists.

#### Why a non-zero exit is not an error

Because it is not one. `grep` exiting 1 is `grep` answering the
question. The shell conflates "the program said no" with "the program
broke" because it only has one channel — the exit status — and then
`set -e` turns every "no" into a fatal error, which is why real shell
scripts are full of `|| true`.

Glide has two channels: `Result` says whether the program *ran*, and
`status()` says what it *said*. `?` propagates the first and never the
second, so a script can use `?` freely without accidentally aborting on
a "no".

#### Why there is no shell

Word-splitting is the root of most shell bugs, and command injection is
word-splitting with an adversary. A filename with a space in it breaks
an unquoted script; a filename with a `;` in it runs whatever comes
after.

Passing an executable and a list removes the entire class. There is
nothing to quote because nothing is parsed. And when you genuinely want
a pipeline, `process.run("sh", ["-c", …])` says so at the call site —
which means a reviewer can find every place a shell is involved with
one grep, instead of auditing every string that ever reaches a
`system()` call.

Python's `subprocess` gets this right and then supplies `shell=True`
anyway; that flag is the single most common source of injection bugs in
Python code. Glide's version of the flag is a different program name.

#### Why the surface is still small

The designed standard library is explicitly **batteries-included**
(HTTP, TLS, crypto, JSON, time, regex, structured logging, templating,
compression, a `database/sql`-style interface). `DESIGN.md`'s three-way
argument:

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

So the small surface is a *sequencing* decision. What ships must be
right, and what ships gets built when a real program forces the
question.

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

A real flag parser (`--verbose`, `-o file`, subcommands) is genuinely a
library and is on the committed list. Positional arguments are not.

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

---

### 4. Competing Approaches

**Bash.** The incumbent, and the thing Chapter 32 replaces. Everything
is a string, every command substitution is a word-splitting hazard,
`set -euo pipefail` is a three-flag apology for the defaults, and there
is no way to say "this function returns a list".

**Go.** `os.Args`, `flag`, `os`, `io/fs`, `os/exec`, `//go:embed`. The
model for most of this surface. `os/exec` is where `process.run` comes
from, minus the `Cmd` struct's twenty configurable fields — those
arrive as named parameters when a program needs them. Go's `flag`
package is deliberately limited, which is why every serious Go CLI uses
`cobra` — a case where the stdlib's minimalism lost.

**Rust.** `std::fs`, `std::env`, `std::process::Command`, and `clap` as
a near-universal dependency. Rust's `Path`/`PathBuf` vs `OsString` vs
`String` split is the ceremony Glide declines by treating strings as
UTF-8 bytes (Chapter 6) — at the cost of being wrong on Windows paths
that are not valid UTF-16, which is a trade recorded rather than
overlooked.

**Python.** `sys.argv`, `argparse`, `pathlib`, `subprocess`,
`os`/`shutil`. Batteries-included and showing its age — three
overlapping filesystem APIs, and `subprocess` with a security-relevant
`shell=True` footgun.

**Zig.** `std.fs`, `std.process`, explicit allocators everywhere, and
the best cross-compilation in the industry. Glide's cross-compilation
is trivial by *avoidance* — no C FFI — rather than by Zig's heroics.

**Node.** `process.argv` (with two leading entries, not one), `fs` with
three parallel APIs (callback, promise, sync), and npm lifecycle
scripts as the supply-chain door.

---

### 5. Common Mistakes

**Treating a non-zero exit as a failure.**

```glide
// Bad — `?` here is right, but the check that follows is missing
let out = process.run("git", ["rev-parse", "HEAD"])?
let head = out.stdout().trim()        // empty when git said "not a repo"

// Good
let out = process.run("git", ["rev-parse", "HEAD"])?
if out.ok() == false {
    return Err("not a git repository: {out.stderr().trim()}")
}
let head = out.stdout().trim()
```

`?` catches "git is not installed". Only `out.ok()` catches "git ran
and said no". They are different failures and they need different
handling — which is the entire point of splitting them.

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

**Using `os.env` as if it were a String.** It is an `Option`. `??`
supplies a default; `match` distinguishes unset from empty. If you find
yourself writing `os.env("X") ?? ""`, ask whether unset and empty
really are the same thing for your program — sometimes they are, and
then the `??` is correct and deliberate.

**Building paths with `+`.**

```glide
// Bad — double separators, no cleaning, wrong on Windows
let p = dir + "/" + name

// Good
let p = fs.join([dir, name])
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
        return Err("bad config")     // unwinds; the defer runs
    }
    Ok(())
}
```

**Sending diagnostics to stdout.** It breaks pipelines. `eprintln` for
anything that is not the program's output.

**Reading a whole file when you want to stream it.**
`fs.read_string` loads the entire file into memory. For a
multi-gigabyte log that is a problem, and streaming IO is ○. Until then
this is a real limitation, not a style choice.

**Expecting `process.run` to stream output to the terminal.** It
captures. A long build's progress is invisible until it finishes, and
streaming is ○ — and not a triviality: the interpreter's stdout is an
`io.Writer` a test can redirect, and a subprocess writing to the real
file descriptor bypasses it, so the two tiers could disagree about
where output went.

**Forgetting `.context()` on file errors.** `open /tmp/x: no such file`
tells you what failed; `loading config: open /tmp/x: no such file`
tells you why you cared.

---

### 6. Performance Considerations

**`fs.read_string` reads the whole file into memory** in one
allocation. Fine for configuration files and source code; not fine for
arbitrary user input of unknown size.

**`process.run` buffers both streams in full.** A child that produces a
gigabyte of stdout produces a gigabyte of Glide `String`. The designed
fix is streaming (○); the mitigation today is to make the child produce
less — `grep -c` rather than `grep | count the lines`.

**A process is orders of magnitude more expensive than a function
call.** A shell script pays this constantly because everything is a
program; a Glide script should pay it only where the work genuinely
lives in another program. Counting lines with `text.lines().len()` beats
`process.run("wc", ["-l", path])` by about four orders of magnitude,
and it cannot be defeated by a filename with a space in it.

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

**Startup is nothing** (Chapter 30) — no init graph, no static
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

**Wrap a conversation with an external program in one function that
returns an Option or a Result.**

```glide
// Good — the two failure kinds are handled once, at the boundary
fn git(dir: String, args: List<String>) -> String? {
    let mut full = ["-C", dir]
    full.extend(args)
    match process.run("git", full) {
        Ok(out) => if out.ok() { Some(out.stdout().trim()) } else { None }
        Err(e)  => {
            eprintln("git is not runnable: {e}")
            None
        }
    }
}
```

Callers then write `git(dir, ["rev-parse", "HEAD"])` and deal with one
`Option` instead of an `Output` and a status code.

**Put a timeout on anything that talks to another program.**

```glide
scope(timeout: 10.s) {
    // every child started in here dies with the scope
}
```

A shell script that hangs hangs forever. This is the cheapest
reliability win in the chapter.

**Add context to every file error.**

```glide
let text = fs.read_string(path).context("reading {path}")?
```

**Keep the happy path unindented with `let … else`.**

```glide
fn main() -> Result<(), Error> {
    let [_, path] = os.args() else { usage() }
    let text = fs.read_string(path).context("reading {path}")?
    let Some(config) = parse(text) else {
        return Err("{path}: not a valid config")
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

**A directory tally — `fs` and a sort:**

```glide-run
import fs
import os

fn main() -> Result<(), Error> {
    let dir = if os.args().len() > 1 { os.args()[1] } else { os.cwd()? }

    let mut sized: List<(String, Int)> = []
    for name in fs.list_dir(dir)? {
        let path = fs.join([dir, name])
        if fs.is_dir(path) { continue }
        match fs.read_string(path) {
            Ok(body) => sized.push((name, body.len()))
            Err(_)   => {}          // unreadable: skip, don't fail the run
        }
    }

    sized.sort_by(|a, b| b.1.cmp(a.1))
    println("{sized.len()} readable file(s) in {dir}")
    for entry in sized.slice(0, if sized.len() < 5 { sized.len() } else { 5 }) {
        println("  {entry.1:8}  {entry.0}")
    }
    Ok(())
}
```

The `Err(_) => {}` arm is the interesting line. A file that cannot be
read — a socket, a permission problem, a dangling symlink — should not
fail a directory tally, and writing that decision as an explicit empty
arm makes it a choice a reviewer can see, rather than a missing check.

**Talking to another program, safely:**

```glide-run
import process

fn main() -> Result<(), Error> {
    // An argument with a space in it is one argument. There is nothing
    // to quote, and no way for the string to become two arguments.
    let name = "a file with spaces.txt"
    let out = process.run("echo", ["--", name])?
    print(out.stdout())

    // A shell, when you actually want one — visible at the call site.
    let piped = process.run("sh", ["-c", "printf 'b\\na\\n' | sort"])?
    print(piped.stdout())
    Ok(())
}
```

```
-- a file with spaces.txt
a
b
```

**Bad versus good: the exit that loses data**

```glide
// Bad
fn main() {
    let f = fs.create("out.txt") ?? { os.exit(1) }     // ○ fs.create
    defer { _ = f.close() }                            // never runs
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
fn main() -> Result<(), Error> {
    let f = fs.create("out.txt")?
    defer { _ = f.close() }
    write_report(f)
    if verify_failed() {
        return Err("verification failed")     // unwinds; the defer runs
    }
    Ok(())
}
```

The rule in one line: **`os.exit` before you open anything; `Err`
after.**

---

### 9. Summary & Exercises

**Summary**

- **`os`** ✓: `args`, `exit`, `env`, `set_env`, `cwd`, `chdir`.
  `os.env` returns an `Option` because unset and empty are different
  states. `os.chdir` is process-global — a warning, not a feature.
- **`fs`** ✓: `read_string`, `write_string`, `append_string`, `exists`,
  `is_dir`, `remove`, `remove_all`, `mkdir_all`, `rename`, `list_dir`,
  `join`. Paths are Strings until a typed `path` module lands (○).
  `list_dir` returns sorted *names*, so output never depends on the
  filesystem's whim.
- **`process`** ✓: `run(cmd, args)` returning `Result<Output, Error>`,
  with `status()`, `ok()`, `stdout()`, `stderr()`.
- **A non-zero exit is not an error.** `Err` means "could not run";
  `status()` means "what it said". `?` propagates the first and never
  the second.
- **There is no shell**, so there is nothing to quote and no injection
  to audit. `process.run("sh", ["-c", …])` is available and visible.
- **`process.run` is a cancellation point** — `scope(timeout:)` kills
  the child rather than abandoning it.
- **`os.args()[0]` is the program name.** Argument handling is a **list
  pattern** — `let [_, path] = os.args() else { … }` validates arity in
  both directions, binds, and handles failure in one line.
- **Exit codes:** `Ok(())` → 0, `Err(e)` → 1 with the error on stderr,
  `os.exit(n)` → n, panic → 1. **`os.exit` skips `defer`s.** The rule:
  **`os.exit` before you open anything; `Err` after.**
- **Stream discipline:** stdout is the program's output, stderr is
  everything else. Both unbuffered.
- ○ and honestly missing: streaming IO in both directions, file
  metadata (size, mtime, mode), stdin, a per-call environment or
  working directory for a child, a typed `path` module, `flag`, a
  buffered writer, `embed`, and `glide build -target`.
- Host shims are **injectable per embedder** (○), which buys
  capability-style sandboxing for free.

**Exercises**

1. **Write `cat` with three failure modes.** Handle no arguments (exit
   2, usage on stderr), a missing file (exit 1, error propagated from
   `main`), and success. Then add a `-n` line-numbering flag using only
   pattern matching, and note exactly where a real flag parser would
   start earning its place.

2. **Break the shell, then fail to break Glide.** Write a bash script
   that takes a filename argument and `cat`s it. Run it against a file
   named `a b.txt`, then against one named `$(whoami).txt`, then
   against `; echo pwned`. Write the same program in Glide with
   `process.run` and try all three again. Count the quoting rules you
   had to learn on each side.

3. **Find the orphaned process.** Write a Glide program that runs
   `sleep 30` inside `scope(timeout: 1.s)`, and check with `ps` that
   the child is gone after the program exits. Then write the Go
   equivalent using `context.WithTimeout` and `cmd.Run` — not
   `CommandContext` — and check again. That difference is why
   cancellation is part of the language rather than a library
   convention.

4. **Design the `flag` module.** Given that Go's `flag` was too limited
   and every serious Go CLI uses `cobra`, write the signature for a
   Glide `flag` API that handles short and long options, subcommands,
   and required arguments — using named parameters and sum types rather
   than a builder. Then check it against the "no DSL" principle: if
   your design has a fluent chain of `.Flag().Short().Required()`, it
   has become the thing `DESIGN.md` declines.
