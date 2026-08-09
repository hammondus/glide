# Chapter 29: Modules, Imports, and Visibility

Glide's module system is Go's, kept almost unchanged, with one
substitution: **`pub` instead of capitalisation**.

A directory is a module. Imports are qualified and **inert**. There are
exactly two visibility levels. And nothing runs before `main` —
no `init()`, no module-level `let`, no import side effects.

That last property is load-bearing and is the most interesting part of
the chapter.

The single-file interpreter means much of this is ○ today — there is no
package manager and no multi-file resolution yet — but `pub`, imports,
`const`, and the no-life-before-`main` guarantee all run.

---

### 1. Basic Usage

#### A directory is a module

All `.gld` files in a directory share **one namespace**. There are no
intra-module imports, no per-file declarations, and no `mod.rs` tree.
Split a module across files however reads best; the files are not a
structure, they are a filing decision.

This is Go's model exactly, and Rust's module tree (`mod.rs`,
`pub(super)`, path attributes) is declined as ceremony that buys almost
nothing.

#### Imports

```glide
import http                          // stdlib: a bare name
import "github.com/x/y" as y         // external: a URL, aliased  ○
```

At the top of the file, one per line. Modules are used **qualified**:

```glide
import os
import fs

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: prog <file>")
        os.exit(2)
    }
    let text = fs.read_string(path)?
    println(text)
    Ok(())
}
```

There is no `from x import y`, no wildcard import, and no way to bring
an individual name into scope unqualified.

#### Imports execute nothing

This is the guarantee, and it is absolute. Importing a module runs no
code. There is no registration magic, no `import _ "lib/pq"` running
hidden driver setup, and no "what happens on import" question to ask.

A module you import is inert until you call it.

#### `pub` is the visibility system

```glide
pub type Note = struct {
    pub id: NoteId
    pub title: String
    body: String              // private to the module
}

pub fn create(title: String) -> Note { … }

fn validate(t: String) -> Bool { … }      // private to the module
```

Two levels, and that is all:

| | Visible where |
|---|---|
| default | inside the module |
| `pub` | outside the module too |

Struct fields take `pub` **individually**, so a public type with
private fields is the natural encapsulation shape (Chapter 12).

There is no `pub(crate)`, no `internal`, no friend classes. Wanting a
third level usually means the module boundary is drawn wrong.

#### Module level is `const` only

```glide
const max_retries = 3
const default_timeout = 30.s
```

Module level holds **functions, types, `impl` blocks, and `const`
bindings**. There is no module-level `let`, no mutable global, and no
`init()`.

`const` is evaluated once at load, in declaration order, and is
immutable. In the designed language it is **comptime-evaluated**
(Chapter 34), so it can hold anything a compile-time computation can
produce:

```glide
const table = make_crc_table()                  // ○
const re = regex.compile("^[a-z]+$")            // ○ — a bad pattern is
                                                //   a compile error
```

**M2 shim:** today, `const` initialisers are restricted to pure
expressions — no function or module calls. Full comptime evaluation
arrives with the compiler.

Const names are `snake_case`, like any binding. There is no
SCREAMING_CASE convention.

#### Runtime state lives in `main`

Since there are no mutable globals, the pattern is: create in `main`,
pass down.

```glide
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))

    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

`db` is created once, at the top, and threaded into the handlers by
closures. That is the design's grain everywhere.

#### Declarations are order-independent

Module-level declarations are a **set**. Call a function declared
below; reference a type declared later. File order is *narrative*
order, and the formatter deliberately does not reorder declarations
(Chapter 3).

#### Unused imports

Dev builds warn; release builds and `glide test` error (Chapter 2). And
in the designed toolchain the **formatter owns the import block** —
save the file and the imports are correct. Humans do not curate
imports.

#### Shadowing an import

A local named after an imported module is **legal**:

```glide
import sql

