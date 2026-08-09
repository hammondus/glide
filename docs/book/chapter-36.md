# Chapter 36: Effective Glide

Thirty-five chapters have covered what the language *is*. This one is
about what a good Glide program *looks like* — the judgement calls that
no individual feature chapter can settle because they are about
choosing between features.

There is a single organising heuristic, and it comes from Chapter 1's
third pillar:

> **The source code should tell you what it costs.**

Almost every style question in this chapter reduces to that.

---

### 1. Basic Usage

#### Naming

| Kind | Convention | Examples |
|---|---|---|
| Types, variants, constructors | `PascalCase` | `Note`, `NoteId`, `NotFound`, `Some` |
| Bindings, functions, fields, modules | `snake_case` | `note_id`, `read_config`, `http` |
| Constants | `snake_case` — same as any binding | `max_retries`, `default_timeout` |

**The case rule is enforced by the formatter**, and it is grammar
rather than style: pattern matching depends on it (Chapter 3).

There is no SCREAMING_CASE. `DESIGN.md`: it is C-preprocessor scar
tissue, and an earlier evaluation time is not a siren.

#### Naming, beyond case

**Types are nouns; functions are verbs; predicates ask questions.**

```glide
type Connection = struct { … }
fn connect(host: String) -> Result<Connection, Error>
fn is_open(c: Connection) -> Bool
```

**Methods that mutate are verbs; methods that read are nouns.** Because
there is no call-site `mut` marker on receivers (Chapter 16), the
method name is the only signal a reader gets:

```glide
fn len(self) -> Int              // reads
fn is_empty(self) -> Bool        // reads
fn push(mut self, v: T)          // mutates
fn clear(mut self)               // mutates
```

**A past participle means "give me a new one".** `sorted()` returns a
copy; `sort_by()` mutates in place. Rust's convention, and it maps
exactly onto `self` versus `mut self`.

**Traits name capabilities; types name identities.** `Reader`,
`Iterable`, `Ord`, `Display` — what you can *do*. `SqlStore`,
`Circle`, `Config` — what something *is*.

**Do not repeat the module in the name.** `http.get`, not
`http.http_get`. Cross-module calls are already qualified.

#### File and declaration order

**File order is narrative order.** The formatter deliberately does not
reorder declarations, because sequence is the author's storytelling
channel. Put the important thing first; helpers after.

```glide
// Good
fn main() -> Result<(), Error> { … }
fn run(cfg: Config) -> Result<(), Error> { … }
fn load_config(path: String) -> Result<Config, ConfigError> { … }
fn parse_line(s: String) -> Result<Entry, ConfigError> { … }
```

**Split a module across files freely.** Files share one namespace
(Chapter 29), so splitting is editorial and has no API consequence.

#### Signature design

A reader should be able to use a function without reading its body.
That means:

**Precise error types.** `Result<Note, ApiError>` where the failure
modes are known; `Result<Note, Error>` only in application code where
the caller is `main`.

**Optionality in the type.** `User?` when absence is possible; `User`
when it is not — and the second half matters as much as the first
(Chapter 14).

**Distinct types for identifiers.** `fn get(id: NoteId)`, not
`fn get(id: Int)` (Chapter 15).

**Named arguments for anything a reader cannot infer from the value** —
booleans always, and any two parameters of the same type:

```glide
copy(from: src, to: dst)
resize(width: 800, height: 600)
connect("db.local", tls: false)
```

**Defaults for optional configuration**, not a `Config` struct — unless
the configuration travels to more than one function, in which case it
is a struct (Chapter 7).

#### Choosing between constructs

The decision table that this book has been building toward:

