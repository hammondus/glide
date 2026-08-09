# Chapter 38: Architecture and Project Structure

Most architecture advice is compensation for missing language features.
Repository patterns exist because ORMs leak. Dependency-injection
containers exist because constructors cannot fail and globals are the
alternative. Hexagonal architecture exists to keep an untyped boundary
from contaminating a domain model.

Glide deletes several of those pressures, so this chapter is shorter
than the equivalent chapter in a Java or Go book — and its central
claim is that **for most programs, the right architecture is almost
none**.

Most of this chapter is judgement rather than syntax, and the
package-manager pieces are ○.

---

### 1. Basic Usage

#### The three-section `main`

Almost every Glide program has this shape, and it is worth memorising:

```glide
fn main() -> Result<(), Error> {
    // 1. Dependencies — created once, visibly.
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    // 2. Wiring — closures inject dependencies into handlers.
    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    r.post(`/notes`, |req| create_note(db, req))
    let r = r

    // 3. Lifetime — the scope owns everything that runs.
    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve(addr, r)
    }
}
```

**That is the dependency-injection framework.** No container, no
registry, no annotations, no reflection. Section 1 constructs, section
2 wires with one-line closures, section 3 bounds lifetimes.

#### Small programs need no structure

```
tool/
  main.gld
  glide.mod
```

One module, one file, `main` at the top and helpers below in narrative
order (Chapter 3). A CLI of a few hundred lines does not need a
`cmd/` directory, an `internal/` directory, or a layer.

Files within a module share one namespace (Chapter 29), so splitting is
editorial:

```
tool/
  main.gld        # main, run, argument handling
  parse.gld       # the parser
  render.gld      # output formatting
  glide.mod
```

Splitting changes nothing about the API. It is a filing decision.

#### Modules are boundaries with contracts

A directory is a module. Reach for a second module when there is a
genuine **contract** — a set of `pub` items that other code depends on
and that you intend to keep stable:

```
service/
  main.gld              # module: service
  notes/                # module: notes
    store.gld
    types.gld
  billing/              # module: billing
    charge.gld
  glide.mod
```

The test: can you write down what the module promises without
describing its implementation? If not, it is not a boundary.

**Name modules after the domain, not the pattern.** `notes`,
`billing`, `auth` — not `services`, `helpers`, `utils`. A `utils`
module has no boundary, so it grows until it depends on everything.

#### Consumer-defined traits are the seam

When you need to substitute an implementation — for a test, for a
second backend — define the trait **where it is used**, not where the
type lives (Chapter 17):

```glide
// In the module that needs it
trait NoteStore {
    fn get(self, id: NoteId) -> Result<Note?, StoreError>
    fn put(mut self, n: Note) -> Result<(), StoreError>
}

// Also here — conformance for the dependency's type
impl NoteStore for SqlStore { … }
impl NoteStore for MemStore { … }
```

This is Go's "accept interfaces, return structs", and it survives
explicit conformance intact (Chapter 17). The pattern was always about
*who owns the abstraction*.

#### Layering, when it earns its place

The pressure that produces layers is real: you want the code that knows
about HTTP separated from the code that knows about SQL, so that
neither leaks into your domain logic.

Glide's version is usually **two** layers, not four:

```glide
// Transport: knows HTTP, knows nothing about storage
fn get_note(store: SqlStore, req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(n) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    match store.get(NoteId(n))? {
        Some(note) => Ok(http.json(note))
        None       => Ok(http.not_found())
    }
}

// Domain + storage: knows SQL, knows nothing about HTTP
impl SqlStore {
    fn get(self, id: NoteId) -> Result<Note?, StoreError> { … }
}
```

Two functions, one boundary, and the boundary is a *type* (`NoteId`,
`Note`, `StoreError`) rather than an interface.

#### Testing shapes the seams

```glide
// A real implementation, for tests
type MemStore = struct { notes: List<Note> }

impl NoteStore for MemStore {
    fn get(self, id: NoteId) -> Result<Note?, StoreError> { … }
    fn put(mut self, n: Note) -> Result<(), StoreError> { … }
}

test "get returns None for a missing note" {
    let store = MemStore{ notes: [] }
    expect(store.get(NoteId(1)) == Ok(None))
}
```

