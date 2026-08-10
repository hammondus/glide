# Chapter 21: Panics

A **panic** is what happens when a program discovers it is wrong.

Not "the file was missing" — that is an error, and Chapter 20 covered
it. A panic is for indexing past the end of a list, dividing `MinInt`
by `-1`, or violating an invariant your own type promised to maintain.
Things a correct program never does.

Glide's position is short and unusually firm: **panics kill the task,
they are for bugs only, and there is no `recover`. Permanently.**

Everything here is ✓, with the caveat that "kills the task" is
currently "kills the program" outside a scope.

---

### 1. Basic Usage

#### What panics

You do not usually write a panic. You encounter one:

```glide
fn main() {
    let xs = [1, 2, 3]
    println(xs[7])
}
```

```
error: line 3: list index 7 out of range (len 3)
$ echo $?
1
```

The current panic sources:

| Cause | Example |
|---|---|
| List index out of range | `xs[7]` on a 3-element list |
| Integer overflow (dev tier) | `MaxInt + 1` |
| Division by zero | `n / 0` |
| `MinInt / -1`, `-MinInt` | overflow with no representable result |
| No matching `match` arm | non-exhaustive match on a sum type |
| Unwrapping a non-matching `if let` | a value that is neither `None` nor the pattern |
| A method that does not exist | dynamic dispatch failure (checker-era gone) |
| `String.split("")` | empty separator |
| `repeat(k)` with `k < 0` | |
| `send` on a closed channel | a sender coordination bug (Chapter 28) |

Note what is *not* on that list: file-not-found, parse failures,
network timeouts, missing map keys. Those are `Result` and `Option`.

#### Panics are not caught

There is no `try`, no `catch`, and no `recover`. A panic unwinds and
that is the end of the discussion for ordinary code.

What *does* run on the way out: `defer` blocks and `errdefer` blocks
(Chapter 22). A panicking task must release its locks and close its
files as the failure propagates.

#### Panics and tasks

The designed rule is: **a panic kills the task, not the process.**
Structured concurrency gives it a principled boundary — a panicking
task fails its scope, which cancels siblings and re-panics at the scope
exit (Chapter 26).

Today, outside a scope, a panic ends the program with exit code 1.
Inside a scope, the ratified behaviour already works: the panicking
child cancels its siblings immediately, and the scope re-panics at
exit.

#### `expect` in tests is different

```glide
test "add works" {
    expect(add(2, 2) == 5)
}
```

```
FAIL  this one fails
      line 13: expect failed: left == right
        left:  4
        right: 5
```

A failed `expect` reports and **continues** — it is not a panic.
`require` (fail and stop) is ○. Chapter 23 covers testing.

---

### 2. Under the Hood

#### Implementation

In the interpreter, a Glide panic is a Go panic carrying an `rtErr`
value with a line number, recovered once in `Run` and printed as
`error: line N: …`.

`glide/DESIGN-DECISIONS.md` records the split explicitly: **`return`
and `?` thread a signal value up the evaluator** (they are semantics),
while **runtime errors panic** (they are diagnostics). That is why you
cannot catch one — there is no signal to intercept.

In the designed compiler, a panic unwinds the stack, running `defer`
and `errdefer` frames, until it reaches the task boundary.

#### Three unwinds

Glide has three distinct ways a computation can stop early, and it is
worth keeping them separate in your head:

| Unwind | Cause | Catchable? | Runs `defer`? | Runs `errdefer`? |
|---|---|---|---|---|
| **Error** | `?` propagating an `Err` | Yes — it is a value | Yes | Yes |
| **Panic** | A bug | **No** | Yes | Yes |
| **Cancellation** | The enclosing scope dying | **No** | Yes | Yes |

Cancellation is Chapter 27's subject. The important structural point is
that only the first is a *value* — the other two are control flow that
user code cannot observe or intercept.

#### Why `defer` runs during a panic

It is required rather than nice-to-have. A panicking task must release
its locks as the failure propagates to its scope, or the dead task
deadlocks the program. `DESIGN.md` states this as a hard constraint on
the defer design.

---

### 3. Why This Design?

#### Why no `recover`

Go has `recover`, and `DESIGN.md` explains precisely why Glide does
not: **Go's `recover` exists because unstructured goroutines gave
panics nowhere to go.**

In Go, a panic in any goroutine kills the entire process. That is
catastrophic for a server, so every serious Go program wraps its
handlers in `defer func() { recover() }()`. Once the mechanism exists,
it gets used for other things — and `recover` becomes exception
handling with worse ergonomics, which is exactly what the
errors-as-values philosophy was avoiding.

Structured concurrency removes the original need. A panicking task
fails its scope; the scope cancels its siblings and propagates. The
blast radius is the scope, not the process, *by construction* rather
than by everyone remembering the incantation.

