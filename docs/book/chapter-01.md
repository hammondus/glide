# Chapter 1: Why Glide Exists

> "Do we need ANOTHER programming language? I would definitely say NO."
> — the first line of Glide's own README

Every language is a reaction to a set of problems its creators faced.
Usually those problems are external: Google's build times, Mozilla's
segfaults, Sun's applets. Glide's problem is different, and stating it
honestly is the only way the rest of the book makes sense.

---

### The Problem Space: Choosing a Language in 2026

Picture an engineer who has written Go for a decade. They like it. The
build takes under a second. The binary is one file, and it runs on the
ARM box behind the nginx proxy without a runtime install. Goroutines
are cheap enough that "just spawn one" is usually right. The formatter
ended an argument that C++ teams are still having. When they read
someone else's Go, they can follow it.

They also keep writing the same four bugs.

A `User{}` with an empty ID type-checks, flows three layers down, and
breaks somewhere far from where it was made. A `nil` map read returns
a zero value that is indistinguishable from a stored zero. An `err` is
checked in eleven places and forgotten in the twelfth, because
`if err != nil` is four lines of visual noise per call and the eye
slides off it. A `switch` over a "kind" field silently ignores the new
case that was added last week, because Go has no way to say "this value
is exactly one of these five things, and the compiler should tell me
when I miss one."

Now picture the same engineer trying Rust to fix those bugs. The type
system does fix them: no null, sum types, exhaustive matching, `Result`
and `?`. And in exchange they get lifetimes, `Pin`, `Send + 'static`,
`Box<dyn Fn>`, the turbofish, a macro system that tooling cannot see
through, a fifteen-second incremental build, and an async ecosystem
that the language's own maintainers describe as a second, harder
language.

There is no third door. That is the problem Glide exists to answer, and
it is a problem of *combination*, not invention. The features that fix
Go's bugs have all shipped somewhere. The features that make Rust
expensive are, for the most part, downstream of one decision Rust had
to make — no garbage collector — that a Go-shaped language does not
have to make.

---

### Nothing Here Is New, and That Is the Point

Glide's design document is not a list of inventions. It is a list of
*adoptions*, each with a citation. `LINEAGE.md`, the companion to
`DESIGN.md`, exists specifically to record for every decision: who
invented it, who adopted it, who tried living without it, and what that
evidence says.

The short version of where the pieces come from:

| From | Kept | Rejected |
|---|---|---|
| **Go** | The entire runtime model — green threads, channels, tracing GC, `defer`. Sub-second builds. One static binary. One canonical formatter. Errors as values. Directory-is-a-package. Inert, order-independent declarations. | `nil`, zero values, implicit interfaces, `init()`, runtime reflection, `:=`, `context.Context` threading. |
| **Rust** | `Result` + `?`, sum types with exhaustive `match`, `let`/`mut` immutability, `\|x\|` closure syntax, `Mutex<T>` that owns its data, sequential redeclaration, the orphan rule. | The borrow checker, lifetimes, macros, async/await, the turbofish, `Fn`/`FnMut`/`FnOnce`. |
| **Zig** | Comptime instead of macros, `errdefer`, `test` blocks as a language construct, `//`-only comments. | Manual memory management, comptime-as-generics, errors on every unused variable. |
| **Swift** | `T?` optionals, `if let`, `let … else`, declared trait conformance with structural satisfaction, leading-dot enum shorthand, block-scoped `defer`, trapping overflow in *every* build. | Trailing closures, `$0`, the two-name parameter split. |
| **Haskell / ML** | The data model: sum types, pattern matching, no null, type classes (as traits), typed holes. | Whole-program type inference, user-invented operators. |
| **Kotlin** | Named arguments and default parameter values, wholesale. | Trailing closures, `it`. |
| **Nim** | `distinct` types. | |
| **Python's Trio** | Nurseries — structured concurrency. | |
| **CLU** | Generators. | |
| **Java** | `java.time`'s type set. The virtual-threads vindication of green threads. | Everything else, with feeling. |

