# Chapter 22: `defer` and `errdefer`

`defer` schedules a block to run when the enclosing scope exits — on
success, on error, and during a panic — so that acquisition and cleanup
sit together on adjacent lines.

Glide takes Go's construct with **three defects fixed** and adds a
sibling Go lacks. The fixes are not speculative: Swift and Zig already
shipped them, and Glide takes theirs.

Everything here is ✓.

---

### 1. Basic Usage

#### `defer`

```glide
let db = sql.open("sqlite:notes.db")?
defer { _ = db.close() }
```

The block runs when the enclosing block exits. Acquisition and release
are two adjacent lines, so a reader never has to scan to the bottom of
the function to check that the file gets closed.

Multiple defers run **LIFO** — last registered, first run — which is
what you want, because resources acquired later usually depend on ones
acquired earlier.

#### `defer` takes a block, only a block

```glide
defer { conn.close() }        // right
defer conn.close()            // does not parse
```

This is Swift's form, and it means Go's argument-evaluation puzzle
cannot be written. In Go, `defer f(x)` evaluates `x` **now** and calls
`f` **later**, which is a genuine source of bugs:

```go
// Go — logs the value of `status` at the defer line, not at return
defer log.Printf("finished with %s", status)
```

A block closes over variables like any closure, so everything is
evaluated at scope exit. There is no second rule to remember.

#### `defer` is **block**-scoped, not function-scoped

This is the big one:

```glide-run
fn main() {
    for i in 0..2 {
        defer { println("  cleanup {i}") }
        println("  body {i}")
    }
}
```

```
  body 0
  cleanup 0
  body 1
  cleanup 1
```

The defer runs at the end of **each iteration**. In Go, a `defer`
inside a loop accumulates until the *function* returns:

```go
// Go — every file stays open until the function ends
for _, path := range paths {
    f, _ := os.Open(path)
    defer f.Close()          // fd exhaustion on a long list
    process(f)
}
```

That is the classic file-descriptor-exhaustion bug, and it is invisible
because the code looks correct. In Glide the shape is not reproducible.

Function-end cleanup is a defer at function scope, where it reads as
what it is.

#### `errdefer`

Runs **only when the scope exits on the error path** — a `return`
carrying an `Err` (including what `?` propagates), or a panic. Not a
plain return, not loop control.

```glide-run
type E = Boom

fn work(fail: Bool) -> Result<Int, E> {
    defer { println("  always") }
    errdefer { println("  on error only") }

    if fail { return Err(.Boom) }
    Ok(1)
}

fn main() {
    println("success:")
    println("{work(false):?}")
    println("failure:")
    println("{work(true):?}")
}
```

```
success:
  always
Ok(1)
failure:
  on error only
  always
Err(Boom)
```

Note the ordering on the error path: `errdefer` ran before `defer`,
because they run LIFO and `errdefer` was registered second.

The canonical use is compensation — undo a partial effect:

```glide
fn store(path: String, data: String) -> Result<(), Error> {
    let f = fs.create(path)?
    errdefer { _ = fs.remove(path) }     // no partial files on failure
    f.write(data)?
    f.close()?                            // success: error propagates
    Ok(())
}
```

`defer` = always. `errdefer` = only on failure. They are not two ways to
do one thing.

#### Errors inside a defer must be visible

The unused-`Result` rule applies inside defer blocks:

```glide
defer { _ = db.close() }        // the discard is explicit
```

You cannot silently drop the error, which is Go's worst `defer` defect
— `defer f.Close()` discards the error that surfaces buffered-write
failures, and that is silent data loss.

#### What `defer` may not do

```glide
defer { return 5 }              // runtime error
defer { break }                 // parse error
defer { continue }              // parse error
```

A defer block runs during unwinding. Letting it `return` or redirect a
loop would make the control flow of the enclosing function depend on
cleanup code, which is exactly the confusion the construct exists to
remove.

#### When defers run

| Exit path | `defer` | `errdefer` |
|---|---|---|
| Normal fall-through | ✓ | |
| `return` with a value | ✓ | |
| `return Err(…)` / `?` propagating | ✓ | ✓ |
| `break` / `continue` | ✓ | |
| Panic unwind | ✓ | ✓ |
| Cancellation (Chapter 27) | ✓ | ✓ |
| `os.exit` | **skipped** | **skipped** |