With the need gone, the escape hatch goes too. `DESIGN.md` marks it
**permanent**.

#### Why panics are not errors

Because the distinction is about *who is wrong*.

If the file might not be there, the caller can reasonably encounter
that, and it belongs in the type: `Result<String, Error>`. If the index
is out of range, *your code* computed a bad index, and no caller can
sensibly handle that — what would they do? Retry with a different
index?

The test: **could a correct program encounter this?** If yes, it is a
`Result` or an `Option`. If no, it is a panic.

The consequence for API design is firm: **APIs never use panics to
report expected failures.** A library that panics on malformed input is
a library you cannot use safely, because there is no `recover`.

#### Why bounds checks panic rather than returning an Option

`xs[7]` could have returned `T?`. It does not, deliberately.

Indexing is the operation you use when you *know* the index is valid —
you just checked the length, or you are iterating a range derived from
it. Making it return an Option would put a `??` or `let … else` on
every array access in every loop, which is a tax on correct code to
launder a bug into a value.

When you do not know the index is valid, the answer is not indexing:

```glide
// You know it is valid
for i in 0..xs.len() { use(xs[i]) }

// You do not know — do not index
let [_, path] = args else { usage() }
```

Rust makes the same call (`xs[i]` panics, `xs.get(i)` returns
`Option`). Glide has no `get` yet; when it lands it will be for exactly
this case.

#### Why send-on-closed panics

Chapter 28 covers channels, but the reasoning belongs here: sending on
a closed channel is a **coordination bug between senders**, not an
expected outcome.

A `Result`-returning `send` would tax every correct program with a
check, to launder a bug into a value. And shutdown in Glide flows
*down* the scope tree via cancellation, not *up* via send failures, so
Rust's Err-on-receiver-gone pattern is not needed.

Recorded cost: full static prevention would need affine senders —
ownership machinery Glide sacrificed.

#### Why overflow panics in dev and wraps in release

Chapter 5 covered this. It is here as an example of the panic
philosophy: an overflow *is* a bug, so a panic is right; and it is a
bug you want to find during development, when the developer is
watching. Release builds wrap because a check on every arithmetic
operation is a tax production should not pay.

The cost — dev and release differ — is taken knowingly, as Zig did.

---

### 4. Competing Approaches

**Go.** `panic`/`recover`, with a panic in any goroutine killing the
whole process. `recover` exists to work around that, and is then abused
into exception handling — several popular Go web frameworks use
panic/recover for control flow, which the language designers explicitly
warned against. Glide removes the original problem and therefore
removes the workaround.

**Rust.** `panic!` with two modes: unwind (runs `Drop`, can be caught
with `catch_unwind`) or abort. `catch_unwind` exists mainly for FFI
boundaries and thread-pool workers, and the documentation discourages
using it as exception handling. Rust also has `Result`, so the
philosophical position is Glide's.

**Zig.** `@panic`, `unreachable`, and safety-checked undefined
behaviour that panics in safe modes. No catching at all — the closest
relative to Glide's position. Zig's tiered safety modes are the direct
model for trap-in-dev/wrap-in-release.

**Java / C# / Python / JavaScript.** Exceptions for everything, with no
distinction between "the file is missing" and "this code is wrong".
`NullPointerException` and `IndexOutOfBoundsException` are catchable,
which means a `catch (Exception e)` swallows the bug and the program
limps on in an unknown state. This is the failure mode the
bugs-are-not-catchable rule prevents.

**C.** Undefined behaviour. No panic at all: an out-of-bounds write
corrupts adjacent memory and the failure surfaces somewhere else
entirely. Every safety feature in every language above is a response to
this.

**Erlang.** "Let it crash" plus supervision trees — a process dies, a
supervisor restarts it. Glide's structured concurrency is
architecturally similar (a scope is the boundary), and `DESIGN.md`
flags Erlang-style supervision policies (`supervise(restart:
.on_failure, …)`) as a likely future stdlib addition. The philosophies
agree: do not try to patch up a process that has proven itself wrong;
kill it at a known boundary and restart from a known state.

---

### 5. Common Mistakes

**Designing an API that panics on bad input.**

```glide
// Bad — a library that panics is unusable, because nobody can recover
pub fn parse_port(s: String) -> Int {
    let Some(n) = s.parse_int() else {
        panic("bad port")        // ○ there is no `panic` builtin, and good
    }
    n
}

// Good
pub fn parse_port(s: String) -> Result<Int, ParseError> { … }
```

Note that there is no user-facing `panic()` builtin today, which makes
this mistake harder to commit. That is deliberate.