| Question | Answer |
|---|---|
| One of N shapes, closed set, want exhaustiveness | **Sum type** (13) |
| One of N shapes, open set, third parties extend | **Trait** (17) |
| Same code for many types | **Generics** (18) |
| Might be absent | **`T?`** (14) |
| Might fail, caller distinguishes causes | **`Result<T, SumError>`** (19) |
| Might fail, caller only reports | **`Result<T, Error>`** (19) |
| Bug, no caller can handle it | **panic** (20) |
| Named identifier, no arithmetic | **`distinct`** (15) |
| Two things, briefly | **Tuple** (11) |
| Three things, or crossing an API boundary | **Struct** (12) |
| Assign once, two branches | **`if` expression** (9) |
| Assign once, many branches | **`match`** (10) |
| Transform a value through stages | **Sequential redeclaration** (4) |
| Accumulate across iterations | **`let mut`, then seal** (4) |
| Absence has a sensible default | **`??`** (14) |
| Absence aborts | **`let … else`** (14) |
| Both cases have code | **`if let`** (14) |
| Transforming a sequence | **Iterator adapters** (23) |
| Doing something to each element | **`for` loop** (23) |
| Writing an iterator | **Generator** (24) |
| Bound a task's lifetime | **`scope`** (25) |
| Bound an operation's time | **`scope(timeout:)`** (26) |
| Stream of values between tasks | **Channel** (27) |
| One value from a task | **`Task` + `join()`** (25) |
| Wait on several channels | **`select`** (28) |
| Cleanup always | **`defer`** (21) |
| Cleanup only on failure | **`errdefer`** (21) |

#### Reading a diff

What a reviewer should look for, in rough order of value:

1. **A new `mut`.** Is it a genuine accumulator, or an
   assign-in-both-branches that wants an expression-`if`?
2. **A new `_ =>` arm.** Has someone just opted out of exhaustiveness?
3. **A new `??` on a `Result`.** An error was discarded — deliberately?
4. **A new `?` on a line that had none.** A new early-exit path.
5. **A widened signature** — `T` becoming `T?`, or a precise error type
   becoming `Error`. Both are work pushed onto every caller.
6. **A `pub` added.** A one-line diff that says "this became public",
   and now it is a contract.
7. **A `spawn` without a visible join or discard.**
8. **An interpolation inside a query string.** The one place discipline
   is entirely manual (Chapter 33).

That list is short *because* the language makes most other changes
either impossible or visible. There is no "did they forget a null
check", no "does this new field break the JSON decoder", no "is this
goroutine leaked".

---

### 2. Under the Hood

#### Why the formatter enforces case

Because pattern matching is ambiguous without it (Chapter 3), and
because "conventions the tool enforces" and "conventions people
remember" have very different adherence rates.

The formatter is a **pure function from AST to bytes** with **no
configuration file format** (Chapter 2). Same code, byte-identical
output, everywhere.

One escape valve, and it is grammatical rather than configurable: **a
trailing comma forces one-element-per-line.** That lets you preserve
structural intent — a matrix-shaped literal, a routing table where each
line is a route — through the grammar rather than through whitespace
the formatter would erase.

#### Why `glide test` is the style gate

Format check, lints, unused-code errors, doc-link validation, and the
race detector all run there, and **none of them is a compile error**.

The reasoning (Chapter 2): a codebase compiling with 400 warnings
trains everyone to ignore number 401, so hygiene must be an error
*somewhere* — but a compiler that yells about whitespace while you
think is hostile, and Go's comment-out-a-line-and-get-a-cascade
experience is the failure mode. Dev builds warn; `glide test` and
release builds error.

#### Documentation is ordinary comments

```glide
// read_config loads and parses the configuration at path.
// It returns Missing when the file does not exist, and Malformed
// with a line number when parsing fails.
pub fn read_config(path: String) -> Result<Config, ConfigError> { … }
```

Four rules, each with a reason:

**No tag language.** `@param`/`@returns` is write-only noise restating
a signature that is already explicit. Prose says what the signature
cannot.

**The first sentence starts with the identifier name.** Load-bearing,
not fussy: it is what makes one-line summaries readable in search,
tooltips, and listings. A lint at test tier.

