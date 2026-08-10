# Chapter 2: The Toolchain

Glide's toolchain is one binary. That is not a convenience claim; it is
a design constraint recorded as principle five in `DESIGN.md`: *the
toolchain is the language*. Formatter, test runner, documentation
generator, LSP, and package manager ship in the same executable as the
compiler, on day one, with no configuration file format.

Today that binary is a tree-walking interpreter with three subcommands.
This chapter covers what exists (✓), what is designed (○), and — more
usefully — *why* a language project would spend its earliest effort on
tooling rather than on features.

---

### 1. Basic Usage

#### Building the interpreter ✓

There is no installer yet. You need a Go toolchain (1.21 or later) and
the repository:

```bash
git clone <the glide repo>
cd glide/glide          # the interpreter lives in the glide/ subdirectory
make build              # → bin/glide
```

That is the whole build. It takes a couple of seconds and produces a
single static binary at `glide/bin/glide`. Put it on your `PATH` or
invoke it by path; the examples in this book assume it is called
`glide`.

The `Makefile` follows the canonical target set used across this
author's projects, so `make test`, `make release`, and `make clean` all
do what you would expect:

```bash
make test      # go vet ./... && go test -count=1 ./...
make release   # cross-compiled, stripped binaries for macOS, linux
               # (arm64 + amd64), and Windows
make clean     # remove bin/ and dist/
```

Note the `release` target: `CGO_ENABLED=0` for every target, so the
outputs are genuinely static and cross-compile without a C toolchain.
That is inherited from Go and is exactly the property Glide intends to
keep for its own compiler.

#### Running a program ✓

```bash
glide run hello.gld
glide run wordfreq.gld notes.txt        # arguments after the file
                                        # reach os.args()
```

`glide run` parses the file, evaluates it, and calls `main`. There is no
separate compile step at this tier — the interpreter *is* the dev
backend.

Try it on the examples that ship with the repository:

```bash
$ glide run examples/wordfreq.gld testdata/sample.txt
     3  the
     2  quick
     1  lazy
     1  dog
```

```bash
$ glide run examples/pipeline.gld
pool_sum(10)  = 385
fan_in()      = 165
with_deadline = timed out (children cancelled and joined)
```

That second one is a worker pool, a `select`-based fan-in, and a
cancelling timeout, all running on the tree-walker. Chapter 26 through
Chapter 29 pull it apart.

#### Running tests ✓

```bash
glide test file.gld
```

Tests are a language construct, not a naming convention — a `test "…"
{ … }` block is legal in any `.gld` file. The runner executes plain
test blocks once, and property-test blocks (the ones that take
parameters) 100 times with generated inputs:

```bash
$ glide test examples/tree.gld
ok    in-order traversal is sorted  (100 cases)
skip  bench "insert 10k" (benchmarks not implemented yet)
```

The exit code reflects failures, so `glide test` slots into CI without
a wrapper script. Chapter 23 covers the testing model in full.

#### Checking without running ✓

```bash
glide check file.gld
```

Report and stop, in the shape of `go vet`: every diagnostic the checker
can produce, nothing executed, non-zero exit if anything was found.

```bash
$ glide check app.gld
app.gld:6:19: expected String, found Int
 6 |     println(greet(42))
   |                   ^^
```

This is a *convenience*, never a way to skip checking. `glide run` and
`glide test` run the identical check first, there is no `--no-check`,
and there is not going to be one — Chapter 19 explains why that
particular door stays shut.

Put it in a Makefile and in your editor's save hook; it is the fast
feedback loop.

#### Script mode ✓

Because the dev tier is an interpreter, type-checked scripting is
nearly free — and a `#!` line on the first line is skipped, so a script
is an executable:

```glide
#!/usr/bin/env -S glide run
// tidy.gld — a maintenance script with real types
import os

fn main() {
    println("{os.args()}")
}
```

```bash
$ chmod +x tidy.gld
$ ./tidy.gld alpha "two words"
["./tidy.gld", "alpha", "two words"]
```

Arguments reach `os.args()` exactly as they would through
`glide run tidy.gld alpha "two words"`, which is the same program.

**Write `env -S`, not bare `env`.** Linux passes everything after the
interpreter path to `execve` as a *single* argument, so
`#!/usr/bin/env glide run` sends `env` looking for a binary literally
named `glide run` and fails. macOS splits the line instead, so the bare
form works there and breaks the moment the script reaches a Linux box —
the worst shape a portability bug can take. `-S` (GNU coreutils 8.30+,
and BSD/macOS `env`) does the splitting explicitly on both.