fn build(sql: String) -> String {     // fine — no module use here
    "prepared: {sql}"
}
```

What is an error (○, checker era) is using the module *through* a live
shadow — binding `sql` and calling `sql.open()` in the same function
produces a diagnostic naming both parties.

---

### 2. Under the Hood

#### Load order

The interpreter collects all module-level declarations first —
functions, types, `impl` blocks — then evaluates `const` initialisers
in declaration order, then calls `main`.

That is the entire startup sequence. There is no dependency graph to
walk, no initialisation order to reason about, and no way for one
module's setup to depend on another's having run.

#### What `const` will be ○

A `const` binding is evaluated at **comptime**: the same functions that
run at runtime, executed by the compiler, with three discipline rules
(Chapter 34): no IO, fuel-limited evaluation, deterministic by
construction.

The results land in **read-only data** — shared across all uses, zero
startup cost, immutable by memory protection.

`const re = regex.compile("…")` is the example worth holding onto: a
bad pattern becomes a *compile error*, and the compiled automaton ships
in rodata. Compare Go's `regexp.MustCompile` in a `var`, which is a
runtime panic during `init()`.

#### The designed package model ○

- **Imports are URLs; there is no central registry.**
  `import "github.com/…"` resolves via git. Registries are
  infrastructure to run and a supply-chain single point of failure;
  decentralised imports plus lockfile hashes give integrity without a
  middleman. (Go added a proxy a decade in; that is a later scaling
  concern.)
- **Vendoring by default.** The manifest names dependencies,
  `vendor/` contains them, and builds read only from `vendor/`. No
  network at build time, and the vendored tree is the auditable
  artifact — what you read is what links.
- **`glide.mod` is data, not a program.** Module name, toolchain pin,
  dependencies with hashes. No scripts, hooks, profiles, or feature
  flags.
- **Toolchain pinning** from day one: the manifest pins the Glide
  version, and newer toolchains build *as* the pinned one or refuse.

#### The `x/` porch ○

A designed middle tier: `glide.x/smtp`, `glide.x/mail` — first-party
code with the same authorship, review bar, tooling, and `glide doc`,
sitting **outside** the toolchain distribution and its stability
promise, versioned independently, allowed to churn.

The reasoning is a specific Go post-mortem. `net/smtp` rotted *inside*
the standard library, frozen at 2011's assumptions. Its pain was never
the four-verb protocol; it was the living periphery — XOAUTH2, TLS
posture changes, provider quirks. **Location does not preserve
batteries; maintenance cadence does.**

---

### 3. Why This Design?

#### Why `pub` instead of Go's capitalisation

Go's capitalisation trick works because nothing else competes for the
case distinction. Here, **pattern matching needs it**:

```glide
match shape {
    Circle(r) => …
    point     => …
}
```

Capitalised tests, lowercase binds (Chapter 3). Go never hit this
conflict because Go has no pattern matching. The case axis is spent, so
visibility needs a keyword.

And the keyword turns out to be better anyway, for a reason unrelated
to patterns: **a visibility change is a one-line diff that says "this
became public."** Reviewable. With capitalisation it is a
whole-codebase rename, and the diff shows a hundred call sites with the
actual decision buried among them.

The accepted cost: no exported-ness signal at use sites. Small in
practice — cross-module calls are already qualified (`http.get`), and
within a module everything is accessible anyway.

#### Why exactly two levels

Because `pub(crate)`, `internal`, `protected`, and friend declarations
are all answers to "this module is too big" that let you avoid fixing
it.

If you want something visible to *some* outside code but not all,
either the boundary is in the wrong place or you have two modules that
should be one. `DESIGN.md`: wanting a third level usually means the
module boundary is drawn wrong.

Field-level `pub` covers the case that actually matters — a public type
with private internals — without a level.

#### Why imports are inert

This is the decision with the longest causal chain in the language, and
it is worth tracing.

**Go's `const` can only hold scalars.** So anything structured —
a lookup table, a compiled regex, a map — has to be a `var`. A `var`
is a mutable global built at runtime. Building it needs somewhere to
run, which is `init()`. And once `init()` exists, importing a package
runs code, which enables `import _ "github.com/lib/pq"` — importing a
package purely so its `init()` registers a driver in a global.

All of it is downstream pressure from one limitation.

Glide's `const` is comptime-evaluated and can hold anything, so the
pressure never arises. With that in place:

- Module level can be `const` only.
- Imports can be inert.
- **There is no initialisation-order fiasco** — no C++ static-init
  order problem, no Go init graph.
- `main` is line one of runtime.

The cost is real and is the design's grain: runtime state is created in
`main` and passed down. A database handle, a logger, a clock. Nothing
materialises behind your back.

For the rare genuinely-lazy global, the designed answer is a stdlib
`Lazy<T>`, chosen by name — the same doctrine as `BigInt` and
`StringBuilder`.

#### Why a directory is a module

Go got this right. The alternatives:

**Rust's module tree** — `mod` declarations, `mod.rs` or `foo/mod.rs`,
`pub(super)`, `use` paths, `#[path]` attributes. The file system and
the module tree are separate structures you must keep in sync, and the
sync is manual.

