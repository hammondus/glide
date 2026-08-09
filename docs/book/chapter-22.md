# Chapter 22: Testing

Tests are a **language construct** in Glide, not a naming convention.
There is exactly one assertion, and it is known to the compiler. And
property-based testing — thirty years proven, never mainstream because
the setup friction was always a dependency and an afternoon — ships in
the box with generation and shrinking.

`DESIGN.md` starts from the position that Go's testing model is the
best mainstream one, for precise reasons, and then applies three
upgrades.

Everything in this chapter is ✓ except `require`, benchmark execution,
and the deterministic-scheduler mode.

---

### 1. Basic Usage

#### A test

```glide
fn add(a: Int, b: Int) -> Int { a + b }

test "add works" {
    expect(add(2, 2) == 4)
}
```

```bash
$ glide test math.gld
ok    add works
```

`test "name" { … }` is legal in **any** `.gld` file. There is no magic
prefix, no `_test` filename requirement, no `t *testing.T` parameter to
thread, and no framework to import.

Placement is style, not semantics: inline next to a small invariant
reads as documentation; a separate `_test.gld` file suits larger
suites. Tests see module internals; black-box API testing is what a
separate test module is for.

#### `expect` — the only assertion

```glide
test "this one fails" {
    expect(add(2, 2) == 5)
}
```

```
FAIL  this one fails
      line 13: expect failed: left == right
        left:  4
        right: 5
```

`expect` is **compiler-known**. It does not receive a boolean; it
receives the *expression*, and on failure it reports both sides. That
is why there is no `assertEqual`, `assertGreater`, `assertContains`, or
any of the forty matchers a typical assertion library ships.

`expect` **fails and continues** — the rest of the test body runs.
`require` (fail and stop) is ○.

#### Property-based tests

Give the test block parameters and it becomes a **property test**:

```glide
fn reverse(xs: List<Int>) -> List<Int> {
    let mut out = []
    for i in 0..xs.len() {
        out.push(xs[xs.len() - 1 - i])
    }
    out
}

test "reverse twice is identity" (xs: List<Int>) {
    expect(reverse(reverse(xs)) == xs)
}
```

```
ok    reverse twice is identity  (100 cases)
```

You do not choose the inputs. You state a **property** that should hold
for all of them, and the runner generates 100 cases trying to falsify
it.

If example-based testing is all you have used, this is the feature to
sit with. It is a different activity: instead of asking "does it work
for the cases I thought of", you ask "is this statement true", and the
machine hunts for counterexamples in the space you would never have
enumerated — empty lists, duplicates, negatives, orderings.

#### Shrinking

When the runner finds a failure, it does not hand you the 27-element
monster it found first. It **shrinks** — re-running with structurally
smaller variants until the failure is minimal:

```glide
test "broken property" (xs: List<Int>) {
    expect(reverse(xs) == xs)
}
```

```
FAIL  broken property
      input: xs = [0, 1]
      line 21: expect failed: left == right
        left:  [1, 0]
        right: [0, 1]
```

`[0, 1]` is the smallest input that breaks the property. That tells you
something precise: reversal differs from identity as soon as there are
two distinct elements. A random 27-element failure would have told you
nothing.

The runner uses a **fixed seed** per test, so a failure is reproducible
and a fix stays fixed. Case 0 is always the type's simplest value
(empty list, `0`, `""`) because empty-case bugs dominate.

#### Benchmarks ○

```glide
bench "insert 10k" {
    let mut t = Tree.new()
    for x in 0..10_000 { t.insert(x) }
}
```

```
skip  bench "insert 10k" (benchmarks not implemented yet)
```

Benchmarks parse and are skipped today. The designed model: **the
runner owns the timing loop.** There is no `b.N` to get wrong, and
setup before the body is naturally excluded.

#### Running

```bash
glide test file.gld
```

Exit code reflects failures, so it slots into CI without a wrapper.

