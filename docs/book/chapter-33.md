# Chapter 33: SQL and Databases

Glide's database design has three load-bearing positions, and all three
are refusals:

- **No ORM. Ever.** An ORM is a language dispute, not a battery.
- **No query-builder DSL.** A worse SQL that compiles to SQL serves the
  library, not you.
- **No live-schema checking.** Validating against a running database at
  compile time trades away hermeticity.

What is left is making raw SQL plus mapping ergonomic — and the pieces
that do that come from decisions made in earlier chapters: `Option`
kills `NULL`, comptime kills the placeholder bug, closures make
transactions structural, and scopes make `QueryContext` unnecessary.

The M2 shim (✓) is SQLite only, with dynamic rows. `derive Row` and
typed queries are ○.

---

### 1. Basic Usage

#### Opening

```glide
import sql

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    …
}
```

DSNs today: `"sqlite:path"` or `"sqlite::memory:"`. The driver is
`modernc.org/sqlite` — **pure Go, no CGO**, so cross-compilation stays
`GOOS=… go build` and no C toolchain enters the build.

#### The surface

| Call | Returns |
|---|---|
| `sql.open(dsn)` | `Result<Db, Error>` |
| `db.exec(q [, params])` | `Result<Int, Error>` — rows affected |
| `db.query(q [, params])` | `Result<List<Map>, Error>` |
| `db.query_one(q [, params])` | `Result<Option<Map>, Error>` |
| `db.close()` | `Result<(), Error>` |

`db.exec`, `db.query`, and `db.query_one` are **cancellation points**.

#### Named parameters, and only named parameters

```glide
let found = db.query_one(
    "select id, title from notes where id = :id",
    ["id": id],
)?
```

`:name` is the **one canonical placeholder syntax**. Parameters are a
`Map`. Drivers translate to whatever the wire protocol wants — the
`?`-versus-`$1` roulette is interface negligence.

**Missing and unused names are both errors, naming the parameter.**

```glide
db.exec("insert into notes (title) values (:title)", ["ttile": "x"])
```

errors rather than silently inserting nothing. That is the comptime
check (○), enforced dynamically for now.

#### Rows are maps today

```glide
let rows = db.query("select id, title from notes")?
for row in rows {
    let id = row["id"] ?? 0
    let title = row["title"] ?? ""
    println("{id}: {title}")
}
```

`db.query` returns `List<Map>` — column name to value, in column order.
`db.query_one` returns `Option<Map>`: `None` for no row, and **an
`Err` for more than one row**.

Typed rows arrive with `derive Row`.

#### NULL is `Option`, in both directions

A NULL column reads as `None`. Binding `None` writes NULL.

```glide
let row = db.query_one("select body from notes where id = :id", ["id": id])?
match row {
    Some(r) => {
        let body = r["body"] ?? "(no body)"     // NULL becomes None
        println(body)
    }
    None => println("no such note")
}
```

**`sql.NullString` never exists.** It is the zero-value disease in a
database costume, and Chapter 14's decision cures it here for free.

#### `distinct` values bind by unwrapping

```glide
db.exec("insert into notes (id) values (:id)", ["id": NoteId(7)])
```

binds `7`. Codecs unwrap at the boundary (Chapter 15) — a codec's
conversion is the explicit kind.

`Instant` values store as RFC 3339.

#### A complete program

```glide
import sql

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }

    _ = db.exec(`create table notes (
        id integer primary key,
        title text not null,
        body text
    )`)?

    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "first", "b": "hello"])?
    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "second", "b": None])?

    let rows = db.query("select id, title, body from notes order by id")?
    for row in rows {
        let id = row["id"] ?? 0
        let title = row["title"] ?? ""
        let body = row["body"] ?? "(null)"
        println("{id}  {title:8}  {body}")
    }

    let one = db.query_one("select title from notes where id = :id",
                           ["id": 1])?
    println("{one:?}")

    let missing = db.query_one("select title from notes where id = :id",
                               ["id": 99])?
    println("{missing:?}")

    Ok(())
}
```

```
1     first  hello
2    second  (null)
["title": "first"]
None
```

Note the second insert binds `None` for `body`, and the query reads it
back as `None`, which `?? "(null)"` renders. Nothing anywhere mentions
a nullable-string wrapper type.

#### Raw strings for multi-line SQL

```glide
let rows = db.query(`
    select id, title
    from notes
    where org = :org
    order by created desc