**Markdown subset from day one** — headings, lists, fences, links. Go
spent twelve years on plain text before conceding in 1.19.

**Checked identifier links.** `[Config]` resolves against real
declarations, and a stale link fails at the test tier. `DESIGN.md` is
blunt about the consequence: "renaming a function can fail
`glide test` over a comment. That's correct."

Examples are Go-style **Example functions** — real compiled,
output-checked code in test files, rendered into docs. Rust's
doc-tests are rejected: runnable code inside comments is code invisible
to the formatter, LSP, refactoring, and grep.

Undocumented `pub` items are a **vet-tier lint, advisory not gating** —
full strictness breeds `// Foo does foo.`, which is worse than no doc
because it occupies the space where a real one would go.

---

### 3. Why This Design?

#### Why `mut` scarcity is the highest-value habit

`mut` is an audit mark, and **an audit mark is only worth anything if
it is rare**.

If 90% of your locals are `let`, then `mut` on a binding is a signal a
reviewer reads. If everything is `mut` by reflex, the marker carries no
information, readers stop seeing it, and the one genuinely stateful
variable in a function gets missed.

This is why Chapter 4 spends so long on the alternatives —
expression-`if`, `match`, sequential redeclaration, block expressions,
build-then-seal. Each one is a way to not spend a `mut`.

The same logic applies to every marker in the language. `_ =` is
meaningful because silent discards do not exist. `?` is scannable
because there is no invisible propagation. `pub` is reviewable because
it is a keyword rather than a capitalisation.

#### Why "parse, don't validate" is the central pattern

Three chapters build to it — private fields (12), `Option` (14), and
`distinct` (15) — and it is the most valuable pattern in the book:

```glide
type Email = struct { raw: String }

impl Email {
    pub fn parse(s: String) -> Email? { … }
    pub fn value(self) -> String { self.raw }
}

fn send(to: Email, body: String) { … }
```

`send` does not validate, because it **cannot receive an invalid
address**. Validation happened once, at the boundary, and the type
carries the proof forward.

Count what disappears: every downstream null check, every "is this
string actually an email" comment, every defensive re-validation, and
the possibility of two call sites validating differently.

It needs three things together — mandatory initialisation so a bare
`Email{}` is unwritable, private fields so `parse` is the only door,
and `Option` so the failure has somewhere to go. Chapter 12's summary
called it "the single most valuable pattern in the chapter", and it is
the single most valuable pattern in the language.

#### Why "maps at the boundary, structs inside"

The same shape, applied to data rather than validation (Chapters 11,
31, 33).

Untrusted or unstructured data arrives as a `Map` — from JSON, from a
config file, from a database row. That is the honest representation of
"text of unknown shape". Then it lives for three lines and becomes a
type.

The alternative — letting the map flow downstream — means every
function knows the schema, restates the defaults, and handles absence.
Two call sites can disagree about the default for a field, and nothing
notices.

#### Why signatures are the documentation

Because Glide spent its budget making signatures carry information:
explicit types, precise errors, `T?` for absence, `distinct` for
identity, `mut` for mutation, default values for optional
configuration, and named arguments at call sites.

A signature that says
`fn get(db: Db, id: NoteId) -> Result<Note?, StoreError>` tells you:
what it needs, that the ID cannot be confused with another ID, that the
note might not exist, that it might fail, and how it might fail.

A signature that says `fn get(db, id)` tells you nothing, and it is
what you get if you skip the annotations because the checker is not
watching yet.

---

### 4. Competing Approaches

**Go.** *Effective Go* plus the community's accumulated conventions:
short receiver names, `err` last, accept interfaces return structs,
don't panic in libraries. Most of it transfers. What does not: Go's
"a little copying is better than a little dependency" applies with less
force when generics work, and Go's naming conventions around
capitalisation do not exist here.

**Rust.** The API guidelines checklist plus clippy. Glide's style is
mostly Rust's, minus lifetime-related conventions, minus the
combinator-heavy style (`ok_or_else`, `map_err`, `and_then`) that
Glide's three-construct approach replaces.