In the designed toolchain, `glide test` is also the **enforcement
boundary**: format check, lints, unused-code errors, doc-link
validation, and the race detector all run here (Chapter 2).

#### `test` is a contextual keyword

`test` and `bench` are only special at top level when followed by a
string literal. `let test = 5` is perfectly legal — they are too good
as variable names to reserve.

---

### 2. Under the Hood

#### How `expect` reports both sides

The compiler (interpreter, today) inspects the expression passed to
`expect`. When it is a comparison, it evaluates both operands
separately and reports them on failure.

This is **power-assert semantics as a builtin**. Swift needed macros to
get `#expect`; Glide banned macros, and `DESIGN.md` says builtins are
for exactly this.

The consequence worth noticing: `expect` cannot be a library function,
because a library function receives a `Bool` and the operands are
already gone. That is why the testify war is dissolved rather than
won — there is nothing for a library to add.

#### How generation works

The runner inspects the test's parameter types and generates values.
`List<Int>` produces lists of varying lengths with varying elements;
`Int` produces integers biased toward small values and boundaries;
`String` produces strings.

100 cases, fixed per-test seed. Case 0 is always the simplest value.

The designed extension is `derive Arbitrary` (○) via comptime, so a
user struct becomes generatable with one annotation. That is the piece
that deletes the last of the setup friction — in QuickCheck-descended
libraries, writing generators for your own types is the work that stops
people adopting it.

#### How shrinking works

Greedy: structurally smaller first (shorter lists), then simpler values
(integers toward zero), re-running the test body at each step and
keeping any variant that still fails. It stops when no smaller variant
fails.

This is not the most sophisticated shrinking algorithm — integrated
shrinking, as in Hedgehog, produces better results for generated
structures — but it is simple, it has no false positives (every
reported case genuinely fails), and it handles the common cases.

#### Serial by default

Tests within a module run **serially** by default, with parallelism
opt-in. `DESIGN.md` is explicit about why: default-parallel breaks
every suite that touches a port or a file, and the race detector in
`glide test` catches sharing bugs without making the default flaky.

#### Deterministic scheduling ○

The most differentiating designed capability, and worth knowing about
even though it does not exist yet.

Because Glide owns the scheduler, the clock, and the build hermeticity,
`glide test` can run a **seeded deterministic scheduler**: a failing
concurrent interleaving becomes a rerunnable seed, and the runner can
fuzz schedules hunting for races.

This is the FoundationDB / TigerBeetle lineage. Go and Rust cannot do
it without heroic external simulation, because they do not own all
three pieces. `DESIGN.md` calls it "possibly Glide's most
differentiating capability", and it composes with property testing —
generated inputs *and* generated schedules.

---

### 3. Why This Design?

#### Why tests are a language construct