`, ["org": org])?
```

Backticks: no escapes, no interpolation, multiline. And critically —
**never interpolate a value into a query string**:

```glide
// Bad — SQL injection, and it does not even work with `?? `
let q = "select * from notes where title = '{title}'"

// Good
db.query("select * from notes where title = :title", ["title": title])
```

#### The designed surface ○

**`derive Row`** — comptime column mapping:

```glide
type Note = struct {
    pub id: NoteId
    pub title: String
    pub body: String?
} derive(Row)

let note = db.query_one<Note>("select id, title, body from notes where id = :id",
                              ["id": id])?
```

**Transactions are a closure:**

```glide
db.tx(|tx| {
    tx.exec("update accounts set bal = bal - :n where id = :id", …)?
    tx.exec("update accounts set bal = bal + :n where id = :id", …)?
    Ok(())
})?
```

Commit on `Ok`, rollback on `Err` or panic.

**Drivers are packages** against a small driver trait. Postgres and
MySQL speak wire protocols, so they are pure Glide and cross-
compilation is untouched.

---

### 2. Under the Hood

#### The shim does dynamically what derive will do statically

`glide/DESIGN-DECISIONS.md` is explicit: `db.query` returns rows as
column→value maps, and `:name` placeholders are verified at call time.
**None of this survives into the compiled tier.** `derive Row`
generates the typed version, and the comptime check moves the
placeholder verification to compile time.

The shim proves the *surfaces*, not the mechanism.

#### Why `modernc.org/sqlite`

The interpreter's **first and only third-party dependency**, and the
choice is recorded: chosen over `mattn/go-sqlite3` because it is
**pure Go**. No CGO means cross-compilation stays a `GOOS` environment
variable and no C toolchain ever enters the build.

The cost accepted: a large transitive module tree — a
machine-translated SQLite. The alternative of a fake in-memory store
would have dogfooded nothing real.

This mirrors the language-level position: `DESIGN.md` notes that
**SQLite is C, and will be FFI's first paying customer** in the native
era, with different cross-compile ergonomics — exactly like cgo-sqlite
in Go.

#### A known bug, today

While writing this chapter, a genuine interpreter bug surfaced and is
worth documenting so you recognise it.

**A failing `db.exec` or `db.query` panics with an internal
`cancelUnwind` instead of returning `Err`.**

```glide
import sql

fn main() {
    match sql.open("sqlite::memory:") {
        Ok(db) => {
            match db.exec("delete from nosuchtable") {
                Ok(n)  => println("ok {n}")
                Err(e) => println("err: {e}")
            }
        }
        Err(e) => println("open failed: {e}")
    }
}
```

```
panic: (interp.cancelUnwind) …
```

The cause is in `internal/interp/sqlmod.go`: the host context's
`release()` is deferred *inside* the closure that runs the query, so by
the time the code checks `cancelled()` — `ctx.Err() != nil` — the
context has already been cancelled by `release()`. Every failure
therefore looks like a cancellation.

Successful queries are unaffected, which is why the working examples in
this chapter and in Chapter 32 run fine. Errors detected *before* the
query reaches the driver are also unaffected — a bad placeholder is
caught by `bindNamed` and returns properly:

```glide
db.exec("insert into notes (title) values (:title)", ["ttile": "x"])
```

```
err: query names :title but params do not supply it
```

Until the context bug is fixed, **do not rely on catching errors that
come back from the database itself** at this tier.

#### Cancellation

`db.exec`, `db.query`, and `db.query_one` obtain a host context wired
to the task's cancellation channel and the nearest enclosing deadline.
A query inside a dying `scope(timeout:)` is aborted at the driver
level.

That is what makes `QueryContext` unnecessary — see below.

#### Placeholder verification

`bindNamed` parses the query string for `:name` occurrences and matches
them against the parameter map's keys. Both directions are checked:
a placeholder with no parameter, and a parameter with no placeholder.

This is a **pure string operation on a literal** — no database, no
network, no IO. Which is precisely why it can move to comptime.

---

### 3. Why This Design?

#### Why no ORM, ever

`DESIGN.md` states it as a recorded position: **an ORM is a language
dispute, not a battery.**

The argument: an ORM is an attempt to replace SQL — a declarative
language that is very good at its job — with method calls in your host
language, and the replacement is always partial. You get 80% coverage,
then hit a query the ORM cannot express, then drop to raw SQL anyway,
now with two mental models and an object graph whose loading behaviour
you cannot see.