**Java's package-per-directory with a declaration in every file** —
the same information in two places.

**Python's per-file modules** — so splitting a file changes the API,
which means files are load-bearing and you cannot reorganise freely.

Directory-is-a-module means splitting a module across files is a purely
editorial decision with no API consequence. That is a genuine freedom
and it is why Go files are often organised by "what a reader wants to
find" rather than by dependency.

#### Why no central registry

npm, PyPI, crates.io, and RubyGems are all infrastructure someone must
run and a single point of failure for the entire ecosystem's integrity.
Every one of them has had a supply-chain incident.

URL imports plus lockfile hashes plus vendoring give integrity without
a middleman: the dependency is a git repository you can read, the hash
is in the manifest, and the vendored tree is what actually compiles.

#### Why vendoring by default

`DESIGN.md`: **a dependency is a liability to justify.** Vendoring
makes the liability visible — the code is in your tree, in your diffs,
readable in review, and a new dependency shows up as thousands of added
lines rather than one manifest line.

It also means no network at build time, which is a hermeticity
requirement.

---

### 4. Competing Approaches

**Go.** Directory-is-a-package, qualified imports, capitalisation for
visibility, `init()` and import-for-side-effect, module proxy and
checksum database. Glide keeps the first two, replaces the third,
removes the fourth, and defers the fifth.

**Rust.** Crates and a module tree, `pub`/`pub(crate)`/`pub(super)`,
`use` for unqualified imports, crates.io as a central registry,
`build.rs`. Glide takes `pub` and the orphan rule; it declines the
module tree, the visibility zoo, `use`, the registry, and build
scripts.

**Python.** File-is-a-module, packages via `__init__.py`, imports that
execute the module body, `from x import *`, no visibility at all
(a leading underscore is a convention). Executing imports is the source
of Python's startup-order bugs and of import cycles that are runtime
errors.

**Java.** Package declarations in every file, four visibility levels
(`public`, `protected`, package-private, `private`), Maven Central,
static initialisers with an order that surprises people. Java's
`static { }` blocks are `init()` with the same problems.

**JavaScript.** ES modules with named and default exports, `import`
executing the module, npm with lifecycle scripts (the supply-chain
door), and a resolution algorithm complex enough to need its own
specification.

**Zig.** `@import` returning a struct, `pub` for visibility, no
registry (URLs plus hashes in `build.zig.zon`), and `build.zig` as a
build script. Very close to Glide on imports and visibility; the build
script is the divergence.

---

### 5. Common Mistakes

**Looking for a way to import a name unqualified.** There is none.
`http.get(url)`, always. This is deliberate: a qualified call tells you
where the function came from without scrolling to the imports.

**Reaching for a module-level `let`.**

```glide
// Bad — does not exist
let db = sql.open(dsn)

// Good — create in main, pass down
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    …
}
```

The absence is the feature. A mutable global is a thing that can change
under any caller, and a global initialised at load is a thing that runs
before `main`.

**Expecting `init()`.** There is none, permanently. If you want
something to happen at startup, it goes in `main` — where a reader can
find it.

**Expecting an import to register something.** Imports are inert.
Driver registration, plugin discovery, and `metrics.MustRegister` all
have to be explicit calls.

**Putting a function call in a `const` today.**

```glide
// ○ — will work when comptime lands
const table = make_table()

// ✓ today
const max_retries = 3
```

**Making everything `pub` reflexively.** The default is module-private
for a reason, and a non-`pub` declaration is one you can change freely.

**Splitting a module because a file got long.** Files within a module
share one namespace, so splitting is free and changes nothing. Split
when a *reader* would find it easier, not when a compiler demands it.

**Assuming module boundaries are cheap to move later.** They are the
one thing `pub` makes visible in a diff, and moving a boundary is the
one refactor `glide fix` cannot do for you.

---

