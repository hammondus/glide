# Chapter 36: Comptime, `derive`, and Metaprogramming

Everything in this chapter is **○**. None of it runs. It is the design
for how Glide does the things other languages use macros or reflection
for, and it is worth a chapter because three features you have already
met — `derive Json`, `derive Row`, and flexible `const` — are waiting
on it, and because the *fences* around it are as important as the
feature.

The one-line summary: **comptime is ordinary language code executed at
compile time**, it exists for exactly two purposes, and there is a hard
rule that it is **not a second generics system**.

---

### 1. Basic Usage

#### Const evaluation

An ordinary function, run at compile time because a `const` position
demands it:

```glide
fn make_crc_table() -> List<Int> {
    let mut t: List<Int> = []
    for i in 0..256 {
        let mut c = i
        for _ in 0..8 {
            c = if c % 2 == 1 { 0xEDB88320 ^ (c / 2) } else { c / 2 }
        }
        t.push(c)
    }
    t
}

const crc_table = make_crc_table()          // ○ evaluated at build time
```

**Same function, either phase.** There is no `constexpr` sub-language
and no separate const-evaluable dialect.

The result lands in **read-only data**: shared across the program, zero
startup cost, immutable by memory protection.

The example that shows why this matters:

```glide
const re = regex.compile("^[a-z][a-z0-9_]*$")        // ○
```

A bad pattern is a **compile error**, and the compiled automaton ships
in rodata. Compare Go's `regexp.MustCompile` in a `var`, which is a
runtime panic during `init()`.

#### `derive`

```glide
type Note = struct {
    pub id: NoteId
    pub title: String
    pub created: Instant
    pub body: String?
} derive(Json, Row, Debug)                   // ○
```

`derive` asks the compiler to generate implementations by walking the
type's structure at compile time. `derive(Json)` writes the encoder and
decoder you would have hand-written; `derive(Row)` writes the
column-name mapping; `derive(Debug)` writes the structural printer.

It looks like magic. It is ordinary code generation, and the output is
ordinary code.

Options are **typed comptime arguments**, never string tags:

```glide
type User = struct { … } derive(Json(rename_all: camel))     // ○
```

A typo'd option is a compile error. Compare `json:"nmae"`, which
compiles and ships.

#### The `derive` roster

| Derive | Generates | Chapter |
|---|---|---|
| `Json` | encoder + decoder | 31 |
| `Row` | database column mapping | 33 |
| `Debug` | structural `{x:?}` rendering | 6 |
| `Enum` | `all()`, `name()`, `from_name()` | 13 |
| `Arbitrary` | property-test generators | 22 |

#### The reflection API

Comptime code can inspect a type's structure — fields, names, types,
variants. That API is what `derive` implementations are written
against, and `DESIGN.md` calls it "the genuinely hard design problem"
with prior art in Zig's `@typeInfo` and C# source generators, "neither
fully right".

It is explicitly to be proven **in the interpreter, before any backend
exists**.

#### The discipline rules

Three, and they are load-bearing:

**No IO at comptime.** Builds stay hermetic and reproducible.
Code-generation-from-a-schema is a build step you run, not a comptime
trick.

**Fuel-limited evaluation.** An instruction quota; exceeding it is a
compile error, explicitly raisable. This is the halting-problem answer:
comptime cannot hang your build indefinitely.

**Deterministic by construction**, which follows from the first two —
and which makes caching comptime results always sound. The fast dev
backend leans on this.

#### What comptime is not

**Not macros.** No AST manipulation, no new syntax, no token trees.

**Not a generics system.** No user-written functions that take or
return types.

**Not runtime reflection.** There is none, at all, anywhere.

---

### 2. Under the Hood

#### How a `derive` works

`derive Json` is an ordinary comptime function. It receives the type's
structure through the reflection API, walks the fields, and emits code:

```
for each field:
    emit: write the key
    emit: write the value via its Json impl
```