Because mandatory initialisation makes real values cheap to construct
and sum types make states explicit, a real in-memory implementation is
usually less work than a mock — and it is honest.

#### The designed project layout ○

```
myservice/
  glide.mod          # module name, toolchain pin, deps with hashes
  vendor/            # the dependencies, committed
  main.gld
  notes/
  billing/
```

`glide.mod` is **data, not a program** — no scripts, hooks, profiles,
or feature flags. `vendor/` is committed and is what actually builds.
There is no `build.gld`, ever.

---

### 2. Under the Hood

#### Why the wiring closure works

```glide
r.get(`/notes/{id}`, |req| get_note(db, req))
```

The handler `get_note(db: Db, req: Request)` is an ordinary function
you can call from a test. The closure adapts it to the router's
expected shape and captures `db`.

That is the whole mechanism, and it costs one line per route. In Java
it is a container, annotations, and a lifecycle; in Go it is either a
struct with methods or the same closure.

#### Why there is no framework-shaped pressure

Four language decisions remove it:

**No globals** (Chapter 29) — so dependencies must be passed, so they
must be visible in signatures, so the graph is explicit.

**Constructors can fail** — `sql.open` returns a `Result`, so there is
no "construct then initialise" two-phase problem that DI containers
exist to sequence.

**No reflection** (Chapter 34) — so a container *cannot* autowire, and
the honest alternative is the only alternative.

**Scopes own lifetimes** (Chapter 25) — so "when is this shut down" has
a structural answer rather than a lifecycle interface.

#### Why the module namespace matters for structure

A directory is a module and files share one namespace, which means the
file system is **not** the dependency structure. That decouples two
things most languages conflate:

- **Files** organise for a *reader* — "where would I look for this?"
- **Modules** organise for a *contract* — "what does this promise?"

In Python and JavaScript, splitting a file changes the import graph. In
Rust, the module tree must be maintained alongside the file tree. Here,
you can reorganise files freely and only module boundaries are API.

---

### 3. Why This Design?

#### Why "almost none" is the right amount of architecture

Because architecture is inventory. Every layer, interface, and
indirection is something a reader must traverse and a change must
propagate through, and it earns its place only by absorbing variation
that actually happens.

`DESIGN.md`'s posture throughout is **add it when a program needs it**
— the dogfood rule (Chapter 30) applied to the standard library, and it
applies to your architecture too.

The specific claim: for a service with one database, one transport, and
one deployment target, `main` plus a handful of modules is not a
compromise. It is correct, and adding layers makes it worse.

#### Why repository patterns mostly disappear

A repository exists to hide an ORM's leakage — lazy loading, identity
maps, session lifecycle, N+1 queries that look like field access.

Glide has no ORM, permanently (Chapter 33). `db.query` returns rows;
`derive Row` (○) maps them to a type. There is nothing to hide, so the
"repository" is a module with functions that run SQL — which is what
you wanted, without the pattern name.

What survives is the *seam*: `trait NoteStore` when you genuinely need
to substitute. That is one trait with two implementations, not a
repository interface per entity.

#### Why hexagonal architecture is mostly type-shaped here

The valuable insight in hexagonal/clean architecture is real:
**the boundary should not contaminate the core.** Untyped JSON should
not reach your domain logic; HTTP concepts should not appear in your
business rules.

Glide achieves most of it with types rather than layers
(Chapters 12, 31):

- **Maps at the boundary, structs inside.** JSON becomes a `Note` in
  three lines, and the map never travels.
- **Parse, don't validate.** An `Email` that exists is valid, so
  validation does not spread inward.
- **`distinct` types.** A `NoteId` cannot be confused with a raw `Int`
  from a query.

What is left of hexagonal architecture is one honest boundary — the
transport function and the domain function — and a trait if you need
substitution.

The ports-and-adapters *ceremony* (an interface per port, a DTO per
boundary, a mapper per DTO) exists because the languages it was
designed for cannot express the boundary in the type system.

#### Why "when structure becomes ceremony"

The test `DESIGN.md` implies and this chapter states:

> **A layer earns its place if it absorbs variation that actually
> happens.**

A `NoteService` between `NoteController` and `NoteRepository` earns its
place if there are two controllers, or two repositories, or business
logic that belongs to neither. If it is a pass-through that calls one
method on one repository, it is inventory.

