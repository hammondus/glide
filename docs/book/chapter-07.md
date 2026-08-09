# Chapter 7: Functions

Functions in Glide carry no ceremony and almost no surprises, which
frees the design budget for two things Go refused and one thing Rust
did not need: **default parameters**, **named arguments**, and **nested
functions that deliberately cannot capture**.

The chapter also covers three permanent absences — no overloading, no
variadics, no method-value syntax yet — each of which is load-bearing
rather than an omission waiting to be filled.

Marked ○ in this chapter: `mut` parameters on free functions (parsed
nowhere yet), and the rule that function *values* have full arity.

---

### 1. Basic Usage

#### Declaration

```glide
fn add(a: Int, b: Int) -> Int {
    a + b
}

fn log_line(msg: String) {
    println("[log] {msg}")
}
```

Every parameter has a declared type. The return type follows `->`. No
arrow means the function returns nothing.

**Signatures are always explicit.** Type inference is aggressive
*inside* bodies and does not cross a signature boundary. This is a
constraint, not a limitation of the inference engine: signatures are
documentation, and local-only inference is what keeps error messages
pointing at the right line. Whole-program Hindley–Milner inference is
why Haskell's errors are notoriously baffling — the compiler reports a
conflict at the point where two distant constraints finally collide,
which is rarely where you made the mistake.

#### The tail expression is the return value

```glide
fn double(n: Int) -> Int { n * 2 }
```

No `return`. A function body is a block; a block's value is its final
expression; that is what the function returns.

`return` exists for early exits:

```glide
fn classify(n: Int) -> String {
    if n < 0 { return "negative" }
    if n == 0 { return "zero" }
    "positive"
}
```

```
negative
zero
positive
```

Note the shape: guard clauses early-return, and the happy path is the
unindented tail. That is the idiomatic layout, and it is the same shape
Go's `if err != nil` produces — the difference is that here it is a
choice rather than an obligation.

Because `if` and `match` are expressions, most "compute then return"
shapes collapse into a tail expression anyway:

```glide
// Bad — mutable variable as a return slot
fn classify(n: Int) -> String {
    let mut out = ""
    if n < 0 { out = "negative" } else if n == 0 { out = "zero" } else { out = "positive" }
    return out
}

// Good
fn classify(n: Int) -> String {
    match {
        n < 0  => "negative"
        n == 0 => "zero"
        _      => "positive"
    }
}
```

#### The no-arrow rule is enforced

```glide
fn add(a: Int, b: Int) -> Int { a + b }

fn noret() {
    add(1, 2)
}
```

```
error: line 4: noret declares no return value but its body ends with a Int;
       discard it with `_ = …` or declare `-> Int`
```

Chapter 3 covered this; it is repeated here because it is the rule
people trip over most in their first day.

#### Declaration order does not matter

Module-level functions are a *set*, not a sequence. Call a function
declared below you; it works. This is why file order can be narrative
order.

#### Default parameters

```glide-run
fn connect(host: String, port: Int = 5432, tls: Bool = true) -> String {
    "{host}:{port} tls={tls}"
}

fn main() {
    println(connect("db.local"))
}
```

```
db.local:5432 tls=true
```

Defaults are **real expressions**, evaluated **per call**, left to
right, and may reference earlier parameters:

```glide-run
fn width(s: String, w: Int = s.len()) -> Int { w }

fn main() {
    println(width("hello"))       // 5  — default computed from s
    println(width("hello", 2))    // 2
}
```

Per-call evaluation is not a detail. Python evaluates defaults once at
definition time, which makes a mutable default (`def f(xs=[])`) a
shared object across all calls — a thirty-year cautionary tale that
still catches people.

#### Named arguments

Any parameter can be named at the call site:

```glide
fn main() {
    println(connect("db.local", tls: false))
    println(connect(host: "x", port: 1))
}
```

```
db.local:5432 tls=false
x:1 tls=true
```

Two rules:

**Positionals come before named ones.**

```glide
connect(port: 5, "db")
```

```
error: line 2: positional arguments go before named ones
```

**Parameter names are API.** Misspelling one is an error that names the
parameter:

```glide
connect("db", prt: 5)
```

```
error: line 2: connect has no parameter "prt"
```