The emitted code is a straight-line encoder — no loop over field
metadata at runtime, no string parsing, no boxing. It is what you would
write by hand, generated.

That is the whole mechanism, and it is why the performance claim
("serde-class speed") is credible: the generated code has no
abstraction left in it.

#### Why this is not runtime reflection

Go's `json.Marshal` does the *same walk*, at runtime, on every call:
look up the type descriptor, iterate the fields, parse each struct tag
string, switch on the kind, box the value. Per call.

Comptime does the walk **once, at build time**, and the runtime sees
only the result. Same information, different phase.

#### Why this is not proc macros

Rust's `serde` derives are procedural macros: they receive a token
stream and produce a token stream, executed by a compiler plugin
compiled as a separate crate.

The costs `DESIGN.md` names: **a second compiler** (proc-macro crates
build first, and they build for the host, which complicates
cross-compilation), **slow** (macro expansion is a significant fraction
of Rust build times), and **opaque** (the formatter, LSP, and `grep`
cannot see through the expansion).

Comptime functions are just functions. The same tooling sees them.

#### The `const` M2 shim

Today, `const` initialisers are restricted to **pure expressions** — no
function or module calls:

```glide
const max_retries = 3            // ✓
const table = make_table()       // ○
```

Full comptime evaluation arrives with the compiler.

#### Literals are arbitrary-precision until they land in a type

This falls out of comptime, and it is Go's untyped constants / Zig's
`comptime_int`:

```glide
const k = 1 << 100               // ○ fine in constant math
let x: u8 = 300                  // ○ compile error, not a wrap
```

---

### 3. Why This Design?

#### Why comptime instead of macros

Zig's insight, adopted: **run ordinary language code at compile time**.

`DESIGN.md`'s claim is that this covers roughly 90% of macro use cases
without creating a second language that tooling cannot see through.

The 10% it does not cover is named honestly as a **known gap, accepted
forever**: DSL-style macros — `html!` templates, embedded SQL syntax,
new syntactic forms. Comptime functions over string constants get you
compile-checked SQL; they do not get you new syntax.

And that is the point. Embedded custom-syntax DSLs are exactly the
"second language tooling cannot see" that macros were banned for. `//
AST macros, ever. They're how ecosystems become unreadable.`

#### Why the "comptime is not generics" fence matters more than the feature

This is the most important paragraph in the chapter.

Zig uses comptime *as* its generics system: `fn List(comptime T: type)
type` returns a type. It is elegant and it has a specific consequence —
**C++-template-tier error messages, deep inside the callee.**

When a generic is checked at its *use* site rather than its
declaration, a mismatch surfaces as a failure inside the library's
body, with a stack of instantiation frames. That is why C++ template
errors are legendary, and why C++20 added Concepts.

Zig had no choice; comptime is all they have. Glide has trait-bounded
generics (Chapter 18), where bounds are checked **at the declaration**,
so:

- The library author gets an error in their own code if they use
  something the bound does not provide.
- The caller gets "your `T` does not implement `Ord`" at the call site.

`DESIGN.md` states the danger of removing the fence precisely:

> Without the fence, hard generic bounds get "solved" by escaping to
> comptime and the ecosystem ends up duck-typed and undiagnosable.

That is a prediction about *ecosystem behaviour*, not about the
feature. Given an escape hatch from a hard bound, people take it, and
the resulting libraries have no checkable contracts.

So: **no user-written functions that take or return types.** `List<T>`
comes from generics, never from a comptime function.

#### Why no runtime reflection — absent, not discouraged

Everything Go uses `reflect` for happens at comptime here, against
static types.

The costs of runtime reflection, from `DESIGN.md`: it is "an
interpretive loop per call, unauditable", "the biggest hole in Go's
auditability story", and "a permanent performance tax".

The auditability point is the interesting one. You cannot tell by
reading a function what it will do to a value if it decides at runtime
by inspecting the value's type. Reflection defeats the whole
"skim a function and know what it can do" property that Chapter 1
listed as a pillar.

