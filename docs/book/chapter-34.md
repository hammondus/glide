# Chapter 34: HTTP

Glide's HTTP design changes one thing about Go's, and everything else
follows from it: **handlers return values.**

```glide
fn(Request) -> Result<Response, Error>
```

Go's `func(w http.ResponseWriter, r *http.Request)` cannot return an
error, and that single limitation is the root of reinvented
error-middleware, the forgotten-`return`-after-`http.Error` bug class,
and the difficulty of composing handlers.

The second change is that **cancellation is ambient** — an HTTP request
inside a `scope(timeout:)` is aborted when the scope dies, with no
`ctx` in any signature.

The M2 shim (✓) is deliberately small: a Go-1.22-level router, a
handful of response constructors, and a client with real defaults.

---

### 1. Basic Usage

#### A server

```glide
import http

fn main() -> Result<(), Error> {
    let mut r = http.router()
    r.get(`/health`, |req| http.text("ok"))

    scope s {
        http.serve("127.0.0.1:8080", r)
    }
}
```

`http.router()` returns a `Router`. `r.get/post/put/delete(pattern,
handler)` registers a route and requires a `mut` path.
`http.serve(addr, r)` blocks, serving a green thread per request.

**Route patterns want raw strings.** `` `/notes/{id}` `` — in a regular
string, `{id}` interpolates.

#### Handlers return values

```glide
fn get_note(db: Db, req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(id) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let found = db.query_one(
        "select id, title from notes where id = :id",
        ["id": id],
    )?
    match found {
        Some(row) => Ok(http.json(row))
        None      => Ok(http.not_found())
    }
}
```

A handler returns a `Response` or a `Result<Response, E>`. An `Err`
maps to 500 plus the rendered error — **the one default mapping** until
middleware is designed. A handler panic is 500 plus a stderr trace.

Note the `?` on the database call: it propagates through the handler
signature, converting into `ApiError` via `from` (Chapter 20). No
error-handling middleware, no forgotten return.

#### Registering handlers with dependencies

```glide
let mut r = http.router()
r.get(`/notes/{id}`, |req| get_note(db, req))
r.post(`/notes`, |req| create_note(db, req))
```

The closure adapts the handler signature and supplies the dependency.
That one-line closure is Glide's dependency injection (Chapter 30) —
no container, no globals, no framework.

#### Request and response surfaces

| Request | Returns |
|---|---|
| `req.path_param(name)` | `String?` — `None` when absent |
| `req.body()` | `String` |
| `req.method()` | `String` |
| `req.path()` | `String` |

| Constructor | Produces |
|---|---|
| `http.text(s)` | 200 with a text body |
| `http.json(v)` | 200 with a JSON body |
| `http.created()` | 201 |
| `http.bad_request(msg)` | 400 |
| `http.not_found()` | 404 |

That constructor set is **closed** for now — it grows under the
dogfood rule.

#### A client with real defaults

```glide-run
import http

fn main() {
    match http.get("http://example.com/") {
        Ok(resp) => println("{resp.status()} {resp.body().len()} bytes")
        Err(e)   => println("failed: {e}")
    }
}
```

| Client call | Notes |
|---|---|
| `http.get(url)` | 30s timeout **out of the box** |
| `http.post(url, body)` | body sent as `application/json` |
| `resp.status()` | `Int` |
| `resp.body()` | `String` |

**The default client has a timeout.** Go's does not, and an infinite
hang out of the box is an incident generator.

#### Cancellation is ambient

```glide
scope(timeout: 5.s) {
    let resp = http.get(url)?
    …
}
```

`http.get` is a **cancellation point**, so the scope's deadline aborts
the in-flight request. Nothing in the signature mentions a timeout.

`http.serve` is a cancellation point too: **the enclosing scope's death
gracefully shuts the server down.**

```glide
scope s {
    _ = s.spawn(|| sweeper(db))
    http.serve("127.0.0.1:8080", r)
}
```