The consequence is owned explicitly in `DESIGN.md`: renaming a
parameter is a breaking change for callers. That is the price of the
feature and it is considered worth paying, because parameter names were
already documentation.

Together, defaults and named arguments replace Go's functional-options
pattern — thirty lines of ceremony per configurable function (an option
type, N closure constructors, an application loop, and a slice of
function pointers at every call site) to express what the signature
should have said.

#### Nested functions

A `fn` may be declared inside another function's body:

```glide
fn outer(n: Int) -> Int {
    fn helper(x: Int) -> Int { x * 2 }
    fn even(x: Int) -> Bool { if x == 0 { true } else { odd(x - 1) } }
    fn odd(x: Int) -> Bool { if x == 0 { false } else { even(x - 1) } }

    if even(n) { helper(n) } else { helper(n) + 1 }
}
```

```glide
println(outer(4))     // 8
println(outer(5))     // 11
```

Nested functions are **items, not statements**: they are hoisted to
block entry, so they may be declared below their callers, and siblings
can be mutually recursive (`even` and `odd` above).

**A nested `fn` cannot capture enclosing locals:**

```glide
fn main() {
    let n = 5
    fn bad() -> Int { n }
    println(bad())
}
```

```
error: line 3: undefined name "n"
```

This is Rust's rule and it is deliberate. A nested `fn` is a plain
private helper — a module-level function whose visibility has been
shrunk to one body. Capture is what closures are for (Chapter 8).

#### `mut` parameters ○

A free function that mutates a parameter declares it, and the *call
site* repeats the marker:

```glide
fn sort_desc(mut xs: List<Int>) {          // ○ not implemented
    xs.sort_by(|a, b| b.cmp(a))
}

let mut xs = [3, 1, 2]
sort_desc(mut xs)                          // ○ marker at the call site
```

Today the parser rejects `mut` in a parameter list:

```
error: expected identifier in parameter list, found 'mut'
```

so this whole feature is designed-only. Method receivers are different
and *do* work — `fn insert(mut self, …)` is ✓ and is covered in
Chapter 16.

#### What functions do not have

- **No overloading.** One name, one signature. Permanent.
- **No variadics.** A function takes a `List<T>`; callers write two
  brackets. Permanent.
- **No method values yet.** `x.method` (unapplied, as a closure) is ○.
  Write `|a| x.method(a)` today.

---

### 2. Under the Hood

#### Hoisting and the two namespaces

The interpreter collects declarations before evaluating statements, at
every nesting level. Module-level functions, types, `impl` blocks, and
`const` bindings are gathered into the module environment first; nested
`fn`s are gathered into their block's environment at block entry.

This is the same rule applied fractally, and it gives the reader a
uniform contract: **`fn` is callable anywhere in its scope; `let` exists
only downstream of its line.**

#### Why a nested `fn` cannot see enclosing locals

Mechanically, a nested `fn` is created with the *module* environment as
its parent, not the enclosing block's. There is no capture record and
no closure object. That is the entire implementation of "does not
capture" — the name simply is not in scope.

The alternative — a nested `fn` that captures — would make capture
invisible at the declaration. You would have to read the body to know
whether a `fn` is a plain function or a closure in disguise, and
closures would grow a second syntax. Python and JavaScript both took
that route; Glide follows Rust in refusing it.

#### How defaults are filled

Defaults are **declaration sugar, not type**. `fn(Int, Int = 5)` is not
a type; the type is `fn(Int, Int) -> …`. At a direct call site of a
declared function or method, the compiler (interpreter, today) matches
the supplied positional and named arguments against the parameter list
and evaluates any missing defaults, in declaration order, in a scope
where earlier parameters are already bound.

That last part is what makes `w: Int = s.len()` work.

The designed rule is that defaults fill at **direct call sites only** —
function values, closures, and builtins stay full-arity positional.
This keeps "one function type" true.

**M2 gap:** the interpreter currently fills defaults through function
values too:

```glide-run
fn connect(host: String, port: Int = 5432) -> String { "{host}:{port}" }

fn main() {
    let f = connect
    println(f("db"))      // prints db:5432 — should be an arity error
}
```

The checker era will make this an error. Do not rely on it.

#### Calling convention ○