The real cost is named: **no deserialising into a type named by a
runtime string.** The rare dynamic case hand-rolls a registry, visibly:

```glide
// The escape hatch, written out rather than provided
let decoders: Map<String, fn(String) -> Result<Event, Error>> = [
    "created": decode_created,
    "deleted": decode_deleted,
]
```

Explicit, greppable, and typed.

#### Why no IO at comptime

Hermetic, reproducible builds. If comptime could read a file or hit the
network, a build's output would depend on the machine it ran on and the
day it ran.

The legitimate use case — generating code from a schema file — is a
**build step you run, visibly, with the output committed**. That is the
same position as "no build scripts" (Chapter 2), and the same position
as `sqlc`-style schema codegen (Chapter 35). The schema becomes a
versioned artifact.

`embed` is the designed exception, and it is not really one: `embed
"static/**" as assets` is *declarative grammar*, and the **build
system** provides the bytes. Comptime never does the IO.

#### Why fuel-limited evaluation

Because comptime is Turing-complete and a compiler that can hang is
hostile.

An instruction quota with an explicit way to raise it means a runaway
comptime computation is a compile error with a clear cause, not a build
that never finishes.

#### Why determinism makes caching sound

If comptime is deterministic — no IO, no clock, no randomness — then
the result of evaluating a `const` or expanding a `derive` depends only
on its inputs. So the fast dev backend can cache those results across
builds without any invalidation logic beyond hashing the input.

That is a real compile-speed lever, and it is only available because of
the discipline rules.

---

### 4. Competing Approaches

**Zig.** Comptime as the whole metaprogramming story, including
generics. The direct inspiration for the *mechanism* and the direct
counterexample for the *scope* — Glide takes comptime and fences it
away from generics.

**Rust.** Declarative macros (`macro_rules!`) and procedural macros.
Enormously powerful — `serde`, `tokio::select!`, `sqlx::query!` — and
the costs are a second compiler, significant build time, and opacity to
tooling. Rust's `const fn` is the closest thing to Glide's const
evaluation and is deliberately restricted.

**C++.** Templates (accidentally Turing-complete), `constexpr`,
`consteval`, and Concepts. The cautionary tale for use-site checking,
and `constexpr` is a genuine success — C++ arrived at
"run ordinary code at compile time" from the other direction.

**C#.** Source generators — compile-time code generation with access to
the semantic model. `DESIGN.md` cites them as prior art for the
reflection API, "neither fully right" alongside Zig's `@typeInfo`. C#
also has full runtime reflection, which is how most of its
serialisation works.

**Go.** Runtime reflection plus `go generate` (a comment that runs a
command — a build step by convention). `stringer` exists as an external
tool because Go's enums cannot enumerate themselves, which is exactly
the `derive Enum` use case.

**Java.** Annotation processors (compile-time, verbose) plus runtime
reflection (used by everything). Lombok is the annotation processor
that rewrites your AST, and its relationship with IDEs is the standard
argument against tooling-opaque codegen.

**Lisp.** Macros that operate on the language's own data structures,
with no syntax barrier. The most powerful version, and the source of
the "every codebase becomes its own dialect" critique that `DESIGN.md`
invokes when it bans AST macros.

---

### 5. Common Mistakes

*(Anticipated — none of this runs yet.)*

**Reaching for comptime to solve a generics problem.** If you find
yourself wanting a function that takes a type and returns a type, stop:
the answer is a trait bound. That instinct is exactly what the fence
exists to catch.

**Expecting IO at comptime.** Reading a schema file at compile time is
the thing that is banned. It is a build step with committed output.

**Expecting new syntax.** Comptime gives you code generation, not
grammar extension. `html!{ <div>…</div> }` will never exist.

**Putting expensive computation in a `const` without thinking about
build time.** Comptime evaluation costs build time, bounded by fuel. A
`const` that computes a million-entry table costs that computation on
every clean build.

