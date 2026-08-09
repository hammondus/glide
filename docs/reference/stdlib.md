# Glide Standard Library Reference

What a running program can call **today**, in the M4b interpreter.
The designed stdlib — what this grows into — lives in
`STDLIB-GOALS.md` (aspirational inventory) and `DESIGN.md` (committed
designs); this file only documents what executes. In the interpreter
era every module is a Go host shim behind a Glide-shaped interface,
so surfaces here are deliberately minimal — they grow when a program
needs them (the dogfood rule).

Same markers as the language reference: everything listed bare is
implemented ✓; pointers to the designed future are marked ○.

## Builtins (no import)

| Function | Signature | Notes |
|---|---|---|
| `print(v)` | `(T) -> ()` | stdout, no newline, unbuffered |
| `println(v)` | `(T) -> ()` | stdout + newline |
| `eprint(v)` | `(T) -> ()` | stderr, no newline |
| `eprintln(v)` | `(T) -> ()` | stderr + newline |
| `Ok(v)` | `(T) -> Result<T, E>` | success constructor |
| `Err(e)` | `(E) -> Result<T, E>` | failure constructor |
| `Some(v)` | `(T) -> T?` | identity at runtime — Option is still unboxed, so `Option<Option<T>>` is unrepresentable ○ |
| `None` | `T?` | the absent value (a literal, not a call) |
| `expect(cond)` | test blocks only | on failure reports both sides of a comparison and continues |

All print functions take exactly one argument; formatting happens in
the interpolated string, not the call (`println("{n:6} {word}")`).
These names are reserved — they cannot be bound or redeclared.

## Modules

Import at the top of the file; imports are inert (run nothing).

### `os` ✓

| Function | Signature | Notes |
|---|---|---|
| `os.args()` | `() -> List<String>` | program name first, then arguments |
| `os.exit(code)` | `(Int) -> !` | immediate exit with status |

### `fs` ✓

| Function | Signature | Notes |
|---|---|---|
| `fs.read_string(path)` | `(String) -> Result<String, Error>` | whole file as a String |

### `json` ✓ (M2 shim — `derive Json` is the real design)