### 6. Performance Considerations

**Startup is nothing.** No init graph to walk, no static constructors,
no module bodies to execute. A Glide binary's startup cost is the
runtime's own initialisation.

This is a real difference from Go at scale: a Go program with a deep
dependency tree can spend milliseconds in `init()` before reaching
`main`, and that cost is invisible and hard to attribute.

**`const` costs nothing at runtime** (○). Comptime evaluation happens
at build time, and the result lands in read-only data — shared, zero
startup cost, immutable by memory protection. A `const` lookup table is
strictly better than Go's `var` equivalent, which is built at startup
in every process.

**Comptime evaluation costs build time**, bounded by the fuel limit.

**Qualified calls cost nothing.** `http.get` resolves statically.

**Vendoring costs disk and repository size**, which is the price of the
audit trail.

**Monomorphisation interacts with module boundaries** (○): a generic
function used across modules is instantiated per concrete type in the
using module, so binary size grows with usage rather than with
definition.

---

### 7. Best Practices

**Create dependencies in `main` and pass them down.**

```glide
// Good
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    r.post(`/notes`, |req| create_note(db, req))

    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

Every dependency is visible in one place, the handlers are ordinary
testable functions, and there is no global to reach for in a test.

The closures adapting `|req| get_note(db, req)` are the idiomatic
dependency-injection shape, and they cost one line each.

**Keep the public surface small.** A module's `pub` items are its
contract. Everything else you can change without breaking anyone.

```glide
// Good
pub type Store = struct { db: Db }          // opaque: no pub fields

pub fn open(dsn: String) -> Result<Store, Error>
pub fn get(s: Store, id: NoteId) -> Result<Note?, Error>

fn build_query(id: NoteId) -> String        // private helper
```

**Name modules after the domain, not the pattern.** `notes`, `billing`,
`auth` — not `services`, `helpers`, `utils`. A `utils` module is a
module with no boundary, and it grows until it depends on everything.

**Put the important declaration first.** File order is narrative order,
and the formatter will not touch it.

**Let the formatter own imports** (○). Do not hand-curate the list.

**Use `const` for things that are genuinely constant** — and note that
in the designed language that includes compiled regexes, lookup tables,
and embedded data, not just scalars.

**Do not create a module per type.** A module is a boundary with a
contract, not a namespace for one struct. If a module's public surface
is one type and its methods, it probably belongs with its neighbours.

**Treat a new dependency as a decision to justify.** Vendoring makes
this concrete: adding one puts its source in your repository and your
diffs. That friction is deliberate.

---

### 8. Examples

**The dependency-injection shape, complete:**

```glide
import http
import sql
import time

type Store = struct { db: Db }

fn get_note(store: Store, req: Request) -> Result<Response, Error> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(id) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let found = store.db.query_one(
        "select id, title from notes where id = :id",
        ["id": id],
    )?
    match found {
        Some(row) => Ok(http.json(row))
        None      => Ok(http.not_found())
    }
}

fn sweeper(store: Store) {
    for {
        time.sleep(10.mins)
        _ = store.db.exec("delete from stale") ?? 0
    }
}

fn main() -> Result<(), Error> {
    // Every dependency is created here, once, visibly.
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    let store = Store{ db: db }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(store, req))

    scope s {
        _ = s.spawn(|| sweeper(store))
        http.serve("127.0.0.1:8080", r)
    }
}
```

Notice what is *not* here: no global `db`, no `init()` opening a
connection pool, no service locator, no dependency-injection framework,
and no `sync.Once`. The handler functions take their dependencies as
parameters, which makes them ordinary functions you can call from a
test with a test store.

**Visibility doing real work:**

```glide
// --- module `ids` ---

pub type NoteId = distinct Int

impl NoteId {
    // The only way to make one from untrusted input.
    pub fn parse(raw: String) -> NoteId? {
        let Some(n) = raw.parse_int() else { return None }
        if n <= 0 { return None }
        Some(NoteId(n))
    }

    // Note: `.value()` is the builtin unwrap for a distinct type,
    // so do NOT define a method called `value` — it would shadow the
    // builtin and `self.value()` inside it would recurse forever.
    pub fn render(self) -> String { "note-{self.value()}" }

    // Private: used by the module's own query builder.
    fn as_param(self) -> Int { self.value() }
}

