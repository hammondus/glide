# Appendix B: Glide for Go Programmers

Glide is a Go-tradition language: same runtime model, same tooling
philosophy, same deployment story. Most of your instincts transfer.
This appendix is about the ones that do not.

---

## The translation table

| Go | Glide | Notes |
|---|---|---|
| `func f(a int) int` | `fn f(a: Int) -> Int` | |
| `var x = 1` / `x := 1` | `let x = 1` | immutable |
| (no equivalent) | `let mut x = 1` | mutable |
| `const X = 1` | `const x = 1` | `snake_case`; comptime-evaluated (○) |
| `return x` at the tail | `x` | tail expression is the value |
| `if c { } else { }` (statement) | same, **and** an expression | `let s = if c { a } else { b }` |
| `switch` | `match` | exhaustive on sum types |
| `switch { case c: }` | `match { c => … }` | subjectless |
| `for i := 0; i < n; i++` | `for i in 0..n` | no three-clause form |
| `for cond { }` | same | |
| `for { }` | same | |
| `for i, v := range xs` | `for (i, v) in xs.iter().enumerate()` | |
| `for k, v := range m` | `for (k, v) in m` | **insertion order** |
| `[]T` | `List<T>` | no array/slice split |
| `map[K]V` | `Map<K, V>` | ordered; `m[k]` returns `V?` |
| `v, ok := m[k]` | `m[k]` → `V?` | use `??`, `if let`, `let … else` |
| `nil` | **does not exist** | `T?` |
| zero values | **do not exist** | mandatory initialisation |
| `(T, error)` | `Result<T, E>` | one value, not two |
| `if err != nil { return err }` | `?` | |
| `fmt.Errorf("…: %w", err)` | `.context("…")` | |
| `errors.Is` / `errors.As` | `match` on an error sum type | |
| `panic` / `recover` | panic; **no `recover`** | permanently |
| `interface{}` / `any` | **does not exist** | sum types, generics, `any Trait` (○) |
| implicit interface satisfaction | `impl Trait for Type` | declared, one line |
| struct embedding | **does not exist** | write the delegating method |
| `func (v T) M()` / `func (p *T) M()` | `fn m(self)` / `fn m(mut self)` | mutability, not passing |
| `go f()` | `s.spawn(\|\| f())` inside a `scope` | no ambient spawn |
| `sync.WaitGroup` | scope exit joins | |
| `errgroup` | `a.join()?` composition | |
| `context.Context` | **does not exist** | `scope(timeout:)` |
| `ctx.Done()` in a select | **no such arm** | scopes cancel blocked selects |
| `make(chan T)` | `let (tx, rx) = channel()` | split halves |
| `close(ch)` | `tx.close()` | idempotent; receiver has none |
| `v, ok := <-ch` | `rx.recv()` → `Option<T>` | |
| `select { case … }` | `select { pat = rx.recv() => … }` | plus arm guards |
| `defer f()` | `defer { f() }` | **block-scoped** |
| (the `committed := false` idiom) | `errdefer { … }` | |
| `fmt.Printf("%d", n)` | `"{n}"` | no printf |
| `%v` | `{x}` Display / `{x:?}` Debug | separate traits |
| `strings.Builder` | `StringBuilder` (○) | |
| `s[i]` on a string | **does not exist** | `bytes()` / `runes()` |
| `Exported` / `unexported` | `pub` / default | |
| `init()` | **does not exist** | nothing before `main` |
| `import _ "driver"` | **does not exist** | imports are inert |
| `//go:embed` | `embed "…" as x` (○) | grammar, not a comment |
| struct tags | `derive(Json(…))` (○) | typed comptime args |
| `sql.NullString` | `String?` | |
| `rows.Scan(&a, &b)` | `derive Row` (○), by name | |
| `?`/`$1` placeholders | `:name` everywhere | |
| `func TestX(t *testing.T)` | `test "name" { }` | a language construct |
| `t.Errorf` / testify | `expect(a == b)` | compiler-known |
| `b.N` | `bench "name" { }` (○) | runner owns the loop |
| `/* */` | **does not exist** | `//` only |
| `;` | **does not exist** | newline rules |
| `++` | `+= 1` | |
| `goto` | **does not exist** | labeled break survives |