**Assuming `derive` output is invisible.** It is ordinary generated
code. The designed tooling can show it to you — which is the difference
from a proc macro whose expansion you must ask a special tool to
reveal.

**Using a runtime string to select a type.** There is no runtime
reflection, so there is no `decode_into(typeName)`. Write the registry.

---

### 6. Performance Considerations

**Comptime shifts cost from runtime to build time.** That is the entire
trade, and it is almost always the right one — a program runs many
times and builds fewer times.

**`const` values cost nothing at runtime.** They land in read-only
data: no startup construction, shared across the process, immutable by
memory protection. A `const` lookup table strictly beats Go's `var`
equivalent, which is built in every process at startup.

**`derive`d code is straight-line.** No metadata loop, no string
parsing, no boxing. That is the "serde-class speed" claim, and it is
what separates comptime derive from reflection.

**Comptime evaluation costs build time**, bounded by the fuel limit and
mitigated by caching (which determinism makes sound).

**Monomorphisation and comptime interact** (Chapter 18): each is a
build-time cost that buys runtime speed, and together they are why
"compile speed is a feature" needs to be a stated principle rather than
an aspiration.

**No runtime reflection means no reflection tax.** Every Go program
that encodes JSON pays an interpretive loop per field per call. That
cost is simply absent.

---

### 7. Best Practices