**Python.** PEP 8 (formatting, superseded in practice by Black) and
PEP 20 (the Zen). "Explicit is better than implicit" is Glide's
visibility pillar; "there should be one obvious way to do it" is
`DESIGN.md`'s second principle.

**Java.** Effective Java — still the best book of its kind, and
half of its items are workarounds for language limitations Glide does
not have. "Prefer immutability", "consider a builder", "use Optional
sparingly", "prefer enums to int constants" — each is a chapter here
instead of an item.

---

### 5. Common Mistakes

**Go-in-Glide, Rust-in-Glide, Java-in-Glide.** Chapter 37 is entirely
about these.

**Spending `mut` on assign-once variables.** The single most common
translation artifact.

**Widening a signature to avoid a decision.** Changing `Note` to
`Note?` because one caller might not have one, or `ApiError` to `Error`
because a new failure mode does not fit — both push work onto every
caller, and both are usually a sign the boundary is wrong.

**Adding `_ =>` to silence an exhaustiveness error.** You have
converted a compile-time work list into a silent default.

**Letting a map escape the boundary.** Every downstream function now
knows the schema.

**Validating instead of parsing.** If a function starts with a
precondition check, ask whether the type could make the precondition
unnecessary.

**Over-abstracting early.** A trait with one implementation is a type
with extra steps; a generic parameter that never varies is pure
overhead; a module per type is a namespace, not a boundary.

**Under-abstracting where it is cheap.** A `distinct` type is ten
characters and kills a bug class. A sum type is one line and makes
illegal states unrepresentable. These are not the expensive
abstractions.

**Writing documentation that restates the signature.**

```glide
// Bad
// get_note gets a note.
// Params: db - the database. id - the id.
// Returns: the note, or an error.
pub fn get_note(db: Db, id: NoteId) -> Result<Note?, StoreError>

// Good
// get_note loads a single note. It returns None when no note has
// that id, which is not an error — callers routinely check for
// existence this way.
pub fn get_note(db: Db, id: NoteId) -> Result<Note?, StoreError>
```

Prose says what the signature cannot.

---

### 6. Performance Considerations

**The pricing pillar in one table.** The expensive thing has the longer
name — which means style and performance mostly agree:

| Idiomatic (cheap) | Named alternative (costly, deliberate) |
|---|---|
| `String` interpolation | `StringBuilder` for loops (○) |
| `Int` | `BigInt` (○) |
| Generic bound `<T: Shape>` (static) | `any Shape` (boxed, ○) |
| `for x in xs` | adapter chain (lazy, composable) |
| Immutable + struct update | `mut` accumulation |
| Unbuffered `println` | buffered writer (○) |
| `Duration` literals | — |
| Scope-based cancellation | — |

Two rules of thumb follow:

**Write the idiomatic version first.** It is usually the fast one, and
where it is not, the slow-but-clear version is a better starting point
than a fast-but-wrong one.

**When you do reach for the costly form, the source says so.** That is
the property worth protecting — a reviewer can see the decision.

**Do not tune against the interpreter.** Two orders of magnitude
slower than compiled Go, with a goroutine per generator, an environment
allocation per call and per block, hash lookups for field access, and
one interpreter lock.

---

### 7. Best Practices

The distilled list. Most have appeared in a chapter; the value here is
seeing them together.

**Structure**

1. **Parse, don't validate.** Private fields plus a validating
   constructor, at the boundary, once.
2. **Maps at the boundary, structs inside.**
3. **Make illegal states unrepresentable.** Correlated booleans and
   correlated Options are sum types in disguise.
4. **Create dependencies in `main` and pass them down.** No globals,
   no `init()`, no service locator.
5. **Attach data to the state that owns it.** `Running{ since }`, not
   a `since` field that is meaningless in three of four states.

**Mutability and bindings**

