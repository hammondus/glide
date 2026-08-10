# Glide Standard Library Reference

What a running program can call **today**, in the M4c interpreter.
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
| `Some(v)` | `(T) -> T?` | boxes: `Option<Option<T>>` is representable, and `Some(None)` differs from `None` ✓ |
| `None` | `T?` | the absent value (a literal, not a call) |
| `expect(cond)` | test blocks only | on failure reports both sides of a comparison and continues |

All print functions take exactly one argument; formatting happens in
the interpolated string, not the call (`println("{n:6} {word}")`).
These names are reserved — they cannot be bound or redeclared.

## Universe traits

Declared by the host, in Glide, and visible to every program without
an import. Both are reserved: a program cannot redeclare them.

| Trait | Requires | Notes |
|---|---|---|
| `Ord` | `cmp(self, other: Self) -> Int` | three-way ordering; drives `< <= > >=` and `sorted()` |
| `Iterable<T>` | `iter(self) -> Iterator<T>` | what `for … in` consumes |

**`Ord` drives all four comparison operators** from the one `cmp`, and
`sorted()` uses the same path, so `a < b` and a sort can never
disagree. `==` does **not** go through a trait: equality is structural,
universal, and cannot be redefined.

`Float`'s `cmp` is a **total** order — NaN sorts after every number and
equals itself, `-0.0` compares equal to `0.0` — so `nan.cmp(nan)` is 0
while `nan == nan` is `false`. Deliberate, and the same split Java's
`Double.compare` and Rust's `total_cmp` ship: a sort needs a total
order, equality has to obey IEEE 754. A partial `cmp` would let sorting
a list containing NaN silently drop elements.

The set is deliberately two. A universe trait names machinery that
*already runs*: `Int` and `String` both have `cmp`, and the evaluator
already treats anything with an `iter()` as iterable. A trait whose
required method the runtime cannot execute would be drift between the
tiers wearing a type's clothes.

Still ○, and why: **`Hash`** needs a `hash()` that does not exist, and
adding one means committing to a hash function — stable across runs?
across versions? — with nothing yet forcing the answer. **`Display`**
needs `to_string()`, and would constrain nothing anyway: interpolation
is universal here, so `fn log<T>(v: T) { println("{v}") }` already
accepts every `T`. A bound everything satisfies is decoration.

Builtins satisfy a universe trait **structurally** — `Int` cannot
write `impl Ord for Int`, and does not have to. User types must
*declare* conformance (`impl Ord for Blob { }`), though the body
carries only what is missing.

## Modules

Import at the top of the file; imports are inert (run nothing).

### `os` ✓

