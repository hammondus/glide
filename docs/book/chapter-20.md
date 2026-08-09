# Chapter 20: Errors as Values

Glide's error philosophy is Go's — errors are ordinary values, visible
in signatures, with no invisible control flow — with the boilerplate
removed and the failure modes made enumerable.

`DESIGN.md` puts the diagnosis crisply: **Go and Rust each shipped half
the answer.** Rust gave `Result` and no default error type, and spent
eight years watching its ecosystem churn through `error-chain` →
`failure` → `anyhow`/`thiserror`. Go gave only the dynamic half, so
failure modes can never be enumerated and `errors.Is` is pattern
matching rebuilt from pointer comparisons.

Glide ships both halves in the standard library on day one.

All of this chapter is ✓ except error return traces. The dynamic
`Error` type is constructible, boxed, and carries four methods —
`message`, `cause`, `context` and `find`. `find` takes the type **as a
value** (`e.find(ConfigError)`), superseding the `find<T>()` spelling
`DESIGN.md` originally sketched; Glide has no turbofish and this was
not the feature to invent one for.

---

### 1. Basic Usage

#### `Result<T, E>`

A two-variant sum type: `Ok(T)` carrying a success, or `Err(E)`
carrying a failure.

```glide
fn read_config(path: String) -> Result<Config, ConfigError>
```

A function that can fail **says so in its return type**. There are no
exceptions. Nothing propagates invisibly. Every failure path is in a
signature.

Coming from Go, this is the `(Config, error)` return pair fused into
one value that cannot hold both. Go hands you both slots and trusts you
to check `err` before touching the config; nothing stops the code that
forgets. A `Result` makes the either-or physical: there is no config to
touch until you have gone through the check.

#### `?` — propagate

```glide
let text = fs.read_string(path)?
```

On success, the expression produces the inner value and execution
continues. On failure, the function **returns immediately**, handing
the error to its caller.

It is Go's `if err != nil { return err }` in one character. The early
exit is still visible — on the line where it happens — but it no longer
dominates the page:

```go
text, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("reading input: %w", err)
}
```

```glide
let text = fs.read_string(path).context("reading input")?
```

A skim of any function shows every line that can exit early: look for
`?`.

#### `.context(…)` — add a breadcrumb

```glide
fs.read_string(path).context("loading config")?
```

Wraps an `Err` with a message on its way out and passes `Ok` through
untouched. The chain renders as a trail:

```glide
fn load(p: String) -> Result<String, Error> {
    fs.read_string(p).context("loading config")?
}

fn outer() -> Result<String, Error> {
    load("/nope").context("startup")
}
```

```
startup: loading config: open /nope: no such file or directory
```

A deep failure surfaces with its route attached. Compare Go's `%w`,
which gets these semantics right with the worst possible syntax —
wrapping controlled by a format verb.

#### Sum-type errors

Because errors are ordinary values, an error type is an ordinary sum
type — and this is sum types' best use case:

```glide
type AppError =
    NotFound{ path: String }
    | Parse{ line: Int, why: String }
    | Io{ cause: Error }
```

The signature now documents exactly what can go wrong, and `match` is
exhaustive:

```glide
fn describe(r: Result<Int, AppError>) -> String {
    match r {
        Ok(n)                   => "port {n}"
        Err(NotFound{ path })   => "no file {path}"
        Err(Parse{ line, why }) => "line {line}: {why}"
        Err(Io{ cause })        => "io: {cause}"
    }
}
```

No sentinel error values. No `errors.Is` or `errors.As`. Adding a
variant breaks every caller that pattern-matches, at compile time.

#### `?` converts error types

This is the piece that makes sum-type errors practical.

When `?` propagates an error out of a function whose declared error
type is `E`, and the error is not already an `E`, the compiler calls
`E.from(err)` to convert it:

```glide
impl AppError {
    fn from(e: Error) -> AppError { Io{ cause: e } }
}

fn read_config(path: String) -> Result<Int, AppError> {
    let text = fs.read_string(path)?        // Error → AppError, automatically
    let Some(n) = text.trim().parse_int() else {
        return Err(.Parse{ line: 1, why: "not a number" })
    }
    Ok(n)
}
```

```glide
println(describe(read_config("/nonexistent")))
```

```
io: open /nonexistent: no such file or directory
```

`fs.read_string` returns `Result<String, Error>`; `read_config` returns
`Result<Int, AppError>`; the `?` bridged them by calling
`AppError.from`.

Rules: declare `fn from(e: …) -> E` in `impl E`. If there is no `from`,
the error propagates untouched. **Closures never convert**, because
they have no declared return type.

This is **the one implicit conversion in the language**, and it fires
only at `?`.

#### The dynamic `Error`

`Error` is the type application code returns. **Anything is assignable
to it**, which is what makes this need no ceremony:

```glide-run
fn port(raw: String) -> Result<Int, Error> {
    let Some(n) = raw.parse_int() else {
        return Err("{raw} is not a number")
    }
    if n < 1 || n > 65535 {
        return Err("port {n} out of range")
    }
    Ok(n)
}

fn main() {
    println(port("http"))
    println(port("70000"))
    println(port("8080"))
}
```

```
Err(http is not a number)
Err(port 70000 out of range)
Ok(8080)
```

That is the *type*-level erasure: no `from`, no wrapper type, no
conversion. It is also why `?` propagates any callee's error into an
`Error` slot for free.

At the **value** level, `Error` is a real boxed value with four
methods:

| Method | Signature | Notes |
|---|---|---|
| `message()` | `-> String` | **this link only** |
| `cause()` | `-> Error?` | the next link, `None` at the end |
| `context(msg)` | `(String) -> Error` | a new Error wrapping this one |
| `find(SomeType)` | `(type) -> SomeType?` | walks the **whole** chain for a concrete typed error |

```glide-run
type PortErr = NotANumber(String) | OutOfRange(Int)

fn port(raw: String) -> Result<Int, PortErr> {
    let Some(n) = raw.parse_int() else { return Err(.NotANumber(raw)) }
    if n < 1 || n > 65535 { return Err(.OutOfRange(n)) }
    Ok(n)
}

fn app(raw: String) -> Result<Int, Error> {
    port(raw).context("reading $PORT")
}

fn main() {
    match app("http") {
        Ok(n)  => println("port {n}")
        Err(e) => {
            println("{e}")                  // the whole chain
            println(e.message())            // this link only
            println(e.cause())              // the next link
            match e.find(PortErr) {
                Some(NotANumber(s)) => println("not a number: {s}")
                Some(OutOfRange(n)) => println("out of range: {n}")
                None                => println("something else")
            }
        }
    }
}
```

```
reading $PORT: NotANumber("http")
reading $PORT
Some(NotANumber("http"))
not a number: http
```

Three things to take from that.

**`message()` is this link only, and `"{e}"` is the whole chain.** A
`message()` that returned the chain would leave no way to get just this
one; interpolation already renders the trail.

**`find` walks the whole cause chain** and takes the type as an
ordinary value, because `e.find<T>()` cannot be parsed — it reads as a
field access followed by `<`. Only *declared* types are accepted;
`find(String)` would be a backdoor to a message `message()` already
gives properly.

**A variant pattern cannot match an `Error`.** The concrete error is
inside the box, so this is a compile error rather than a silently dead
arm:

```
app.gld:14:13: NotANumber cannot be matched against an Error: it is the
dynamic error type, so the concrete error is inside it — recover it
with e.find(PortErr)
```

Needing `find` deep in application code is a smell that a boundary
should have been typed. Type your library's failure modes as a sum
type and let `?` box them at the application edge.

#### `??` — fall back, discarding the error

```glide
println(read_config("/nonexistent") ?? -1)      // -1
```