`os.exit` skips everything, by design — it is an immediate exit, and
that is what it is for.

---

### 2. Under the Hood

#### Implementation

Each block maintains a stack of registered defer blocks. On exit —
whatever the exit path — the evaluator pops and runs them in LIFO
order.

`glide/DESIGN-DECISIONS.md` records a nice consequence: the panic path
in `evalBlockDeferred` already runs both `defer` and `errdefer` on the
way out, which turned out to be **exactly** the ratified cancellation
behaviour. So when cancellation was implemented as a Go panic
(`cancelUnwind`), the cleanup semantics came for free.

#### Error-path detection

`errdefer` fires when the block is exiting with an `Err`-carrying
return or a panic. The evaluator knows which signal is propagating, so
this is a check on the exit path rather than an inspection of the
returned value.

#### In the designed compiler

Defers with statically known registration compile to inline cleanup
code in the block's epilogue, duplicated across exit paths. Defers in a
loop or behind a conditional need a small runtime record. This is Go's
post-1.13 optimisation, and it makes the common case roughly the cost
of the cleanup call itself.

---

### 3. Why This Design?

#### Why `defer` and not RAII

RAII — cleanup attached to a value's destruction, as in C++ and Rust's
`Drop` — is the obvious alternative and it does not work here.

RAII needs **deterministic destruction**: the runtime must know exactly
when the last reference dies. C++ gets that from scope-based lifetimes;
Rust gets it from the borrow checker. A tracing garbage collector gives
neither — objects are collected eventually, at a time nobody chooses.

Finalizers running "eventually" is precisely how file descriptors leak.
Java's `finalize()` was deprecated for this reason; C#'s `IDisposable`
plus `using` is the admission that the GC cannot handle resources.

`DESIGN.md` names it directly: **`Drop` is a borrow-checker dividend we
declined.** Having declined the borrow checker, `defer` is the right
primitive.

#### Why not `with`/`using` blocks

Python's `with`, C#'s `using`, and Java's try-with-resources are the
other alternative, and they **nest**:

```python
with open(a) as fa:
    with open(b) as fb:
        with open(c) as fc:
            ...
```

Three resources, three indent levels. Python eventually added
comma-separated forms and parenthesised groups to mitigate it, which is
a tell.

`defer` is flat:

```glide
let fa = fs.open(a)?
defer { _ = fa.close() }
let fb = fs.open(b)?
defer { _ = fb.close() }
let fc = fs.open(c)?
defer { _ = fc.close() }
```

And a `with` block would be a second way to do what `defer` already
does.

#### Why block-scoped

Because function-scoped `defer` in a loop is a bug generator, and the
bug is invisible.

Go's rule made sense when `defer` was introduced (functions were the
only cleanup boundary anyone considered), and the loop case has been
biting people ever since. The standard Go workaround is to extract the
loop body into a function purely so the `defer` fires — which is
extracting a function to work around the language.

Zig and Swift both chose block scope. Glide takes theirs.

The cost: a `defer` inside an `if` block runs at the end of that `if`,
which occasionally surprises someone expecting function scope. That is
a smaller and more visible surprise than fd exhaustion.

#### Why a block and not a call

Go's `defer f(x)` evaluates the arguments immediately and calls later.
That is a defensible choice (it captures the value at registration
time, which is sometimes what you want) and it is a rule you must know,
and people do not.

Taking only a block means there is nothing to explain: it is a closure,
it runs at scope exit, and everything inside is evaluated then. If you
*want* registration-time capture, bind a variable first — which is
visible.

#### Why `errdefer` exists

Because `defer` = always and "only on failure" is a genuinely different
operation that Go programmers hand-write constantly:

```go
// Go — the rollback-if-failed pattern, written out
tx, err := db.Begin()
if err != nil { return err }
committed := false
defer func() {
    if !committed {
        tx.Rollback()
    }
}()
… work …
if err := tx.Commit(); err != nil { return err }
committed = true
```