| Function | Signature | Notes |
|---|---|---|
| `os.args()` | `() -> List<String>` | program name first, then arguments |
| `os.exit(code)` | `(Int) -> !` | immediate exit with status |
| `os.env(name)` | `(String) -> String?` | `None` when unset — which is a different thing from set-and-empty, hence the Option. Pair with `??` for a default |
| `os.set_env(name, value)` | `(String, String) -> Result<(), Error>` | affects this process and its children |
| `os.cwd()` | `() -> Result<String, Error>` | resolved, symlinks followed (Go's `Getwd`) |
| `os.chdir(path)` | `(String) -> Result<(), Error>` | **process-global**: two tasks calling it under `spawn` interleave, and a third resolving a relative path sees whichever won. Fine in a single-threaded script, which is what it is for |

### `fs` ✓

Paths are Strings. A typed `path` module is designed (○,
`STDLIB-GOALS.md`); until it exists, a String is what a script has and
converting at every boundary would buy no checking.

| Function | Signature | Notes |
|---|---|---|
| `fs.read_string(path)` | `(String) -> Result<String, Error>` | whole file as a String |
| `fs.write_string(path, s)` | `(String, String) -> Result<(), Error>` | creates or **truncates**, mode 0644 — the shell's `>` |
| `fs.append_string(path, s)` | `(String, String) -> Result<(), Error>` | creates if absent — the shell's `>>` |
| `fs.exists(path)` | `(String) -> Bool` | bare Bool, not Result: a Result here is one you could only ever unwrap |
| `fs.is_dir(path)` | `(String) -> Bool` | `false` for a missing path too |
| `fs.remove(path)` | `(String) -> Result<(), Error>` | one file, or one *empty* directory |
| `fs.remove_all(path)` | `(String) -> Result<(), Error>` | the whole tree; named so it is never reached by accident |
| `fs.mkdir_all(path)` | `(String) -> Result<(), Error>` | parents too, mode 0755; already-exists is `Ok` |
| `fs.rename(from, to)` | `(String, String) -> Result<(), Error>` | same-filesystem move |
| `fs.list_dir(path)` | `(String) -> Result<List<String>, Error>` | entry **names**, not paths; sorted, so a program's output never depends on the filesystem's whim |
| `fs.join(segments)` | `(List<String>) -> String` | cleaned, platform separator. A List rather than a variadic because the language has no variadics; moves to `path` when that module lands |

### `process` ✓

| Surface | Signature | Notes |
|---|---|---|
| `process.run(cmd [, args])` | `(String, List<String>?) -> Result<Output, Error>` | runs to completion, capturing both streams. **Cancellation point** — the child dies with its enclosing scope |
| `out.status()` | `() -> Int` | the exit code; `-1` if a signal killed it |
| `out.ok()` | `() -> Bool` | `status() == 0` |
| `out.stdout()` / `out.stderr()` | `-> String` | captured in full |

Three things about this surface are deliberate and are the opposite of
the shell:

**A non-zero exit is not an error.** `Err` means the program could not
be run *at all* — not on PATH, not executable, killed by the scope. A
program that ran and exited 1 produced an answer: that is how `grep`
says "no match", `diff` says "they differ" and `test` says "false".
Folding it into `Err` would make `?` propagate an ordinary result, and
every caller would have to un-propagate it. So the status is a field of
the `Ok` value.

**There is no shell.** The command and its arguments are separate, and
an argument containing a space stays one argument. Nothing is
word-split, so there is no quoting to get wrong and no injection to
audit — which is most of what makes shell scripts fragile. A shell is
still available and then it is visible at the call site:
`` process.run("sh", ["-c", "a | b > c"]) ``.

**It is a cancellation point**, like `http.get` and `time.sleep`.
`scope(timeout: 5.s)` kills the child; without that the scope would
return and leave the process running, which is the leak structured
concurrency exists to prevent.

Still ○: streaming a child's output to the terminal instead of
capturing it (a long build), an environment or working directory per
call, and stdin. The streaming one is not a triviality — the
interpreter's stdout is an `io.Writer` a test can redirect, and a
subprocess writing to the real file descriptor bypasses it, so the two
tiers could disagree about where output went.

### `math` ✓

The Float-only operations, and the constants. Everything that has to
work at *every* numeric width — `abs`, `min`, `max`, `pow` — is a
method on the number instead; see [Float](#float) for why the line
falls there.

| Surface | Signature | Notes |
|---|---|---|
| `math.pi` / `math.e` | `Float` | **values, not calls** — `math.pi()` is an error naming the constant |
| `math.inf` / `math.nan` | `Float` | spelled out rather than reachable only as `1.0 / 0.0` |
| `math.sqrt(x)` | `(Float) -> Float` | a negative operand gives `NaN`, the IEEE 754 answer — not a trap. `Float` already admits NaN and `math.is_nan` is right there |
| `math.floor(x)` / `math.ceil(x)` / `math.trunc(x)` | `(Float) -> Float` | still a Float; `Int(math.floor(x))` when you want the integer |
| `math.round(x)` | `(Float) -> Float` | half **away from zero** (Go's `math.Round`), not banker's rounding. Money wants `Decimal` ○, not a second rounding mode here |
| `math.is_nan(x)` / `math.is_infinite(x)` / `math.is_finite(x)` | `(Float) -> Bool` | |

`math.nan` is a *value*; testing is still `math.is_nan(x)`, because
`x == math.nan` is false by IEEE 754 and always will be.

An untyped literal adapts, so `math.sqrt(9)` works — the same rule as
any other Float parameter, and the same as Go's `math.Sqrt(9)`. A
*typed* `Int` does not: conversion is explicit here, so it is
`math.sqrt(Float(n))`.

**Modules can hold values as of this module.** Before `math` they held
functions only, which is why `pi` had nowhere in the language to
exist — it cannot be a method on a number.

Designed growth (○): `log`, `exp`, the trigonometric set, and the
two-argument symmetric ones (`atan2(y, x)`, `hypot(a, b)`) which
belong here on their own merits — `y.atan2(x)` is bad shape even in
Rust, where it exists. They arrive when a program needs them.

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
`crypto`, `flag`, `regex`, `log`, `template`, `rand`, compression,
persistent collections, `Mutex<T>` — per `STDLIB-GOALS.md`. That
document's `math` entry now means the *numerics* surface
(transcendentals, correct rounding modes), not `abs`.

## Concurrency (M3)

| Surface | Signature | Notes | Status |
|---|---|---|---|
| `s.spawn(f)` | `(fn() -> T) -> Task<T>` | scope handles only; error if the scope has ended. The closure **may not capture a `mut` binding** ✓ — the parent may still be writing it, which is the data-race archetype. Freeze first (`let frozen = building`) or send it over a channel. Immutable captures cross freely | ✓ |
| `t.join()` | `() -> T` | blocks (cancellation point); returns exactly what the closure returned | ✓ |
| `channel(T)` | `(type) -> (Sender<T>, Receiver<T>)` | unbuffered rendezvous. The element type is named as a **value**, the `e.find(MyError)` spelling — simple type names only, since `List<Int>` in expression position parses as a comparison. Bare `channel()` is legal only where something else supplies the type (a parameter, or the annotation `let (tx, rx): (Sender<Int?>, Receiver<Int?>) = channel()`); with nothing supplying it, the pair would be `(Sender<?>, Receiver<?>)` and every `send` would go unchecked, which is a compile error | ✓ |
| `channel(T, cap: n)` | `(type, Int) -> (Sender<T>, Receiver<T>)` | buffered; no unbounded variant | ✓ |
| `tx.send(v)` | `(T) -> ()` | blocks per capacity (cancellation point); **panics** on closed channel (sender coordination bug) | ✓ |
| `tx.close()` | `() -> ()` | idempotent; only the sender half has it | ✓ |
| `rx.recv()` | `() -> Option<T>` | blocks (cancellation point); `None` = closed and drained. A *sent* `None` is an ordinary element — Option is boxed as of M4c, so a payload can no longer impersonate the end-of-stream signal | ✓ |
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

### Int (and every integer width)

These are shared by `Int`/`i64` and by `i8`–`i32`, `u8`–`u32` and
`u64`. `Self` is the receiver's own width: `u8.wrapping_add` takes and
returns a `u8`, never an `Int`.

| Method | Signature | Notes |
|---|---|---|
| `cmp(other)` | `(Self) -> Int` | three-way: negative / 0 / positive |
| `abs()` | `() -> Self` | **signed only** — an unsigned `abs` would be the identity, and writing it reads like a sign was handled. Traps at the type's minimum, which has no positive counterpart (same rule as `-x`) |
| `min(other)` / `max(other)` | `(Self) -> Self` | ordered by the same comparison `<` and `sorted()` use |
| `pow(exp)` | `(Int) -> Self` | the exponent is an `Int` at every width — it counts multiplications. Traps on overflow; a negative exponent is an error (convert to Float) |
| `wrapping_add(other)` | `(Self) -> Self` | modular `+`; no trap |
| `wrapping_sub(other)` | `(Self) -> Self` | modular `-`; no trap |
| `wrapping_mul(other)` | `(Self) -> Self` | modular `*`; no trap |
| `wrapping_neg()` | `() -> Self` | modular negation; a type's minimum negates to itself |

Plus one truncating conversion per integer width:
`wrapping_i8`, `wrapping_i16`, `wrapping_i32`, `wrapping_i64`,
`wrapping_u8`, `wrapping_u16`, `wrapping_u32`, `wrapping_u64` — each
`() -> <that width>`.

The `wrapping_*` family is the explicit escape from trap-on-overflow.
Plain `+` `-` `*` `/` trap at the declared width in every tier
(DESIGN.md), so hashes, checksums, PRNGs and wrapping counters say
`wrapping_add` where a reader can see it. `wrapping_div` does not
exist: division cannot wrap except for minimum ÷ -1, which is
`wrapping_neg`.

### Float

Everything under Int above except the `wrapping_*` family (nothing to
wrap), with `Self` = `Float` or `f32`. That is `cmp`, `abs`, `min`,
`max` and `pow` — and `pow`'s Float form takes a Float exponent, so
`(2.0).pow(0.5)` is a square root.

`min`/`max` order by the same **total** order `cmp` and `sorted()` use,
where NaN sorts after every number — so `nan.min(1.0)` is `1.0` and
`nan.max(1.0)` is `nan`. Rust's `f64::min` agrees about the first and
Go's `math.Max` about the second; one coherent order beats matching
either piecemeal, because here `min`, `<` and `sorted()` must never
disagree.

Everything Float-*only* — `sqrt`, the rounding family, the
classification family — is in the [`math` module](#math-) instead.

**The dividing line is width.** A method earns its place on a number by
needing the receiver to say which of the nine numeric types it is:
`abs` must work at `u8` and `Int` alike, and `Self` binds that for
free. As a free function it would need the checker to infer from one
argument and unify an untyped literal against a later one — machinery
that exists nowhere else and that the Glide frontend would have to
reproduce exactly. `sqrt` needs none of that: there is only ever one
type involved.

Go's history is the corroboration, not the counterexample. `math.Min`
was float64-only because Go could not write it generically — and Go
1.21's fix was **not** a generic `math.Min`, it was to make `min`/`max`
universe *builtins*. That third option is worse here for its own
reason: it would reserve `min` and `max` program-wide, and both are
common variable names.

Not permanent: when operator traits (`Add`, `Mul`) make a `Numeric`
bound expressible ○, `math.abs<T: Numeric>(v: T) -> T` types itself
with no special case, and the four can move.

Reaching for a `math` function as a method reports where it went:
`x.sqrt()` says *write `math.sqrt(x)`*, and `5.sqrt()` adds the
conversion, since nothing converts silently here.

f32 arithmetic is computed at **f64** precision in the interpreter, as
it already is for `+` and `*`; rounding happens only at an `f32(x)`
conversion ○.

### Conversion

A primitive numeric type's own name is its conversion — Go's spelling,
and the only way between widths, since implicit conversion is
forbidden by design.

| Form | Meaning |
|---|---|
| `u8(n)`, `i32(n)`, `u64(n)`, … | between integer widths; **traps** if out of range |
| `Float(n)`, `f32(n)` | integer or float to float |
| `Int(f)`, `u8(f)`, … | float to integer, truncating toward zero; traps if the truncated value is out of range, or on NaN/infinity |
| `Int(c)`, `u32(c)` | `Rune` to an integer — its code point |
| `Rune(n)` | integer to `Rune`; traps unless `n` is a Unicode scalar value (in range, not a surrogate half) |
| `n.wrapping_u8()`, … | the truncating counterpart: two's-complement, never traps |

Out of range traps rather than truncating, which is where this parts
company with Go — `uint8(300)` is `44` there, silently. A constant
that cannot fit is rejected at compile time (`u8(300)`), and a value
that cannot fit is a positioned runtime error naming `wrapping_u8`.

Conversion is defined between numbers and `Rune` and nowhere else.
`String(65)` and `Bool(1)` are errors: Go's `string(65) == "A"` is the
wart its own vet tool warns about, and Glide already spells that
`"{n}"`.

Every primitive type name is reserved as a result — `fn u8()`,
`type Int = …` and `const u8 = …` are all errors — because `u8` now
means something in expression position. A local `let u8 = 5` still
shadows it, exactly as a local shadows a predeclared identifier in Go,
and the conversion is then simply gone.

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
| `contains(v)` | `(T) -> Bool` | structural `==`, same as everywhere else |
| `index_of(v)` | `(T) -> Int?` | first match; shares one scan with `contains`, so the two cannot disagree |
| `first()` / `last()` | `-> T?` | `None` on an empty list |
| `pop()` | `-> T?` | removes and returns the last; requires `mut`. `None` on empty is deliberate — an empty worklist is a loop condition, not a bug |
| `insert(i, v)` | `(Int, T) -> ()` | shifts right; requires `mut`. `i == len()` appends; anything past that traps |
| `remove(i)` | `(Int) -> T` | shifts left, returns what was there; requires `mut`; out of range traps |
| `extend(other)` | `(List<T>) -> ()` | appends every element; requires `mut`. `xs.extend(xs)` doubles the list rather than looping |
| `reversed()` | `-> List<T>` | a **copy**, like `sorted()` |
| `slice(lo, hi)` | `(Int, Int) -> List<T>` | half-open `[lo, hi)`, and a **copy** — nothing in Glide aliases a list, and a shared-storage view would make `mut` a lie about a binding you cannot see. Out of range, or `lo > hi`, traps |

Naming rule: a past participle returns a new list (`sorted`,
`reversed`), a verb mutates (`push`, `pop`, `sort_by`, `insert`,
`remove`, `extend`). No negative indices — `xs[-1]` meaning "last" is a
convenience that turns an off-by-one into a silent read of the wrong
end, and `last()` says it plainly.

Indexing: `xs[i]` reads ✓; `xs[i] = v` and the compound forms (`+=`
`-=` `*=` `/=` `%=`) assign in place (requires a `mut` path) ✓. Out
of bounds panics — bug territory.

### Map

| Method | Signature | Notes |
|---|---|---|
| `len()` | `-> Int` | |
| `entries()` | `-> List<(K, V)>` | insertion order |
| `keys()` | `-> List<K>` | insertion order |
| `values()` | `-> List<V>` | insertion order, parallel to `keys()` |
| `contains_key(k)` | `(K) -> Bool` | spelled `_key` because `contains` on a Map is ambiguous about which half it means |
| `remove(k)` | `(K) -> V?` | removes and returns; `None` if absent. Requires `mut`. Drops the key from the insertion order — re-inserting later appends at the end |

Indexing: `m[k]` returns `V?` (absent key → `None`; pair with `??`)
✓. `m[k] = v` inserts or updates (requires `mut`) ✓. Iterating a map
yields `(k, v)` tuples in insertion order ✓.

### Result

| Method | Signature | Notes |
|---|---|---|
| `context(msg)` | `(String) -> Result<T, Error>` | wraps an `Err` with a breadcrumb; passes `Ok` through |

Consume with `?` (propagate) or `match Ok(v) / Err(e)`.

### Error

The dynamic error type application code returns. **Anything is
assignable to it**, so `Err("config is empty")` needs no ceremony and
`?` propagates any callee's error into it with no `from` to write —
but the value in an `Error` slot is always an `Error`, never the raw
thing it was built from. Erased at the *type* level, boxed at the
*value* level.

| Method | Signature | Notes |
|---|---|---|
| `message()` | `-> String` | **this link only.** The whole chain is what interpolation renders (`"{e}"`), and a `message()` that returned the chain would leave no way to get just this one |
| `cause()` | `-> Error?` | the next link, `None` at the end |
| `context(msg)` | `(String) -> Error` | a new Error wrapping this one. The `Result` form above is the same thing where you usually want it |
| `find(SomeType)` | `(type) -> SomeType?` | walks the **whole** cause chain for a concrete error of that type |

`find` takes the type **as a value** — `e.find(ConfigError)`, not
`find<ConfigError>()`. Glide has no turbofish and this is not the
feature to invent one for: `e.find<T>()` cannot be parsed (it reads as
a field access followed by `<`), while types already appear in value
position (`Tree.new()`). Only *declared* types are accepted:
`find(String)` would be a way to read a message that `message()`
already gives properly.

Because `Error` is boxed, a **variant pattern cannot match one** —
`Err(NotFound(id))` against a `Result<_, Error>` is a compile error
naming `find`, not a silently dead arm. Needing `find` deep in
application code is a smell that a boundary should have been typed;
type your library's failure modes as a sum type and let `?` box them
at the application edge.

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