Go's model is a naming convention: a function named `TestFoo(t
*testing.T)` in a file named `*_test.go`. It works, and it has three
costs a language construct avoids.

**The `t` parameter threads everywhere.** Every helper takes `t
*testing.T`, so test utilities have a different signature from real
code.

**"Excluded from release builds" is a statement about filenames.** With
`test "…" { }`, it is a statement about a language construct — the
compiler knows what a test is.

**Tooling has to guess.** An LSP listing tests, a coverage tool, or a
build system determining what to rebuild all have to implement the
naming convention. When tests are syntax, they are in the AST.

Zig demonstrated the construct form works. Glide takes it.

#### Why exactly one assertion

Go refused an assertion DSL — the right instinct — and landed on
`t.Errorf` boilerplate:

```go
if got != want {
    t.Errorf("add(2, 2) = %d, want %d", got, want)
}
```

Four lines and a format string per assertion, with the message
hand-written and frequently out of sync with the code. The community's
response was `testify`, one of the most-imported Go modules, offering
`assert.Equal`, `assert.NotNil`, `assert.Contains`, and dozens more.

**The standard library lost that argument.** And the fix is not forty
matchers — it is one assertion that the compiler understands:

```glide
expect(add(2, 2) == 4)
```

On failure you get both sides automatically. `assert.Equal(t, 4,
add(2,2))` exists only because the language could not see the
expression; when it can, the matcher zoo has nothing to add.

The knock-on effect matters: **the ecosystem never grows one.** There
is no reason to write an assertion library, so there is no fragmentation
and no "which assertion style does this project use" question.

#### Why property testing is in the box

QuickCheck was published in 2000. Thirty years of evidence says
property testing finds bugs example tests miss. It has never gone
mainstream, and the reason is friction: a dependency, a generator DSL,
learning shrinking, writing `Arbitrary` instances for your own types.

`DESIGN.md`'s position is that **culture follows cost**. Go's testing
culture exists because `go test` needed no setup. Property testing's
absence is a setup-cost problem, and the fix is to make it
`test "name" (xs: List<Int>) { … }` — one extra parameter list.

The composition is where it gets interesting: property tests plus
`derive Arbitrary` (○) plus deterministic schedule fuzzing (○) means
generated inputs *and* generated interleavings, reproducible from a
seed.

#### Why shrinking is not optional

Because an unshrunk counterexample is nearly useless.

`xs = [83, -12, 0, 7, 7, 99, -4, …]` tells you the property is false.
`xs = [0, 0]` tells you the property is false *when there are exactly
two equal elements*, which is a diagnosis. Shrinking converts a failure
into information.

It also means the printed case is stable across runs, so it can go into
a bug report and into a regression test.

#### Why benchmarks let the runner own the loop

Go's `b.N` protocol hands the user the measurement loop:

```go
func BenchmarkFoo(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Foo()
    }
}
```

People get it wrong — setup inside the loop, results not consumed so
the compiler eliminates the work, timer not reset after setup. Go
conceded the point in 1.24 by adding `b.Loop()`.

`bench "name" { … }` gives the runner control of timing and count, and
setup sits before the body where it is naturally excluded.

#### Why serial by default

Because default-parallel breaks every suite that touches a port, a
file, a database, or a global. Go made tests serial within a package
and parallel across packages, with `t.Parallel()` opt-in, and that
balance has held up.

The usual argument *for* default-parallel is that it forces you to
write isolated tests. In practice it forces you to write flaky tests
and then debug them. The race detector running inside `glide test`
catches actual sharing bugs, which is the underlying concern, without
making the default unreliable.

---

### 4. Competing Approaches

**Go.** `func TestX(t *testing.T)` in `*_test.go`, table-driven tests
as the idiom, no assertions, built-in coverage, fuzzing since 1.18,
test caching. The model Glide starts from. Its three weaknesses —
naming convention, no assertion, `b.N` — are the three upgrades.

**Zig.** `test "name" { … }` as a language construct, `std.testing.
expect`, tests run with `zig test`. The direct source of the construct
form.

**Rust.** `#[test]` attribute, `assert!`/`assert_eq!` macros (which do
report both sides, via macros), `cargo test`, doc-tests. Rust's
doc-tests are explicitly **rejected** by `DESIGN.md`: runnable code
inside comments is code invisible to the formatter, LSP, refactoring
tools, and grep — a second place code lives, which is the exact thing
macros were banned for. Glide uses Go-style Example functions instead:
real compiled, output-checked code in test files, rendered into docs.

**Python.** `unittest` (xUnit, verbose), then `pytest` (the community
standard, with assertion rewriting that reports both sides — the same
insight as `expect`, implemented by rewriting bytecode). `hypothesis`
is the property-testing library, and it is excellent and is a
dependency.

**Java / C#.** JUnit / NUnit plus AssertJ / FluentAssertions plus
Mockito. Four dependencies before you write a test. The matcher-DSL
arms race in this ecosystem is the strongest argument for the
one-compiler-known-assertion approach.

