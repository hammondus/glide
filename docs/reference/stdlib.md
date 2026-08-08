# Glide Standard Library Reference

What a running program can call **today**, in the M2 interpreter.
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
| `Some(v)` | `(T) -> T?` | identity in M2 (Option is unboxed) |
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

Designed growth (○): the committed core set — `http`, `tls`,
`crypto`, `json`, `time`, `process`, `flag`, `regex`, `log`,
`template`, `sql`, `rand`, compression, persistent collections,
`Mutex<T>` — per `STDLIB-GOALS.md`. None callable yet; `http`/`sql`/
`json` host shims arrive with M3.

## Methods, by receiver type

Dispatch is on the value's runtime type. Anything not listed is a
runtime error ("X has no method …").

### String

| Method | Signature | Notes |
|---|---|---|
| `len()` | `-> Int` | bytes |
| `trim()` | `-> String` | strip leading/trailing whitespace |
| `split_whitespace()` | `-> List<String>` | splits on any whitespace run |
| `cmp(other)` | `(String) -> Int` | three-way: negative / 0 / positive |

There is no `s[i]` — by design, permanently. (○: the designed
surface — `bytes()`, `runes()`, `contains`, `split`, `StringBuilder`,
etc.)

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
| `collect()` | `-> List<T>` | drains into a List |

Iterators come from `.iter()`, generators (`yield`), or any type
with an `iter()` method (which is also what makes a user type
`for`-able). Adapters are lazy: nothing runs until consumed.
(○: `map`, `filter`, `zip`, `enumerate`, … — the designed adapter
set rides the Iterator trait.)

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
