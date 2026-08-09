# Appendix A: Command Reference

The `glide` command surface is **closed** — ten commands, and
`DESIGN.md` commits to not growing the list. There is no plugin system
and no `glide-*` subcommand discovery: Cargo's extension mechanism
reopens the arbitrary-code-execution door that banning build scripts
closed.

---

## Today

Three subcommands exist. All three **type-check the program first**;
there is no way to skip it (Chapter 19).

### `glide run <file.gld> [args...]`

Parse, check, evaluate, and call `main`. Arguments after the filename
reach `os.args()` (with the filename itself at index 0).

```bash
$ glide run hello.gld
$ glide run wordfreq.gld notes.txt
$ glide run examples/pipeline.gld
```

Exit codes: `main` returning `()`/`Ok(())` → 0; `Err(e)` → 1 with the
error on stderr; `os.exit(n)` → n; a panic → 1.

Errors with no `main`:

```
error: no main function
```

### `glide test <file.gld>`

Run `test` blocks. Plain blocks once; property blocks (those with
parameters) 100 times with generated inputs, a fixed per-test seed, and
greedy shrinking on failure. `bench` blocks parse and are skipped.

```bash
$ glide test examples/tree.gld
ok    in-order traversal is sorted  (100 cases)
skip  bench "insert 10k" (benchmarks not implemented yet)
```

Exit code reflects failures.

### `glide check <file.gld>`

Parse and type-check; report every diagnostic; change nothing; execute
nothing. Non-zero exit if anything was found.

```bash
$ glide check app.gld
app.gld:6:19: expected String, found Int
 6 |     println(greet(42))
   |                   ^^
$ echo $?
1
```

This is `go vet`-shaped: a **convenience**, never a way to skip
checking. `glide run` and `glide test` perform the identical check
before doing anything else, and no flag disables it.

One difference worth knowing: the tail-value rule is enforced by the
evaluator rather than the checker, so `glide check` does not report it
(Appendix D, *Known checker gaps*).

---

## Building the interpreter

```bash
cd glide/glide
make build      # go build -o bin/glide ./cmd/glide
make test       # go vet ./... && go test -count=1 ./...
make release    # cross-compiled stripped binaries for four targets
make clean      # rm -rf bin/ dist/
```

`make release` produces:

```
dist/glide-darwin-arm64
dist/glide-linux-arm64
dist/glide-linux-amd64
dist/glide-windows-amd64.exe
```

`CGO_ENABLED=0` on every target, so the outputs are genuinely static
and no C toolchain is needed. `-ldflags "-s -w"` strips symbol tables.

The `test` target uses `-count=1` deliberately: a doc-example test
reads a Markdown file outside the module, and Go's test cache does not
track it, so a cached run would pass against a stale document.

---

## The designed surface ○

| Command | Purpose |
|---|---|
| `glide build` | Compile to a native binary |
| `glide run` | Build (or interpret) and execute |
| `glide test` | Tests **and** format check, lints, race detector, doc-link validation, examples |
| `glide fmt` | Canonical formatter |
| `glide vet` | Advisory lints |
| `glide doc` | Terminal lookup and a local HTML docs server |
| `glide fix` | Mechanical rewrites for breaking language changes |
| `glide get` / `glide tidy` | Dependency management |
| `glide version` | Toolchain version |

### `glide test` is the enforcement boundary

The most important design point in the toolchain. What runs there:

- Test and property-test execution
- `glide fmt -check`
- Unused-variable and unused-import **errors**
- Doc-link validation (a stale `[Identifier]` fails the build)
- The race detector
- Example functions, output-checked

**None of these is a compile error.** Dev builds warn; `glide test` and
release builds error. The reasoning (Chapter 2): a codebase compiling
with 400 warnings trains everyone to ignore number 401, so hygiene must
be an error *somewhere* — but a compiler that faults you mid-bisect is
hostile, and Go's comment-out-a-line-and-get-a-cascade experience is
the failure mode being avoided.

### `glide build -target`

```bash
glide build -target linux/arm64       # ○
```

Any host to any target, no sysroots, static by default,
`FROM scratch`-ready. This stays trivial *because* C FFI was exiled to
the margins — cgo is what collapses Go's cross-compilation story.

### `glide fmt`