6. **Default to `let`. Earn every `mut`.**
7. **Build with `mut`, then seal** — `let mut acc = …; …; let acc = acc`.
8. **Use sequential redeclaration for refinement pipelines.**
9. **Prefer expression-`if` and `match` to assign-in-every-branch.**

**Types**

10. **Wrap identifiers in `distinct` types.** Ten characters, one bug
    class.
11. **Let the signature carry the information** — `T?` for absence,
    precise error sums, distinct IDs, defaults for optional config.
12. **Do not widen a signature to avoid a decision.**

**Errors**

13. **Libraries enumerate failures; applications use dynamic `Error`.**
14. **Write `from` for every error type that wraps another.**
15. **Add context at boundaries, not at every frame.**
16. **Panics are for bugs.** A library that panics on bad input is
    unusable.

**Control flow**

17. **Guard clauses first, happy path last, unindented.**
18. **Avoid `_ =>` on closed types.** It spends the exhaustiveness
    guarantee.
19. **Adapters for transformations, loops for effects.** `?` does not
    work inside a closure, which settles most cases.

**Resources and concurrency**

20. **`defer` on the line after the acquisition. Always.**
21. **`errdefer` for compensation; do the success path explicitly.**
22. **Let the scope own the lifetime** — of a task, a server, a
    timeout.
23. **Freeze before spawning.**
24. **`os.exit` before you open anything; `Err` after.**

**Testing and docs**

25. **Write the property, then the examples.**
26. **Do not mock what you can construct.**
27. **Documentation says what the signature cannot.**

---

### 8. Examples

**One function, refactored through the whole book.**

Version 1 — a faithful translation of how this would be written in Go:

```glide
fn process_order(db: Db, order_id: Int, user_id: Int, notify: Bool) -> Int {
    let mut status = 0
    let rows = db.query("select * from orders where id = :id",
                        ["id": order_id]) ?? []
    if rows.len() == 0 {
        status = 404
    } else {
        let row = rows[0]
        let total = row["total"] ?? 0
        if total <= 0 {
            status = 400
        } else {
            _ = db.exec("update orders set state = 'paid' where id = :id",
                        ["id": order_id])
            if notify {
                _ = send_email(user_id, "paid")
            }
            status = 200
        }
    }
    status
}
```

Everything here compiles. What is wrong with it:

- `order_id` and `user_id` are both `Int` and adjacent —
  **transposable**.
- `notify: Bool` at the call site is a bare `true` or `false` —
  **a boolean trap**.
- The return type is `Int`, so the caller must know that 404 means
  not-found — **stringly-typed, in integers**.
- `?? []` and `?? 0` **discard two errors silently** (well —
  visibly, but without thought).
- `rows[0]` **panics** if the invariant breaks.
- `status` is a **`mut` return slot**, which is what expression-`if`
  exists to delete.
- Three levels of nesting for three preconditions.

Version 2 — the same function written in the language:

```glide
type OrderId = distinct Int
type UserId = distinct Int

type OrderError =
    NotFound{ id: OrderId }
    | InvalidTotal{ id: OrderId, total: Int }
    | Db{ cause: Error }

impl OrderError {
    fn from(e: Error) -> OrderError { Db{ cause: e } }
}

fn process_order(db: Db,
                 order: OrderId,
                 user: UserId,
                 notify: Bool = true) -> Result<(), OrderError> {
    let Some(row) = db.query_one(
        "select total from orders where id = :id",
        ["id": order],
    )? else {
        return Err(.NotFound{ id: order })
    }

    let total = row["total"] ?? 0
    if total <= 0 {
        return Err(.InvalidTotal{ id: order, total: total })
    }

    _ = db.exec("update orders set state = 'paid' where id = :id",
                ["id": order])?

    if notify {
        _ = send_email(user, "paid")?
    }
    Ok(())
}
```

And the call site:

