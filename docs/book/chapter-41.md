# Chapter 41: Case Study — A Complete Service

This chapter builds one program from requirements to running code: a
URL shortener with a database, an HTTP API, typed errors, background
capability, and tests. About 160 lines.

Everything here **runs** — the final program was executed against the
interpreter and its output pasted back in, including the tests.

The value is not the program. It is watching the decisions from
thirty-eight chapters get made in sequence, and seeing which of them
turn out to matter.

---

### 1. Basic Usage: Requirements

**The service:**

- `POST /links` with `{"code": "gl", "url": "https://example.com"}`
  creates a short link.
- `GET /links/{code}` returns the link and increments a hit counter.
- Codes are 1–16 characters, lowercase, no slashes.
- URLs must be absolute.
- A duplicate code is rejected.
- A missing code is a 404.

**Non-functional:**

- SQLite storage.
- Errors distinguishable by the caller, mapped to HTTP statuses in one
  place.
- Tested, including a property.
- No shutdown code.

---

### 2. Under the Hood: The Design, Decision by Decision

#### Step 1 — types before functions

The first question is not "what functions do I need" but **what states
exist**.

```glide
type Code = distinct String
type LinkId = distinct Int

type Link = struct {
    pub id: LinkId
    pub code: Code
    pub url: String
    pub hits: Int
}
```

Two `distinct` types (Chapter 15) for ten characters each. `Code` and
`LinkId` are both wrappers over primitives, and neither can be passed
where the other belongs — nor where a bare `String` or `Int` belongs.

`Link` has no optional fields, because every field is always present.
Mandatory initialisation (Chapter 12) means a `Link` that exists is
complete.

#### Step 2 — enumerate the failures

```glide
type StoreError = Backend{ cause: Error } | Duplicate{ code: Code }

type ApiError =
    BadInput{ msg: String }
    | NotFound{ code: Code }
    | Store{ cause: StoreError }

impl StoreError { fn from(e: Error) -> StoreError { Backend{ cause: e } } }
impl ApiError   { fn from(e: StoreError) -> ApiError { Store{ cause: e } } }
```

**Two error types, one per layer** (Chapter 20). The storage layer's
failures are `Backend` and `Duplicate`; the transport layer's add
`BadInput` and `NotFound` and *wrap* the storage layer's.

The two `from` implementations are what make `?` work across the
boundary. A `db.exec` returning `Result<Int, Error>` becomes a
`StoreError` automatically inside a storage function; a `StoreError`
becomes an `ApiError` automatically inside a handler. Three error types
in play, zero `.map_err` calls.

#### Step 3 — parse, don't validate

```glide
impl Code {
    pub fn parse(raw: String) -> Code? {
        let s = raw.trim().to_lower()
        if s == "" || s.len() > 16 || s.contains("/") { return None }
        Some(Code(s))
    }
}
```

The rules live here, once (Chapter 12). Every function downstream takes
a `Code` and does no validation — because it cannot receive an invalid
one.

`parse` returns `Code?` rather than a `Result` because the failure has
no information in it beyond "no" (Chapter 14's test: does the absent
case carry information?).

#### Step 4 — storage, knowing nothing about HTTP

```glide
fn lookup(db: Db, code: Code) -> Result<Link?, StoreError> {
    let found = db.query_one(
        "select id, code, url, hits from links where code = :code",
        ["code": code])?
    match found {
        None    => Ok(None)
        Some(r) => Ok(Some(Link{
            id:   LinkId(r["id"] ?? 0),
            code: Code(r["code"] ?? ""),
            url:  r["url"] ?? "",
            hits: r["hits"] ?? 0,
        }))
    }
}
```

Three chapters visible in nine lines. **Named parameters** (`:code`,
Chapter 35), with the `Code` binding by unwrapping. **`query_one`
returning `Option`** rather than a sentinel — "no row" is an ordinary
outcome. And **maps at the boundary, structs inside** (Chapter 11): the
row map lives for four lines and becomes a `Link`.

Note the return type: `Result<Link?, StoreError>`. Two different
"nothing" cases, distinguished — the query might *fail*, or it might
*succeed and find nothing*. In Go that is `(*Link, error)` with the
nil-and-nil case meaning "not found", which is a convention rather than
a type.