If you know two of those languages you already know most of Glide. The
novelty budget went into *semantics* — specifically into making
structured concurrency and no-null a default rather than a library —
and deliberately not into syntax.

---

### The Freedom That Makes This Possible

Glide is new and has no compatibility promise. Breaking changes are
free until further notice.

This sounds like an apology. It is a design asset, and it is worth
dwelling on, because it explains decisions that would otherwise look
reckless.

Go's v1 compatibility guarantee bought enormous trust. It also froze
every mistake that had been made by 2012. `if err != nil` could not be
improved without breaking every program ever written. Generics arrived
thirteen years late and had to be bolted on with square brackets chosen
for parser convenience. `math/rand` needed a v2 because the v1 API was
wrong and could not be changed. `net/smtp` sits in the standard library
frozen at 2011's assumptions about authentication, rotting in place.

Glide keeps Go's *discipline* — small, boring, orthogonal, one way to do
things — without the freeze. When a decision turns out wrong, it gets
fixed, and `glide fix` mechanically rewrites the callers. That is what
canonical formatting is *for*: when a formatter is a pure function from
AST to bytes, an automated rewrite produces a zero-noise diff, and a
breaking change costs one command instead of a migration project.

The cost is real and is named in the design document: you cannot build
a production system on Glide today and expect it to compile next year.
That is fine. Today it is a tree-walking interpreter and a design
document. The compatibility conversation starts when there is something
to be compatible with.

---

### What Glide Refuses

Like Go, Glide is defined as much by its absences as its features. Each
one below was argued, and the argument is recorded.

| Absent | The Glide answer | Why |
|---|---|---|
| **`null` / `nil`** | `T?`, an Option type the compiler forces you to unwrap | Every mainstream C-lineage language has a value that lies about its type. Making absence a *type* rather than a *value* moves the failure from production to compile time. This is the single highest-value decision in the language. |
| **Zero values** | Mandatory initialisation; a struct literal must account for every field | Go's zero values are null by another name. `User{}` type-checks and flows. Types can opt into a default via a trait, so `Mutex` and `Builder` still construct bare; *domain* types get no fake instances for free. |
| **Exceptions** | `Result<T, E>` and the `?` operator | Same reasoning as Go's: hidden control flow makes resource cleanup and reasoning hard. Unlike Go, the boilerplate is one character rather than four lines. |
| **Inheritance** | Composition for data, traits for behaviour | Inheritance is the feature every ecosystem regrets by year five. Fragile base classes, diamond problems, taxonomies that stop fitting. Its legitimate uses are covered by the other two mechanisms. |
| **The borrow checker** | A garbage collector, plus `mut` for auditability | Fighting the borrow checker is the single biggest reason people bounce off Rust. A good GC is fine for 95% of programs. The explicit cost: no compile-time "no data races ever" guarantee, and roughly the last 20% of performance. |
| **`async`/`await`** | Green threads and structured concurrency | Async makes concurrency a viral property of every function signature. Python maintains two parallel ecosystems because of it. Java spent fifteen years on reactive frameworks and then shipped virtual threads. Glide has a runtime, so it does not need the compiler transform that async exists to be. |
| **Macros** | Comptime — ordinary language code executed at compile time | Macros create a second language that the formatter, LSP, and grep cannot see through. Comptime covers roughly 90% of macro use cases with code that is just code. |
| **Runtime reflection** | Comptime reflection and `derive` | Reflection is an interpretive loop per call and the biggest hole in Go's auditability story. `derive Json` walks the type at compile time and emits the encoder you would have hand-written. |
| **`++` / `--`** | `x += 1` | Two characters of value. Swift shipped it and paid to remove it in Swift 3. In an expression-oriented language, a statement-only magic operator is a grammatical special case. |
| **A ternary operator** | `if` is an expression | Go refused the operator without providing the compensation, so Go programs spend four lines and a mutable variable on the innocent case. `let s = if ok { "on" } else { "off" }` is both halves. |
| **Function overloading** | Default parameters and named arguments | Overloading swamps resolution and produces dissertation-length errors. But Go's ban was paid for in `NewX`/`NewXWithTimeout` proliferation and the thirty-line functional-options pattern. Defaults plus named args cover overloading's legitimate 90%. |
| **Variadic functions** | Take a `List<T>`; callers write two brackets | Every Go variadic customer is dead here: `Printf` → interpolation, `append` → `push`, `New(1,2,3)` → list literals. |
| **Build scripts** | Nothing. `glide build` executes no user code | Cargo's `build.rs` and npm lifecycle scripts mean compiling someone's code runs their program on your machine. Most supply-chain attacks in both ecosystems ride exactly this. |
| **A top type (`any`)** | Sum types, generics, and `any Trait` | `any` is the escape hatch reflection crawls through. A top type can be added deliberately later; it can never be removed. |