Two details of the implementation are worth knowing, because both are
the kind of thing that is annoying to retrofit:

- The line is **skipped, not stripped**. Its newline is still counted,
  so every diagnostic in the file reports the line your editor shows.
  A `sed 1d` approach would put every error one line out.
- `#` is a comment character **nowhere else** — not on line 2, not
  mid-line. It is recognised only as the first two bytes of a file, so
  nothing about the rest of the grammar changes.

#### The designed command surface ○

The full command set is *closed* — this list is the whole thing, and
`DESIGN.md` commits to not growing it:

| Command | Purpose | Status |
|---|---|---|
| `glide build` | Compile to a native binary | ○ |
| `glide run` | Build (or interpret) and execute | ✓ (interprets; checks first) |
| `glide test` | Run tests — **and** format check, lints, race detector, doc-link validation, examples | ✓ (tests only; checks first) |
| `glide check` | Type-check and report, changing nothing | ✓ |
| `glide fmt` | Canonical formatter | ○ |
| `glide vet` | Advisory lints | ○ |
| `glide doc` | Terminal lookup + local HTML docs server | ○ |
| `glide fix` | Mechanical rewrites for breaking language changes | ○ |
| `glide get` / `glide tidy` | Dependency management | ○ |
| `glide version` | Toolchain version | ○ |

There is **no plugin system** and no `glide-*` subcommand discovery.
Cargo's extension mechanism is a hole through which arbitrary code runs
during what looks like a build; the closed surface is the same
supply-chain decision as banning build scripts.

#### Editor setup ○

The designed answer is one shipped LSP server inside the same binary,
so every editor gets format-on-save, go-to-definition, inline
diagnostics, and a test list with zero configuration. Nothing exists
today; syntax highlighting for `.gld` files can be faked by telling
your editor to treat them as Rust, which gets keywords, strings, and
comments approximately right and gets `match` arms wrong.

---

### 2. Under the Hood

#### What `glide run` actually does today

The pipeline is short, and every stage is worth knowing because its
error messages are what you will spend time reading:

1. **Lex.** The lexer produces tokens, tracking line *and column*. It
   errs at the first impossible character rather than guessing and
   recovering — a deliberate decision recorded in
   `glide/DESIGN-DECISIONS.md`. The motivating bug: a missing `}` in a
   string interpolation used to swallow the following interpolations as
   format-spec text, and the intended closing quote opened a phantom
   nested string, so the reported error ("unterminated string") pointed
   at an entirely different construct. Now, since `{` and `"` can never
   legally appear after a format spec's `:`, the lexer stops right
   there with a column and a `missing '}'?` hint.

2. **Parse.** Recursive descent with a small Pratt core for expression
   precedence. There is no separate grammar file: *the parser is the
   grammar*, and an EBNF will be extracted from the working parser
   later rather than written ahead of it. This is an unusual choice and
   an honest one for a language that is still moving.

3. **Check.** Every program is type-checked before any of it runs.
   Annotations are not decoration: they are read, resolved to real
   types, and enforced. This stage runs in every tier — `glide run`,
   `glide test`, and eventually the compiler — with no `--no-check`
   and no plan for one. `glide check` is this stage on its own.

4. **Evaluate.** A tree-walking evaluator over the AST. Declarations
   (functions, types, `impl` blocks, `const`) are collected first and
   are order-independent; then `main` is called.

The checker was the M4 milestone, and it is done. An earlier plan
deferred it to a compiler frontend written in Glide; that was reversed,
for reasons Chapter 37 covers — the frontend would have been the most
type-dense Glide program ever written, written in the one tier that
checked nothing. The checker was built in Go instead, and the
interpreter keeps it. `glide run` is a statically-checked scripting
language, not a stepping stone. Chapter 19 is the chapter on the
checker itself: what it proves, and where it stops.

#### Everything is checked, including the rules that are not about types