**Looking for `recover`.** There is none, permanently. If you find
yourself wanting it, one of two things is true: either the condition is
an expected failure and should be a `Result`, or you want a task
boundary — which is a `scope` (Chapter 26).

**Indexing without checking.**

```glide
// Bad
let first = xs[0]

// Good
let [first, ..rest] = xs else {
    return Err(.Empty)
}

// Also good, when emptiness is not exceptional
let first = if xs.len() == 0 { None } else { Some(xs[0]) }
```

**Assuming a `_ =>` arm prevents a panic.** It prevents *this* panic
and creates a silent bug instead. A non-exhaustive match that panics is
telling you something true.

**Expecting `expect` in a test to stop the test.** It reports and
continues. `require` is ○.

**Relying on overflow wrapping.** It does not. Overflow traps in every
tier and at every width, and `wrapping_add` / `wrapping_sub` /
`wrapping_mul` / `wrapping_neg` ✓ are how you ask for the modular
answer — by name, where a reader can see it.

**Trying to turn a panic into an error at a boundary.** You cannot. If
a subsystem can fail in ways you need to contain, run it in a scope —
the panic will kill that task and fail that scope, which is the
containment mechanism the language offers.

---

### 6. Performance Considerations

**Bounds checks cost a compare and a predicted branch.** In tight loops
the compiler eliminates them when it can prove the index is in range —
`for i in 0..xs.len()` is the shape that allows this.

**Overflow checks cost roughly one instruction and a predicted branch
per arithmetic operation** in the dev tier, and nothing in release.
Typically a few percent, more in numeric-heavy loops.

**Panicking is expensive** and that is fine — it happens once, before
the task dies. Unwinding runs every `defer` and `errdefer` frame on the
way out.

**There is no happy-path cost to panics existing.** Unlike an
exception-handling runtime, there is no landing-pad table to consult
and nothing to set up at function entry. The unwind machinery is used
only when unwinding.

**`defer` running during unwind** is required for correctness, not an
optimisation target.

---

### 7. Best Practices

**Never panic for something a caller could encounter.** The rule, one
more time, because it is the whole chapter:

| Condition | Mechanism |
|---|---|
| File missing, network down, bad input | `Result` |
| Key absent, no match found, empty list | `Option` |
| Index out of range, broken invariant, unreachable state | panic |

**Make invariants unrepresentable rather than asserted.** This is the
best panic-avoidance strategy, and it is what Chapters 12–15 were
about:

```glide
// Panics if the invariant is violated somewhere
fn area(r: Rect) -> Float {
    // assumes r.width > 0 — nothing enforces it
    r.width * r.height
}

// The invariant cannot be violated
type Rect = struct { width: Float, height: Float }

impl Rect {
    pub fn new(w: Float, h: Float) -> Rect? {
        if w <= 0.0 || h <= 0.0 { return None }
        Some(Rect{ width: w, height: h })
    }
}
```

Private fields plus a validating constructor means every `Rect` that
exists is valid, so nothing downstream needs to check or panic.

**Use a scope as the containment boundary.**

```glide
scope s {
    _ = s.spawn(|| handle_request(req))     // a panic here kills this task
    …
}
```

This is the structural answer to "what if that code has a bug". Not
`recover` — a boundary.

**Let bounds-check panics stand during development.** They are telling
you the index computation is wrong. Silencing them with a clamp usually
moves the bug rather than fixing it.

**Prefer patterns to indexing** where the shape is uncertain.
`let [a, b] = xs else { … }` both checks and destructures.

**Do not write defensive checks against your own invariants in
release-critical paths.** If a private field can only be set by a
validating constructor, re-checking it in every method is noise. Check
at the boundary, trust inside.

---

### 8. Examples

**The distinction, made concrete:**

```glide
type ParseError = Empty | NotANumber{ text: String } | OutOfRange{ n: Int }

// Expected failure — a Result. The caller decides what to do.
fn parse_port(s: String) -> Result<Int, ParseError> {
    let s = s.trim()
    if s == "" {
        return Err(.Empty)
    }
    let Some(n) = s.parse_int() else {
        return Err(.NotANumber{ text: s })
    }
    if n < 1 || n > 65535 {
        return Err(.OutOfRange{ n: n })
    }
    Ok(n)
}

// A bug — indexing past the end. Nothing catches this.
fn third(xs: List<Int>) -> Int {
    xs[2]
}

fn main() {
    for s in ["8080", "", "abc", "99999"] {
        match parse_port(s) {
            Ok(n)                      => println("{s:8} -> port {n}")
            Err(Empty)                 => println("{s:8} -> empty")
            Err(NotANumber{ text })    => println("{s:8} -> not a number: {text}")
            Err(OutOfRange{ n })       => println("{s:8} -> out of range: {n}")
        }
    }
    println(third([1, 2, 3]))
    println(third([1]))          // panics
}
```