Read that table again with an eye on the third column. Almost none of
these are matters of taste. Each is a specific bug class or a specific
ecosystem failure, named, with the language that demonstrated it.

---

### Why Glide Is Not "Go With Sum Types"

That description is close enough to be tempting and wrong in ways that
matter.

Adding sum types to Go would be an improvement. But sum types only pay
off in combination with three other things Glide has and Go does not:

**Exhaustive matching.** A sum type without a compiler that checks
coverage is a discriminated union you still have to grep. The value is
not "I can express one-of-N"; it is "when I add a sixth variant, the
compiler hands me the complete list of every place in the codebase that
needs updating." Go's `switch` cannot do this, and adding it would
break every existing `switch`.

**No zero values.** A sum type in a language with zero values has a
silent default variant. Go's `iota` enums demonstrate this precisely:
the zero value of a `Color` is `Red`, whether or not anyone chose it,
and a `Color` field on a freshly-made struct is `Red` by accident. The
moment you allow zero values, "exactly one of these N shapes" becomes
"one of these N shapes, or an uninitialised thing that claims to be the
first one."

**Pattern matching that binds.** Testing which variant you have and
extracting its payload must be one step. Go's type switch does this for
interfaces, at one level of nesting, with no payload destructuring and
no coverage check. `match { Ok(User{ role: Admin, name, .. }) => … }`
reaches two levels down, tests three things, and binds one — in one
pattern, with the compiler tracking what is left over.

Each of those requires breaking Go. Together they require a different
language. That is the honest answer to "why not just propose this to
the Go team."

And the same argument runs the other way. Glide is not "Rust with a
GC", either. Removing the borrow checker from Rust does not merely make
it easier; it deletes the reason for `Fn`/`FnMut`/`FnOnce`, for `move`,
for `Pin`, for lifetime annotations, for `&`/`&mut` at every call site,
for `Cow`, for most of `async`'s difficulty, and for the decade-long
struggle to stabilise generators. Roughly half of Rust's conceptual
weight is borrow-checker overhead, and a GC pays it off in one
transaction. What is left — the type system — is the part people
actually want.

---

### The Three Pillars

Three principles recur in every chapter of this book. They are not
slogans; they are constraints that produced specific decisions you will
meet later.

**1. Make illegal states unrepresentable.**

Not "check for illegal states" — *unrepresentable*. There is no null to
check for. There is no zero value to accidentally accept. A `Result`
cannot hold both a value and an error, the way Go's `(T, error)` pair
can. A `NoteId` cannot be passed where an `OrderId` is expected. A
`Loading | Loaded(Data) | Failed(Error)` value cannot be both loading
and failed, which the two-boolean version (`isLoading`, `hasError`)
absolutely can.

Every time you meet a design decision in this book that seems fussy,
check it against this pillar. Mandatory struct initialisation is fussy
until you notice it is the same decision as no-null.

**2. Auditability over cleverness.**

A reader should be able to skim a function and know what it can and
cannot do. This is why `mut` exists at all: it is the mark that tells
you, at a glance, which locals can change. It is why free functions
that mutate a parameter repeat the marker at the call site
(`sort(mut xs)`). It is why `?` is a visible character on the line
where the early exit happens, rather than an invisible exception. It is
why `unsafe` will be a block and not a file-level import. It is why
imports execute nothing.