When `serve` returns, the sweeper is cancelled and joined. When the
scope dies for any other reason, `serve` shuts down. **There is no
shutdown code anywhere.**

#### A complete, self-driving service

This is the repository's own `notes.gld`, reduced. It serves, exercises
its own API over real HTTP, and returns — which cancels the server.

```glide
import http
import sql
import time

fn get_note(db: Db, req: Request) -> Result<Response, Error> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(id) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let found = db.query_one(
        "select id, title from notes where id = :id",
        ["id": id],
    )?
    match found {
        Some(row) => Ok(http.json(row))
        None      => Ok(http.not_found())
    }
}

fn run() -> Result<String, Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec(`create table notes (id integer primary key, title text)`)?
    _ = db.exec("insert into notes (title) values (:t)", ["t": "hello"])?

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))

    scope s {
        _ = s.spawn(|| http.serve("127.0.0.1:17651", r))
        time.sleep(80.ms)
        let resp = http.get("http://127.0.0.1:17651/notes/1")?
        return Ok("{resp.status()} {resp.body().trim()}")
    }
}

fn main() {
    match run() {
        Ok(s)  => println(s)
        Err(e) => eprintln("failed: {e}")
    }
}
```

```
200 {"id":1,"title":"hello"}
```

Note the `return` inside the scope — an early exit, which cancels the
spawned server (Chapter 28's sharpest edge). A tail expression would
join the server forever.

#### The designed surface ○

- **Middleware is `fn(Handler) -> Handler`.** Because handlers return
  values, composition is function composition.
- **`?` propagates to one error-to-status mapping**, configurable
  rather than reinvented per project.
- **Streaming**: a `Response` body can be a reader or a generator —
  iterators paying off.
- **HTTP/2 in; HTTP/3 when it earns entry.**
- TLS, connection pooling, and retries.

---

### 2. Under the Hood

#### Green thread per request

Each request runs in its own task. Because tasks are cheap and
stackful, a handler is ordinary blocking code — no callbacks, no
`async`, no colouring.

That is the Go model, and it is the reason Go took over backend
services. Glide adds the scope: a request's task is a child of the
server's scope, so a request that is still running when the scope dies
is cancelled.

#### Why `serve` being a cancellation point matters

`http.serve` blocks. Making it a cancellation point means the *scope*
controls the server's lifetime rather than the server controlling the
program's.

Compare Go, where graceful shutdown is:

```go
srv := &http.Server{Addr: ":8080", Handler: r}
go func() { srv.ListenAndServe() }()

stop := make(chan os.Signal, 1)
signal.Notify(stop, os.Interrupt)
<-stop

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

Roughly ten lines, present in every Go service, written slightly
differently each time. In Glide the scope already does it.

#### The one default error mapping

`Err(e)` from a handler becomes 500 plus the rendered error. That is
the *only* mapping in M2, recorded in `DESIGN-DECISIONS.md` as "until
middleware is designed".

It is deliberately crude. A real service wants `NotFound` → 404,
`BadInput` → 400, `Db` → 500, and that mapping is exactly what
middleware is for — one function, applied once, that matches the
error sum type and picks a status.

Today you write it inline by returning `Ok(http.not_found())` rather
than `Err(.NotFound)`, which is why the examples above do that.

#### Client defaults are production defaults

`http.get` has a 30-second timeout out of the box. `DESIGN.md` is
pointed about this: **Go's default client has no timeout — an infinite
hang out of the box, an incident generator.**

Every Go codebase eventually learns to construct its own
`http.Client{Timeout: …}`, usually after an outage. A default that is
wrong for production is not a default.

#### Route patterns are Go-1.22 level

Methods plus `{wildcards}`. Not a DSL, not a framework. `DESIGN.md`:
routing is part of the shared currency a standard library provides —
the same reasoning that made Go's `Handler` interface the ecosystem's
common language.

---

### 3. Why This Design?

#### Why handlers return values

Go's signature is `func(w http.ResponseWriter, r *http.Request)`. It
returns nothing, and three problems follow.

**Errors have nowhere to go.** A handler that fails must write the
response itself:

```go
if err != nil {
    http.Error(w, err.Error(), 500)
    return          // ← forget this and execution continues
}
```

That missing `return` is a real, recurring Go bug: `http.Error` writes
a response and the function keeps going, writing a second one.

**Error handling cannot be centralised.** Every Go web framework
reinvents an error-returning handler type and an adapter:

```go
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func wrap(h HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := h(w, r); err != nil { … }
    }
}
```

Echo, Gin, Chi, and every in-house framework has this. It exists purely
to work around the signature.

**Composition is awkward.** With a returned `Response`, middleware is
`fn(Handler) -> Handler` — plain function composition. With a
`ResponseWriter`, middleware must wrap the writer to inspect what a
handler did, which is why Go middleware that needs the status code
implements a `responseWriter` shim.

Returning `Result<Response, E>` fixes all three. `?` propagates,
one mapping turns errors into statuses, and middleware composes.

#### Why cancellation is ambient here

Chapter 27 made the general argument. HTTP is where it pays the most,
because HTTP is where Go's `ctx` is most viral: every handler takes
one, every function a handler calls takes one, every database call
takes one, and Go duplicated its entire `database/sql` API
(`Query`/`QueryContext`) to retrofit it.

`scope(timeout: 5.s) { http.get(url)? }` cancels the request when the
scope dies, and no signature changed. `DESIGN.md`: "the ctx-replacement
covers HTTP for free."

#### Why a router in the standard library

Because **routing is shared currency**.

Go's `net/http` made `Handler` the ecosystem's common interface, and
that is why Go middleware from different authors composes. But Go's
router was too weak until 1.22 (no method matching, no path
parameters), so every project imported `gorilla/mux` or `chi` — and
those routers have incompatible parameter-extraction APIs, which
fragmented the middleware ecosystem after all.

Shipping a Go-1.22-level router — methods plus wildcards — from day one
means the shared currency includes routing. Not a DSL, not a
framework.

#### Why the response constructor set is closed (for now)

`http.text`, `http.json`, `http.created`, `http.bad_request`,
`http.not_found`. Five constructors.

That is the dogfood rule (Chapter 31): the set grows when a program
needs a member. A closed set also keeps the API from becoming a status
code enumeration with a function per code, which is what happens when
you start (`http.conflict()`, `http.teapot()`, …).

#### Why green thread per request rather than async

Chapter 26's argument, applied. A handler is ordinary blocking code
that can call any function, use `?`, hold a database transaction across
an await-shaped boundary that does not exist, and be tested by calling
it.

Node's and Python's async ecosystems both split their libraries in two
over this. Java spent fifteen years on reactive HTTP and shipped
virtual threads.

---

### 4. Competing Approaches

**Go.** `net/http` — the model, and genuinely excellent: `Handler` as
shared currency, green thread per request, a standard library server
you can run in production. Its three weaknesses that Glide targets: the
handler signature cannot return an error, the default client has no
timeout, and `ctx` must be threaded manually.

**Rust.** `axum`, `actix-web`, `hyper` — async, so handlers are
`async fn` and everything they call must be async-aware. Excellent
performance and the function-colouring cost. Axum's handler return type
(`impl IntoResponse`) is close in spirit to Glide's returned
`Response`.

**Python.** Flask/Django (sync, WSGI) and FastAPI/Starlette (async,
ASGI) — the two parallel ecosystems the async decision produced.
FastAPI's typed request/response models via Pydantic are the closest
thing to what `derive Json` plus typed handlers will give.

**Node.** Express with `(req, res, next)` — the same
no-return-value problem as Go, plus `next()` for error propagation and
a middleware chain that is a list rather than a composition.
`async`/`await` on top means an unhandled promise rejection can hang a
request silently.

**Java.** Servlets (`doGet(req, resp)` — the same shape as Go's) and
Spring MVC, where a handler returns a value and an
`@ExceptionHandler` maps errors to statuses. Spring got the returned-
value design right decades ago, at the cost of the rest of Spring.

**Elixir/Phoenix.** `Plug` — a function `conn -> conn`, so middleware
composes cleanly. Different shape from Glide's, same insight: make the
handler a function that returns something.

---

### 5. Common Mistakes

**Using a regular string for a route pattern.**

```glide
// Bad — {id} interpolates
r.get("/notes/{id}", handler)