**JavaScript.** Jest, Mocha, Vitest, Chai, Sinon, and a configuration
file each. The fragmentation is total, and choosing is a project-level
decision made once and regretted continuously.

**Haskell / Erlang.** QuickCheck (the origin of property testing) and
PropEr. Thirty years of evidence that the technique works and that
adoption is gated on friction.

---

### 5. Common Mistakes

**Writing example tests where a property fits.**

```glide
// Weak — three cases you thought of
test "reverse works" {
    expect(reverse([1, 2, 3]) == [3, 2, 1])
    expect(reverse([]) == [])
    expect(reverse([1]) == [1])
}

// Strong — a statement that must hold for everything
test "reverse twice is identity" (xs: List<Int>) {
    expect(reverse(reverse(xs)) == xs)
}
```

Keep both, in fact — the example test documents the shape, the property
finds the bugs. But if you only write one, write the property.

**Writing a property that is not one.**

```glide
// Bad — this just reimplements the function in the test
test "reverse reverses" (xs: List<Int>) {
    let mut expected = []
    for i in 0..xs.len() { expected.push(xs[xs.len() - 1 - i]) }
    expect(reverse(xs) == expected)
}
```

If the test contains the implementation, it tests that you can write
the same bug twice. Good properties are *relations*: round-trips
(`decode(encode(x)) == x`), invariants (`sorted(xs).len() == xs.len()`),
comparisons against a slow-but-obvious reference, and idempotence
(`f(f(x)) == f(x)`).

**Expecting `expect` to stop the test.** It reports and continues.
Everything after it still runs, which may cascade into confusing
follow-on failures. `require` is ○.

**Reaching for an assertion library.** There is nothing for one to add.
If you find yourself wanting `assert.contains`, write
`expect(xs.contains(x))` — the compiler will report both sides of the
comparison it can see.

**Putting a test in a file and running `glide run`.** Tests run with
`glide test`. `glide run` needs a `main`.

**Assuming a benchmark ran.** They are skipped, and reported as `skip`.

**Relying on test order or shared state.** Tests are serial today,
which makes shared state *work* — and it will stop working the moment
anything is parallelised. Each test should set up what it needs.

**Forgetting that tests see module internals.** That is a feature
(testing a private invariant is legitimate) and a hazard (a test that
reaches into internals breaks when you refactor). Test through the
public API by default; reach inside when the invariant is genuinely
internal.

---

### 6. Performance Considerations

**A property test runs the body 100 times**, plus shrinking steps on
failure. If your test body is expensive — hitting a database, sleeping
— 100 cases is 100 times the cost. Property tests want pure, fast
functions; that is the natural fit anyway.

**Shrinking re-runs the body per candidate.** A failing property with
an expensive body and a large counterexample can take a while to
shrink. This is a good reason to keep property-test bodies cheap.

**Fixed seeds mean no flakiness from generation.** The same 100 cases
run every time, so a passing test does not become a failing test
because of luck. The trade: the generator explores the same space every
run, so a bug that needs case 101 is never found. Real QuickCheck
implementations vary the seed by default; Glide chose reproducibility.

**Test caching** is kept from Go, and `DESIGN.md` notes it is *sounder*
here: comptime is hermetic and does no IO, so the inputs to a test are
more completely known than in Go, where a test can read a file the
cache does not track. (The repository's own `Makefile` uses
`go test -count=1` for exactly that reason on the Go side.)

**In the interpreter**, a property test is 100 tree-walked runs. That
is slow enough to notice on a heavy body and fine for the pure
functions properties are best at.

---

### 7. Best Practices

**Write the property, then the examples.** The property states the
contract; the examples document the shape and catch the case where the
property itself is wrong.

```glide
// The contract
test "sorting preserves length" (xs: List<Int>) {
    expect(xs.sorted().len() == xs.len())
}

test "sorting is idempotent" (xs: List<Int>) {
    expect(xs.sorted().sorted() == xs.sorted())
}

// The documentation
test "sorted example" {
    expect([3, 1, 2].sorted() == [1, 2, 3])
}
```