The specific costs: N+1 queries that look like field access, lazy
loading that fires a query from a getter, a migration DSL that diverges
from what the database can express, and an entity lifecycle nobody
fully understands.

The alternative Glide takes: make raw SQL ergonomic. `derive Row` for
mapping, named parameters for binding, comptime for checking. The SQL
stays SQL.

#### Why no query-builder DSL either

A query builder — `db.select("id").from("notes").where("org", org)` —
is *a worse SQL that compiles to SQL*. It has fewer features, needs
learning, produces worse error messages, and serves the library's need
for type safety rather than yours.

The exception people cite is dynamic query construction (optional
filters), and that is real — but it is a string-building problem with a
comptime-checkable answer, not a reason for a whole parallel language.

#### Why named parameters only

Go's `database/sql` uses positional placeholders whose *syntax depends
on the driver*: `?` for MySQL and SQLite, `$1` for Postgres, `:name`
for Oracle. `DESIGN.md` calls that **interface negligence** — the whole
point of a driver interface is that the caller should not care.

Positional parameters also produce a specific runtime failure that
named ones cannot:

```
sql: expected 2 arguments, got 3
```

which Go finds in production. With `:name`, the check is a pure parse
of a literal string, so it moves to compile time.

And the transposition bug disappears: with `?, ?, ?` you can swap two
arguments of the same type silently. With `:title, :body, :created`
you cannot.

#### Why comptime placeholder checking is the unoccupied sweet spot

This is the sharpest argument in the chapter.

**Schema checking needs a database.** Rust's `sqlx` validates queries
against a running, migrated database at compile time. That is genuinely
powerful and it makes your build depend on a database being up — the
hermeticity Glide defends in three separate sections.

**Placeholder checking needs only the literal query string.** It is a
pure comptime parse: no IO, no network, deterministic. So it is
compatible with hermetic builds, and it catches the most common class
of parameter bug.

That gap — checking what can be checked without IO — is the unoccupied
sweet spot, and it is available only to a language with comptime.

For schema awareness, `DESIGN.md` points at the `sqlc` approach:
schema in the repository, explicit code generation, committed output.
The schema becomes a **versioned artifact** rather than a compile-time
network dependency.

#### Why NULL dies like absent-JSON died

`sql.NullString` is `struct { String string; Valid bool }` — a value
plus a flag saying whether the value means anything. That is `Option`,
hand-rolled, once per type, with no ergonomics.

It exists because Go has no `Option` and no way to say "this column may
be absent". Glide has both:

| Column | Field type |
|---|---|
| `text not null` | `String` |
| `text` (nullable) | `String?` |

NULL into a non-Option field is a **decode error**. One doctrine
(Chapter 14), third application — after JSON's missing fields
(Chapter 31) and struct initialisation (Chapter 12).

#### Why transactions are a closure

Go's idiom:

```go
tx, err := db.Begin()
if err != nil { return err }
defer tx.Rollback()          // a no-op after a successful commit
… work …
return tx.Commit()
```

`defer tx.Rollback()` works because rollback-after-commit is a silent
no-op. That is relying on a *coincidence* — and the moment you need
conditional commit logic, you get the `committed := false` flag from
Chapter 21.

`db.tx(|tx| { … })` makes it structural: commit on `Ok`, rollback on
`Err` or panic. There is no path where you forget, and `errdefer`
(Chapter 21) is the primitive that makes it implementable.

#### Why no `QueryContext` zoo

Go retrofitted cancellation by **duplicating its entire database API**:
`Query`/`QueryContext`, `Exec`/`ExecContext`, `Begin`/`BeginTx`,
`Prepare`/`PrepareContext`. Every method has a twin, and the
non-context one is a trap.

Scopes make cancellation ambient (Chapter 26), so queries are
cancellation points and there is **one method name each**.

That is the ctx-replacement paying for itself a third time — after HTTP
(Chapter 32) and background tasks (Chapter 25).

---

### 4. Competing Approaches

**Go.** `database/sql` with driver-specific positional placeholders,
`sql.NullString` and friends, `rows.Scan(&u.ID, &u.Name)` which
**silently misassigns on column reorder**, and the `Context` API
duplication. `sqlx` adds struct scanning via runtime reflection —
banned here anyway. Glide keeps the thin-interface-plus-drivers shape
and fixes all four.