```glide
match process_order(db, order_id, user_id, notify: false) {
    Ok(())                          => http.text("ok")
    Err(NotFound{ .. })             => http.not_found()
    Err(InvalidTotal{ total, .. })  => http.bad_request("bad total {total}")
    Err(Db{ cause })                => { eprintln("{cause}"); http.text("error") }
}
```

Count the changes:

| Was | Now |
|---|---|
| Two adjacent `Int` parameters | `OrderId` and `UserId` — transposition is a type error |
| `notify` as a positional bool | `notify: false`, named, with a default |
| `Int` return encoding statuses | `Result<(), OrderError>` — three named failures |
| Two silent `??` | Two `?`, propagating with conversion |
| `rows[0]` | `query_one` + `let … else` |
| `mut status` | no mutable bindings at all |
| Three levels of nesting | zero |
| Caller must know 404 means not-found | caller `match`es, exhaustively |

Nothing here is clever. Every change is a feature from an earlier
chapter, applied because the situation called for it — and the result
is shorter, flatter, and impossible to misuse in four specific ways.

**The idiomatic service skeleton**, which is worth memorising because
almost every Glide program has this shape:

```glide
import http
import sql
import time

fn main() -> Result<(), Error> {
    // 1. Dependencies, created once, visibly.
    let db = sql.open(dsn)?
    defer { _ = db.close() }

    // 2. Wiring: closures inject dependencies into handlers.
    let mut r = http.router()
    r.get(`/notes/{id}`, |req| get_note(db, req))
    r.post(`/notes`, |req| create_note(db, req))
    let r = r                                     // sealed

    // 3. Lifetime: the scope owns everything that runs.
    scope s {
        _ = s.spawn(|| sweeper(db))
        http.serve(addr, r)
    }
}
```

Three sections, in that order, and the third one is what makes the
program correct on shutdown without any shutdown code.

---

### 9. Summary & Exercises

**Summary**

- The organising heuristic: **the source code should tell you what it
  costs.** Almost every style question reduces to it.
- **Case is grammar**, enforced by the formatter: `PascalCase` for
  types and variants, `snake_case` for everything else, including
  constants. No SCREAMING_CASE.
- **Method names carry mutation intent**, because receivers take no
  call-site marker. Verbs mutate; nouns read; past participles return a
  copy.
- **File order is narrative order** — the formatter will not reorder
  declarations.
- **`mut` scarcity is the highest-value habit.** An audit mark is worth
  nothing if it is common, and the language provides five ways to avoid
  spending one.
- **Parse, don't validate** is the central pattern, and it needs
  mandatory initialisation, private fields, and `Option` together.
- **Maps at the boundary, structs inside.**
- **Signatures are the documentation**, and Glide spent its budget
  making them carry information. Do not widen one to avoid a decision.
- **Documentation says what the signature cannot** — no tag language,
  first sentence starts with the identifier, checked `[Identifier]`
  links that break the build when stale, Example functions rather than
  doc-tests.
- **`glide test` is the style gate**, and formatting is never a compile
  error, because a compiler that yells about whitespace while you think
  is hostile.
- The construct decision table in section 1 is the book's index in one
  page.

**Exercises**

1. **Run the Version 1 → Version 2 refactor on your own code.** Take a
   function from a service you maintain, translate it literally, then
   apply the changes in the Examples section one at a time. Note which
   changes were mechanical and which required a decision you had been
   avoiding — the second category is where the language earns its keep.

2. **Audit `mut` in a file.** Write anything of a hundred lines, then
   count the `mut` bindings and, for each, identify which of the five
   alternatives from Chapter 4 could replace it. If more than a third
   survive, look again at the ones assigned in every branch of an `if`.

3. **Write the review checklist.** Take the "reading a diff" list from
   section 1 and adapt it to a codebase you actually review. Then note
   which items on your *existing* mental checklist disappear entirely —
   null checks, goroutine leaks, forgotten error handling, switch
   statements missing a case. The size of that deleted list is the
   argument for the whole language.