The honest version of the question: *what would break if I deleted this
layer?* If the answer is "nothing, I would just call the next one
directly", delete it.

#### Why vendoring is an architectural decision

`DESIGN.md`: **a dependency is a liability to justify.**

Vendoring makes that concrete — a new dependency puts its source in
your repository, in your diffs, and in review. A one-line manifest
change becomes thousands of added lines, which is an honest
representation of what you just took on.

That friction is deliberate, and it shapes architecture: a codebase
that vendors reaches for the standard library first, which is exactly
why the standard library is batteries-included.

---

### 4. Competing Approaches

**Go.** `cmd/`, `internal/`, `pkg/`, and a long-running community
argument about whether `pkg/` is useful (consensus: no).
`internal/` is a real language feature and is Glide's two-level
visibility, roughly. Go's architecture culture is notably lighter than
Java's, and Glide's is lighter still because sum types and `Option`
absorb more of the variation.

**Java / Spring.** Layers, DI containers, annotations, and
reflection-driven autowiring. Every piece of that is a response to a
constraint Glide removed — no globals means explicit passing, no
reflection means no autowiring, failing constructors mean no two-phase
initialisation.

**Rust.** Crates, modules, and a preference for concrete types with
traits introduced when needed — very close to Glide's posture. Rust's
architecture discussions are dominated by ownership questions that do
not arise here.

**Elixir/Phoenix.** Contexts as domain boundaries, explicitly designed
to resist the layer-cake reflex. Phoenix's "contexts" documentation is
the best mainstream writing on when a boundary earns its place.

**Node.** Historically almost no structure, then a wholesale import of
Java patterns (NestJS) — the same overcorrection, twenty years later.

**Clean/Hexagonal architecture as a movement.** The insight is right
and the ceremony is language-specific. Read it for the boundary
argument; do not import the ports-and-adapters file layout.

---

### 5. Common Mistakes

**Building the four-layer cake.**

```
NoteController → NoteService → NoteRepository → NoteEntity
```

Each layer a thin pass-through, each interface with one implementation,
existing for a DI framework that does not exist here.

**Creating a module per type.** A module is a boundary with a contract,
not a namespace for one struct.

**Creating `utils`.** A module with no boundary grows until it depends
on everything and everything depends on it. Name modules after
domains.

**Introducing a trait before the second implementation.** A trait with
one implementation is a type with extra steps (Chapter 17). Introduce
the seam when a test or a second backend actually needs it — and make
the trait the shape the *test* needs, which is usually much smaller
than the real type's API.

**Mocking what you could construct.** Mandatory initialisation makes
real values cheap. A test with a real `MemStore` is more honest than
one with a mock and a verification script.

**Threading a dependency through layers that do not use it.** If
`render(db, page)` passes `db` to something three calls down and
touches it nowhere, the layering is wrong.

**Splitting files for architectural reasons.** Files within a module
share a namespace. Split them for readers, not for structure.

**Reaching for a DI container.** There is no reflection, so it cannot
autowire, so it would be a hand-maintained registry — which is
strictly worse than the three-section `main`.

**Structuring for a future that has not arrived.** Two databases, three
transports, plugin architecture. Add the abstraction when the second
case exists; the language makes that refactor cheap because signatures
are explicit and `glide fix` handles mechanical rewrites.

---

### 6. Performance Considerations

Architecture's performance cost is mostly **indirection**, and Glide
makes it visible:

**A trait used through a generic bound is free** (monomorphised,
Chapter 18). A trait used through `any Trait` (○) boxes and dispatches
dynamically. Layers built on trait objects pay per call, and the `any`
keyword is where you see it.

**Each layer is a function call.** In the compiled tier, thin
pass-throughs inline. In the interpreter they do not, which is one more
reason not to benchmark architecture on the tree-walker.

**Explicit dependency passing costs nothing** — a pointer per argument,
usually in a register.

**Module boundaries cost nothing at runtime.** They are a compile-time
visibility concept.

**Monomorphisation interacts with boundaries** (○): a generic used
across modules is instantiated per concrete type in the *using* module,
so binary size grows with usage. Keep generic shells small and delegate
the bulk to a non-generic inner function.

**Vendoring costs disk and repository size** — the price of the audit
trail.