**Rust.** `sqlx` (live-schema checking at compile time — powerful,
non-hermetic), `diesel` (a query-builder DSL with a type-level schema),
and `rusqlite`. `sqlx`'s offline mode exists precisely because the
database dependency was a problem, which is the evidence for Glide's
position.

**Python.** DB-API 2.0 with `%s` or `?` placeholders depending on
driver — the same negligence — plus SQLAlchemy as both a core (query
builder) and an ORM. SQLAlchemy Core is the strongest argument for
query builders and is still a second language.

**Java.** JDBC with `?` placeholders, plus Hibernate/JPA — the ORM
whose lazy-loading and session-lifecycle problems are the canonical
case against ORMs. Also jOOQ, a query builder generated from the
schema, which is the closest thing to a query builder that works.

**Elixir.** Ecto — explicitly *not* an ORM: changesets and queries,
with no identity map and no lazy loading. Closest in spirit to Glide's
position.

**C#.** Entity Framework (ORM) and Dapper (thin mapping over raw SQL).
Dapper's popularity in the .NET world — where a first-party ORM
ships — is evidence for the raw-SQL-plus-mapping approach.

---

### 5. Common Mistakes

**Interpolating a value into a query.**

```glide
// Bad — SQL injection
let q = "select * from notes where title = '{title}'"
db.query(q)

// Good
db.query("select * from notes where title = :title", ["title": title])
```

Interpolation is for building *messages*, never queries. Note that the
type system does not stop you here — this is the one place in the book
where the discipline is entirely yours.

**Forgetting a parameter, or misspelling one.** Both are errors naming
the parameter, which is the feature. But note the current bug above:
error *paths* through `db.exec` may panic rather than returning `Err`
at this tier.

**Expecting typed rows.** `db.query` returns maps today. Read with
`??`, and convert to a struct at the boundary (Chapter 31's pattern
applies exactly).

**Expecting `query_one` to return the first of several rows.** More
than one row is an `Err`, deliberately — if the query can match
multiple rows, `query_one` is the wrong call.

**Reaching for `sql.NullString`.** There is none. Nullable column,
`Option` field.

**Using a regular string for multi-line SQL with braces.** Rare in SQL,
but JSON functions and some dialects use them. Backticks are safer, and
they are the idiom for multi-line queries anyway.

**Forgetting `defer { _ = db.close() }`.** And note the `_ =`: the
discard must be visible (Chapter 21).

**Assuming a transaction API exists.** `db.tx(|tx| …)` is ○. Today,
`begin`/`commit` are not exposed.

**Relying on catching a SQL error.** The `cancelUnwind` bug means a
failing query panics at this tier. Successful paths are fine.

---

### 6. Performance Considerations

**Named-parameter binding parses the query string per call** in the
shim. With comptime (○) the parse happens once, at build time, and the
runtime cost is zero.

**`db.query` materialises all rows into a `List<Map>`.** For a large
result set that is the whole thing in memory, plus a map per row with a
string key per column. Streaming and typed rows (○) fix both — a
`derive Row` decoder writes directly into a struct with no intermediate
map.

**Each row map allocates one entry per column** plus the keys slice for
insertion order (Chapter 11). Typed rows eliminate this entirely.

**Queries are cancellation points**, which costs a context per call in
the interpreter.

**`modernc.org/sqlite` is a machine translation of SQLite to Go.** It
is slower than the C original — roughly a factor of two on many
workloads — and it buys CGO-free cross-compilation. That trade was made
deliberately for the interpreter; the native era revisits it.

**Connection pooling** is the driver's business. The designed interface
follows `database/sql`'s shape here.

---

### 7. Best Practices

**Open in `main`, close with `defer`, pass the handle down.**

```glide
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    …
}
```

Chapter 29's pattern. No global, no `init()`, and a test can pass an
in-memory database.

**Use raw strings for anything longer than one line.**

```glide
let rows = db.query(`
    select n.id, n.title, count(c.id) as comments
    from notes n
    left join comments c on c.note_id = n.id
    where n.org = :org
    group by n.id
    order by n.created desc
    limit :limit
`, ["org": org, "limit": 50])?
```

The SQL reads as SQL. That is the payoff of not having a query builder.

**Name parameters after the column, not the variable.** `:org`, not
`:o` — the placeholder appears in the query text, so it should read as
part of the query.

**Convert rows to types at the boundary.**