A **pure function from AST to bytes**. Same code, byte-identical
output, everywhere. Width-aware wrapping at a fixed column (~100),
Prettier/Black/dart-format style rather than gofmt style.

**There is no configuration file format.** Not "the defaults are good"
— the format does not exist. rustfmt shipped `rustfmt.toml` and then
reinstated, one knob at a time, every argument the tool existed to end.

One grammatical escape valve: **a trailing comma forces
one-element-per-line**, so an author can preserve structural intent
(a matrix-shaped literal, a routing table) through the grammar rather
than through whitespace the formatter would erase.

The formatter also owns the import block — save the file and the
imports are correct. Humans do not curate imports.

### `glide fix`

Mechanical rewrites for breaking language changes. This is why
canonical formatting matters beyond aesthetics: when a formatter is a
pure function from AST to bytes, an automated rewrite produces a
**zero-noise diff**, so a breaking change costs one command rather than
a migration project.

`DESIGN.md`: "canonical formatting is migration infrastructure."

### `glide doc`

Terminal lookup plus a local HTML server, in the one binary. Markdown
subset from day one (headings, lists, fences, links) — Go spent twelve
years on plain text before conceding in 1.19.

Checked identifier links: `[Config]` resolves against real
declarations, and a stale link fails at the test tier.

---

## The manifest ○

`glide.mod` is **data, not a program**:

```
module github.com/craig/myservice
glide 0.4.0

require (
    github.com/x/y v1.2.0 h1:abc123…
)
```

Module name, toolchain pin, dependencies with hashes. **No scripts,
hooks, profiles, or feature flags.** Cargo features produce 2ⁿ build
variants, most never compiled by anyone.

**Vendoring by default:** the manifest names dependencies, `vendor/`
contains them, and builds read only from `vendor/`. No network at build
time, and the vendored tree is the auditable artifact.

**Toolchain pinning** from day one (Go's 1.21 lesson): the manifest
pins the version, and newer toolchains build *as* the pinned one or
refuse. Breaking changes being free makes pinning more necessary, not
less.

**Conditional compilation** is platform-suffix files
(`net_linux.gld` — whole-file, greppable, no preprocessor) plus
comptime constants (`if target.os == .linux`) for small forks.

---

## What will never be a command

**Build scripts.** `glide build` executes no user code, reads nothing
outside the module tree and `vendor/`, and touches no network. Cargo's
`build.rs` and npm's lifecycle scripts mean compiling someone's code
runs their program on your machine — most supply-chain attacks in both
ecosystems ride exactly that. Code generation is a step you run,
visibly, with the output committed.

**A plugin system.** The closed surface is load-bearing.

**A package registry.** Imports are URLs resolved via git; integrity
comes from lockfile hashes and vendoring. A read-through proxy is a
later scaling concern (Go added theirs a decade in).

---

## Script mode ✓

```glide
#!/usr/bin/env -S glide run
fn main() {
    println("hello from a script")
}
```

```bash
$ chmod +x tool.gld
$ ./tool.gld
hello from a script
```

The `#!` line is skipped by the lexer, but only as the first two bytes
of a file — `#` is not a comment character anywhere else. It is
skipped rather than stripped, so line numbers in diagnostics match what
your editor shows.

**Use `env -S`.** Linux hands `execve` everything after the interpreter
path as a single argument, so bare `#!/usr/bin/env glide run` searches
for a binary named `glide run`. macOS splits the line, so the bare form
works there and fails on deployment — `-S` splits explicitly on both.

`glide run tool.gld` is the same program, and takes the same arguments.
Because the dev tier is an interpreter, type-checked scripting is
nearly free.

A **REPL** is likely an interpreter byproduct rather than a
commitment — REPL semantics in a statically typed language is real
design work.

---

## Editor setup ○

One shipped LSP server, inside the same binary: format-on-save,
go-to-definition, inline diagnostics, and a test list, with zero
configuration.

Nothing exists today. Syntax highlighting for `.gld` files can be faked
by telling your editor to treat them as Rust — you get keywords,
strings, and comments approximately right, and `match` arms wrong.

A **DAP server** (Debug Adapter Protocol) in the interpreter is
designed and cheap: a tree-walker is a debugger that has not been
asked — breakpoints are a per-statement check, stepping is eval-loop
flags, inspection is the environment already held. Weeks, not months,
and every editor gets it free.