```
    8080 -> port 8080
         -> empty
     abc -> not a number: abc
   99999 -> out of range: 99999
3
error: line 18: list index 2 out of range (len 1)
```

Four expected outcomes, handled exhaustively. One bug, which stops the
program with a line number.

Note *which* line number: the panic is reported at `xs[2]` inside
`third`, not at the call site in `main`. That is correct — the bad
index was computed there — and it is also the limitation that dev-tier
error return traces (○, Chapter 20) exist to fix.

**Making the invariant unrepresentable instead:**

```glide-run
type NonEmpty = struct { head: Int, tail: List<Int> }

impl NonEmpty {
    pub fn parse(xs: List<Int>) -> NonEmpty? {
        if xs.len() == 0 { return None }
        let mut tail: List<Int> = []
        for i in 1..xs.len() { tail.push(xs[i]) }
        Some(NonEmpty{ head: xs[0], tail: tail })
    }

    // Cannot panic: `head` always exists.
    pub fn first(self) -> Int { self.head }

    pub fn max(self) -> Int {
        let mut best = self.head
        for x in self.tail {
            if x > best { best = x }
        }
        best
    }
}

fn main() {
    let Some(ne) = NonEmpty.parse([3, 1, 4]) else {
        println("empty")
        return
    }
    println(ne.first())
    println(ne.max())

    match NonEmpty.parse([]) {
        Some(x) => println(x.first())
        None    => println("rejected empty list")
    }
}
```

```
3
4
rejected empty list
```

`first()` and `max()` cannot panic, and they contain no checks. The
possibility of emptiness was resolved once, at `parse`, and the type
carries the proof forward. This is the same parse-don't-validate
pattern as Chapter 12, applied to panic avoidance.

**Bad versus good: the library that panics**

```glide
// Bad — nobody can use this safely, because nobody can recover
pub fn decode(data: String) -> Config {
    let parts = data.split("=")
    Config{ key: parts[0], value: parts[1] }     // panics on malformed input
}
```

A caller passing user-supplied data has no defence. There is no
`recover`, so the only option is to validate the input *before* calling
— which means duplicating the parser.

```glide
// Good
pub fn decode(data: String) -> Result<Config, DecodeError> {
    let parts = data.split("=")
    let [key, value] = parts else {
        return Err(.Malformed{ text: data })
    }
    Ok(Config{ key: key, value: value })
}
```

The list pattern does the checking and the destructuring together, and
the failure is a value the caller can handle. Note that this is
*shorter* than the panicking version's honest equivalent would be with
manual length checks.

---

### 9. Summary & Exercises

**Summary**

- A **panic** means the program is wrong: out-of-range index, overflow
  in dev builds, division by zero, a non-exhaustive match, a broken
  invariant. It is not an error value and not control flow.
- **There is no `recover`, permanently.** Go needs it because
  unstructured goroutines gave panics nowhere to go; structured
  concurrency gives them a principled boundary, so the escape hatch is
  unnecessary and is not provided.
- **A panic kills the task, not the process** (○ outside a scope
  today). A panicking child cancels its siblings and the scope
  re-panics at exit.
- `defer` and `errdefer` **do** run during unwind — required, because a
  panicking task must release its locks.
- Glide has **three unwinds**: error (a value, catchable), panic (a
  bug, uncatchable), and cancellation (Chapter 27, uncatchable). Only
  the first is observable by user code.
- The test for which mechanism: **could a correct program encounter
  this?** Yes → `Result`/`Option`. No → panic.
- **APIs never panic to report expected failures.** A library that
  panics on bad input is unusable, because callers cannot recover.
- Indexing panics rather than returning an Option, because indexing is
  what you use when you *know* the index is valid; when you do not
  know, use a pattern.
- The best panic-avoidance strategy is making invariants
  unrepresentable — private fields plus a validating constructor —
  rather than asserting them.

**Exercises**

1. **Audit a `catch`.** In a Java, C#, or Python codebase, find a
   `catch (Exception e)` (or bare `except:`). List every distinct thing
   that could reach it. Separate them into "expected failures the code
   should handle" and "bugs that should have killed something". Most
   such blocks are catching both, and the second category is being
   silently survived.

2. **Remove a panic by construction.** Find a function in your code
   that starts with a precondition check — a null check, a length
   check, a range check. Redesign the type so the precondition cannot
   be violated. Then count how many other functions could drop the
   same check.

3. **Design the containment boundary.** You are writing an HTTP server
   where handler code is written by other teams and may contain bugs.
   Without `recover`, describe how you would stop one bad handler from
   taking down the process. Then check your answer against Chapter 26 —
   the mechanism exists and is the reason `recover` was removable.