```glide
// Good — the map lives for three lines
fn load_note(db: Db, id: Int) -> Result<Note?, Error> {
    let found = db.query_one(
        "select id, title, body from notes where id = :id",
        ["id": id],
    )?
    match found {
        None      => Ok(None)
        Some(row) => Ok(Some(Note{
            id:    row["id"] ?? 0,
            title: row["title"] ?? "",
            body:  row["body"],          // stays Option — the column is nullable
        }))
    }
}
```

Downstream code gets a `Note`. When `derive Row` lands, the middle of
this function becomes one line and the *shape* is unchanged.

**Let the column's nullability decide the field type.** `text not null`
→ `String`; `text` → `String?`. Keep the schema and the type in
agreement, and NULL handling disappears.

**Never build a query with interpolation.** Not even for a column name
you "control". If you need dynamic columns, build the query from a
closed set:

```glide
let column = match sort_by {
    Created => "created"
    Title   => "title"
    _       => "id"
}
let q = "select * from notes order by " + column      // from a sum type
```

The sum type is what makes this safe — the set of possible strings is
closed and visible.

**Prefer `query_one` when you expect one row.** It makes
"more than one" an error rather than a silently-ignored surprise.

---

### 8. Examples

**The whole surface, in one runnable program:**

```glide
import sql

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }

    _ = db.exec(`create table notes (
        id integer primary key,
        title text not null,
        body text
    )`)?

    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "first", "b": "hello"])?
    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "second", "b": None])?

    let rows = db.query("select id, title, body from notes order by id")?
    for row in rows {
        let id = row["id"] ?? 0
        let title = row["title"] ?? ""
        let body = row["body"] ?? "(null)"
        println("{id}  {title:8}  {body}")
    }

    let one = db.query_one("select title from notes where id = :id",
                           ["id": 1])?
    println("{one:?}")

    let missing = db.query_one("select title from notes where id = :id",
                               ["id": 99])?
    println("{missing:?}")

    Ok(())
}
```

```
1     first  hello
2    second  (null)
["title": "first"]
None
```