The checker is not only a type checker. Every rule the language has is
enforced before a line executes: `mut` on bindings, receivers and
assignment paths; sum-type match exhaustiveness; reserved-name
protection for the builtins; the tail-value rule; the nested-shadow
ban; `let … else` divergence; and every annotation in the program.

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
error: line 4: cannot shadow "count" from an enclosing block (redeclaring in the same scope is fine; nested shadowing is not)
```

Note the shape of that message: what is wrong, and what to do about
it. That is a standard the project holds itself to, and it is a
standard worth stealing.

The last three of those rules — tail-value, nested-shadow, let-else
divergence — were the evaluator's alone until M4d, and fired only when
the offending line actually ran. Chapter 19 covers what closing them
required, and the one edge that remains.

The evaluator still carries its own copy of each rule. That is
deliberate belt-and-braces: reaching one now means the checker has a
bug, so it should be loud rather than silent. `DESIGN.md` keeps the
question of whether the duplicates stay forever as an open one.

#### The interpreter's concurrency model, briefly

Spawned tasks run on real goroutines, but exactly one of them
interprets at a time — there is a single interpreter lock, released
around blocking operations. This is not a shortcut to be apologised
for. Tasks therefore interleave *exactly at blocking operations*, which
is precisely the cancellation-point rule that `DESIGN.md` ratifies. The
lock is the semantics, not just a guard.

The cost, stated plainly: two compute-bound tasks serialise. The
semantics permit any scheduling, so no program can detect this except
through throughput, and release backends will add true parallelism
without changing observable behaviour.

#### The designed compiler ○

Two backends, which is the plan that dissolves the classic "fast
compiler or fast code" dilemma:

- **Dev tier:** a custom backend built for compile speed over
  optimisation quality. Sub-second builds are non-negotiable.
- **Release tier:** LLVM, eventually. In between, a Glide→Go
  transpiler, because Glide's runtime model *is* Go's — green threads,
  tracing GC, `defer`, channels, value structs — so Glide lowers onto
  Go source nearly one-to-one and inherits Go's GC, scheduler,
  cross-compilation, and static linking for free.

The tiering pays rent repeatedly throughout this book. Backtraces are
captured in dev and skipped in release. Unused variables are a warning in dev and
an error in release and in `glide test`. Complete debug info is a
dev-tier guarantee and best-effort in release. Each of those is a case
where the right answer for the edit loop and the right answer for
production genuinely differ, and having two tiers means not having to
pick one.

---

### 3. Why This Design?

#### Why the toolchain is a language decision

The usual argument for good tooling is developer happiness. The real
argument is that **culture follows cost**.

Go's testing culture exists because `go test` needed no configuration,
no dependency, and no decision. Go's formatting arguments ended because
`gofmt` was not optional. Neither outcome came from a policy document;
both came from the friction being zero. Conversely, property-based
testing has been available for thirty years and never went mainstream
because the setup friction in most ecosystems is a dependency, a
generator DSL, and an afternoon. Glide ships it as `test "name"
(xs: List<Int>) { … }` and the friction is zero, which is the entire
bet.

So the toolchain is where behaviour gets decided, and shipping it late
means shipping the culture late — by which time the ecosystem has grown
its own, incompatible answers. Rust's error-handling libraries churned
through `error-chain` → `failure` → `anyhow`/`thiserror` for eight
years because the standard library shipped `Result` and no default
error type. Go's assertion story lost to testify because the standard
library shipped `t.Errorf` and hoped.

#### Why `glide test` is the enforcement boundary

Notice what is bundled into `glide test`: the format check, the lints,
the race detector, doc-link validation, and unused-code errors.

This is a specific answer to a specific tension. Go makes unused
variables a *compile* error, which is correct in spirit — a codebase
compiling with 400 warnings trains everyone to ignore number 401, the
real bug — but hostile in practice. Comment out one line to bisect a
problem and the compiler faults you through a cascade: unused variable,
then unused import, then restore everything. The `_ = x` incantation is
the tell that something is wrong: an error routinely silenced by a
magic no-op is a warning with extra steps. Zig copied strict-everywhere
and it became their most-resented decision.

Glide's answer is to put the hygiene boundary somewhere other than the
compiler:

- **Dev builds:** loud warning. The bisect loop never breaks.
- **Release builds and `glide test`:** error. Unused code cannot ship
  or land.

Same guarantee as Go, moved to a boundary that does not interrupt
thinking. The formatter follows the identical rule: format-on-save via
the LSP, `glide fmt -check` inside `glide test`, and **never a compile
error**, because code is legitimately misformatted mid-edit and a
compiler that yells about whitespace while you think is hostile.

#### Why zero configuration

There is no config file format for the formatter. Not "the defaults are
good" — the format does not exist.

rustfmt shipped `rustfmt.toml` and then reinstated, one knob at a time,
every argument the tool existed to end. `gofmt` only got halfway: it
canonicalises indentation and braces but preserves author line breaks,
so Go teams still argue about column width and where to split a long
call. Glide's formatter is specified as **a pure function from AST to
bytes** — same code, byte-identical output, everywhere, with
width-aware wrapping at a fixed column.

The cost is real and recorded: a width-aware formatter is a
constraint-solving pretty-printer in the Wadler tradition, a genuinely
harder program than `gofmt`. Dart rebuilt theirs this way and it is the
best-regarded formatter shipping.

There is one escape valve, and it is grammatical rather than
configurable: **a trailing comma forces one-element-per-line.** That
lets an author preserve structural intent — a matrix-shaped literal, a
routing table where each line is a route — through the grammar rather
than through whitespace the formatter would erase.

#### Why no build scripts, ever

`DESIGN.md` calls this "the hill".

Cargo's `build.rs` and npm's lifecycle scripts mean that compiling
someone else's code executes their program on your machine, with your
credentials, on your network. Most supply-chain attacks in both
ecosystems ride exactly that mechanism. Hermeticity dies with it too:
if a build can run arbitrary code, a build is not reproducible.

`glide build` executes no user code, reads nothing outside the module
tree and `vendor/`, and touches no network. Code generation is a step
you run yourself, visibly, with the output committed — Makefile
territory. The manifest (`glide.mod`) is *data*: module name, toolchain
pin, dependencies with hashes. No scripts, no hooks, no profiles, no
feature flags.

Go proved builds do not need any of it. This is one of the places where
Glide simply takes Go's answer and refuses to reopen the question.

---

### 4. Competing Approaches

**Go.** The model Glide copies. One binary containing build, test, fmt,
vet, doc, and module management; no external build system; hermetic
builds; trivial cross-compilation. Go's weaknesses that Glide targets:
`gofmt` stops halfway, `go doc` spent twelve years on plain text before
conceding Markdown in 1.19, the module proxy is central infrastructure,
and `cgo` collapses the cross-compilation story the moment you touch C.

**Rust.** Cargo is genuinely excellent at dependency management and
genuinely dangerous at build scripts. `rustfmt` and `clippy` are
separate components with separate configuration. The compile-time
story — proc macros are a second compiler, and `sqlx` validates against
a *running database* at compile time — trades hermeticity for
convenience in ways Glide explicitly declines. Rust's compile times are
the standing counterexample that keeps "compile speed is a feature" in
Glide's principles list.

**Zig.** `build.zig` is a build system written in Zig, which is elegant
and is also a build script by another name. Zig's cross-compilation is
the best in the industry — it ships a C compiler and libc for every
target — and is a genuine advantage Glide does not attempt to match,
because Glide exiled C FFI to keep cross-compilation trivial by
avoidance rather than by heroics.

**Python / Node.** The counterexample. `pip`, `poetry`, `uv`, `pyenv`,
`virtualenv`, `tox`, `black`, `ruff`, `mypy`, `pytest` — a dozen tools
with a dozen configuration files, each a decision every project must
make and re-litigate. The productivity cost of that fragmentation is
invisible because it is paid continuously.

**C / C++.** No toolchain in the language sense at all. CMake, Make,
Autotools, Bazel, Meson; three formatters; four static analysers; a
package manager per decade. Every project is a bespoke build
archaeology exercise. This is the world Go was reacting to, and the
reaction is worth repeating.

---

### 5. Common Mistakes

**Expecting a way to skip the checker.** There isn't one, and there
will not be one.

```glide
fn main() {
    let x: Int = "not an int"
    println(x)
}
```

```
error: line 2: expected Int, found String
```

This ran and printed `not an int` through M3, when annotations were
parsed and ignored. As of M4 every annotation is checked, in every
tier. If you have read older notes on the project — or an older
printing of this chapter — that describe Glide as a dynamically-typed
interpreter with decorative types, they describe a language that no
longer exists.

**Assuming `glide check` is weaker than `glide run`.** It was, through
M4c — three rules fired only at runtime. As of M4d it reports
everything the run would have reported before the run starts.

**Running a file with no `main`.** Library files parse fine but
`glide run` needs an entry point:

```
$ glide run examples/tree.gld
error: no main function
```

Use `glide test` for a library file with test blocks, which is exactly
what `tree.gld` is.

**Forgetting that arguments come after the filename.** `glide run
prog.gld a b c` passes `a b c` to the program. `os.args()` returns the
program name first, then those arguments — so a one-argument program
destructures `[_, path]`, matching a two-element list. Chapter 31
covers this properly.

**Assuming `glide test` is only tests.** In the designed toolchain it
is the enforcement boundary — formatting, lints, unused code, doc
links, and the race detector all run there. If a change passes locally
because you only ran `glide run`, it may still fail `glide test`. That
is intentional.

**Treating benchmark blocks as running code.** `bench "…" { … }` parses
and is skipped today, reported as `skip` in the test output. Do not
conclude your benchmark passed.

**Expecting the test cache to notice external files.** The repository's
own `Makefile` uses `go test -count=1` with a comment explaining why: a
doc-example test reads a Markdown file outside the module, and Go's
test cache does not track it, so a cached run would pass against a
stale document. If you add tooling that reads files outside the module
tree, remember this.

---

### 6. Performance Considerations

**Interpreter speed.** A tree-walker is roughly two orders of magnitude
slower than compiled Go on compute-bound work. The sieve example in the
repository computes primes below a million in a second or so. This is
fine: the interpreter's job is to prove semantics, and the dev tier for
real work will be a compiling backend.

Two specific costs are worth knowing because they are structural, not
incidental:

- **Generators cost a goroutine each.** `yield` is implemented as a
  send on a channel and `next()` as a receive, which is the cheapest
  *correct* lazy implementation for a tree-walker. `yield from`
  recursion therefore costs one goroutine per delegation level — a
  depth-20 tree traversal has 20 live goroutines. Irrelevant to the
  compiled tier, where generators lower to state machines, but
  measurable today.
- **One interpreter lock.** Compute-bound parallelism does not exist at
  this tier. IO-bound concurrency works properly, because the lock is
  released around blocking operations.

**Build speed.** `make build` on the interpreter is a couple of
seconds; a rebuild after a one-file change is well under one. That is
Go's compile speed, inherited.

**Binary size.** The stripped interpreter is a few megabytes, dominated
by the one third-party dependency: `modernc.org/sqlite`, a pure-Go
SQLite translation. It was chosen over `mattn/go-sqlite3` specifically
because it needs no CGO, so cross-compilation stays `GOOS=… go build`
and no C toolchain ever enters the build. The cost — a large transitive
module tree — was accepted knowingly. Everything else in the
interpreter is standard library.

**The designed compiler's targets.** Sub-second dev builds are a
non-negotiable principle rather than an aspiration; the release tier
aims for "faster than Go, usually competitive with Rust." Those numbers
do not exist yet and this book will not pretend otherwise.

---

### 7. Best Practices

**Keep `glide test` green, not just `glide run`.** It is the boundary
where hygiene is enforced, and treating it as the real gate — locally,
in your editor's task runner, in CI — is the habit the design is
built around.

**Let inference do the locals; annotate the boundaries.** Signatures
are not optional and never were — the parser requires a type on every
parameter and the checker holds the function to it:

```glide
fn process(items, limit) {          // error: expected ':' in parameter list
    ...
}
```

That mandatory boundary is what makes the checker cheap: it never has
to infer across a call, so a local `let x = …` can be left bare and
still be fully known. Annotate a local when the type is the point —
when it documents an intent the right-hand side does not make
obvious — not as insurance. Chapter 19 explains why the trade lands
this way.

Good:

```glide
fn process(items: List<Note>, limit: Int) -> Result<Int, Error> {
    ...
}
```

The unannotated form is not even a shortcut worth wanting: signatures
are the documentation *and* the thing the checker holds every caller
to.

**Let the formatter own the import block.** In the designed toolchain,
saving the file fixes the imports; humans do not curate them.
`goimports` proved the list is derivable from usage, and Go's
unused-import error enforces a list no human edits by hand.

**Do not build a wrapper script.** Every time you feel like adding a
shell script around `glide`, check whether the thing you want is one of
the closed command set. The closedness is load-bearing: the moment a
project grows `scripts/build.sh`, the hermeticity guarantee is
someone's honour system.

**Pin the toolchain.** The manifest pins the Glide version, and newer
toolchains build *as* the pinned one or refuse. Breaking changes being
free makes pinning more necessary, not less. This is Go's 1.21 lesson,
adopted from day one.

---

### 8. Examples

**A complete session, from clone to green tests:**

```bash
$ cd glide/glide
$ make build
go build -o bin/glide ./cmd/glide