`??` on a `Result` unwraps `Ok` and takes the default on `Err`. The
error is discarded — **deliberately**. Use it when you genuinely do not
care why.

#### `match` — handle in place

When you want to inspect the error rather than propagate or default:

```glide
match load_config(path) {
    Ok(c)  => c
    Err(e) => {
        eprintln("config failed ({e}); using defaults")
        Config.default()
    }
}
```

#### `?` in `main`

```glide
fn main() -> Result<(), Error> {
    let _ = fs.read_string("/nope")?
    Ok(())
}
```

```
error: open /nope: no such file or directory
$ echo $?
1
```

The runtime turns `Ok(())` into exit code 0 and an `Err` into the error
on stderr plus exit 1. Without this, every CLI would open with a
ceremonial `run()` wrapper.

#### The two error styles

`DESIGN.md` draws the line explicitly:

- **Libraries define sum-type errors.** The signature documents what
  can go wrong; callers can `match` and behave differently per case.
- **Applications use the stdlib dynamic `Error`.** It holds any
  concrete error plus a context chain. `fn run() -> Result<(), Error>`
  and everything flows.

`?`-conversion is what lets the two coexist: a library's `ApiError`
converts into an application's `Error` for free.

#### Panics are not errors

Out-of-bounds indexing, a broken invariant, division of `MinInt` by
`-1`: things a correct program never does. They are not control flow,
they are not caught in ordinary code, and APIs never use them to report
expected failures. Chapter 21 covers them.

---

### 2. Under the Hood

#### `Result` is not magic

`Result<T, E>` is an ordinary two-variant sum type that happens to be
built in, so the whole ecosystem agrees on one. `Ok` and `Err` are
reserved constructor names. Everything you know about matching sum
types applies.

In the designed compiler it compiles to a tagged union — a
discriminant plus the larger of `T` and `E`. There is no allocation and
no unwinding machinery. A function returning a `Result` returns a
small value in registers.

That is the performance story in one line: **error handling costs a
branch.** Exceptions cost a table lookup and a stack unwind on the
error path and (in zero-cost implementations) nothing on the happy
path; the trade is that exceptions are invisible.

#### How `?` is implemented

`expr?` desugars to roughly:

```glide
match expr {
    Ok(v)  => v
    Err(e) => return Err(E.from(e))     // or Err(e) if no `from` exists
}
```

In the interpreter, `?` produces a signal value that threads up through
the evaluator — the same mechanism as `return`. `glide/DESIGN-
DECISIONS.md` records the distinction: `return` and `?` are
*semantics*, so they are signals; runtime errors are *diagnostics*, so
they panic and are recovered once in `Run`.

The conversion lookup is static: propagating an error the target type
cannot accept is a compile error naming both types and the missing
`from`. One case stays in the evaluator — `?` into a
`Result<_, Error>`, where the expression belongs to the callee while
the expectation is the enclosing function's return type — and that
path boxes rather than converting, since `Error` needs no `from`.

#### Why the conversion is safe

It is the one implicit conversion, and it is fenced three ways: it
fires only at `?`, only when the target error type declares `from`, and
only in a function with a declared return type. Closures are excluded
precisely because they have no declared return type, so there would be
nothing to convert *to*.

#### Error return traces ○

Zig's genuine novelty, adopted: record the chain of `?` propagation
points an error travelled through, not just where it was created.
Dev-tier only, riding the same machinery as backtraces.

This is the answer to the standard complaint that errors-as-values lose
the stack trace. You get the *propagation* trace, which is arguably
more useful — it shows the path the error took through your code, not
the call stack at the moment of creation.

#### Backtraces are tiered

Captured in dev builds, skipped in release. The tiered-backend design
paying rent again, as with overflow and hygiene.

---

### 3. Why This Design?

#### Why not exceptions

Three reasons, in order of weight.