`insert` is where a driver error becomes a typed one:

```glide
fn insert(db: Db, code: Code, url: String) -> Result<LinkId, StoreError> {
    match db.exec("insert into links (code, url) values (:code, :url)",
                  ["code": code, "url": url]) {
        Ok(_)  => {}
        Err(e) => {
            if "{e}".contains("UNIQUE") {
                return Err(.Duplicate{ code: code })
            }
            return Err(.Backend{ cause: e })
        }
    }
    let row = db.query_one("select id from links where code = :code",
                           ["code": code])?
    match row {
        Some(r) => Ok(LinkId(r["id"] ?? 0))
        None    => Err(.Backend{ cause: "inserted row not found" })
    }
}
```

The `UNIQUE` constraint on `code` is the duplicate check — atomic in
the database, where an application-level check-then-insert is a race
under concurrency. The price is that the driver reports the violation
as message text (Chapter 35), so the translation into `.Duplicate` is
textual — and it happens *here*, at the storage boundary, so no caller
ever parses a driver string. Well-factored Go makes the same move with
`sqlite3.ErrConstraintUnique`; drivers are where stringly errors meet
typed ones, in any language.

#### Step 5 — transport, knowing nothing about SQL

```glide
fn create(db: Db, req: Request) -> Result<Response, ApiError> {
    let Ok(v) = json.decode(req.body()) else {
        return Err(.BadInput{ msg: "invalid JSON" })
    }
    let Some(code) = Code.parse(v["code"] ?? "") else {
        return Err(.BadInput{ msg: "code must be 1-16 chars, no slashes" })
    }
    let url = v["url"] ?? ""
    if !url.starts_with("http") {
        return Err(.BadInput{ msg: "url must be absolute" })
    }
    _ = insert(db, code, url)?
    Ok(http.created())
}
```

Three guard clauses, each `let … else` or `if`, each returning early —
and **zero nesting** (Chapter 9). The happy path is the last two lines.

The `?` on `insert` converts `StoreError` into `ApiError` via `from`.
Nothing in this function mentions the conversion — and nothing here
mentions duplicates, because the database's `UNIQUE` constraint rejects
them inside `insert`, which transport sees only as another
`StoreError`.

#### Step 6 — one place maps errors to statuses

```glide
fn to_response(r: Result<Response, ApiError>) -> Response {
    match r {
        Ok(resp)              => resp
        Err(BadInput{ msg })  => http.bad_request(msg)
        Err(NotFound{ code }) => http.not_found()
        Err(Store{ cause })   => {
            match cause {
                Duplicate{ code } => http.bad_request("code already taken")
                Backend{ .. }     => http.text("internal error")
            }
        }
    }
}
```

This is the error middleware (Chapter 34), hand-written because
`fn(Handler) -> Handler` is ○.

The important property: **both `match`es are exhaustive**. Add a
variant to `ApiError` or `StoreError` and this function stops
compiling, forcing you to choose a status. Compare Go's `errors.Is`
ladder, which silently falls through to 500.

#### Step 7 — wiring and lifetime

```glide
let mut r = http.router()
r.post(`/links`, |req| to_response(create(db, req)))
r.get(`/links/{code}`, |req| to_response(resolve(db, req)))
let r = r

scope s {
    _ = s.spawn(|| http.serve("127.0.0.1:17699", r))
    …
}
```

Raw strings for route patterns (Chapter 6). One-line closures injecting
`db` and applying the error mapping (Chapter 30). `let r = r` seals the
router after registration (Chapter 4). The scope owns the server's
lifetime (Chapter 26).

#### Step 8 — tests

```glide
test "Code.parse rejects junk" {
    expect(Code.parse("") == None)
    expect(Code.parse("a/b") == None)
    expect(Code.parse("  Hi  ") == Some(Code("hi")))
}

test "Code.parse is idempotent" (s: String) {
    match Code.parse(s) {
        None    => expect(true)
        Some(c) => expect(Code.parse(c.value()) == Some(c))
    }
}
```

An example test for the shape, a property for the contract
(Chapter 23). Idempotence is one of the four property shapes, and it is
the right one here: `parse` normalises, so parsing a normalised value
must be a fixed point.

---

### 3. Why This Design?

#### Why types came first

Because every later decision followed from them.