| Function | Signature | Notes |
|---|---|---|
| `json.encode(v)` | `(T) -> String` | structural walk: structs and String-keyed Maps → objects (insertion/field order), Lists and tuples → arrays, `None` → `null`, distinct unwraps, `Instant` → RFC 3339. Variants and functions error (they wait for derive) |
| `json.decode(s)` | `(String) -> Result<T, Error>` | dynamic values: objects → Map (keys sorted — Go's decoder loses order), arrays → List, whole numbers → Int else Float, `null` → `None`. Trailing garbage is an error. Typed decode arrives with `derive Json` |

JSON literals in source want raw strings: `` `{"k": 1}` `` — in an
always-interpolating string the `{` would open an interpolation.

### `http` ✓ (M2 shim)

| Surface | Signature | Notes |
|---|---|---|
| `http.router()` | `() -> Router` | Go-1.22 mux level: methods + `{wildcards}` |
| `r.get/post/put/delete(pat, h)` | `(String, fn(Request) -> …) -> ()` | requires a `mut` path. Write patterns as raw strings (`` `/notes/{id}` ``) — `{id}` would interpolate. Handler returns a `Response` or `Result<Response, E>`; `Err(e)` maps to 500 + rendered error (the one default mapping); a handler panic is 500 + stderr |
| `http.serve(addr, r)` | `(String, Router) -> Result<(), Error>` | blocks; green thread per request; **cancellation point** — the enclosing scope's death gracefully shuts the server down. `Err` only on listener failure |
| `http.get(url)` | `(String) -> Result<Response, Error>` | 30s timeout out of the box; scope cancellation/deadline abort the request |
| `http.post(url, body)` | `(String, String) -> Result<Response, Error>` | body sent as `application/json` |
| `req.path_param(name)` | `(String) -> String?` | `None` when absent |
| `req.body()` / `req.method()` / `req.path()` | `-> String` | |
| `resp.status()` / `resp.body()` | `-> Int` / `-> String` | client-side reading |
| `http.text(s)` / `http.json(v)` / `http.created()` / `http.bad_request(msg)` / `http.not_found()` | `-> Response` | the closed constructor set |

### `sql` ✓ (M2 shim — sqlite only; `derive Row` is the real design)

| Surface | Signature | Notes |
|---|---|---|
| `sql.open(dsn)` | `(String) -> Result<Db, Error>` | `"sqlite:path"` or `"sqlite::memory:"` (pure-Go driver — no CGO) |
| `db.exec(q [, params])` | `(String, Map?) -> Result<Int, Error>` | rows affected; **cancellation point** |
| `db.query(q [, params])` | `(String, Map?) -> Result<List<Map>, Error>` | rows as column→value Maps, column order |
| `db.query_one(q [, params])` | `(String, Map?) -> Result<Option<Map>, Error>` | `None` = no row; >1 row is an `Err` |
| `db.close()` | `() -> Result<(), Error>` | pairs with `defer` |

Named parameters only (`:name`, the one canonical syntax); params are
a Map (`["id": 7]`). Missing and unused names are both errors naming
the parameter — the comptime check, enforced dynamically for now.
NULL is `None` in both directions (`sql.NullString` never exists);
distinct values bind by unwrapping; `Instant` stores as RFC 3339.

Designed growth (○): the rest of the committed core set — `tls`,
`crypto`, `process`, `flag`, `regex`, `log`, `template`, `rand`,
compression, persistent collections, `Mutex<T>` — per
`STDLIB-GOALS.md`.

## Concurrency (M3)

| Surface | Signature | Notes | Status |
|---|---|---|---|
| `s.spawn(f)` | `(fn() -> T) -> Task<T>` | scope handles only; error if the scope has ended | ✓ |
| `t.join()` | `() -> T` | blocks (cancellation point); returns exactly what the closure returned | ✓ |
| `channel()` | `() -> (Sender<T>, Receiver<T>)` | unbuffered rendezvous | ✓ |
| `channel(cap: n)` | `(Int) -> (Sender<T>, Receiver<T>)` | buffered; no unbounded variant | ✓ |
| `tx.send(v)` | `(T) -> ()` | blocks per capacity (cancellation point); **panics** on closed channel (sender coordination bug) | ✓ |
| `tx.close()` | `() -> ()` | idempotent; only the sender half has it | ✓ |
| `rx.recv()` | `() -> Option<T>` | blocks (cancellation point); `None` = closed and drained. M2 wart: Option is unboxed, so a *sent* `None` reads as end-of-stream | ✓ |
| `for v in rx` | — | consumes until closed (receiver satisfies the iteration protocol) | ✓ |
| `s.deadline()` | `() -> Option<Instant>` | nearest enclosing timeout/deadline, inherited | ✓ |

Both halves clone (mpmc). Blocking operations here are cancellation
points. Send transfers ownership — enforced in the checker era,
dormant today ○. Calling `close()` or `send()` on a receiver (or
`recv()` on a sender) is a compile error naming the right half ✓.

Time ✓: `Duration` and `Instant` are distinct types. Constructors
are Int/Float suffix properties `.ns` `.us` `.ms` `.s` `.mins` `.h`
(`250.ms`, `0.5.s`; no `.days` — calendar arithmetic belongs to the
future `time` module). Arithmetic: `Duration ± Duration`,
`Duration * Int` (either order), `Duration / Int`, comparisons;
`Instant - Instant -> Duration`, `Instant ± Duration -> Instant`,
comparisons. `import time` gives `time.now() -> Instant`,
`time.sleep(d)` (cancellation point), `time.after(d) ->
Receiver<()>` (a timeout arm in select is an ordinary recv case).
Instant `==` compares like Go's `Equal` (wall+monotonic aware).
Duration renders as Go does (`1.5s`, `2m30s`).

## Methods, by receiver type

Dispatch is on the value's runtime type. Anything not listed is a
runtime error ("X has no method …").

### String

| Method | Signature | Notes |
|---|---|---|
| `len()` | `-> Int` | bytes |
| `trim()` | `-> String` | strip leading/trailing whitespace |
| `trim_prefix(p)` | `(String) -> String` | unchanged if absent (Go semantics) |
| `trim_suffix(s)` | `(String) -> String` | unchanged if absent |
| `contains(sub)` | `(String) -> Bool` | |
| `starts_with(p)` | `(String) -> Bool` | |
| `ends_with(s)` | `(String) -> Bool` | |
| `split(sep)` | `(String) -> List<String>` | empty separator panics (per-character iteration arrives with `runes()`) |
| `split_whitespace()` | `-> List<String>` | splits on any whitespace run |
| `lines()` | `-> List<String>` | Rust semantics: split on `\n`, trailing `\r` stripped, no phantom empty last line |
| `replace(old, new)` | `(String, String) -> String` | all occurrences |
| `to_upper()` / `to_lower()` | `-> String` | Unicode simple case mapping, no locale (locale is a library — Turkish-i) |
| `repeat(k)` | `(Int) -> String` | like `List.repeat`; k < 0 panics |
| `parse_int()` | `-> Int?` | base 10, surrounding whitespace tolerated; `None` on anything else |
| `runes()` | `-> Iterator<Rune>` | lazy; invalid UTF-8 yields U+FFFD per byte (recorded) |
| `bytes()` | `-> Iterator<Int>` | lazy; raw bytes |
| `cmp(other)` | `(String) -> Int` | three-way: negative / 0 / positive |

There is no `s[i]` — by design, permanently. No `find`/`index_of`
yet either — a byte offset is useless until byte-offset slicing
exists; `contains`/`starts_with`/`ends_with` cover the real uses.
(○: `StringBuilder`.)

### Int

| Method | Signature | Notes |
|---|---|---|
| `cmp(other)` | `(Int) -> Int` | three-way |

### List

| Method | Signature | Notes |
|---|---|---|
| `len()` | `-> Int` | |
| `push(v)` | `(T) -> ()` | append; requires a `mut` path |
| `sorted()` | `-> List<T>` | copy, natural ascending order (Int/Float/String) |
| `sort_by(cmp)` | `(fn(T, T) -> Int) -> ()` | in place, **stable**; requires a `mut` path; comparator is three-way (`\|a, b\| a.1.cmp(b.1)`) |
| `repeat(k)` | `(Int) -> List<T>` | new list, elements repeated k times (`[0].repeat(n)` is the fill constructor; Go 1.23's `slices.Repeat`). **Shallow**: repeats the value, so `[[]].repeat(2)` is two slots sharing one inner list — build fresh inner values with `(0..n).iter().map(\|_\| []).collect()` instead. k < 0 panics |
| `join(sep)` | `(String) -> String` | elements must all be Strings (runtime error otherwise) |
| `iter()` | `-> Iterator<T>` | |

Indexing: `xs[i]` reads ✓; `xs[i] = v` and the compound forms (`+=`
`-=` `*=` `/=` `%=`) assign in place (requires a `mut` path) ✓. Out
of bounds panics — bug territory.

### Map

| Method | Signature | Notes |
|---|---|---|
| `len()` | `-> Int` | |
| `entries()` | `-> List<(K, V)>` | insertion order |

Indexing: `m[k]` returns `V?` (absent key → `None`; pair with `??`)
✓. `m[k] = v` inserts or updates (requires `mut`) ✓. Iterating a map
yields `(k, v)` tuples in insertion order ✓.

### Result

| Method | Signature | Notes |
|---|---|---|
| `context(msg)` | `(String) -> Result<T, Error>` | wraps an `Err` with a breadcrumb; passes `Ok` through |

Consume with `?` (propagate) or `match Ok(v) / Err(e)`.

### Iterator

| Method | Signature | Notes |
|---|---|---|
| `take(n)` | `(Int) -> Iterator<T>` | lazy; stops the source after n |
| `map(f)` | `(fn(T) -> U) -> Iterator<U>` | lazy |
| `filter(pred)` | `(fn(T) -> Bool) -> Iterator<T>` | lazy; non-Bool predicate result is an error |
| `enumerate()` | `-> Iterator<(Int, T)>` | lazy; indexes from 0 |
| `zip(other)` | `(iterable) -> Iterator<(T, U)>` | lazy; other may be any iterable (List, Range, Iterator, …); stops at the shorter side |
| `collect()` | `-> List<T>` | drains into a List |
| `count()` | `-> Int` | drains |
| `sum()` | `-> T` | drains; folds `+` from the first element, so Int, Float and String all work; empty sums to Int 0 |

Iterators come from `.iter()` — on List, Map (yields `(k, v)`), and
Range — from generators (`yield`), or from any type with an `iter()`
method (which is also what makes a user type `for`-able). Adapters
are lazy: nothing runs until consumed. (Further adapters — `skip`,
`take_while`, `fold`, … — arrive on demand ○.)

## Testing runner

`glide test file.gld` runs `test` blocks: plain blocks once;
property blocks 100 cases with a fixed per-test seed, shrinking
failures to a minimal case and printing it. `bench` blocks are
parsed and skipped. Exit code reflects failures.

## Maintenance

Hand-written against the interpreter source
(`glide/internal/interp/builtins.go` is the ground truth for
builtins/methods/modules). When a feature or method lands, its row
lands here in the same commit — same discipline as `DESIGN.md` /
`LINEAGE.md`.

As of M4b there is a second place every row must exist: the checker's
signature tables in `glide/internal/check/universe.go`, which give
these surfaces *types*. A method implemented but not typed is
reported as unknown by the checker and cannot be called; a method
typed but not implemented crashes at runtime. `TestHostSurfaceMatchesRuntime`
holds the builtin and module *names* in step automatically; the
per-method signatures are hand-kept, and this file is the list they
are kept against.