**Invisible control flow.** Any call might unwind. You cannot tell from
a signature, you cannot tell from the call site, and you cannot
enumerate what might come out. Reasoning about resource cleanup becomes
"which of these forty lines can throw?"

**Easy to ignore.** An uncaught exception propagates by default. An
unhandled `Result` is a value sitting there — and in the designed
toolchain, an unused `Result` is a hygiene error at `glide test`.

**Java's checked exceptions are the proof by counterexample.** They
tried to make failures visible in signatures. It worked, and the
ceremony (declare-or-catch, exception signatures polluting every
interface, versioning pain when a method adds a throws clause) drove
people to `catch (Exception e) {}` and unchecked exceptions. A
`Result` in the return type gets the visibility without the parallel
declaration channel.

#### Why `?` and not Go's `if err != nil`

Go's explicitness is right. Its ergonomics are not, and the cost is
measurable in three ways.

**Volume.** Four lines per fallible call. In a function with five
calls, twenty of the thirty lines are error plumbing, and the actual
logic is scattered among them.

**The eye slides off it.** When every fourth line is the same four
lines, you stop reading them, and the one that returns the wrong
variable or forgets to return at all survives review. Every Go
programmer has shipped `if err != nil { return nil }`.

**It discourages wrapping.** Adding context means `fmt.Errorf("...:
%w", err)`, which makes the block five lines, so people skip it, so
errors arrive at the top with no route attached.

`?` costs one character, is visible on the line where the exit happens,
and composes with `.context(…)` in the same expression. The
explicitness is preserved; the ceremony is not.

#### Why sum-type errors instead of a single dynamic error

Because "what can go wrong here" is exactly the question a signature
should answer, and Go's `error` interface cannot answer it.

In Go, `func Get(id int) (*Note, error)` tells you nothing about the
failure modes. To handle "not found" differently from "database down",
you need `errors.Is(err, ErrNotFound)` against a sentinel the library
had to export, or `errors.As` with a concrete type — pattern matching
rebuilt from pointer comparison and downcasting, with no compiler help
and no exhaustiveness.

```glide
type ApiError =
    NotFound{ id: NoteId }
    | BadInput{ msg: String }
    | Db{ cause: Error }

fn get_note(id: NoteId) -> Result<Note, ApiError>
```

Now the caller matches, the compiler checks coverage, and adding a
fourth failure mode breaks every caller that needs to know.

#### Why the dynamic `Error` also exists

Because Rust's experiment says it must.

Rust shipped `Result` with no default error type, so every application
had to choose or invent one. The ecosystem churned for eight years
before settling on the `anyhow` (applications) / `thiserror`
(libraries) split — which is exactly the split `DESIGN.md` bakes into
the standard library on day one.

The insight is that libraries and applications want different things. A
library's callers need to *distinguish* failures, so enumerate them. An
application's `main` needs to *report* failures, so a chain of context
strings is the right shape and enumerating them is pointless work.

#### Why `?`-conversion is worth an implicit conversion

Without it, every `?` crossing an error-type boundary needs a
`.map_err(…)`:

```rust
let text = fs::read_string(path).map_err(AppError::Io)?;
```

which is `if err != nil` reincarnated — mechanical noise at every call
site. Rust tried life without automatic conversion before the `From`
impl mechanism matured, and nobody would go back.

The fences (only at `?`, only with a declared `from`, never in
closures) keep it from becoming a general implicit-conversion system.

#### Why `or |e| { … }` was declined

`GRAMMAR.md`'s original sketch proposed a handle-in-place construct:

```glide
let cfg = load_config() or |e| { default_config() }
```

`DESIGN.md` fought it and declined it, in favour of its parts:

- Wrap-and-propagate — the flagship use — is `?`-conversion.
- Fallback is `??`.
- Inline error inspection is `match`.

And a decisive technical objection: **the pipes lied.** `return` inside
the proposed block would return from the *enclosing function*, which a
closure's `return` cannot do. A construct that impersonates a closure
but is not one starts life owing an explanation.