$ cat > /tmp/greet.gld <<'EOF'
fn greet(name: String) -> String {
    "Hello, {name}!"
}

fn main() {
    println(greet("world"))
}

test "greet includes the name" {
    expect(greet("ada") == "Hello, ada!")
}
EOF

$ ./bin/glide run /tmp/greet.gld
Hello, world!

$ ./bin/glide test /tmp/greet.gld
ok    greet includes the name
```

Two things worth noticing. The `test` block sits in the same file as
the code — that is idiomatic for small invariants, and it reads as
documentation. And `expect` needs no assertion library: it is
compiler-known, so a failure reports both sides of the comparison
rather than "assertion failed."

**Watching an error message do its job:**

```bash
$ cat > /tmp/oops.gld <<'EOF'
fn main() {
    let xs = [1, 2, 3]
    println(xs[7])
}
EOF

$ ./bin/glide run /tmp/oops.gld
error: line 3: list index 7 out of range (len 3)
$ echo $?
1
```

Out-of-bounds indexing is a **panic**, not an error value — bug
territory, per the philosophy in Chapter 21. The exit code is 1, so
scripts notice.

**Cross-compiling the toolchain itself** (a preview of what
`glide build -target` will do for your programs):

```bash
$ make release
$ ls dist/
glide-darwin-arm64  glide-linux-amd64  glide-linux-arm64  glide-windows-amd64.exe
```

Four targets, one host, no sysroots, no C toolchain. This is the
property Glide intends to preserve for its own compiler, and it is the
direct reward for exiling C FFI to the margins.

---

### 9. Summary & Exercises

**Summary**

- The toolchain is one binary and is treated as part of the language,
  because culture follows cost: frictionless testing produces tested
  code, and frictionless formatting ends formatting arguments.
- Today that binary is a tree-walking interpreter with `glide run` and
  `glide test`. The designed command surface is closed at nine
  commands, with no plugin mechanism.
- Type annotations are checked, as of M4b — written in Go, reversing
  an earlier plan to defer the checker to a Glide-written frontend.
  Checking is mandatory in every tier and there is no way to skip it.
  **M4c** added generic bounds, trait conformance, `Ord`, boxed
  `Option`, sized numerics and match exhaustiveness. What is left is
  the checker staying quiet where it could speak — no remaining case
  computes the wrong value.
- `glide test` is the hygiene boundary: format check, lints, unused
  code, doc links, race detector. The compiler never errors on
  formatting or unused variables, because that breaks the edit loop.
- The formatter is a pure function from AST to bytes with no
  configuration file format, and canonical formatting is what makes
  mechanical migrations (`glide fix`) produce zero-noise diffs.
- No build scripts, ever. Builds execute no user code, read nothing
  outside the module tree, and touch no network.
- Two backends are designed — a fast dev backend and an optimising
  release backend — and the split is what lets overflow, backtraces,
  hygiene, and debug info each have the right answer at each tier.

**Exercises**

1. **Read an error message backwards.** Write a five-line program that
   triggers each of these: mutating a `let` binding, shadowing an
   enclosing name, indexing past the end of a list, and a `let … else`
   whose else-block falls through. For each, write down what the
   message tells you *and* what it would have to say to be useless.
   This is calibration for the rest of the book — you will meet most of
   these rules again as design decisions.

2. **Measure the tiers.** Run `examples/sieve.gld` and time it. Then
   write the equivalent Go program and time that. The ratio you get is
   the price of the current dev tier; keep the number in mind when
   Chapter 37 discusses the transpiler. (Do not draw conclusions about
   the *language* from it — you are measuring a tree-walker.)

3. **Design the tenth command.** The command surface is closed at nine.
   Pick a tool you rely on in your current ecosystem that is not on the
   list — a coverage reporter, a dependency-license auditor, a
   migration runner — and argue either that it belongs inside one of
   the nine (say which, and what its flag is called) or that it belongs
   outside the toolchain entirely. Note where your argument would
   reopen the arbitrary-code-execution door.