The `committed` flag is the tell. Zig noticed and added `errdefer`;
Glide takes it.

The canonical uses: rollback a transaction, delete a partially written
file, release a half-acquired resource, undo a registration.

Crucially, this is **not** two ways to do one thing. The success path
still does explicit work — `f.close()?`, `tx.commit()?` — so no error
is discarded on either route. `defer` handles what must happen
regardless; `errdefer` handles compensation.

#### Why the discarded-error rule

Go's `defer f.Close()` silently drops the error. For a write-buffered
file that error is *the* signal that your data did not reach the disk —
silent data loss, and the folklore workaround is a named return plus a
closure that assigns to it.

Making the discard visible (`_ = db.close()`) costs four characters and
makes the decision reviewable. And if you actually care, handle it:

```glide
defer {
    match db.close() {
        Ok(())  => {}
        Err(e)  => eprintln("closing db: {e}")
    }
}
```

#### Why linear "must-close" types were rejected

A type system that *proves* every resource is closed is possible —
linear or affine types. `DESIGN.md` calls it heavier than the problem
in a GC language, and notes that a vet-tier "resource never closed on
some path" lint retrofits most of the value at a fraction of the
conceptual cost.

---

### 4. Competing Approaches

**Go.** Function-scoped `defer` taking a call with immediately
evaluated arguments, LIFO, runs on panic. Glide's three fixes — block
scope, block-only form, visible error discard — plus `errdefer`. Go's
`defer` is otherwise the direct model and a genuinely good idea.

**Zig.** `defer` and `errdefer`, both block-scoped. The direct source
of both of Glide's additions. Zig's `errdefer` can bind the error
(`errdefer |e|`), which Glide does not currently provide.

**Swift.** `defer { … }`, block-scoped, block-only. The source of the
block-only form. No `errdefer` — Swift's `do`/`catch` covers the error
path differently.

**Rust.** `Drop`, plus the `scopeguard` crate for defer-like behaviour.
RAII works because ownership is tracked; the cost is the borrow
checker. Rust's `?` interacting with `Drop` gives you `errdefer`
semantics for free, since the value is dropped on the early return —
which is elegant and is downstream of the ownership system.

**C++.** RAII destructors, and `std::unique_ptr` / `std::lock_guard`
as the idiomatic wrappers. Deterministic and excellent when it works;
the exception-safety rules around destructors (never throw from one)
are the sharp edge.

**Python / C# / Java.** `with` / `using` / try-with-resources —
block-structured, nesting, and requiring the resource type to implement
an interface. The nesting is the problem; all three languages added
mitigations for it.

**C.** `goto fail`. Glide's `defer` plus `?` is the direct replacement,
and it is why `goto` could be removed entirely (Chapter 9).

---

### 5. Common Mistakes

**Writing `defer f.close()` without the block.** Does not parse. It is
`defer { f.close() }` — or, since `close` returns a `Result`,
`defer { _ = f.close() }`.

**Forgetting the `_ =`.** The tail-value rule applies inside defer
blocks, so a bare `db.close()` in a defer is an error. That is the
feature.

**Expecting a loop defer to run at function end.** It runs each
iteration. If you genuinely want to accumulate cleanup until the end,
hoist the acquisition out of the loop.

**Putting a `return` in a defer.**

```glide
// Bad — runtime error
defer { return 5 }
```

**Using `defer` where the success path should be explicit.**

```glide
// Bad — the commit error is discarded
defer { _ = tx.commit() }

// Good
errdefer { _ = tx.rollback() }
… work …
tx.commit()?
```

The success path does its own work, visibly, and `errdefer` handles the
failure. That is the pattern `errdefer` exists for.

**Assuming `os.exit` runs defers.** It does not. If you need cleanup
before exit, return an `Err` from `main` instead — that unwinds
normally.

**Registering a defer conditionally and losing track of the order.**
Defers run LIFO in *registration* order, so a defer inside an `if` that
did not execute was never registered.

**Deferring in a hot loop.** Each iteration registers and runs a block.
In the interpreter this is cheap but not free; in compiled code the
optimiser handles the static cases. If a loop body acquires nothing,
it needs no defer.

---

### 6. Performance Considerations