It is deferred with a test rather than killed. The open question in
`DESIGN.md`: writing real Glide, count the sites where none of the
three reads well. Frequent and ugly → ratify, probably with Zig's
`catch` spelling rather than pipes. Rare → the deferral becomes
permanent.

You will not find `or |e|` anywhere in this book, because it does not
exist. If you find yourself wanting it while writing Glide, that
observation is data — record it.

---

### 4. Competing Approaches

**Go.** `(T, error)` pairs, the `error` interface, `errors.Is`/`As`,
`%w` wrapping. Same philosophy, half the mechanism. What Go gets right:
errors are values, visible in signatures, no invisible control flow.
What it lacks: enumerable failure modes, a propagation operator,
compile-checked handling, and a way to stop the caller touching the
result before checking the error.

**Rust.** `Result<T, E>`, `?`, `From` conversion, no default error
type. Glide is Rust's mechanism plus the standard-library `Error` that
Rust's ecosystem eventually built for itself. Glide also declines
Rust's combinator surface (`map_err`, `and_then`, `ok_or_else`,
`unwrap_or_default`, …) in favour of three constructs plus `match`.

**Zig.** Error unions (`!T`), `try`, `catch`, error sets that the
compiler infers and can enumerate, and `errdefer`. Zig's inferred error
sets are arguably better than declared sum types for ergonomics and
worse for API stability (the set changes when the body changes). Glide
takes `errdefer` (Chapter 22) and error return traces from Zig.

**Java.** Checked exceptions — the right goal, the wrong mechanism. The
declare-or-catch obligation propagates through every signature, adding
a method's exception is a breaking change, and the escape hatches
(`RuntimeException`, `catch (Exception e)`) get used because the
ceremony is too high.

**C#, Python, Ruby, JavaScript.** Unchecked exceptions. Concise, and
you cannot tell from a signature what might come out. JavaScript
additionally has promise rejections as a parallel channel with its own
`.catch`, and `async`/`await` reintroduces `try`/`catch` on top —
two error mechanisms in one language.

**C.** Integer error codes, `errno`, and the caller's discipline. The
baseline everything else improves on, and the source of the
"return value or error, never both" confusion that `Result` fixes.

**Haskell.** `Either e a` with monadic composition, which is `Result`
with `?` generalised into `do` notation. More powerful, and requires
understanding monads.

---

### 5. Common Mistakes

**Using `??` where you meant `match`.**

```glide
// Bad — you will never know why this failed
let cfg = load_config(path) ?? Config.default()

// Good, if you would want to know
let cfg = match load_config(path) {
    Ok(c)  => c
    Err(e) => {
        eprintln("config failed ({e}); using defaults")
        Config.default()
    }
}
```

`??` discards the error deliberately. That is a feature when the error
is genuinely uninteresting and a bug when it is not.

**Forgetting `?` and letting the `Result` sit there.**

```glide
// Bad — the tail-value rule catches this one, at least
fn setup(db: Db) {
    db.exec("create table …")
}
```

```
error: setup declares no return value but its body ends with a Result;
       discard it with `_ = …` or declare `-> Result<…>`
```

Either propagate (`?`), discard visibly (`_ =`), or handle. The
language will not let you do nothing.

**Returning `Result<T, Error>` from a library.** Use a sum type. The
dynamic `Error` is for applications, where the caller is `main` and
reporting is the only response.

```glide
// Bad, in a library
fn parse(s: String) -> Result<Ast, Error>

// Good
type ParseError =
    Unexpected{ line: Int, found: String }
    | Unterminated{ line: Int, what: String }
    | Empty

fn parse(s: String) -> Result<Ast, ParseError>
```

**Not writing `from`.** Without it, `?` cannot bridge error types and
the error propagates untouched — which today silently produces a
`Result` whose error type does not match the signature. Write the
conversion:

```glide
impl AppError {
    fn from(e: Error) -> AppError { Io{ cause: e } }
}
```

**Using `?` inside a closure and expecting it to exit the enclosing
function.** It exits the closure. This is the same rule as `return`
(Chapter 8), and it is why iterator adapters cannot propagate errors:

```glide
// Bad — the ? returns from the closure
let results = paths.iter().map(|p| fs.read_string(p)?).collect()

// Good — a loop can propagate
let mut results = []
for p in paths {
    results.push(fs.read_string(p)?)
}
```

**Stacking `.context` on every call.** Context is for the *boundary*
where a reader would lose track, not for every frame. Three levels of
"reading config: opening file: opening file: no such file" is noise.

**Treating a panic as an error.** Indexing out of range, dividing by
zero, and a failed invariant are bugs. Do not design an API that
panics for an expected condition, and do not try to catch one — there
is no `recover`.

**Expecting `?` to work on an Option.** Not adopted (Chapter 14). Use
`??`, `if let`, or `let … else`.

---

### 6. Performance Considerations

**A `Result` is a tagged union** (○) — a discriminant plus the larger
of `T` and `E`, returned in registers when small. There is no
allocation on either path.

**`?` compiles to a branch.** Test the tag, and either continue or
return. On the happy path that is one well-predicted branch.