In the designed compiler, arguments pass in registers where possible,
as in Go's post-1.17 ABI. Small functions inline. Since there are no
multiple return values (tuples are ordinary values), the ABI question
Go had to solve — how do you return two things cheaply — does not
arise; a tuple return is a small struct return.

In the interpreter, a call allocates a child environment map, binds
parameters, and evaluates the body. That environment allocation is one
of the tree-walker's dominant costs and is a reason not to draw
performance conclusions from it.

---

### 3. Why This Design?

#### Why signatures are always explicit

Three reasons, in increasing order of importance.

1. **Documentation.** A signature is the thing you read to know how to
   use a function. Inferring it means reading the body.
2. **Error locality.** Local-only inference means a type error is
   reported where the mismatch is, not where two distant constraints
   collide.
3. **API stability.** An inferred return type changes silently when the
   body changes. An explicit one changes only when you change it, and
   the change is a reviewable one-line diff.

#### Why the tail expression is the return value

Covered in Chapter 3 and worth one more angle here: it is what makes
small functions read as *what they compute*.

```glide
fn double(n: Int) -> Int { n * 2 }
fn is_admin(u: User) -> Bool { u.role == .Admin }
fn full_name(u: User) -> String { "{u.first} {u.last}" }
```

Adding `return` to each of those adds a keyword that carries no
information — the arrow already said a value comes out.

#### Why defaults and named arguments, when Go refused both

Go's ban is defensible on its own terms and expensive in practice.
Without defaults, a configurable function needs either a proliferation
of names (`New`, `NewWithTimeout`, `NewWithTimeoutAndTLS`) or the
functional-options pattern:

```go
type Option func(*Config)

func WithTimeout(d time.Duration) Option {
    return func(c *Config) { c.Timeout = d }
}
func WithTLS(b bool) Option {
    return func(c *Config) { c.TLS = b }
}

func Connect(host string, opts ...Option) (*Conn, error) {
    c := &Config{Port: 5432, Timeout: 30 * time.Second, TLS: true}
    for _, o := range opts {
        o(c)
    }
    …
}
```

That is roughly thirty lines per configurable function, and it produces
call sites (`Connect("db", WithTLS(false))`) that are *worse* than the
thing they replace. It also allocates a closure per option and a slice
per call.

The direct answer is one line of signature. Dart, Kotlin, and Swift all
converged on it independently, which is strong evidence.

#### Why overloading stays banned anyway