**Statically known defers compile to inline epilogue code** (○) —
roughly the cost of the cleanup call itself, duplicated across exit
paths. This is Go's post-1.13 optimisation and it removed `defer` from
the list of things to avoid in hot paths.

**Conditional or looped defers need a runtime record** — a small push
per registration. Still cheap, and it is why the compiler cannot always
inline them.

**Block scope means more, smaller defer frames** than Go's function
scope. That is a wash on cost and a large win on correctness: the loop
case that Go defers *accumulates* costs O(n) memory and n open file
descriptors.

**Defers run during panic and cancellation unwinding**, which is
required for correctness (locks must be released) and is not a
performance consideration — it happens once, on the way out.

**In the interpreter**, a defer is a closure appended to the block's
defer stack. One small allocation per registration.

---

### 7. Best Practices

**Put the defer on the line after the acquisition. Always.**

```glide
// Good
let db = sql.open(dsn)?
defer { _ = db.close() }

let f = fs.create(path)?
defer { _ = f.close() }
```

This is the whole discipline. A reader checking for leaks scans for
acquisitions and expects the next line to be the cleanup.

**Use `errdefer` for compensation, and do the success path
explicitly.**

```glide
// Good — the canonical shape
fn write_atomic(path: String, data: String) -> Result<(), Error> {
    let tmp = "{path}.tmp"
    let f = fs.create(tmp)?
    errdefer { _ = fs.remove(tmp) }      // clean up the partial file

    f.write(data)?
    f.close()?                            // errors propagate, visibly
    fs.rename(tmp, path)?
    Ok(())
}
```

If any `?` fires, the temporary file is removed. On success it is
renamed. Neither path discards an error.

**Handle the close error when it matters.**

```glide
// Fine for a read-only handle
defer { _ = f.close() }

// Better for a writer, where close() flushes
defer {
    match f.close() {
        Ok(())  => {}
        Err(e)  => eprintln("closing {path}: {e}")
    }
}
```

For a buffered writer, the close error is the one that tells you the
data did not land. Do not discard it out of habit.

**Scope the defer to the resource's actual lifetime.**

```glide
// Good — the lock is held for exactly the critical section
{
    let guard = mutex.lock()             // ○
    defer { guard.release() }
    critical_section()
}
// the lock is free here
```

Block scope makes this natural. In Go you would extract a function.

**Do not defer what the control flow already guarantees.** A defer
whose block is the last statement of a short function is noise —
though if the function grows an early return later, the defer was
right. Judgement call; err toward the defer.

**Keep defer blocks short and non-failing.** A defer that does real
work, allocates, or can panic makes the unwind path complicated. Its
job is to release something.

---

### 8. Examples

**Block scope, demonstrated:**

```glide-run
fn main() {
    println("loop:")
    for i in 0..2 {
        defer { println("  cleanup {i}") }
        println("  body {i}")
    }

    println("nested blocks:")
    {
        defer { println("  outer done") }
        {
            defer { println("  inner done") }
            println("  inner body")
        }
        println("  outer body")
    }
}
```

```
loop:
  body 0
  cleanup 0
  body 1
  cleanup 1
nested blocks:
  inner body
  inner done
  outer body
  outer done
```

Each defer fires at the end of *its own* block. In Go, all four of
those would fire at the end of `main`.

**`defer` versus `errdefer`, both paths:**

```glide-run
type E = Boom

fn work(fail: Bool) -> Result<Int, E> {
    defer { println("  always") }
    errdefer { println("  on error only") }

    if fail { return Err(.Boom) }
    Ok(1)
}

fn main() {
    println("success:")
    println("{work(false):?}")
    println("failure:")
    println("{work(true):?}")
}
```

```
success:
  always
Ok(1)
failure:
  on error only
  always
Err(Boom)
```

**The transaction pattern, side by side with Go:**

```go
// Go — the `committed` flag is the missing construct, hand-written
func transfer(db *sql.DB, from, to int, amount int) (err error) {
    tx, err := db.Begin()
    if err != nil {
        return err
    }
    committed := false
    defer func() {
        if !committed {
            tx.Rollback()
        }
    }()

    if _, err = tx.Exec("update accounts set bal = bal - ? where id = ?", amount, from); err != nil {
        return err
    }
    if _, err = tx.Exec("update accounts set bal = bal + ? where id = ?", amount, to); err != nil {
        return err
    }
    if err = tx.Commit(); err != nil {
        return err
    }
    committed = true
    return nil
}
```