---

### 7. Best Practices

**Start with one module and one file.** Split when a reader would
thank you; add a module when there is a contract.

**Use the three-section `main`.** Dependencies, wiring, lifetime — in
that order, every time. It is recognisable, and section three is what
makes shutdown correct with no shutdown code.

**Make the boundary a type, not a layer.**

```glide
// Good — the boundary is Note/NoteId/StoreError
fn get_note(store: SqlStore, req: Request) -> Result<Response, ApiError>
impl SqlStore { fn get(self, id: NoteId) -> Result<Note?, StoreError> }
```

The transport function knows HTTP and `Note`. The store knows SQL and
`Note`. Neither knows the other's world, and no interface was needed to
achieve that.

**Define traits where they are consumed, and keep them small.** One to
three methods. A small trait is easy to implement for real in a test,
which is what makes the seam useful.

**Let the type system carry the invariants inward.** Parse at the
boundary, and everything inside gets a value that is already valid.
That is what replaces most of the validation layer.

**Apply the deletion test.** For every layer, interface, and
indirection: *what would break if I deleted this?* If the answer is
"nothing, I would call the next thing directly", delete it.

**Justify every dependency.** Vendoring makes it visible; treat the
visibility as the point.

**Refactor toward structure, not into it.** Explicit signatures and
canonical formatting make mechanical refactors cheap, so the cost of
adding a boundary *later* is low — much lower than the cost of
maintaining one you did not need.

---

### 8. Examples

**A complete small service, structured as little as possible:**

```glide
// notes.gld — the whole service, one module, one file.
import http
import sql
import time

// --- Types: the boundary between transport and storage ---

type NoteId = distinct Int

type Note = struct {
    pub id: NoteId
    pub title: String
    pub body: String?
}

type StoreError = Backend{ cause: Error }
type ApiError = BadInput{ msg: String } | Store{ cause: StoreError }

impl StoreError { fn from(e: Error) -> StoreError { Backend{ cause: e } } }
impl ApiError   { fn from(e: StoreError) -> ApiError { Store{ cause: e } } }

// --- Storage: knows SQL, knows nothing about HTTP ---

fn load(db: Db, id: NoteId) -> Result<Note?, StoreError> {
    let found = db.query_one(
        "select id, title, body from notes where id = :id",
        ["id": id],
    )?
    match found {
        None      => Ok(None)
        Some(row) => Ok(Some(Note{
            id:    NoteId(row["id"] ?? 0),
            title: row["title"] ?? "",
            body:  row["body"],
        }))
    }
}

// --- Transport: knows HTTP, knows nothing about SQL ---

fn get_note(db: Db, req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Err(.BadInput{ msg: "missing id" })
    }
    let Some(n) = raw.parse_int() else {
        return Err(.BadInput{ msg: "bad id" })
    }
    match load(db, NoteId(n))? {
        Some(note) => Ok(http.json(note))
        None       => Ok(http.not_found())
    }
}

// --- Background work ---

fn sweeper(db: Db) {
    for {
        time.sleep(10.mins)
        _ = db.exec("delete from notes where title = ''") ?? 0
    }
}

// --- main: dependencies, wiring, lifetime ---

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite:notes.db")?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    let r = r

    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

Sixty lines, one file, and it has: a typed boundary, separated
transport and storage, error types that convert across the boundary
with `?`, background work bounded by the server's lifetime, and no
shutdown code.

Count what is absent: no interface, no repository, no service layer, no
DTO, no mapper, no container, no `internal/` directory, and no
`AbstractNoteServiceFactory`.

**The same service, when it grows.** Two things force structure: a
second implementation, and a second team.

```
service/
  main.gld              # module: service — dependencies, wiring, lifetime
  notes/
    types.gld           # Note, NoteId, StoreError
    store.gld           # trait NoteStore, impl for SqlStore and MemStore
    handlers.gld        # transport
  billing/
    …
  glide.mod
  vendor/
```

And the seam appears only now, because only now is there a second
implementation:

```glide
// notes/store.gld
pub trait NoteStore {
    fn get(self, id: NoteId) -> Result<Note?, StoreError>
    fn put(mut self, n: Note) -> Result<(), StoreError>