// Good
r.get(`/notes/{id}`, handler)
```

The single most common HTTP mistake in Glide, and it was discovered the
hour the shim landed.

**Forgetting `mut` on the router.**

```glide
// Bad
let r = http.router()
r.get(`/health`, h)        // registration requires a mut path

// Good
let mut r = http.router()
r.get(`/health`, h)
```

**Letting a tail expression join the server forever.**

```glide
// Bad — normal scope exit joins the blocked server
scope s {
    _ = s.spawn(|| http.serve(addr, r))
    do_something()
}

// Good — early exit cancels first
scope s {
    _ = s.spawn(|| http.serve(addr, r))
    do_something()
    return
}
```

Chapter 28's sharpest edge, and it bites hardest here because
`http.serve` blocks forever by design.

**Returning `Err` for a client error.** Today, `Err(e)` maps to 500 —
the one default mapping. A 404 or a 400 must be `Ok(http.not_found())`
or `Ok(http.bad_request(msg))` until error middleware exists.

```glide
// Bad today — a missing note becomes a 500
None => Err(.NotFound{ id: id })

// Good today
None => Ok(http.not_found())
```

**Expecting a `ctx` parameter.** There is none. Timeouts come from an
enclosing scope.

**Blocking the server's task with setup.** `http.serve` blocks, so
anything after it in the same task never runs. Spawn it, or make it the
last statement.

**Assuming the client has no timeout.** It has 30 seconds. That is
usually right and occasionally wrong — for a long-poll endpoint, wrap
it in a scope with the deadline you want.

**Reaching for middleware.** It is ○. Today, compose by wrapping the
closure at registration:

```glide
r.get(`/notes/{id}`, |req| with_logging(|r| get_note(db, r), req))
```

Workable, and clearly a placeholder for `fn(Handler) -> Handler`.

---

### 6. Performance Considerations

**Green thread per request** costs a few kilobytes of stack per
in-flight request. At tens of thousands of concurrent connections that
is tens to hundreds of megabytes — fine on a server, and the workload
`DESIGN.md` concedes to async is a million connections on small
hardware.

**A handler returning a `Response`** is a value return, not a write to
a shared writer. That means the framework can inspect, wrap, and
transform it — which is what makes middleware cheap — at the cost of
holding the body in memory until it is written.

For large bodies that matters, and the designed answer is a
**streaming body**: a reader or a generator, so the response is
produced lazily (Chapter 24's laziness paying off in a third place).

**`http.get` has a 30-second timeout** and connection pooling.
Cancellation adds a context per call.

**In the interpreter**, everything above sits on Go's `net/http`, so
the network path is production-grade and the *handler* is
tree-walked — which is where the time goes.

**`http.json(v)` walks the value structurally** (Chapter 33). With
`derive Json` (○) it becomes a generated encoder.

---

### 7. Best Practices

**Let the scope own the server's lifetime.**

```glide
fn run() -> Result<(), Error> {
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    let r = r                                     // seal after registration

    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve("127.0.0.1:8080", r)
    }
}
```

Everything dies together, in the right order, with no shutdown code.
Note the `let r = r` seal (Chapter 4) — registration is over.

**Keep handlers as named functions; use closures only to inject.**

```glide
// Good
r.get(`/notes/{id}`, |req| get_note(db, req))