*(Anticipated, from the design's own guidance.)*

**Use `const` for anything computable at build time.** Lookup tables,
compiled regexes, parsed configuration schemas, precomputed constants.
The rule of thumb: if it does not depend on runtime input, it is a
`const`.

**Prefer `derive` to hand-written boilerplate, and hand-written code to
a clever derive.** A derive is right when the mapping is mechanical
(JSON, database rows, debug output). When the mapping has real
decisions in it — a wire format that differs from your domain model —
write the conversion function (Chapter 33's "keep the wire type
separate").

**Do not write a derive for something used once.** A derive is a code
generator; a code generator with one customer is a function.

**Respect the fence in your own designs.** If your library's ergonomics
would improve with a comptime function that returns a type, the design
document's prediction is that the ecosystem cost outweighs your
convenience. Find the trait bound.

**Keep comptime computations cheap.** Build time is a shared resource
and "compile speed is a feature" is principle three.

**Reach for a build step when you need IO.** Schema codegen, protocol
buffers, embedded asset manifests. Run it, commit the output, and the
build stays hermetic.

---

### 8. Examples

*(All ○ — illustrative of the design.)*

**The three derives on one type:**

```glide
type NoteId = distinct Int

type Note = struct {
    pub id: NoteId
    pub title: String
    pub created: Instant
    pub body: String?
} derive(Json, Row, Debug)
```

Four lines of declaration, and you get:

- A JSON encoder and decoder where `body` is optional because it is
  `String?`, `id` unwraps because it is `distinct`, and `created`
  serialises as RFC 3339 — all falling out of the type, with no tags.
- A database row mapper keyed by **column name**, so reordering a
  SELECT is harmless.
- Structural debug output for `{note:?}`.

The equivalent Go needs struct tags on every field (unchecked strings),
`sql.NullString` for the optional column, positional `rows.Scan`, and
`%+v` that prints struct guts at users.

**Const evaluation earning its keep:**

```glide
// A compiled regex, validated at build time, shipped in rodata.
const slug_pattern = regex.compile("^[a-z][a-z0-9-]*$")

// A lookup table, computed once, at build time.
const crc_table = make_crc_table()

// Configuration derived from other constants.
const max_body = 1024 * 1024
const chunk_size = max_body / 16
```

Every one of these is `var` plus `init()` in Go, which means: built at
startup in every process, mutable in principle, and — for the regex —
a runtime panic if the pattern is wrong.

**Compile-checked SQL, the shape comptime enables:**

```glide
// The placeholder check (Chapter 35) is a pure comptime parse of a
// literal string: no database, no network, hermetic.
db.query<Note>(
    "select id, title, created, body from notes where org = :org",
    ["org": org],
)
```

At compile time: parse the query for `:name` placeholders, compare
against the parameter map's keys, and error on a mismatch naming the
parameter. At runtime: nothing.

This is the "unoccupied sweet spot" from Chapter 35 — schema checking
needs a database, but placeholder checking needs only the string.

**The known gap, stated:**

```glide
// This will never exist.
let page = html! {
    <div class="note">
        <h1>{note.title}</h1>
    </div>
}
```

Templates go through the standard library's templating engine with
contextual auto-escaping, not through a macro that invents syntax.
`DESIGN.md` accepts this gap forever, because the alternative is a
second language the formatter, LSP, and `grep` cannot see through.

**The escape hatch, written out:**

```glide
// No runtime reflection means no decode-by-type-name. Write the
// registry — it is explicit, typed, and greppable.
type Event = Created{ id: Int } | Deleted{ id: Int }

fn decode_event(kind: String, body: String) -> Result<Event, Error> {
    match kind {
        "created" => decode_created(body)
        "deleted" => decode_deleted(body)
        _         => Err(.UnknownKind{ kind: kind })
    }
}
```

Compare Go, where this would be a `map[string]reflect.Type` and a
`reflect.New` call. Six lines instead of three, and every possible
event type is visible in the source.

---

### 9. Summary & Exercises

**Summary**

- **Everything in this chapter is ○.** It is the design behind
  `derive Json`, `derive Row`, `derive Debug`, and flexible `const`.
- **Comptime is ordinary language code executed at compile time** —
  Zig's insight. Same function, either phase; no `constexpr`
  sub-language.
- It exists for exactly **two things**: **const evaluation** (functions
  running in const positions, results landing in read-only data) and
  **derive via comptime reflection** (walking a type's structure to
  emit plain code).
- **The fence matters more than the feature: comptime is not a second
  generics system.** No user functions taking or returning types.
  `List<T>` comes from trait-bounded generics, checked at the
  *declaration*, so errors point at your code rather than deep inside a
  callee — the C++/Zig failure mode, avoided.
- **No AST macros, ever.** Comptime covers ~90% of macro use cases
  without a second language tooling cannot see through. The accepted
  permanent gap is DSL-style macros — `html!` will never exist.
- **No runtime reflection. Absent, not discouraged.** It is an
  interpretive loop per call, a permanent performance tax, and the
  biggest hole in Go's auditability story. The real cost — no
  deserialising into a type named by a runtime string — is paid by
  hand-rolling a visible registry.
- **Three discipline rules:** no IO at comptime (hermetic builds);
  fuel-limited evaluation (a compile error, not a hung build);
  deterministic by construction (so caching comptime results is always
  sound).
- **`derive` options are typed comptime arguments**, not string tags —
  a typo is a compile error rather than a shipped bug.
- The **reflection API is the genuinely hard design problem**, with
  Zig's `@typeInfo` and C# source generators as imperfect prior art,
  and it is to be proven in the interpreter before any backend exists.
- M2 shim: `const` initialisers are restricted to pure expressions.

**Exercises**

1. **Cost the reflection tax.** Benchmark Go's `encoding/json` against
   a hand-written encoder for the same struct. The ratio is what
   comptime derive is claiming to recover. Then benchmark Rust's
   `serde` against the same hand-written encoder — that ratio is the
   target.

2. **Find the escape to comptime.** In a Zig codebase (or a C++ one
   using templates), find a generic function whose error message, when
   misused, points inside the library rather than at the call site.
   Write down what the declaration-site-checked version would have
   said. That difference is what the fence buys.

3. **Design a derive.** Pick a mechanical mapping you write by hand
   today — a builder, an equality function, a CLI flag parser from a
   struct, a fixture generator. Write the comptime function's outline:
   what it reads from the reflection API, what it emits, and what its
   typed options are. Then ask whether the mapping is genuinely
   mechanical or whether it has decisions in it — if it has decisions,
   it wanted a function, not a derive.