---

## The ten things that will trip you

### 1. There is no `nil`, and no zero value

The biggest adjustment. `User{}` is unwritable; a struct literal
accounts for every field. A map read returns `V?`, not a zero. A
"maybe absent" value is `T?` and the compiler makes you handle it.

The corollary people miss: **`fn owner() -> User` now means
something** — you do not defensively check.

### 2. Errors are one value, not two

```go
text, err := os.ReadFile(path)
if err != nil { return fmt.Errorf("reading input: %w", err) }
```

```glide
let text = fs.read_string(path).context("reading input")?
```

Go hands you both slots and trusts you to check `err` first. A `Result`
makes the either-or physical: there is no text to touch until you have
gone through the check.

And **enumerate your library's failures** as a sum type
(Chapter 19) — `errors.Is` archaeology has no equivalent because it has
no need.

### 3. `defer` is block-scoped

```go
for _, p := range paths {
    f, _ := os.Open(p)
    defer f.Close()      // Go: every file open until the function returns
}
```

In Glide that runs each iteration. The fd-exhaustion bug is not
reproducible. And `defer` takes a **block**, so Go's
argument-evaluation puzzle cannot be written.

Also: **the discard must be visible** — `defer { _ = db.close() }`.
Go's `defer f.Close()` silently drops the error that reports failed
buffered writes.

### 4. `go` needs a parent

There is no ambient `spawn`. Every task belongs to a scope, scope exit
joins every child, and leaks are unrepresentable.

The corollary that catches people: **scope exit blocks**. A function
that opens a scope does not return until its children finish. And a
*normal* exit joins without cancelling, so a blocked child deadlocks —
use `return` (an early exit) to cancel first. That is Chapter 27's
sharpest edge and you will hit it once.

### 5. `context.Context` is gone, and you should not rebuild it

Cancellation is ambient within a scope. If you find yourself adding a
`timeout: Duration` parameter that spreads through your call graph, you
have rebuilt `ctx` — and worse, passing the same timeout to two
sequential calls doubles the budget.

```glide
scope(timeout: 2.s) {
    let user = load_user(id)?
    let prefs = load_prefs(user)?
    Ok(render(user, prefs))
}
```

Neither function mentions a timeout.

### 6. The functional-options pattern is dead

```glide
fn connect(host: String, port: Int = 5432, tls: Bool = true) -> Conn
connect("db.local", tls: false)
```

Defaults and named arguments cover overloading's legitimate 90%. Do not
transplant the thirty-line options ceremony; it costs a closure and a
list allocation per call and produces a worse call site.

### 7. `switch` becomes `match`, and `_` is expensive

Arms are **line-separated**, not comma-separated. An arm body is a
**single expression** — use a block for several statements.

The important part: a `match` on a sum type is **exhaustive**, and
adding a variant produces a compile-time work list of every place that
needs updating. Every `default:`-shaped `_ =>` arm opts a site out of
that, permanently and silently. Write it only when "anything else" is
genuinely the meaning.

### 8. Interfaces become traits, declared

```glide
impl Reader for TcpConn {}      // existing read() satisfies it
```

One greppable line. No accidental conformance, and "who implements
Reader?" is a text search.

The payoff Go cannot offer: **default methods**, so a trait can grow
without breaking implementors. Go's standard-library interfaces are
fossils for exactly this reason — which is why new capabilities arrive
as new interfaces (`io.ReaderFrom`, `http.Flusher`) that consumers must
type-assert for.

And there is **no embedding**. Write the delegating method; it is three
lines and the reader can see where the call goes.

### 9. Maps iterate in insertion order

Go randomises deliberately, to stop people depending on an order that
was never guaranteed. Glide solves the same problem by giving a
guarantee instead. Golden tests work; JSON round-trips preserve key
order; you never sort keys just to get deterministic output.

### 10. Capitalisation is grammar, not visibility