    // Added later. Breaks nobody; SqlStore can override with a
    // single query.
    fn count(self) -> Result<Int, StoreError> { … }
}

pub type SqlStore = struct { db: Db }
impl NoteStore for SqlStore { … }

pub type MemStore = struct { notes: List<Note> }
impl NoteStore for MemStore { … }
```

Note `count` with a default (Chapter 17) — the trait can grow without
breaking implementors, which is the property Go's interfaces lack and
the reason a trait here is a safer commitment than a Go interface.

**Bad versus good: the layer cake**

```glide
// Bad — four layers, each a pass-through, three interfaces with
// one implementation each
trait NoteRepository { fn find_by_id(self, id: Int) -> Note? }
trait NoteService    { fn get_note(self, id: Int) -> Note? }
trait NoteMapper     { fn to_dto(self, n: Note) -> NoteDto }

type NoteRepositoryImpl = struct { db: Db }
type NoteServiceImpl    = struct { repo: NoteRepositoryImpl }
type NoteControllerImpl = struct { svc: NoteServiceImpl, mapper: NoteMapperImpl }

impl NoteService for NoteServiceImpl {
    fn get_note(self, id: Int) -> Note? {
        self.repo.find_by_id(id)        // that is the entire layer
    }
}
```

`NoteServiceImpl.get_note` calls one method and returns the result.
Apply the deletion test: what breaks if it goes? Nothing.

```glide
// Good
fn get_note(db: Db, req: Request) -> Result<Response, ApiError> { … }
fn load(db: Db, id: NoteId) -> Result<Note?, StoreError> { … }
```

Two functions, one boundary, typed. When a second store arrives, the
trait appears — and it appears with exactly the methods the second
store needs, which is usually far fewer than the speculative version
would have had.

---

### 9. Summary & Exercises

**Summary**

- **Most architecture advice is compensation for missing language
  features.** Repositories hide ORM leakage; DI containers work around
  globals and failing constructors; ports-and-adapters ceremony
  expresses a boundary the type system could not. Glide removes those
  pressures.
- **The three-section `main` is the DI framework**: dependencies
  created once, wiring by one-line closures, lifetime owned by a scope.
  No container, no annotations, no reflection.
- **Small programs need no structure.** One module, one file, `main` at
  the top. Files within a module share a namespace, so splitting is
  editorial and changes no API.
- **A module is a boundary with a contract.** The test: can you write
  down what it promises without describing how? Name modules after
  domains, never `utils`.
- **Make the boundary a type, not a layer.** `NoteId`, `Note`,
  `StoreError` separate transport from storage without an interface.
- **Consumer-defined traits are the seam** — defined where used, one to
  three methods, introduced when a second implementation or a test
  substitution actually exists. Default methods (Chapter 17) mean a
  trait can grow later, which makes it a safer commitment than a Go
  interface.
- **What survives of hexagonal architecture** is the boundary
  insight — and Glide gets most of it from *maps at the boundary,
  structs inside* plus *parse, don't validate* plus `distinct` types.
- **The deletion test:** for every layer, ask what would break if you
  removed it. "Nothing, I would call the next thing directly" means
  delete it.
- **A layer earns its place if it absorbs variation that actually
  happens** — two transports, two stores, logic belonging to neither.
- **A dependency is a liability to justify**, and vendoring makes the
  liability visible in review.
- Refactor *toward* structure. Explicit signatures and canonical
  formatting make adding a boundary later cheap; maintaining one you
  did not need is not.

**Exercises**

1. **Apply the deletion test.** Take a service you maintain and, for
   every layer and interface, write down what would break if it were
   removed. Be honest about "nothing, I would call the next thing
   directly". Then count how many of the survivors exist because of a
   language limitation Glide does not have.

2. **Rebuild a service in one file.** Take a small service — five or
   six endpoints, one database — and write it as a single Glide module,
   with types as the only boundary. Then identify the first place it
   genuinely wants a module split, and note what triggered it. In most
   cases it is a second team rather than a second implementation, which
   is worth knowing about your own architecture pressures.

3. **Design a trait from its test.** Pick a dependency you currently
   mock. Write the trait containing only what the *test* needs — not
   what the real implementation offers. Compare its size with the real
   type's API. The gap is how much interface you would have written
   speculatively, and it is usually large.