```glide
// Glide — errdefer IS the construct
fn transfer(db: Db, from: Int, to: Int, amount: Int) -> Result<(), Error> {
    let tx = db.begin()?                          // ○ transactions
    errdefer { _ = tx.rollback() }

    _ = tx.exec("update accounts set bal = bal - :n where id = :id",
                ["n": amount, "id": from])?
    _ = tx.exec("update accounts set bal = bal + :n where id = :id",
                ["n": amount, "id": to])?
    tx.commit()?
    Ok(())
}
```

The flag disappears, the closure disappears, and the Go version's
subtle bug — `tx.Rollback()` after a successful commit is a silent
no-op, so forgetting `committed = true` produces code that *works* and
rolls back nothing — cannot be written.

(`db.begin()` is ○; the designed transaction API is
`db.tx(|tx| { … })`, a closure that commits on `Ok` and rolls back on
`Err` — Chapter 35. `errdefer` is what makes that implementable.)

**Bad versus good: the accumulating defer**

```glide
// Bad in Go, unwritable here — shown for the contrast
// for path in paths {
//     f := os.Open(path)
//     defer f.Close()      // Go: every file stays open until return
//     process(f)
// }

// Glide: the same code is correct, because defer is block-scoped
fn process_all(paths: List<String>) -> Result<Int, Error> {
    let mut total = 0
    for path in paths {
        let text = fs.read_string(path).context("reading {path}")?
        total += text.lines().len()
    }
    Ok(total)
}
```

The point is not that this particular function needs a defer — it is
that if it did, the naive placement would be correct.

---

### 9. Summary & Exercises

**Summary**

- `defer { … }` schedules cleanup to run when the **enclosing block**
  exits — normal fall-through, `return`, `break`/`continue`, `?`
  propagation, panic, and cancellation. Not `os.exit`.
- Defers run **LIFO**.
- **Block-scoped, not function-scoped.** A defer in a loop body runs
  each iteration, so Go's file-descriptor-exhaustion bug is not
  reproducible.
- **A block, only a block.** Go's argument-evaluation puzzle
  (`defer f(x)` evaluates `x` now) cannot be written.
- **Discarded errors must be visible** (`_ = db.close()`). Go's
  `defer f.Close()` silently drops the error that reports failed
  buffered writes.
- **`errdefer`** runs only on the error path — an `Err`-carrying return
  (including `?`) or a panic. It is the construct behind Go's
  hand-written `committed := false` rollback pattern.
- `defer` = always, `errdefer` = only on failure. Not two ways to one
  thing: the success path still does explicit work (`tx.commit()?`), so
  no error is discarded on either route.
- A defer block may not `return` (runtime error) or `break`/`continue`
  an enclosing loop (parse error).
- Alternatives rejected: **RAII/`Drop`** needs deterministic
  destruction that a tracing GC cannot provide; **`with`/`using`
  blocks** nest one indent level per resource and would be a second way
  to do the same thing; **linear must-close types** are heavier than
  the problem in a GC language.

**Exercises**

1. **Find the accumulating defer.** In a Go codebase, grep for `defer`
   inside a `for` loop. For each, work out the maximum number of
   resources that can be simultaneously held. If any of them is
   unbounded (driven by input size), you have found the bug that block
   scope prevents.

2. **Write the rollback three ways.** Implement a two-step operation
   that must undo step one if step two fails: (a) with an `if` at every
   exit point, (b) with a `committed` flag and a `defer`, (c) with
   `errdefer`. Count the exit paths each version has to get right. Then
   introduce a new early return into each and see which one stays
   correct without being edited.

3. **Decide who closes.** Design an API where a function returns an
   open resource. Who defers the close — the function or the caller?
   Write both versions and note what each makes impossible. This is the
   question RAII answers automatically and `defer` makes you answer
   deliberately, which is a real cost of the choice.