Capitalised = type/variant/constructor. Lowercase =
binding/function/field/module. Pattern matching depends on it.

Visibility is `pub`, which turns out better anyway: making something
public is a one-line reviewable diff rather than a rename touching
every use site.

---

## What is exactly the same

Worth stating, because it is most of the language:

- **The runtime model.** Green threads, tracing GC, channels, `defer`,
  value structs. Glide lowers onto Go source nearly 1:1 for exactly
  this reason.
- **Directory is a module**, all files sharing one namespace.
- **Declarations are order-independent** at module level; file order is
  narrative order.
- **The newline rule** (extended for leading dots).
- **No implicit numeric conversions.** No truthiness. `Bool`-only
  conditions.
- **Integer division truncates**; `%` takes the sign of the dividend.
- **UTF-8 strings**, `len()` in bytes, iteration yielding runes.
- **Errors are values**, visible in signatures, with no invisible
  control flow.
- **One binary toolchain**, static binaries, trivial
  cross-compilation.
- **Tests live with the code**, one command, no framework.
- **Composition over inheritance**, and no inheritance at all.
- **Small-interface culture**, seeded by one-method stdlib traits.
- **Consumer-defined interfaces** — accept the abstraction, return the
  concrete type — intact.

---

## Habits to keep

- Guard clauses first, happy path unindented.
- Accept interfaces, return structs (now: accept traits, return
  concrete types).
- Small interfaces.
- Errors as values, checked at the call site.
- Explicit over implicit.
- A little copying beats a little dependency — though with generics
  that work, the threshold moves.

## Habits to drop

- Reaching for a global or an `init()`.
- Adding `ctx` to a signature.
- Building a stop channel or a shutdown flag.
- The functional-options pattern.
- A `kind` field with mostly-nil pointers.
- Sentinel returns (`-1`, `""`).
- `defer` at function scope by habit.
- `_ = x` to silence unused (use `_name` at the declaration).
- Capitalising to export.
- Writing `return` at the tail.

---

## A worked translation

```go
// Go
type Note struct {
    ID    int64
    Title string
    Body  *string        // nullable
}

func GetNote(ctx context.Context, db *sql.DB, id int64) (*Note, error) {
    var n Note
    var body sql.NullString
    err := db.QueryRowContext(ctx,
        "select id, title, body from notes where id = $1", id).
        Scan(&n.ID, &n.Title, &body)
    if err == sql.ErrNoRows {
        return nil, nil                   // nil, nil means "not found"
    }
    if err != nil {
        return nil, fmt.Errorf("loading note %d: %w", id, err)
    }
    if body.Valid {
        n.Body = &body.String
    }
    return &n, nil
}
```

```glide
// Glide
type NoteId = distinct Int

type Note = struct {
    pub id: NoteId
    pub title: String
    pub body: String?
}

type StoreError = Backend{ cause: Error }
impl StoreError { fn from(e: Error) -> StoreError { Backend{ cause: e } } }

fn get_note(db: Db, id: NoteId) -> Result<Note?, StoreError> {
    let found = db.query_one(
        "select id, title, body from notes where id = :id",
        ["id": id])?
    match found {
        None    => Ok(None)
        Some(r) => Ok(Some(Note{
            id:    NoteId(r["id"] ?? 0),
            title: r["title"] ?? "",
            body:  r["body"],
        }))
    }
}
```

Seven differences worth naming:

1. **`ctx` is gone.** Queries are cancellation points.
2. **`$1` became `:id`**, the same on every driver.
3. **`sql.NullString` and the `.Valid` dance are gone.** The column is
   nullable, so the field is `String?`.
4. **`*Note` became `Note?`**, so "not found" is a type rather than the
   `nil, nil` convention.
5. **`int64` became `NoteId`**, so it cannot be confused with any other
   ID.
6. **The error type is a sum type** the caller can match on.
7. **Positional `Scan` became name-based mapping**, so reordering the
   SELECT is harmless rather than a silent misassignment.

The Glide version is not much shorter. It is that four distinct classes
of mistake in the Go version are unwritable in it.