`Code` being `distinct` meant `lookup(db, code)` could not accidentally
be called with a URL. `Link` having no optional fields meant no
downstream function checks for absence. `StoreError` and `ApiError`
being separate sum types meant the `?` conversions were three lines
total and the status mapping was exhaustive.

Had the types been `String`, `Int`, and `Error`, none of that would
have been available, and every function would have needed defensive
checks.

#### Why two error types rather than one

Because they answer different questions.

`StoreError` says what can go wrong *in storage* — the backend failed,
or the code is taken. A caller inside the service can handle those
differently.

`ApiError` says what can go wrong *in a request* — including everything
storage can do, wrapped.

One combined type would leak SQL concerns into the transport layer's
vocabulary. Separate types with a `from` conversion cost three lines
and keep the layers honest — and that conversion is exactly what
Chapter 20's `?`-conversion machinery exists for.

#### Why `Result<Link?, StoreError>` rather than an error for not-found

Chapter 14's test: **does the absent case carry information?**

A missing link carries none — the code just is not there. So it is an
`Option`. A failed query carries plenty — connection lost, syntax
error, permissions — so it is a `Result`.

Nesting them is precise: `Result<Link?, StoreError>` says "this might
fail, and if it succeeds it might find nothing", which is three
outcomes, distinguished.

#### Where the interpreter's rough edge showed

One, recorded in an earlier chapter. It is worth seeing it in a real
program rather than in a minimal repro. (An earlier edition had a
second: a driver-level SQL failure escaped as a cancellation panic, so
`create` carried a racy check-then-insert workaround. That bug is
fixed, the workaround is gone, and the `UNIQUE` constraint now does
the checking — Appendix D keeps the record.)

**The scope body ends with `return`, not a tail expression.** Chapter
27's sharpest edge: `http.serve` blocks forever, and a *normal* scope
exit joins children without cancelling them. Falling off the end would
deadlock. `return Ok(out.join("\n"))` is an early exit, which cancels
the server first.

That is rule 1 from Chapter 26 doing exactly what it says, and it is
the kind of thing you learn once.

#### What did not need writing

Worth listing, because absence is the point:

- No shutdown handler, stop channel, or `done` flag.
- No `context.Context` in any signature.
- No null checks anywhere.
- No `sql.NullString` or equivalent.
- No struct tags.
- No `if err != nil` — eleven `?` operators instead.
- No DI container, repository interface, or service layer.
- No assertion library.
- No mocks.

---

### 4. Competing Approaches

The same service in other languages, by rough line count and by what
the extra lines *do*:

**Go** — roughly 250 lines. The additions: `if err != nil` blocks
(about 40 lines), `context.Context` threading, graceful-shutdown
ceremony (about 15 lines), `sql.NullString` handling or pointer fields,
struct tags, and either sentinel errors plus `errors.Is` or a custom
error type per case. Go's version is genuinely readable; it is just
longer, and the error-to-status mapping has no exhaustiveness check.

**Rust (axum + sqlx)** — comparable length, with `async fn` throughout
and `.await` at every IO point. `thiserror` for the error types,
`serde` for JSON, and `sqlx::query_as!` giving *stronger* guarantees
than Glide's (compile-time schema validation) at the cost of a database
during the build.

**Python (FastAPI)** — shortest of the four, with Pydantic doing the
parsing and validation that `Code.parse` does here. No compile-time
guarantees; the error-to-status mapping is exception handlers, and
nothing checks it is complete.

**Java (Spring Boot)** — longest by a distance, and most of the extra
is annotations, a repository interface, an entity, a DTO, and a mapper.

The interesting comparison is not length. It is **which mistakes each
version permits**: a Go version can forget a `return` after
`http.Error`; a Python version can add an error case with no handler; a
Java version can pass the wrong `Long` for the wrong entity. The Glide
version makes all three unwritable, and the cost is the type
declarations at the top.

---

### 5. Common Mistakes (Found While Writing This)

These are real, from building the program.

**Forgetting `return` at the end of a scope body containing a
server.** Deadlock, and the failure is loud but the cause is not
obvious the first time.