**Look for the four property shapes.** Almost every useful property is
one of:

| Shape | Example |
|---|---|
| Round-trip | `decode(encode(x)) == x` |
| Invariant | `sorted(xs).len() == xs.len()` |
| Oracle | `fast(x) == slow_but_obvious(x)` |
| Idempotence | `normalise(normalise(x)) == normalise(x)` |

If you cannot find one of those, the function may be doing too much.

**Put small tests next to the code they test.**

```glide
pub fn slugify(s: String) -> String { … }

test "slugify lowercases and hyphenates" {
    expect(slugify("Hello World") == "hello-world")
}

test "slugify is idempotent" (s: String) {
    expect(slugify(slugify(s)) == slugify(s))
}
```

Inline tests read as executable documentation, and they are right there
when someone changes the function.

**Use a separate `_test.gld` file for suites.** When tests need
fixtures, helpers, and setup, they are a program of their own and
deserve a file.

**Name tests as statements of fact.**

```glide
// Bad
test "test slugify" { … }
test "case 3" { … }

// Good
test "slugify collapses repeated spaces" { … }
test "empty input produces empty output" { … }
```

The name is what you read in a failure report. Make it say what broke.

**Do not mock what you can construct.** Glide's mandatory
initialisation and sum types make real values cheap to build. A test
with a real `Config` and a real in-memory store is more honest than one
with three mocks and a verification script.

```glide
// Good — a real implementation of the trait, for tests
type MemStore = struct { notes: List<Note> }
impl NoteStore for MemStore { … }
```

That is the consumer-defined-trait pattern from Chapter 17, and it is
the reason those traits should be small: a small trait is easy to
implement for real in a test.

**Keep `glide test` green as the gate.** It is the enforcement boundary
in the designed toolchain, not just a test runner.

---

### 8. Examples

**A complete tested module:**

```glide
fn reverse(xs: List<Int>) -> List<Int> {
    let mut out = []
    for i in 0..xs.len() {
        out.push(xs[xs.len() - 1 - i])
    }
    out
}

fn add(a: Int, b: Int) -> Int { a + b }

test "add works" {
    expect(add(2, 2) == 4)
}

test "this one fails" {
    expect(add(2, 2) == 5)
}

test "reverse twice is identity" (xs: List<Int>) {
    expect(reverse(reverse(xs)) == xs)
}

test "broken property" (xs: List<Int>) {
    expect(reverse(xs) == xs)
}

bench "adding" {
    let mut t = 0
    for i in 0..1000 { t += i }
}
```

```
$ glide test example.gld
ok    add works
FAIL  this one fails
      line 13: expect failed: left == right
        left:  4
        right: 5
ok    reverse twice is identity  (100 cases)
FAIL  broken property
      input: xs = [0, 1]
      line 21: expect failed: left == right
        left:  [1, 0]
        right: [0, 1]
skip  bench "adding" (benchmarks not implemented yet)
$ echo $?
1
```

Everything the chapter describes, in one run. Look particularly at the
two failure reports: the example test names both sides of the
comparison, and the property test names the **shrunk** input as well.
`[0, 1]` is not what the generator produced first — it is what survived
shrinking, and it is the minimal witness that reversal is not identity.

**A property test finding a real bug.** Here is a plausible,
wrong-looking-correct implementation:

```glide
// Intended: split a list into two halves.
fn halves(xs: List<Int>) -> (List<Int>, List<Int>) {
    let mid = xs.len() / 2
    let mut a = []
    let mut b = []
    for i in 0..mid { a.push(xs[i]) }
    for i in mid..xs.len() { b.push(xs[i]) }
    (a, b)
}

test "halves example" {
    let (a, b) = halves([1, 2, 3, 4])
    expect(a == [1, 2])
    expect(b == [3, 4])
}

test "halves preserve everything" (xs: List<Int>) {
    let (a, b) = halves(xs)
    let mut joined = a
    for x in b { joined.push(x) }
    expect(joined == xs)
}

test "halves differ by at most one" (xs: List<Int>) {
    let (a, b) = halves(xs)
    expect(b.len() - a.len() <= 1)
    expect(b.len() >= a.len())
}
```