Three things worth noting. The second insert binds `None` and the read
returns `None` — NULL is `Option` in both directions, with no wrapper
type anywhere. `query_one` on a missing id returns `None` rather than
an error, because "no row" is an ordinary outcome (Chapter 14's rule).
And `defer { _ = db.close() }` sits on the line after the open.

**Rows into types:**

```glide
import sql

type Note = struct {
    pub id: Int
    pub title: String
    pub body: String?
}

fn load_all(db: Db) -> Result<List<Note>, Error> {
    let rows = db.query("select id, title, body from notes order by id")?
    let mut out = []
    for row in rows {
        out.push(Note{
            id:    row["id"] ?? 0,
            title: row["title"] ?? "",
            body:  row["body"],
        })
    }
    Ok(out)
}

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec(`create table notes (
        id integer primary key, title text not null, body text)`)?
    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "first", "b": "hello"])?
    _ = db.exec("insert into notes (title, body) values (:t, :b)",
                ["t": "second", "b": None])?

    for n in load_all(db)? {
        println("{n:?}")
    }
    Ok(())
}
```

```
Note{ id: 1, title: "first", body: "hello" }
Note{ id: 2, title: "second", body: None }
```

`body` is `String?` and carries `None` straight through — no
`?? ""` flattening the distinction between "no body" and "empty body".
When `derive Row` lands, `load_all`'s loop becomes
`db.query<Note>(…)`.

**Side by side with Go, on the four fixed problems:**

```go
// Go
rows, err := db.QueryContext(ctx,                     // 4: API duplication
    "select id, title, body from notes where org = $1", org)  // 3: $1 vs ?
if err != nil { return nil, err }
defer rows.Close()

var notes []Note
for rows.Next() {
    var n Note
    var body sql.NullString                            // 2: NULL wrapper
    if err := rows.Scan(&n.ID, &n.Title, &body); err != nil {  // 1: positional
        return nil, err
    }
    if body.Valid { n.Body = &body.String }
    notes = append(notes, n)
}
return notes, rows.Err()
```

The four problems, numbered:

1. **`rows.Scan` is positional.** Reorder the columns in the SELECT and
   it silently misassigns — `title` into `body`, both strings, no
   error.
2. **`sql.NullString`** plus the `.Valid` dance plus a pointer field to
   carry the optionality onward.
3. **`$1` versus `?`** depending on driver.
4. **`QueryContext`** — the duplicated API.

```glide
// Glide, today
let rows = db.query("select id, title, body from notes where org = :org",
                    ["org": org])?
let mut notes = []
for row in rows {
    notes.push(Note{
        id:    row["id"] ?? 0,
        title: row["title"] ?? "",
        body:  row["body"],
    })
}
Ok(notes)
```

```glide
// Glide, with derive Row (○)
db.query<Note>("select id, title, body from notes where org = :org",
               ["org": org])
```

Column mapping is by **name**, so reordering the SELECT is harmless.
NULL is `Option`. Placeholders are `:name` on every driver. And there
is one `query`, because cancellation comes from the scope.

**Bad versus good: the injected query**

```glide
// Bad — and the type system will not save you here
fn search(db: Db, term: String) -> Result<List<Map>, Error> {
    db.query("select * from notes where title like '%{term}%'")
}
```

`term` of `' or 1=1 --` returns every row. This is the one place in the
book where nothing structural prevents the mistake — interpolation is
always available, and it is the wrong tool.

```glide
// Good
fn search(db: Db, term: String) -> Result<List<Map>, Error> {
    db.query("select * from notes where title like :pattern",
             ["pattern": "%{term}%"])
}
```

The interpolation is now building a *parameter value*, not a query. The
driver escapes it.

---

### 9. Summary & Exercises

**Summary**

- Three refusals: **no ORM ever** (a language dispute, not a battery),
  **no query-builder DSL** (a worse SQL that compiles to SQL), and
  **no live-schema checking** (it makes builds depend on a running
  database).
- What is left is raw SQL made ergonomic, and the pieces come from
  earlier chapters: `Option` kills NULL wrappers, comptime kills the
  placeholder bug, closures make transactions structural, scopes make
  the `Context` API duplication unnecessary.
- **Named parameters only** — `:name`, one canonical syntax, drivers
  translate. Missing *and* unused names are both errors naming the
  parameter. The `?`-versus-`$1` roulette is interface negligence.
- **Comptime placeholder verification is the unoccupied sweet spot**:
  schema checking needs a database, but *placeholder* checking needs
  only the literal string — a pure parse, no IO, hermetic.
- **NULL is `Option`, in both directions.** `sql.NullString` never
  exists; a nullable column is a `T?` field, and NULL into a non-Option
  field is a decode error.
- **Column mapping is by name** (○ `derive Row`), so reordering a
  SELECT is harmless. Go's positional `rows.Scan` silently misassigns.
- **`distinct` values bind by unwrapping**; `Instant` stores as RFC
  3339.
- **Transactions are a closure** (○): `db.tx(|tx| { … })`, commit on
  `Ok`, rollback on `Err` or panic — with `errdefer` as the primitive
  underneath.
- **No `QueryContext` zoo.** Queries are cancellation points, so there
  is one method name each.
- Today: SQLite only, via **pure-Go `modernc.org/sqlite`** — the
  interpreter's only third-party dependency, chosen so that
  cross-compilation stays a `GOOS` variable. Rows are maps; typed rows
  are ○.
- **Known bug at this tier:** a failing `db.exec`/`db.query` panics
  with an internal `cancelUnwind` instead of returning `Err`, because
  the host context is released before the cancellation check.
  Successful queries are unaffected.
- **Never interpolate into a query.** This is the one place where the
  discipline is entirely yours.

**Exercises**

1. **Break `rows.Scan`.** In a Go codebase, find a `rows.Scan` with
   three or more columns of the same type. Reorder two columns in the
   SELECT and confirm it compiles, runs, and produces wrong data. Then
   note that name-based mapping makes the same edit a no-op — that
   difference is why positional scanning is on the fixed list.

2. **Find the placeholder bug.** Write a query with four `:name`
   placeholders and pass three parameters. Confirm the error names the
   missing one. Then imagine the same mistake with `?` placeholders,
   where the error is `expected 4 arguments, got 3` with no indication
   of *which*. Estimate how often you have seen the second version in
   production.

3. **Design the schema-checking boundary.** `DESIGN.md` rejects
   `sqlx`-style live validation and points at `sqlc`-style committed
   codegen. Sketch the workflow: where does the schema live, what
   command runs, what gets committed, and what happens when the
   database and the committed schema diverge? Then decide which failure
   you would rather have — a build that needs a database, or a
   committed artifact that can go stale.