Overloading is a different feature and its problems are real:
resolution rules swamp the specification, error messages become
dissertations, and it interacts badly with type inference (which
overload does `f(x)` pick when `x`'s type is being inferred?).

Go was right to ban it. Go was wrong to ban it *without* providing the
alternative, which is why the ecosystem paid in `NewXWithY`
proliferation. Defaults plus named arguments cover overloading's
legitimate 90%; generics and honest names cover the rest.

#### Why no variadics

Every Go variadic customer is dead here:

| Go variadic | Glide |
|---|---|
| `fmt.Printf(format, args...)` | interpolation |
| `append(xs, a, b, c)` | `push` / `extend` |
| `New(1, 2, 3)` | list literal `[1, 2, 3]` |
| `max(a, b, c)` | two-arg form, or a list |

Against an empty benefit column sit three real costs: a second
function-type shape (violating the one-function-type doctrine),
miserable interaction with defaults and named arguments (where does the
variadic go?), and Go's `f(xs...)`-versus-`f(xs)` confusion where both
compile and mean different things.

A function takes a `List<T>` and callers write two brackets.
`DESIGN.md` says "revisit only if the brackets grate — bet: never."

#### Why nested functions cannot capture

The alternative is that `fn` and closure become two syntaxes for the
same thing, differing only in whether they have a name. Then the
question "does this helper capture?" requires reading its body, and the
fn/closure distinction stops carrying information.

Keeping them separate means the choice is *forced by what you are
doing*: `fn` = named, item-like, capture-free, explicit signature;
`|x| …` = value, capturing, inferred parameters.

It also fixes two Go warts at once. Go's recursive closures need a
two-step declaration (`var f func(int) int; f = func(n int) int { … f(n-1) … }`)
because a closure cannot refer to the variable it is being assigned to.
And Go has no nested functions at all, so every helper is promoted to
package level, where a reader must assume any caller in the package.
A nested `fn` has *provable* single ownership — scope is information.

---

### 4. Competing Approaches

**Go.** `func`, multiple return values, no defaults, no named
arguments, no overloading, variadics, no nested functions, closures
for everything. The multiple-return-values design is the big
divergence: Go returns `(T, error)` as a pair, and Glide returns
`Result<T, E>` as one value that cannot hold both. Chapter 20 argues
that at length.

**Rust.** Very close: `fn`, explicit signatures, tail expressions,
nested non-capturing `fn`, no overloading, no defaults, no named
arguments. Glide adds defaults and named arguments — the one place
where it takes Kotlin's answer over Rust's. Rust's absence here is felt
constantly in practice (`Builder` patterns everywhere) and is a
frequently-requested feature.

**Kotlin / Swift / Dart.** All three have defaults and named arguments,
and Glide follows Kotlin's model specifically: any parameter is
nameable at any call site, no separate external label. Swift's
external-label/internal-name split (`func greet(to person: String)`) is
declined as an extra concept — parameter names were already
documentation, and a second name for the same thing is a second thing
to keep in sync.

**Python.** Defaults, keyword arguments, `*args`, `**kwargs`,
positional-only and keyword-only markers. Enormously flexible and
enormously complex; the once-at-definition default evaluation is the
specific mistake Glide avoids. Python's `**kwargs` has no analogue in a
statically typed language that refuses a top type.

**Java / C#.** Overloading as the primary mechanism, with C# adding
optional and named parameters later. Java's lack of defaults is why
every Java API has a builder. C#'s combination of overloading *and*
optional parameters produces resolution rules that fill pages of the
specification — an argument for having one mechanism, not both.

**C++.** Overloading, default arguments, templates, variadic templates,
ADL. The most powerful and least predictable of the set. C++'s
experience is the reason "resolution swamps the specification" is a
concrete claim rather than a fear.

---

### 5. Common Mistakes

**Expecting a nested `fn` to see locals.** The error is clear but the
instinct is strong, especially coming from Python or JavaScript:

```glide
// Bad
fn process(items: List<Int>) -> Int {
    let factor = 3
    fn scale(x: Int) -> Int { x * factor }     // error: undefined name "factor"
    …
}

// Good — pass it
fn process(items: List<Int>) -> Int {
    let factor = 3
    fn scale(x: Int, factor: Int) -> Int { x * factor }
    …
}

// Also good — use a closure, which is what capture is for
fn process(items: List<Int>) -> Int {
    let factor = 3
    let scale = |x| x * factor
    …
}
```

Pick the closure when capture is the point; pick the `fn` when the
helper is genuinely independent and you want that stated.

**Putting named arguments before positionals.** The rule is
positionals-then-named, and the error says so.

**Relying on defaults through a function value.** Works today, will not
work after the checker. Defaults are declaration sugar, not part of the
type.

**Writing `return` at the tail.** Legal, redundant, and it makes the
function look like it has two exits when it has one.

```glide
// Bad
fn area(w: Float, h: Float) -> Float {
    return w * h
}

// Good
fn area(w: Float, h: Float) -> Float {
    w * h
}
```

**Trying to overload.** Two functions with the same name is an error.
Give them honest names — `parse_int` and `parse_float`, not two
`parse`s — or use generics (Chapter 18).

**Reaching for a `mut` parameter today.** It does not parse. If a
function needs to modify a collection, either return the new value or
make it a method on a type with a `mut self` receiver.

**Assuming a boolean parameter reads well at the call site.**

```glide
// Bad — what does `true` mean here?
connect("db.local", 5432, true)

// Good
connect("db.local", 5432, tls: true)
```

The designed toolchain has a **boolean-trap lint** (vet tier) that
nudges bare `true`/`false` literal arguments toward the named form.
Adopt the habit before the lint exists.

**Forgetting that defaults evaluate per call.** This is a difference
from Python that works *in your favour* — but if you are porting Python
code that relies on a shared mutable default (deliberately, as a cache),
that trick does not exist here.

---

### 6. Performance Considerations

**Calls in the interpreter** allocate an environment map per
invocation. That dominates the cost of small functions and is why
recursive tree-walking benchmarks look bad. It is a tree-walker
property, not a language property.

**Calls in the designed compiler** pass arguments in registers where
possible and inline small non-recursive functions. Standard stuff.

**Defaults cost only what the expression costs.** `port: Int = 5432` is
a constant folded at the call site. `w: Int = s.len()` genuinely calls
`len()` on every call where `w` is omitted — which is what "per call"
means, and is why you should not put an expensive computation in a
default.

```glide
// Bad — hidden cost, paid on every call that omits the parameter
fn render(page: Page, theme: Theme = load_theme_from_disk()) -> String

// Good — the cost is at the call site where the caller can hoist it
fn render(page: Page, theme: Theme) -> String
```

(`load_theme_from_disk()` in a default would also be a comptime
violation if it were a `const`, and it is a design smell here for the
same reason: cost should be visible.)

**Named arguments cost nothing at runtime.** They are resolved at the
call site; the emitted call is positional.

**Nested functions cost nothing extra.** No capture record, no closure
allocation. A nested `fn` compiles to the same thing a module-level
`fn` compiles to.

**No variadics means no per-call slice allocation.** Go's
`f(1, 2, 3)` allocates a backing array (on the stack when the compiler
can prove it fits, on the heap otherwise). `f([1, 2, 3])` in Glide
allocates a list — the same cost, but visible in the source.

---

### 7. Best Practices

**Write the signature first, and make it say everything.** A reader
should be able to use a function without reading its body. That means
the parameter names are meaningful, the return type is precise
(`Result<Note, ApiError>` rather than `Result<Note, Error>` where the
failure modes are known), and optional configuration is a defaulted
parameter rather than a `Config` struct with unclear defaults.

**Guard clauses first, happy path last, unindented.**

```glide
// Good
fn charge(order: Order, card: Card) -> Result<Receipt, PaymentError> {
    if order.total <= 0 {
        return Err(.InvalidAmount{ cents: order.total })
    }
    let Some(token) = card.token else {
        return Err(.MissingToken)
    }

    let auth = gateway.authorize(token, order.total)?
    let capture = gateway.capture(auth)?
    Ok(Receipt{ id: capture.id, total: order.total })
}
```

**Use named arguments for anything a reader cannot infer from the
value.** Booleans always. Numbers with units usually. Two parameters of
the same type — always, because that is where transposition bugs live:

```glide
// Bad — which is which?
copy(src_path, dst_path)
resize(800, 600)

// Good
copy(from: src_path, to: dst_path)
resize(width: 800, height: 600)
```

**Draw the line between function knobs and reified configuration.**
`DESIGN.md` states it: function knobs → named arguments; configuration
that gets passed around and stored → a struct with a default. If the
options travel together to more than one function, they are a struct.

**Prefer a nested `fn` for a helper used once, in one function.** It
documents "this exists only here", which a module-level function
cannot. Promote it when a second caller appears.

**Do not write a default you would not want as a decision.** A default
is a statement that this value is right for most callers. `tls: Bool =
true` is a good default because insecure-by-default is a bug.
`retries: Int = 3` is a good default. `timeout: Duration = 30.s` is a
good default because Go's default HTTP client having *no* timeout is a
known incident generator.

**Do not use a default to avoid a decision.** If half your callers want
one value and half want another, that is not a default; that is two
functions or a required parameter.

---

### 8. Examples

**A small module showing the whole surface:**

```glide-run
// retry.gld — a retry helper, built up from nothing.

type Fail = Transient{ n: Int } | GaveUp

fn attempt(n: Int) -> Result<String, Fail> {
    if n < 3 {
        return Err(.Transient{ n: n })
    }
    Ok("succeeded on attempt {n}")
}

fn with_retries(max: Int = 5) -> Result<String, Fail> {
    fn describe(n: Int, max: Int) -> String {
        "attempt {n} of {max}"
    }

    let mut last = Err(.GaveUp)
    for i in 1..=max {
        println(describe(i, max))
        match attempt(i) {
            Ok(v)  => { return Ok(v) }
            Err(e) => { last = Err(e) }
        }
    }
    last
}

fn main() {
    match with_retries(max: 4) {
        Ok(msg) => println(msg)
        Err(e)  => eprintln("gave up: {e:?}")
    }
}
```

```
attempt 1 of 4
attempt 2 of 4
attempt 3 of 4
succeeded on attempt 3
```

Points of interest: `describe` is a nested `fn` and therefore takes
`max` as a parameter rather than capturing it — note that it shadows
nothing, because it has its own `max` parameter and cannot see the
enclosing one. `with_retries` uses a named argument at the call site so
`4` is not a mystery number, and the default `max: Int = 5` means the
common case is just `with_retries()`.

**Comparing the configuration approaches directly:**

```glide
// Bad — Go's functional options, transplanted. Do not do this.
type Option = fn(Config) -> Config

fn with_timeout(d: Duration) -> Option { |c| Config{ timeout: d, ..c } }
fn with_tls(b: Bool) -> Option { |c| Config{ tls: b, ..c } }

fn connect(host: String, opts: List<Option>) -> Conn {
    let mut c = Config{ port: 5432, timeout: 30.s, tls: true }
    for o in opts { c = o(c) }
    …
}

// call site
connect("db.local", [with_tls(false)])
```

```glide
// Good — the signature says it
fn connect(host: String,
           port: Int = 5432,
           timeout: Duration = 30.s,
           tls: Bool = true) -> Conn { … }

// call site
connect("db.local", tls: false)
```

The bad version is thirty lines of machinery, allocates a closure and a
list per call, and produces a worse call site. It exists in Go because
Go left no alternative. Transplanting it here is the archetypal
"Go-in-Glide" antipattern (Chapter 39).

**Mutual recursion with nested functions:**

```glide-run
fn is_even(n: Int) -> Bool {
    fn even(x: Int) -> Bool { if x == 0 { true }  else { odd(x - 1) } }
    fn odd(x: Int)  -> Bool { if x == 0 { false } else { even(x - 1) } }
    even(n)
}

fn main() {
    println(is_even(10))     // true
    println(is_even(7))      // false
}
```

`even` calls `odd`, which is declared after it. Nested `fn`s are
hoisted to block entry, so mutual recursion works with no forward
declaration — the module-level rule, applied fractally.

---

### 9. Summary & Exercises

**Summary**

- Signatures are always explicit; inference works inside bodies only.
  This is for documentation, error locality, and API stability.
- The tail expression is the return value. `return` is for early exits
  and is redundant at the tail. A no-arrow function with a meaningful
  tail value is an error.
- Module-level and nested declarations are both order-independent;
  statements are not.
- **Default parameters** are real expressions evaluated per call, left
  to right, and may reference earlier parameters. Python's
  once-at-definition model is the mistake being avoided.
- **Named arguments** may name any parameter; positionals come first;
  parameter names are API and renaming one breaks callers.
- Together, defaults and named arguments replace Go's functional
  options pattern entirely.
- **Nested `fn`s do not capture.** They are hoisted, may be mutually
  recursive, and are private to their block. Capture is what closures
  are for.
- No overloading, no variadics — both permanent, both with the
  alternatives named.
- `mut` parameters on free functions, with the marker repeated at the
  call site, are ○. Method receivers (`mut self`) are ✓ and are
  Chapter 16.

**Exercises**

1. **Delete a functional-options pattern.** Find one in a Go codebase
   (there is one in every Go codebase). Count the lines: the option
   type, each `WithX` constructor, the application loop, and the
   default struct. Rewrite the signature in Glide with defaults and
   named arguments and count again. Then ask what the options pattern
   bought that the signature does not — the honest answer is
   "extensibility without a breaking change", and decide whether that
   matters for the API you picked.

2. **Find the capture you did not mean.** Take a function from a
   JavaScript or Python codebase with a nested helper function.
   Determine every enclosing variable the helper reads. Would making
   those explicit parameters improve or damage the code? This is the
   exact trade Glide's nested-`fn` rule forces.

3. **Design a bad default.** Write a signature whose default is
   actively harmful — one where the majority of callers should be
   thinking about the value and the default lets them not. Then write
   the version that forces the decision. Common territory: retry
   counts, timeouts, whether to follow redirects, whether to verify
   certificates. Note which of those Go's standard library gets wrong.