```
ok    halves example
ok    halves preserve everything  (100 cases)
ok    halves differ by at most one  (100 cases)
```

The example test passes on the case the author thought of. The two
properties assert the *contract* — nothing is lost, and the split is
balanced — and they hold for empty lists, single elements, and odd
lengths, none of which the example covers and all of which the
generator produces.

**The testing pyramid, in one comparison:**

```glide
// Bad — a test that only restates the implementation
test "slugify" {
    expect(slugify("Hello") == "hello")
}
```

That passes, and it will keep passing if you break the space handling,
the punctuation handling, and the empty case.

```glide
// Good — properties for the contract, examples for the shape
test "slugify is lowercase" (s: String) {
    expect(slugify(s) == slugify(s).to_lower())
}

test "slugify is idempotent" (s: String) {
    expect(slugify(slugify(s)) == slugify(s))
}

test "slugify has no spaces" (s: String) {
    expect(!slugify(s).contains(" "))
}

test "slugify example" {
    expect(slugify("  Hello,  World! ") == "hello-world")
}
```

Three universal statements and one worked example. The properties will
catch the input you did not imagine; the example tells the next reader
what the function is for.

---

### 9. Summary & Exercises

**Summary**

- Tests are a **language construct**: `test "name" { … }`, legal in any
  `.gld` file, no framework, no `t` parameter, no naming convention.
- **`expect` is the only assertion** and is compiler-known: it reports
  both sides of a comparison on failure. That is why there is no
  matcher library and why the ecosystem will never grow one. It fails
  and *continues*; `require` is ○.
- **A test with parameters is a property test.** 100 generated cases, a
  fixed per-test seed, and case 0 is always the simplest value because
  empty-case bugs dominate.
- **Shrinking** reduces a failure to a minimal input, which converts a
  counterexample into a diagnosis.
- Useful properties are round-trips, invariants, oracles, and
  idempotence. A property that reimplements the function tests nothing.
- Benchmarks are `bench "name" { … }` and **the runner owns the timing
  loop** — Go's `b.N` protocol hands users a loop they get wrong. ○
  (parsed and skipped today).
- Tests run **serially by default** within a module; default-parallel
  makes suites flaky, and the race detector covers the real concern.
- `glide test` is the enforcement boundary: tests plus format check,
  lints, unused code, doc links, race detector.
- `test` and `bench` are **contextual keywords** — `let test = 5` is
  legal.
- ○: `require`, benchmark execution, `derive Arbitrary` for user types,
  and seeded deterministic schedule fuzzing — the last of which is
  possible only because Glide owns the scheduler, the clock, and build
  hermeticity.

**Exercises**

1. **Convert three example tests into one property.** Take a function
   in your codebase with three or more example tests. Find the
   statement all three are instances of, and write it as a property.
   Then check whether the property passes — in perhaps a third of
   cases, it will not, and you will have found a bug your examples
   missed.

2. **Find the four shapes.** For a module you know, write down one
   round-trip property, one invariant, one oracle, and one idempotence
   property. If any of the four is impossible to state, ask what that
   says about the module's design — functions without stateable
   contracts are usually doing more than one thing.

3. **Design a shrinker.** Given a failing property over
   `(name: String, age: Int)`, write down the order in which you would
   try smaller variants and the rule for when to stop. Then compare
   your answer with the greedy algorithm described in Under the Hood
   and identify a case where yours does better. That gap is the
   difference between greedy and integrated shrinking, and it is a real
   open question in the property-testing literature.