pub fn next_id(current: NoteId) -> NoteId { … }

fn checksum(id: NoteId) -> Int { … }      // private helper
```

Outside the module you can `parse`, `render`, and unwrap with the
builtin `.value()`. You cannot call `as_param` or `checksum`, and —
because those are not `pub` — the module's author can rename or delete
them without breaking anyone.

**Bad versus good: the global**

```glide
// Bad — does not compile; there is no module-level `let`
let db = sql.open("sqlite:app.db")

fn get_note(id: Int) -> Result<Note, Error> {
    db.query_one(…)        // reaches for a global
}
```

Even setting aside that it does not parse, the shape is the problem:
`get_note` has a hidden dependency that no signature mentions, cannot
be tested without a real database, and cannot be used with a second
database.

```glide
// Good
fn get_note(db: Db, id: Int) -> Result<Note, Error> {
    db.query_one(…)
}
```

The dependency is in the signature. A test passes an in-memory
database. A migration tool passes a different one. Nothing is hidden.

This is the same argument as Chapter 26's against `ctx` parameters,
running in the *opposite* direction — and the difference is
instructive. Cancellation is **observation-adjacent**: it does not
change what a function computes, only whether it finishes, so it can be
ambient. A database handle is **behaviour-affecting**: which database
you query changes the answer, so it travels in a parameter.

`DESIGN.md` calls this **the droppability razor**: could program output
change if this ambient value were dropped? No → observation, ambient is
fine. Yes → parameter.

The closed set of things allowed to be ambient is exactly three:
cancellation and deadlines (scopes), observation (log fields, trace
context), and the clock. Everything else — auth principal, tenant,
transaction, request config — travels visibly.

---

### 9. Summary & Exercises

**Summary**

- **A directory is a module.** All `.gld` files in it share one
  namespace; splitting across files is editorial and has no API
  consequence. No `mod` declarations, no module tree.
- **Imports are qualified and inert.** `import http`, then
  `http.get(…)`. Importing executes nothing — no `init()`, no
  registration magic, no import-for-side-effect.
- **`pub` is the visibility system, and there are exactly two levels.**
  Module-private by default, `pub` outside. Fields take `pub`
  individually. No `pub(crate)` zoo.
- `pub` exists rather than Go's capitalisation because **pattern
  matching needs the case axis** — and it turns out to be better
  anyway, because a visibility change becomes a reviewable one-line
  diff.
- **Module level is `const` only.** No module-level `let`, no mutable
  globals, no `init()`, **no life before `main`**. The whole chain
  traces back to Go's scalar-only `const` forcing structured data into
  `var`, which forced `init()`, which enabled import-for-side-effect.
- `const` is comptime-evaluated (○), lands in read-only data, and can
  hold compiled regexes and lookup tables. M2 shim: pure expressions
  only.
- **Runtime state is created in `main` and passed down.** That is the
  design's grain, and it is why handlers take `db` as a parameter.
- **The droppability razor** decides ambient versus parameter: could
  output change if the value were dropped? No → ambient (cancellation,
  logging, clock — a closed set of three). Yes → parameter.
- ○: URL imports with no central registry, vendoring by default,
  `glide.mod` as data, toolchain pinning, and the `x/` porch for
  first-party code that must churn at the speed of the outside world.

**Exercises**

1. **Trace an `init()`.** In a Go codebase, find an `init()` function
   or an `import _`. Work out what it does and what would break if it
   ran at a different time. Then write the Glide version — which means
   deciding where in `main` it goes and who holds the resulting state.
   In most cases the answer is clearer than the original.

2. **Apply the razor.** List everything your current service carries
   implicitly — through globals, thread-locals, `ctx.Value`, or a DI
   container. For each, ask: could program *output* change if it were
   silently dropped? Sort them into ambient and parameter. Anything in
   the ambient column that is not cancellation, logging, or the clock
   is something the design says should be a parameter — check whether
   you agree.

3. **Draw a module boundary.** Take a package you maintain and list its
   exported symbols. For each, ask whether an external caller genuinely
   needs it or whether it is exported because something else in the
   same repository needed it. The second category is what `pub(crate)`
   exists to serve, and `DESIGN.md` claims it means the boundary is
   wrong — see whether the claim survives contact with your codebase.