fn get_note(db: Db, req: Request) -> Result<Response, ApiError> { … }
```

The handler is now an ordinary function you can call from a test with a
constructed `Request` — no server, no port, no HTTP.

**Validate at the boundary and convert to types immediately.**

```glide
fn get_note(db: Db, req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(n) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let id = NoteId(n)              // typed from here on
    …
}
```

Two guard clauses, then everything downstream has a `NoteId`
(Chapter 15).

**Distinguish client errors from server errors deliberately.** Client
errors (400, 404) are `Ok(…)` responses; server errors are `Err`. That
mapping is temporary but the *distinction* is permanent, and getting it
right now means the middleware migration is mechanical.

**Set a scope timeout per request boundary** rather than per call:

```glide
fn handle(req: Request) -> Result<Response, ApiError> {
    scope(timeout: 2.s) {
        let user = load_user(req)?
        let prefs = load_prefs(user)?
        Ok(http.json(render(user, prefs)))
    }
}
```

**Do not reach for a framework.** The standard library is the shared
currency, and the whole reason it ships a router is so that the
ecosystem does not fragment into incompatible middleware conventions.

---

### 8. Examples

**Hello, server:**

```glide
import http

fn main() -> Result<(), Error> {
    let mut r = http.router()
    r.get(`/health`, |req| http.text("ok"))
    r.get(`/hello/{name}`, |req| {
        let name = req.path_param("name") ?? "world"
        http.text("hello, {name}")
    })

    scope s {
        http.serve("127.0.0.1:8080", r)
    }
}
```

Two routes, a path parameter with a default, and a server whose
lifetime is the scope.

**The self-driving service — server and client in one program:**

```glide
import http
import sql
import time