The test for this pillar is a code review: can a reviewer who did not
write the code tell what changed? Go's capitalisation-as-visibility
fails this test — making a function public is a rename touching every
use site. Glide's `pub` is a one-line diff that says "this became
public."

**3. Human-friendly and performant are both hard requirements.**

Where they genuinely conflict, the default favours the human and an
explicit opt-in favours the machine. Garbage collection by default,
arenas where profiling demands them. Immutable strings by default,
`StringBuilder` when the loop is hot. `Int` is a 64-bit integer with
trapping overflow in every build; `wrapping_add` exists for code that
means it. Unbuffered prints so debug output always lands, with an
explicit buffered writer for bulk output.

The pattern is consistent: the safe, obvious spelling is the default,
and the fast spelling has a name you have to type. This is the exact
inverse of C's defaults, and it is why the target performance envelope
is "faster than Go, usually competitive with Rust, never beating
hand-tuned C" rather than "as fast as possible."

---

### What Running Code Exists Today

This book is unusual in that its subject is half-built, so it is worth
being concrete about what "half" means.

A tree-walking interpreter written in Go runs the core language today:
bindings and mutability rules, functions and closures, lists, maps,
tuples, structs, sum types with `match`, `Result` and `?`, `Option`,
`distinct` types, `impl` blocks and traits with default methods,
generators with `yield`, `defer` and `errdefer`, structured concurrency
with scopes and cancellation, channels and `select`, the time types,
and a testing runner with property-based tests and shrinking. Host
shims provide `os`, `fs`, `json`, `http`, and `sql` (SQLite).

The interpreter is about as fast as you would expect a tree-walker to
be — it is a semantics prover, not a production runtime. Its job is to
find the corners of the design cheaply, and it has: several decisions
in `DESIGN.md` are marked "forced by the interpreter", meaning the
design changed because writing the evaluator revealed a problem.

Running as of M4c: the type checker, sized numerics, explicit numeric
conversion, generic bound checking, trait conformance, `Ord`, boxed
`Option` and match exhaustiveness. Not yet running: comptime and
`derive`, the arithmetic operator traits, `unsafe`, `embed`, and most
of the designed standard library. Chapter 37 covers the path from here
to a compiler.

You will see this reflected in every chapter. Sections marked ✓ have
been run. Sections marked ○ are read from the design document. Where a
feature exists in a reduced form today, the book says so and explains
what the remaining milestones will change.

---

### The Aha! Moment: The Type System Is a Cost Model

If you take one insight from this chapter, take this one, because it
reframes almost every decision in the rest of the book.

Most languages present their type system as a correctness device: it
stops you doing wrong things. Glide's type system is also, and
deliberately, a *pricing* device. It makes costs visible at the place
where you pay them.

Building a string in a loop costs O(n²), so the accumulator has a name
you must type: `StringBuilder`. Arbitrary-precision integers cost an
allocation per operation, so they are `BigInt` chosen by name and never
a silent promotion from `Int`. A heterogeneous collection costs a box
and a vtable dispatch, so it is spelled `any Reader` rather than being
implicit the way Go's interface values are. Cancellation costs a
scope, so it is written `scope(timeout: 5.s)` rather than threaded
through as an invisible first parameter. Mutation costs auditability,
so it is spelled `mut` at the declaration *and* at free-function call
sites.

Compare Go, where a `[]byte`-to-`string` conversion copies and looks
free, where an interface value boxes and looks free, where `append` may
or may not allocate depending on capacity you cannot see, and where
`reflect` runs an interpretive loop inside a function call that looks
like any other. Those are not bugs; they are the deliberate trade Go
made for simplicity, and it mostly works. Glide makes the other trade:
the expensive thing gets a longer name.

Once you see this, the "fussy" decisions stop looking fussy. They are
all the same decision, applied consistently: **the source code should
tell you what it costs.**

---

The next chapter puts a working `glide` binary on your machine, which
is a shorter chapter than it would be for most languages — one `make
build` and you have the whole toolchain. Chapter 3 writes the first
program. From Chapter 4 the book turns systematic, and every chapter
follows the nine-section anatomy described in the [README](README.md).