**Reaching for `Err` on a client error.** `Err(.NotFound{ … })` from a
handler maps to 500 (Chapter 34's one default mapping) unless something
converts it. Here `to_response` does the conversion, which is why the
handlers *can* return `Err` — without it they would need
`Ok(http.not_found())`.

**Assuming a database constraint violation is catchable.** It is not,
at this tier.

**Assuming `Some(x)` and `x` are interchangeable.** They are not:
`Option` is boxed (Chapter 14), so `Some(None)` differs from `None` and
`m[k]` returning `Some(None)` means *present, holding nothing*. Code
written against the old unboxed behaviour — where `Some` was the
identity — reads a present-but-empty entry as absent.

**Putting the status mapping in each handler.** The first draft did.
Extracting `to_response` made the exhaustiveness check work in one
place instead of three, which is the whole benefit.

---

### 6. Performance Considerations

Nothing here is tuned, and the interesting costs are structural rather
than measured:

**`insert` is two round trips** — the insert, then a select for the
generated id. SQLite's `returning` clause would make it one
(`db.query_one("insert … returning id", …)` works today); two plain
statements read more conventionally, which a teaching chapter values
over a round trip.

**`bump` is a second query per read.** A real service would batch hit
counts or write them asynchronously; here it is a query per request.
The point is that it is *visible* — there is no ORM doing it behind a
property access.

**Row maps allocate per query** (Chapter 35). `derive Row` (○) would
write directly into a `Link`.

**`http.json(link)` walks the struct structurally** (Chapter 33).
`derive Json` (○) would generate the encoder.

**One green thread per request**, a few kilobytes each.

**The interpreter is the bottleneck by two orders of magnitude.** The
network path is Go's `net/http` and the database is real SQLite; the
handler bodies are tree-walked.

---

### 7. Best Practices Demonstrated

The chapter checklist, as applied:

| Practice | Where | Chapter |
|---|---|---|
| Types before functions | `Code`, `LinkId`, `Link` | 12–15 |
| `distinct` for identifiers | `Code`, `LinkId` | 15 |
| Parse, don't validate | `Code.parse` | 12 |
| Option when absence carries no information | `Code?`, `Link?` | 14 |
| Enumerate failures per layer | `StoreError`, `ApiError` | 19 |
| `from` for cross-layer `?` | two impls | 19 |
| Maps at the boundary, structs inside | `lookup` | 11, 31, 33 |
| Guard clauses, no nesting | `create`, `resolve` | 9 |
| Exhaustive error-to-status mapping | `to_response` | 10, 32 |
| Raw strings for routes and JSON | `` `/links/{code}` `` | 6 |
| Named SQL parameters | `:code` | 33 |
| Seal after construction | `let r = r` | 4 |
| Dependencies in `main`, passed down | `run` | 29 |
| Scope owns the lifetime | `scope s { … }` | 25 |
| `defer` on the line after acquisition | `db.close()` | 21 |
| Example test plus property test | both `test` blocks | 22 |
| Translate driver errors at the boundary | `insert`'s `UNIQUE` match | 19 |

Zero `mut` bindings except the router and the report list — both
genuine accumulators, both sealed or returned immediately.

---

### 8. The Complete Program

```glide-run
// links.gld — a URL shortener.
import http
import json
import sql
import time

// ---------------------------------------------------------------
// Types: the boundary between transport and storage.
// ---------------------------------------------------------------

type Code = distinct String
type LinkId = distinct Int

type Link = struct {
    pub id: LinkId
    pub code: Code
    pub url: String
    pub hits: Int
}

type StoreError = Backend{ cause: Error } | Duplicate{ code: Code }

type ApiError =
    BadInput{ msg: String }
    | NotFound{ code: Code }
    | Store{ cause: StoreError }

impl StoreError { fn from(e: Error) -> StoreError { Backend{ cause: e } } }
impl ApiError   { fn from(e: StoreError) -> ApiError { Store{ cause: e } } }

impl Code {
    pub fn parse(raw: String) -> Code? {
        let s = raw.trim().to_lower()
        if s == "" || s.len() > 16 || s.contains("/") { return None }
        Some(Code(s))
    }
}

// ---------------------------------------------------------------
// Storage: knows SQL, knows nothing about HTTP.
// ---------------------------------------------------------------

fn schema(db: Db) -> Result<(), StoreError> {
    _ = db.exec(`create table links (
        id integer primary key,
        code text not null unique,
        url text not null,
        hits integer not null default 0
    )`)?
    Ok(())
}

fn insert(db: Db, code: Code, url: String) -> Result<LinkId, StoreError> {
    match db.exec("insert into links (code, url) values (:code, :url)",
                  ["code": code, "url": url]) {
        Ok(_)  => {}
        Err(e) => {
            // The UNIQUE constraint is the duplicate check. Translate the
            // driver's error into the typed variant here, at the storage
            // boundary, so no caller ever parses message text.
            if "{e}".contains("UNIQUE") {
                return Err(.Duplicate{ code: code })
            }
            return Err(.Backend{ cause: e })
        }
    }
    let row = db.query_one("select id from links where code = :code",
                           ["code": code])?
    match row {
        Some(r) => Ok(LinkId(r["id"] ?? 0))
        None    => Err(.Backend{ cause: "inserted row not found" })
    }
}

fn lookup(db: Db, code: Code) -> Result<Link?, StoreError> {
    let found = db.query_one(
        "select id, code, url, hits from links where code = :code",
        ["code": code])?
    match found {
        None    => Ok(None)
        Some(r) => Ok(Some(Link{
            id:   LinkId(r["id"] ?? 0),
            code: Code(r["code"] ?? ""),
            url:  r["url"] ?? "",
            hits: r["hits"] ?? 0,
        }))
    }
}

fn bump(db: Db, code: Code) -> Result<(), StoreError> {
    _ = db.exec("update links set hits = hits + 1 where code = :code",
                ["code": code])?
    Ok(())
}

// ---------------------------------------------------------------
// Transport: knows HTTP, knows nothing about SQL.
// ---------------------------------------------------------------

fn create(db: Db, req: Request) -> Result<Response, ApiError> {
    let Ok(v) = json.decode(req.body()) else {
        return Err(.BadInput{ msg: "invalid JSON" })
    }
    let Some(code) = Code.parse(v["code"] ?? "") else {
        return Err(.BadInput{ msg: "code must be 1-16 chars, no slashes" })
    }
    let url = v["url"] ?? ""
    if !url.starts_with("http") {
        return Err(.BadInput{ msg: "url must be absolute" })
    }
    _ = insert(db, code, url)?
    Ok(http.created())
}

fn resolve(db: Db, req: Request) -> Result<Response, ApiError> {
    let Some(code) = Code.parse(req.path_param("code") ?? "") else {
        return Err(.BadInput{ msg: "bad code" })
    }
    let Some(link) = lookup(db, code)? else {
        return Err(.NotFound{ code: code })
    }
    bump(db, code)?
    Ok(http.json(link))
}

// The error middleware, hand-written. Both matches are exhaustive,
// so a new error variant breaks this function and forces a status.
fn to_response(r: Result<Response, ApiError>) -> Response {
    match r {
        Ok(resp)              => resp
        Err(BadInput{ msg })  => http.bad_request(msg)
        Err(NotFound{ code }) => http.not_found()
        Err(Store{ cause })   => {
            match cause {
                Duplicate{ code } => http.bad_request("code already taken")
                Backend{ .. }     => http.text("internal error")
            }
        }
    }
}

// ---------------------------------------------------------------
// Tests.
// ---------------------------------------------------------------

test "Code.parse rejects junk" {
    expect(Code.parse("") == None)
    expect(Code.parse("a/b") == None)
    expect(Code.parse("  Hi  ") == Some(Code("hi")))
}

test "Code.parse is idempotent" (s: String) {
    match Code.parse(s) {
        None    => expect(true)
        Some(c) => expect(Code.parse(c.value()) == Some(c))
    }
}

// ---------------------------------------------------------------
// main: dependencies, wiring, lifetime. The program serves, drives
// its own API over real HTTP, and returns — which cancels the server.
// ---------------------------------------------------------------

fn run() -> Result<String, Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    schema(db)?

    let mut r = http.router()
    r.post(`/links`, |req| to_response(create(db, req)))
    r.get(`/links/{code}`, |req| to_response(resolve(db, req)))
    let r = r

    scope s {
        _ = s.spawn(|| http.serve("127.0.0.1:17699", r))
        time.sleep(80.ms)

        let mut out: List<String> = []
        out.push(post(`{"code": "gl", "url": "https://example.com"}`))
        out.push(post(`{"code": "gl", "url": "https://dupe.com"}`))
        out.push(post(`{"code": "", "url": "https://x.com"}`))
        out.push(post(`{"code": "ok", "url": "notaurl"}`))
        out.push(get("gl"))
        out.push(get("gl"))
        out.push(get("nope"))
        return Ok(out.join("\n"))
    }
}

fn post(body: String) -> String {
    match http.post("http://127.0.0.1:17699/links", body) {
        Ok(resp) => "POST -> {resp.status()} {resp.body().trim()}"
        Err(e)   => "POST failed: {e}"
    }
}

fn get(code: String) -> String {
    match http.get("http://127.0.0.1:17699/links/" + code) {
        Ok(resp) => "GET {code:5} -> {resp.status()} {resp.body().trim()}"
        Err(e)   => "GET failed: {e}"
    }
}

fn main() {
    match run() {
        Ok(report) => println(report)
        Err(e)     => eprintln("failed: {e}")
    }
}
```

**Running it:**

```
$ glide run links.gld
POST -> 201 created
POST -> 400 code already taken
POST -> 400 code must be 1-16 chars, no slashes
POST -> 400 url must be absolute
GET    gl -> 200 {"id":1,"code":"gl","url":"https://example.com","hits":0}
GET    gl -> 200 {"id":1,"code":"gl","url":"https://example.com","hits":1}
GET  nope -> 404 not found
```

```
$ glide test links.gld
ok    Code.parse rejects junk
ok    Code.parse is idempotent  (100 cases)
```

Seven requests, five distinct outcomes, and the hit counter
incrementing between the two `GET`s. The program starts a server,
exercises it over real HTTP, and shuts everything down by returning.

---

### 9. Summary & Exercises

**Summary**

- The whole service is **one module, one file, about 160 lines**, and
  it needs no framework, no DI container, no repository layer, and no
  shutdown code.
- **Types came first, and every later decision followed.** Two
  `distinct` types for twenty characters; a struct with no optional
  fields; two error sum types with `from` conversions between them.
- **Two error types, one per layer.** `StoreError` for storage,
  `ApiError` wrapping it for transport, and `?` bridging them with zero
  `.map_err` calls.
- **`Result<Link?, StoreError>` is precise**: the query might fail, and
  if it succeeds it might find nothing. Three outcomes, distinguished
  by the type rather than by a nil-and-nil convention.
- **`Code.parse` is the only validation in the program.** Everything
  downstream takes a `Code` and cannot receive an invalid one.
- **`to_response` is the error middleware**, hand-written because
  `fn(Handler) -> Handler` is ○ — and both its matches are exhaustive,
  so a new error variant forces a status choice at compile time.
- **The scope owns the server's lifetime**, and the body ends with
  `return` because a normal exit would join the blocked server forever
  (Chapter 26's rule 1, Chapter 28's sharpest edge).
- **One interpreter rough edge showed up in real code**: the
  scope-exit rule required an early `return`. The `UNIQUE` constraint
  handles duplicates, with the driver's error translated to a typed
  `.Duplicate` at the storage boundary.
- What did not need writing: null checks, `sql.NullString`, struct
  tags, `if err != nil`, `context.Context`, a shutdown handler, an
  assertion library, or a single mock.

**Exercises**

1. **Add a feature and count the compile errors.** Add a `Expired{ at:
   Instant }` variant to `ApiError`. Before running anything, predict
   which functions break. Then check. The list should be exactly one —
   `to_response` — and that is the exhaustiveness guarantee delivering
   the work list that Chapter 13 promised.

2. **Break the boundary.** Delete the `UNIQUE` translation in `insert`
   (return `.Backend` for every driver error) and observe what a
   duplicate now sends the client: "internal error" instead of
   400 "code already taken". Put it back. The layer that knows a
   failure's *meaning* decides what the user sees — that knowledge
   lives in storage, not transport.

3. **Port it.** Write the same service in a language you know well.
   Count the lines, and — more usefully — count the places where a
   mistake would compile: a forgotten status mapping, a transposed ID,
   a missing null check, a leaked goroutine, an unhandled error case.
   That count, not the line count, is what this book has been arguing
   about for thirty-nine chapters.