**Compare exceptions.** Zero-cost exception implementations (C++,
Rust's panics) put nothing on the happy path and pay a table lookup
plus a stack unwind on the error path. So exceptions are *cheaper* on
the happy path and much more expensive when thrown.

The trade Glide makes: one predicted branch per fallible call, in
exchange for visibility. For the error rates real programs have — where
"file not found" is normal — the branch is the right side of the trade.

**`.context(…)` allocates** — it builds a wrapped error with a message.
On the happy path it does nothing (it passes `Ok` through), so the cost
is on the failure path only, which is where you can afford it.

**Sum-type errors are as large as their largest variant.** An error
enum with one variant holding a large struct makes every `Result`
returning that error type large. Box the outlier.

**Backtraces are dev-tier only.** Capturing one is expensive
(hundreds of nanoseconds to microseconds); release builds skip it.

**In the interpreter**, `?` is a signal value threading up through the
evaluator, which is cheap relative to everything else the tree-walker
does.

---

### 7. Best Practices

**Enumerate failure modes at library boundaries; use the dynamic
`Error` inside applications.**

```glide
// A library module
type StoreError =
    NotFound{ id: NoteId }
    | Conflict{ id: NoteId }
    | Backend{ cause: Error }

pub fn get(db: Db, id: NoteId) -> Result<Note, StoreError>
```

```glide
// The application
fn main() -> Result<(), Error> {
    let db = sql.open(dsn)?
    let note = store.get(db, NoteId(7))?      // StoreError → Error via from
    println("{note:?}")
    Ok(())
}
```

**Write `from` for every error type that wraps another.** It is three
lines and it is what makes `?` work across the boundary:

```glide
impl ApiError {
    fn from(e: Error) -> ApiError { Db{ cause: e } }
}
```

**Add context at boundaries, not at every frame.**

```glide
// Good — one breadcrumb per meaningful layer
fn load_user(db: Db, id: UserId) -> Result<User, Error> {
    let row = db.query_one(sql, ["id": id]).context("loading user {id}")?
    …
}
```

The message should name *what you were trying to do*, not repeat the
function name.

**Keep `?` on the line where the risk is.** Do not hoist a fallible
call into a variable just to `?` it later — the whole point is that the
exit is visible where it happens.

```glide
// Bad
let result = fs.read_string(path)
let text = result?

// Good
let text = fs.read_string(path)?
```

**Reserve panics for bugs.** If a caller could reasonably encounter the
condition, it is a `Result`. If it means your code is wrong, it is a
panic.

| Condition | Result or panic |
|---|---|
| File not found | `Result` |
| Malformed input | `Result` |
| Network timeout | `Result` |
| Index out of range | panic |
| Broken invariant inside your own type | panic |
| Unreachable match arm | panic |

**Do not swallow errors in `defer`.** The discard must be visible
(`_ =`), which the language enforces — Chapter 22.

**Prefer `let … else` over `match` for the abort case.**

```glide
// Good
let Ok(cfg) = load_config(path) else {
    return Err(.Startup{ why: "no config" })
}
```

---

### 8. Examples

**A complete error-handling flow, built up in stages:**

```glide-run
import fs

// Stage 1 — the failure modes, enumerated.
type AppError =
    NotFound{ path: String }
    | Parse{ line: Int, why: String }
    | Io{ cause: Error }

// Stage 2 — the conversion that makes `?` work across boundaries.
impl AppError {
    fn from(e: Error) -> AppError { Io{ cause: e } }
}

// Stage 3 — a fallible function. Note the single `?`.
fn read_port(path: String) -> Result<Int, AppError> {
    let text = fs.read_string(path)?
    let Some(n) = text.trim().parse_int() else {
        return Err(.Parse{ line: 1, why: "not a number" })
    }
    if n < 1 || n > 65535 {
        return Err(.Parse{ line: 1, why: "out of range" })
    }
    Ok(n)
}

// Stage 4 — exhaustive handling at the boundary.
fn describe(r: Result<Int, AppError>) -> String {
    match r {
        Ok(n)                   => "port {n}"
        Err(NotFound{ path })   => "no file {path}"
        Err(Parse{ line, why }) => "line {line}: {why}"
        Err(Io{ cause })        => "io: {cause}"
    }
}

fn main() {
    println(describe(read_port("/nonexistent")))
    println(read_port("/nonexistent") ?? -1)
}
```

```
io: open /nonexistent: no such file or directory
-1
```

The `?` on line one of `read_port` did three things: unwrapped the
success, returned early on failure, and converted `Error` into
`AppError` via `from`. In Go that is four lines plus a `fmt.Errorf`
wrap; in Rust before `From` matured it was a `.map_err`.

**The context chain:**

```glide
import fs

fn load(p: String) -> Result<String, Error> {
    fs.read_string(p).context("loading config")?
}

fn startup() -> Result<String, Error> {
    load("/nope").context("startup")
}

fn main() {
    match startup() {
        Ok(s)  => println(s)
        Err(e) => println("{e}")
    }
}
```

```
startup: loading config: open /nope: no such file or directory
```

Three layers, three breadcrumbs, read outside-in. That message tells
you what the program was doing and what actually failed — which is what
you want at 3am and what a bare "no such file or directory" does not
give you.

**Side by side with Go:**

```go
// Go
func readPort(path string) (int, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0, fmt.Errorf("reading %s: %w", path, err)
    }
    n, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        return 0, fmt.Errorf("parsing port: %w", err)
    }
    if n < 1 || n > 65535 {
        return 0, fmt.Errorf("port out of range: %d", n)
    }
    return n, nil
}
```

```glide
// Glide
fn read_port(path: String) -> Result<Int, AppError> {
    let text = fs.read_string(path).context("reading {path}")?
    let Some(n) = text.trim().parse_int() else {
        return Err(.Parse{ line: 1, why: "not a number" })
    }
    if n < 1 || n > 65535 {
        return Err(.Parse{ line: 1, why: "out of range" })
    }
    Ok(n)
}
```

Fifteen lines to eight. But count the *decisions* rather than the
lines: the Go version returns `0, err` three times, and each `0` is a
value the caller must know not to trust. The Glide version cannot
produce a port and an error simultaneously.

And the caller's side is where the real difference shows. The Go
caller gets an `error` and can distinguish cases only by
`errors.Is`/`As` against types the library remembered to export. The
Glide caller gets a three-variant sum type and a compiler that checks
they handled all three.

**Bad versus good: the swallowed error**

```glide
// Bad — three ways to lose information, all in four lines
fn sync(db: Db) -> Int {
    let a = db.exec("delete from stale") ?? 0        // error gone
    let b = db.exec("vacuum") ?? 0                   // error gone
    _ = db.close()                                   // error gone
    a + b
}
```

Nothing here is *illegal* — Glide makes each discard visible, which is
the point — but a reader can see three places where a failure vanishes
and the function still returns a plausible number.

```glide
// Good
fn sync(db: Db) -> Result<Int, Error> {
    let a = db.exec("delete from stale").context("clearing stale rows")?
    let b = db.exec("vacuum").context("vacuuming")?
    db.close()?
    Ok(a + b)
}
```

The signature now says the operation can fail, each failure carries
where it happened, and the caller cannot use the count without dealing
with the possibility.

---

### 9. Summary & Exercises

**Summary**

- `Result<T, E>` is a two-variant sum type: `Ok(T)` or `Err(E)`. A
  function that can fail says so in its return type. There are no
  exceptions and no invisible control flow.
- **`?` propagates**: unwrap the success, or return the failure to the
  caller. It is Go's four-line `if err != nil` in one character, with
  the early exit still visible on the line where it happens.
- **`.context(…)`** wraps an `Err` with a breadcrumb and passes `Ok`
  through, producing a readable outside-in chain.
- **`?` converts error types** by calling `E.from(err)` when the target
  type declares it. This is the one implicit conversion in the
  language, and it fires only at `?`, only with a declared `from`, and
  never in closures.
- **Libraries define sum-type errors** so callers can distinguish
  failures exhaustively; **applications use the dynamic `Error`** with
  its context chain. `?`-conversion bridges them. This is the
  `thiserror`/`anyhow` split, shipped in the standard library.
- **`??` on a Result** unwraps `Ok` and takes the default on `Err`,
  discarding the error deliberately.
- `match` handles in place when you need to inspect the error.
- **`main` may return `Result<(), Error>`** — the runtime prints the
  error to stderr and exits 1.
- **Panics are for bugs only** and cannot be caught (Chapter 21).
- `or |e| { … }` was **declined** in favour of `?`-conversion, `??`,
  and `match`, and is deferred with a count-the-residue test.
- **`Error` is erased at the *type* level and boxed at the *value*
  level** ✓. Anything is assignable to it, so `Err("config is empty")`
  needs no ceremony — but what lands in an `Error` slot *becomes* an
  `Error`, so a program-made error and a host one are the same kind of
  thing and print alike.
- **`Error` has four methods** ✓: `message()` (this link only),
  `cause()`, `context(msg)`, and `find(SomeType)` which walks the whole
  chain. `find` takes the type **as a value**, because `find<T>()`
  cannot be parsed and Glide has no turbofish.
- **A variant pattern cannot match an `Error`** — a reported error
  naming `find`, never a silently dead arm.
- ○: dev-tier error return traces.

**Exercises**

1. **Count the plumbing.** Take a 100-line Go function that does real
   I/O and count the lines that are error handling. Then translate it
   and count again. Separately, count the number of times the Go
   version returns a zero value alongside an error — each one is a
   value the caller must know not to touch.

2. **Enumerate a library's failures.** Pick a library you use whose
   errors are opaque (a `string` or a bare `error`). Write down every
   distinct thing that can go wrong, then write the sum type. Now ask:
   of those, how many would a caller genuinely handle differently? That
   number is the right size for the enum, and anything beyond it
   belongs inside a `Backend{ cause }` variant.

3. **Run the or-block residue test.** `DESIGN.md` has an open question:
   `or |e| { … }` was declined, and the test is to write real Glide and
   count the sites where none of `?`-conversion, `??`, and `match`
   reads well. Write 200 lines of error-heavy Glide and keep a tally.
   If your count is high and the sites are ugly, that is evidence for
   reviving the construct — and it is exactly the kind of evidence the
   design document asks for.