fn get_note(db: Db, req: Request) -> Result<Response, Error> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(id) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let found = db.query_one(
        "select id, title from notes where id = :id",
        ["id": id],
    )?
    match found {
        Some(row) => Ok(http.json(row))
        None      => Ok(http.not_found())
    }
}

fn run() -> Result<String, Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec(`create table notes (id integer primary key, title text)`)?
    _ = db.exec("insert into notes (title) values (:t)", ["t": "hello"])?

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))

    scope s {
        _ = s.spawn(|| http.serve("127.0.0.1:17651", r))
        time.sleep(80.ms)

        let mut report = []
        for id in ["1", "999", "abc"] {
            let resp = http.get("http://127.0.0.1:17651/notes/" + id)?
            report.push("{id:4} -> {resp.status()} {resp.body().trim()}")
        }
        return Ok(report.join("\n"))
    }
}

fn main() {
    match run() {
        Ok(s)  => println(s)
        Err(e) => eprintln("failed: {e}")
    }
}
```

```
   1 -> 200 {"id":1,"title":"hello"}
 999 -> 404 not found
 abc -> 400 bad id
```

Three requests, three status codes, one program. Worth counting what is
absent: no `context.Context`, no `WaitGroup`, no shutdown handler, no
signal trapping, no `errgroup`, and no `defer srv.Shutdown(ctx)`. The
`return` cancels the server, the scope joins it, and the `defer` closes
the database.

**Side by side with Go, on the error path:**

```go
// Go — the missing `return` is a real, recurring bug
func GetNote(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        id, err := strconv.Atoi(r.PathValue("id"))
        if err != nil {
            http.Error(w, "bad id", 400)
            return                              // ← forget this…
        }
        note, err := queryNote(r.Context(), db, id)
        if err != nil {
            http.Error(w, err.Error(), 500)
            return                              // ← …or this
        }
        if note == nil {
            http.NotFound(w, r)
            return                              // ← …or this
        }
        json.NewEncoder(w).Encode(note)
    }
}
```

Three `return`s that must not be forgotten, and forgetting one produces
a superimposed double response rather than a compile error.

```glide
// Glide — every exit is a value, so there is nothing to forget
fn get_note(db: Db, req: Request) -> Result<Response, ApiError> {
    let Some(raw) = req.path_param("id") else {
        return Ok(http.bad_request("missing id"))
    }
    let Some(id) = raw.parse_int() else {
        return Ok(http.bad_request("bad id"))
    }
    let found = db.query_one(sql, ["id": id])?
    match found {
        Some(row) => Ok(http.json(row))
        None      => Ok(http.not_found())
    }
}
```

The function must produce a `Result<Response, ApiError>` on every path,
and the compiler checks that. A forgotten exit is a type error rather
than a runtime double-write.

**Bad versus good: the shutdown ceremony**

```glide
// Bad — Go habits transplanted
fn main() -> Result<(), Error> {
    let (stop_tx, stop_rx) = channel()
    let mut shutting_down = false

    scope s {
        _ = s.spawn(|| {
            for v in stop_rx { shutting_down = true }
        })
        _ = s.spawn(|| sweeper(db, stop_rx))     // takes a stop channel
        http.serve(addr, r)
        stop_tx.close()
        return
    }
}
```

A stop channel, a flag, and a sweeper that has to poll it. Every piece
of this is reconstructing what the scope already does — and the
`mut shutting_down` captured across a task boundary is the exact
pattern the ○ compile rule will reject.

```glide
// Good
fn main() -> Result<(), Error> {
    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve(addr, r)
    }
}
```

`sweeper` has no stop channel, no flag, and no `ctx`. Its
`time.sleep` is a cancellation point, so it stops when the scope dies.

---

### 9. Summary & Exercises

**Summary**

- **Handlers return values**: `fn(Request) -> Result<Response, E>`.
  This single change fixes Go's three handler problems — errors have
  nowhere to go, error handling cannot be centralised, and composition
  requires wrapping the `ResponseWriter`.
- The forgotten-`return`-after-`http.Error` bug class becomes a **type
  error**: every path must produce a `Result<Response, E>`.
- **`Err(e)` maps to 500** — the one default mapping until middleware
  is designed. Client errors are `Ok(http.bad_request(…))` and
  `Ok(http.not_found())` today.
- **Route patterns want raw strings**: `` r.get(`/notes/{id}`, h) ``.
  Registration requires a `mut` router.
- **Dependency injection is a one-line closure**:
  `r.get(pat, |req| get_note(db, req))`. No container, no globals.
- **Cancellation is ambient.** `http.get` and `http.serve` are
  cancellation points, so `scope(timeout:)` bounds a request and the
  scope's death gracefully shuts the server down. **There is no
  shutdown code** — Go's ten-line signal-trap-and-`Shutdown(ctx)`
  ritual is deleted.
- **The client has a 30-second timeout by default.** Go's has none,
  which is an infinite hang out of the box.
- **Green thread per request**, so handlers are ordinary blocking code
  with no function colouring.
- **A router in the standard library** because routing is shared
  currency — Go's weak pre-1.22 router fragmented the middleware
  ecosystem it had otherwise unified.
- Response constructors are a **closed set of five**, growing under the
  dogfood rule.
- ○: middleware as `fn(Handler) -> Handler`, a configurable
  error-to-status mapping, streaming response bodies (a reader or
  generator), TLS, retries, HTTP/2.

**Exercises**

1. **Count the shutdown code.** In a Go service, find every line
   involved in graceful shutdown: signal handling, the `Shutdown`
   call, its timeout context, the `errgroup`, and every `ctx.Done()`
   arm in a background loop. Then write the Glide equivalent and count
   again. The difference is one scope.

2. **Find the missing return.** In a Go or Express codebase, search for
   `http.Error(`, `res.status(`, or equivalent, and check that every
   one is immediately followed by a `return` or `next()`. In a codebase
   of any size you will find at least one that is not, and it will
   either be a latent double-write or a case where the author knew
   something the code does not say.

3. **Design the error middleware.** Write the `fn(Handler) -> Handler`
   that maps an `ApiError` sum type to statuses — `NotFound` → 404,
   `BadInput` → 400, `Db` → 500 — and note that the `match` is
   exhaustive, so adding a variant breaks the middleware at compile
   time and forces you to choose a status. Then compare with Go's
   `errors.Is` ladder, which does not.
