# Chapter 35: The Implementation Path

Most language books do not have this chapter, because most languages
are finished. Glide is not, and the *path* from a tree-walking
interpreter to a self-hosted compiler is a design artifact in its own
right — one with a genuinely clever shortcut in it.

`DESIGN.md` frames it as **two mountains wearing one name**: the
compiler is a hill with a shortcut; the runtime is the actual decade,
deferred indefinitely by the shortcut.

This chapter is entirely about ○ work, except the parts describing what
runs today.

---

### 1. Basic Usage

#### The four steps

```
1. Go tree-walking interpreter          (M1-M3 complete)
2. The checker, in Go                   ← here (M4)
3. Compiler frontend rewritten in Glide, run on the interpreter
4. Glide→Go transpiler, which compiles itself
5. Someday: LLVM + own runtime
```

**Step 1 — the interpreter (done).** Written in Go, it proves
semantics. `DESIGN.md` names the three riskiest: generators, structured
concurrency, and comptime reflection. Two of the three now run.
Standard library modules are Go host shims behind Glide-shaped
interfaces.

**Step 2 — the checker, in Go (M4, in progress).** Annotations stop
being documentation. This step was not in the original plan — the
checker was step 2 *in Glide* — and the next section explains the
reversal.

**Step 3 — the frontend rewritten in Glide.** Lexer, parser, and
checker, run on the interpreter. **Dogfooding at scale starts here**,
against a design that step 2 has already proven.

**Step 4 — the Glide→Go transpiler**, which then compiles itself.

**Step 5 — LLVM and a native runtime.** The real mountain, and the only
step that takes Go out of the pipeline rather than out of the source
tree.

#### One frontend, two backends

The single most important structural fact about Glide's implementation:
**the lexer, parser, and checker are one implementation, shared by the
interpreter and the compiler.** The two tiers differ in how they
execute a checked program. They never differ in what they accept, or in
what it means.

This is not an efficiency measure. It is what makes it safe to ship
both tiers. "Runs interpreted, fails compiled" is the bug class that
discredits every dual-tier language, and it is unreachable when exactly
one thing decides. OCaml has run `ocamlc`, `ocamlopt`, and an
interactive toplevel over one frontend since the early 90s; Rust's Miri
interprets the same MIR its codegen backends consume, and rustc's own
compile-time evaluator *is* that interpreter.

The consequence for you: **the interpreter is not scaffolding.**
`glide run` is meant to be a statically-checked scripting language in a
single static binary — a product, not a stepping stone. An earlier plan
had it freezing at self-hosting so that "the evolving language keeps
exactly one implementation"; the shared frontend delivers that property
outright, and without the scripting tier slowly becoming an older
language.

#### Where the implementation is today

M1, M2, and M3 are complete, and each was defined by a program it had
to run:

| Milestone | Target | Delivered |
|---|---|---|
| **M1** | `wordfreq` | the whole expression language, no user types |
| **M2** | the `Tree` example | `type`, `match` with guards, `impl`, `mut self`, `if let`, generators, `test` blocks with property testing |
| **M3** | the `notes` service | named-field variants, dot shorthand, `distinct`, named arguments, `defer`, structured concurrency, channels, `select`, time, http/sql/json shims |

Defining a milestone by "run this program" rather than "implement this
feature list" is why the surfaces are minimal and coherent — the
dogfood rule (Chapter 30) applied to the implementation itself.

#### What is deliberately absent after M3

`Mutex<T>`, `derive`, typed JSON decode, typed query rows, method
values, `or |e|` blocks (declined), time formatting/parsing/calendars,
and HTTP error middleware. (Static generics and trait conformance were
on this list through M3 and M4b; M4c checks both.)

#### The two tiers

The tiered-backend design is what makes several decisions in this book
possible:

| | Dev tier | Release tier |
|---|---|---|
| Integer overflow | **traps** | wraps |
| Backtraces | captured | skipped |
| Unused code | warning | **error** |
| Debug info | complete (guaranteed) | best-effort |
| Optimisation | speed of compilation | speed of code |

`DESIGN.md` calls this "tiered backends keep paying rent". Each row is
a case where the right answer for the edit loop and the right answer
for production genuinely differ.

**The hard rule:** those may differ. **Semantics may not.** Loop
back-edge cancellation checks were considered and rejected precisely
because dev-tier programs would be more cancellable than release-tier —
and "semantics that differ by tier are poison" (Chapter 26).

---

### 2. Under the Hood

#### Why the interpreter is a tree-walker

Because its first job was to prove **semantics**, cheaply.

A bytecode VM would be faster and would take longer to write and be
harder to change. A tree-walker over the AST is the shortest path from
"we designed this" to "does it actually work" — and several decisions
in `DESIGN.md` are marked *"forced by the interpreter; ratified"*,
meaning the design changed because writing the evaluator revealed a
problem:

- Statement termination rules, including the leading-dot continuation.
- `//`-only comments.
- Struct literals banned in control-flow headers.
- Bare blocks as expressions.

That is the interpreter doing its job.

#### Why it was dynamically checked, and why that is ending

Through M1–M3, type annotations were parsed, kept as strings, and
ignored. `DESIGN-DECISIONS.md` gave the reason in one sentence:
*writing a static checker in Go now would be thrown-away work*, because
the checker was step 2 and would be written in Glide.

**That decision was reversed, and the reversal is worth studying** —
both halves of the argument failed, in instructive ways.

The *thrown-away* half assumed the interpreter retires at self-hosting.
It does not; it ships. Work that lands in a shipping tier is not thrown
away, so the premise simply evaporated. The lesson is not that the
reasoning was sloppy — it was sound *given* the roadmap it was written
against. It is that a decision inherits every assumption of the plan
around it, and when the plan moves, the decision has to be re-derived
rather than re-quoted.

The *sequencing* half is the more interesting failure: it inverted its
own dependency. The bootstrap justified writing the frontend in Glide
on the grounds that compilers are the best-case workload for this
feature set — ASTs are sum types, a checker is exhaustive matching, `?`
threads the phases. All true. **And none of it was available.**
Exhaustiveness was dynamic. Generics never reached the AST. `Option`
could not nest. Trait conformance was asserted rather than verified.
The plan called for writing the largest, most type-dense Glide program
that will ever exist, in the one tier that checked none of the
properties that made it the right program to write.

What actually transfers into Glide was never the Go code. It is the
*design* — the type representation, the bidirectional rules,
expected-type propagation, the diagnostic wording — plus the
conformance corpus. Those are the expensive part, they are far cheaper
to get right in a language that checks your work, and having them makes
the Glide frontend a transcription rather than a rediscovery.

Accepted cost, stated plainly: the Go frontend gets written once and
ported once.

Until M4 lands, what keeps programs honest is that rules a checker
would enforce statically are enforced **dynamically** instead: `mut`,
the nested-shadow ban, `let … else` divergence, the tail-value rule,
and match exhaustiveness. Programs still cannot cheat; they just find
out later.

#### The three implementation decisions worth knowing

**One interpreter lock (a GIL), released around blocking operations.**
Chapter 25 covered it. The point worth repeating: **the lock is the
semantics, not just a guard.** Tasks interleave exactly at blocking
operations, which is the ratified cancellation-point rule.

**Generators run on a goroutine plus a channel.** The cheapest correct
lazy implementation for a tree-walker, because a suspended goroutine
*is* a suspended frame with locals intact (Chapter 24).

**Cancellation is a Go panic, not a signal.** Panics already unwind
uncatchably through every construct, and the panic path already ran
`defer` and `errdefer` — which is exactly the ratified cancellation
behaviour (Chapter 26). The mechanism came for free.

#### Why the transpiler is the clever part

This is the shortcut that defers the decade.

**Glide's runtime model was Go's from day one** — green threads,
tracing GC, `defer`, channels, value structs. So Glide lowers onto Go
source nearly one-to-one, and **every hellish part is prepaid**:

| Needed | Provided by Go |
|---|---|
| Garbage collector | Go's, battle-tested |
| Green-thread scheduler | goroutines, work-stealing |
| Cross-compilation | `GOOS`/`GOARCH` |
| Static binaries | `CGO_ENABLED=0` |
| Debuggability | readable output, `//line` directives, delve |

Writing a garbage collector and a green-thread scheduler is the
multi-year part of a language implementation. Transpiling to Go skips
both, permanently if you want.

It also gives an **auditable bootstrap chain from a mainstream
toolchain**: no binary seed, no trusting-trust anxiety. And it resolves
the tiered-backend plan for years — interpreter as dev tier, transpiled
Go as shipping tier.

#### The known wrinkle

`DESIGN.md` lists what must be lowered and flags the hard one:

| Glide construct | Lowering |
|---|---|
| Sum types | tagged structs — mechanical |
| `match` | switches — mechanical |
| Dev-tier overflow traps | explicit checks in emitted code |
| **Generators** | **goroutine pairs or CPS — the fiddly one** |

Generators are flagged **"prototype before depending on the
transpiler"**. Goroutine pairs are what the interpreter does — correct
and heavy. A CPS or state-machine transformation is correct and fast
and is real work. C# proves it is solvable (Chapter 24).

#### Debugging, staged with the path

Each era gets a different answer, and each is cheap for its era:

**Interpreter era: a DAP server in the interpreter.** A tree-walker is
a debugger that has not been asked — breakpoints are a per-statement
check, stepping is eval-loop flags, inspection is the environment
already held. Weeks, not months, and every editor gets it free through
the Debug Adapter Protocol.

**Transpiler era: `//line` directives and preserved identifier
names** — a *day-one design constraint, not a retrofit*. Go's line
directives exist for exactly this, so delve then debugs Glide at
`.gld`-line granularity, and delve's goroutine awareness covers Glide
tasks because **they are goroutines**. Twenty years of debugger
investment ridden for two transpiler disciplines. The honest seam:
stepping inside desugared constructs like generator state machines —
keep the lowering line-faithful.

**Native era: full DWARF**, with the standard cut — complete debug info
is a dev-tier guarantee, release is best-effort.

Two more designed capabilities worth knowing:

**The scope tree beats the goroutine list.** A debugger shows
`main → serve → request → query` — structure mandated for other reasons
(Chapter 25), surfaced. Delve's 40,000 flat anonymous goroutines is the
anti-pattern.

**Deterministic-seed replay is the concurrency debugging story.** A
race caught in `glide test` is a seed; stepping through the replay
reproduces the exact interleaving. That is the bug class where
debuggers normally fail, covered by two recorded decisions touching
(Chapter 22).

#### Embedding

The interpreter gets a small public Go API — it exists anyway, since
bootstrap steps 1 and 2 require it. Construct a VM, run source, bind Go
functions, convert values, cancel runaway scripts.

Three rules:

**Influence is one-way.** The compiled language is the design
authority. No semantics bent because they are awkward to marshal to Go.
"The moment embedding argues *for* a language change, it loses."

**The interpreter tracks the language; it does not freeze.** An earlier
plan had it freezing at self-hosting so the evolving language would
keep exactly one implementation. The goal was right, the mechanism was
not: the shared frontend already guarantees one implementation, and
freezing would have bought that guarantee at the price of the scripting
tier drifting into an older language. Hosts still pin a version, as
they pin any Go module — that is dependency management, not a freeze.

**Stdlib shims are injectable per host**, which buys capability-style
sandboxing for free: untrusted scripts are simply never handed `fs` or
`net`. `DESIGN.md` calls this "the one embedding requirement worth
honouring while building the interpreter, because it is painful to
retrofit."

---

### 3. Why This Design?

#### Why the frontend still gets written in Glide — just not first

The original argument had two halves. One did not survive; the other is
the whole point and is untouched.

**The half that failed — "writing it in Go is thrown away".** Covered
above: it assumed a retiring interpreter, and the interpreter ships.

**The half that stands: compilers are the best-case workload for this
feature set.** `DESIGN.md` is explicit —

> ASTs are sum types, a checker is exhaustive matching, `?` threads the
> phases; the ML family was designed for this. The compiler in Glide
> will be nicer code than the interpreter that runs it.

That is a testable claim and it is the strongest possible dogfooding.
If sum types, exhaustive `match`, and `?` do not make writing a
compiler pleasant, the language's central bet is wrong. If they do, the
implementation is also the demonstration.

**Which is exactly why it moved to step 3.** The claim is only testable
if the features are real when the test runs. Written before the
checker, the experiment would have measured Glide's *syntax* while
none of the safety was switched on — a rigged trial, and one whose
failures would have been indistinguishable from the tier's failures.
Written after, it measures the thing the claim is actually about.

The ordering is the same lesson twice: a dogfooding exercise is only
evidence if the food is real.

#### Why transpile to Go rather than going straight to LLVM

Because LLVM gets you code generation and **not** a runtime.

A language with green threads and a tracing GC needs: a garbage
collector, a scheduler with work stealing and preemption, growable
stacks, channel primitives, and a `defer` mechanism. `DESIGN.md` notes
that runtime code "runs underneath the language's guarantees" — Go's
runtime is Go with a hundred pragmas, and a no-GC unsafe dialect plus
compiler special-casing is what writing one requires.

That is the decade. Transpiling to Go rents all of it, permanently if
desired, and produces readable and debuggable output as a bonus.

The costs are real: less control over layout and calling conventions, a
Go toolchain in the build, and performance bounded by what Go's
compiler will do with the emitted source. All acceptable for a shipping
tier that aims at "faster than Go".

#### Why two backends at all

The tiered design dissolves the classic "fast compiler or fast code"
dilemma. `DESIGN.md` calls sub-second dev builds non-negotiable and
"arguably Go's most underrated property".

You cannot have both from one backend. Two backends is two things to
maintain, and it is what lets overflow, backtraces, hygiene, and debug
info each have the right answer per tier.

And the constraint that makes it safe: **only costs may differ, never
semantics.**

#### Why "breaking changes are free" is an implementation strategy

It is not just a licence to change your mind. It is what makes the
whole staged path viable.

Because canonical formatting is a pure function from AST to bytes
(Chapter 2), a mechanical rewrite produces a **zero-noise diff**. So
`glide fix` can migrate every caller of a changed API, and a breaking
change costs one command rather than a migration project.

`DESIGN.md`: "canonical formatting is migration infrastructure."

The complement is **toolchain pinning from day one** — the manifest
pins the version, and newer toolchains build *as* the pinned one or
refuse. Breaking changes being free makes pinning more necessary, not
less.

#### Why the interpreter survives

Not out of sentiment, and not as a frozen relic: a statically-checked
scripting language that ships as one static binary is a product, and a
thinly served one. The nearest neighbour is Deno with TypeScript, and
it is not close on the "one binary, nothing to install" axis.

It survives *as the same language*, not an older snapshot of it,
because the frontend is shared. That is the whole argument for the
architecture: without it, the honest options were to freeze the
interpreter or to accept two implementations drifting apart, and both
are worse than sharing the one thing that decides what a program means.

An earlier draft of this chapter argued the opposite — freeze at
self-hosting, cite Lua 5.1. The Lua evidence is real but was read
backwards: it shows *hosts pinning* a version and living happily there
for a decade. Lua itself never stopped shipping.

---

### 4. Competing Approaches

**Go.** Bootstrapped from C, then self-hosted in Go 1.5 via an
automated C-to-Go translation of the compiler. The translation
approach — mechanical, auditable — is the same instinct as Glide's
transpiler step.

**Rust.** Bootstrapped from OCaml, self-hosted early, LLVM backend
throughout. Rust's compile times are the standing argument for
"compile speed is a feature", and its recent Cranelift dev backend is
the tiered design arriving a decade late.

**Zig.** Bootstrapped from C++, self-hosted with its own backends plus
LLVM, and now shipping a fast custom debug backend alongside LLVM
release builds — the same tiered structure Glide plans.

**Nim.** Transpiles to C, which is the closest analogue to Glide's
Go-transpiler step and demonstrates that it works long-term. Nim gets C
portability; Glide gets Go's runtime, which is the bigger prize for a
green-threaded GC language.

**TypeScript.** Transpiles to JavaScript, permanently and by design,
and is one of the most successful languages of the last decade. Strong
evidence that "transpile to a mature host" is not a temporary
embarrassment.

**Kotlin.** Targets the JVM, renting the world's most tuned garbage
collector — the same economics as Glide renting Go's runtime.

**Elixir.** Targets the BEAM, renting Erlang's scheduler and
supervision. The closest analogue to renting a *concurrency* runtime
specifically.

---

### 5. Common Mistakes

*(Reading the implementation, rather than using it.)*

**Benchmarking the tree-walker and concluding something about the
language.** It is two orders of magnitude slower than compiled Go and
it is not the shipping tier. Generators cost a goroutine each; there is
no compute parallelism; every field access is a map lookup.

**Assuming every annotation is checked.** As of M4b most are, but the
checker reports only what it is certain of: generic bounds, trait
conformance and match exhaustiveness pass in silence until M4c. A
program that checks clean is not yet a program that is fully verified.

**Assuming the M2 warts are semantics.** `Option` unboxing (so
`Option<Option<T>>` is unrepresentable, Chapter 14), builtin methods
not enforcing receiver-mut (Chapter 16), defaults filling through
function values (Chapter 7), decoded JSON keys being sorted
(Chapter 31) — all tier artifacts, all recorded as such.

**Assuming ○ features are speculative.** They are recorded designs with
stated rationale, not wishlist items. The distinction the book keeps
making — ✓ runs, ○ is designed — is about *implementation status*, not
confidence.

**Expecting the interpreter to become the compiler.** It will not. It
stays the interpreted tier — same language, same frontend, same
accepted programs, different execution strategy. What it will *not*
give you is a binary that runs on a machine with nothing installed, or
compiled-tier speed.

---

### 6. Performance Considerations

**The interpreter is a semantics prover.** Roughly two orders of
magnitude slower than compiled Go on compute. Its costs are structural:
an environment map allocation per call and per block, hash lookups for
field access, a goroutine per generator, and one interpreter lock.

**The transpiler tier inherits Go's performance**, minus whatever the
lowering costs. Sum types become tagged structs; `match` becomes
switches; both are what you would write by hand. Generators are the
open question — goroutine pairs would be slow, a state machine would
not.

**The dev backend targets sub-second builds** and does not optimise.
The release backend optimises and does not need to be fast.

**Target envelope**, stated in `DESIGN.md`: faster than Go, usually
competitive with Rust, never beating hand-tuned C. The last ~20% of
performance is a recorded sacrifice — the price of no borrow checker.

**Comptime caching is sound** because comptime is deterministic
(Chapter 34), which is a real dev-build lever.

---

### 7. Best Practices

**Write for the compiled tier, test on the interpreter.** Do not
restructure code to avoid blocks, closures, or generators based on
tree-walker timings. Do use the interpreter to check that semantics are
what you expected — that is what it is for.

**Write annotations now.** A codebase full of unbounded `<T>` and
untyped parameters will be miserable to bring under the checker.

**Treat the M2 warts as temporary and avoid depending on them.** Do not
send `None` through a channel; do not rely on `xs.push` working through
a `let`; do not rely on defaults through function values.

**Pin the toolchain.** Breaking changes are free, which means your
build must say which version it expects.

**Report the ugly corners.** The interpreter exists to find them, and
several decisions in `DESIGN.md` are marked "forced by the interpreter"
because someone hit a problem while implementing. That process is still
open — the `or |e|` residue test (Chapter 19) is an explicit
invitation.

**Read `DESIGN.md` second and `LINEAGE.md` third.** This book teaches;
`DESIGN.md` records every decision and its cost; `LINEAGE.md` gives the
history — who invented a feature, who adopted it, who tried living
without it.

---

### 8. Examples

**The bootstrap chain, drawn out:**

```
Go toolchain (mainstream, auditable, no binary seed)
   │
   ├─ builds ─→  Glide interpreter + checker (Go)    ← steps 1 ✓, 2 (M4)
   │                 │
   │                 └─ runs ─→ Glide frontend (Glide)          ← step 3 ○
   │                                │
   │                                └─ emits ─→ Go source
   │                                               │
   └───────────── builds ──────────────────────────┘
                                                   │
                                                   ▼
                                        Glide compiler binary   ← step 4 ○
                                                   │
                                        compiles itself; its frontend
                                        is the same one the interpreter
                                        runs, so the tiers cannot drift
```

No binary seed anywhere, and every link is a mainstream toolchain
building readable source. That is the trusting-trust answer.

**The seed never goes away, and that is normal.** Once the frontend is
Glide, building Glide needs a Glide — Go pins Go 1.4, Rust builds with
the previous release, Zig ships a `zig1.wasm` blob. Glide's version is
unusually cheap, because step 4 emits *Go source*: commit that
generated Go and any Go toolchain rebuilds the whole chain. So "no Go"
means none in the source of truth and none at runtime. It does not mean
none on the build machine, which is the relationship most languages
have with a C compiler. Removing that last link is step 5, and step 5
is a runtime project, not a compiler one.

**Why a compiler is the ideal dogfood, in miniature:**

```glide
// An AST is a sum type.
type Expr =
    Num(Int)
    | Add(Expr, Expr)
    | Mul(Expr, Expr)
    | Var(String)

type TypeError = Unbound{ name: String }

// A checker is exhaustive matching.
fn check(e: Expr, env: Map<String, Bool>) -> Result<(), TypeError> {
    match e {
        Num(_)    => Ok(())
        Add(a, b) => {
            check(a, env)?          // `?` threads the phases
            check(b, env)?
            Ok(())
        }
        Mul(a, b) => {
            check(a, env)?
            check(b, env)?
            Ok(())
        }
        Var(name) => {
            if env[name] ?? false { Ok(()) } else { Err(.Unbound{ name: name }) }
        }
    }
}

fn main() {
    let env = ["x": true]
    let good = Expr.Add(.Num(1), .Var("x"))
    let bad  = Expr.Mul(.Var("y"), .Num(2))
    println("{check(good, env):?}")
    println("{check(bad, env):?}")
}
```

```
Ok(())
Err(Unbound{ name: "y" })
```

Twenty lines, and it demonstrates the claim: the AST is a sum type, the
checker is a `match` the compiler verifies is exhaustive, and `?`
threads the error path without a single `if err != nil`. Add a variant
to `Expr` and this function stops compiling, with the compiler naming
the gap.

That is the workload the ML family was designed for, and it is why
`DESIGN.md` predicts the Glide-written frontend will be nicer code than
the Go interpreter running it.

**The tier table, as a program would see it:**

```glide
fn main() {
    let big = 9223372036854775807
    println(big + 1)
}
```

```
# dev tier (the interpreter, today)
error: line 3: Int overflow: 9223372036854775807 + 1 (use wrapping_add for modular arithmetic)
```

```
# release tier (○)
-9223372036854775808
```

Same program, different tier, different behaviour — deliberately, and
only for *cost* behaviours. A program's *meaning* is identical across
tiers, which is why loop back-edge cancellation checks were rejected.

---

### 9. Summary & Exercises

**Summary**

- **Two mountains wearing one name.** The compiler is a hill with a
  shortcut; the runtime is the actual decade, deferred indefinitely by
  the shortcut.
- **Five steps:** (1) Go tree-walking interpreter ✓, (2) the checker, in
  Go (M4, current), (3) the frontend rewritten in Glide and run on the
  interpreter, (4) a Glide→Go transpiler that compiles itself, (5)
  someday, LLVM plus a native runtime.
- **One frontend, two backends.** Lexer, parser and checker are a single
  shared implementation; the tiers differ in how they execute a checked
  program, never in what they accept. "Runs interpreted, fails compiled"
  becomes unreachable by construction. Precedent: OCaml since the early
  90s, Rust's Miri, Roslyn.
- Milestones M1–M3 were defined by **a program that must run** —
  `wordfreq`, `Tree`, the `notes` service — the dogfood rule applied to
  the implementation. M4 is the first defined by a *property* instead:
  annotations must mean something.
- The interpreter is a **tree-walker** because its first job was to
  prove semantics cheaply. Several `DESIGN.md` decisions are marked
  *"forced by the interpreter; ratified"*.
- It **was** dynamically checked on the argument that a Go checker would
  be thrown-away work. Reversed: the interpreter ships rather than
  retiring, so the work is not thrown away — and the original plan would
  have written a checker in the one tier that checks nothing. What
  transfers to Glide is the design and the conformance corpus, not the
  Go code.
- **The transpiler is the clever part.** Glide's runtime model was
  Go's from day one, so it lowers nearly 1:1 and prepays the hellish
  parts: GC, scheduler, cross-compilation, static binaries,
  debuggability. It also gives an auditable bootstrap chain with no
  binary seed.
- **The known wrinkle: generators.** Sum types → tagged structs and
  `match` → switches are mechanical; generators need goroutine pairs
  (heavy) or CPS (real work), and are flagged "prototype before
  depending on the transpiler".
- **Compilers are the best-case dogfood**: ASTs are sum types, a
  checker is exhaustive matching, `?` threads the phases. The prediction
  is that the Glide frontend will be nicer code than the Go interpreter
  running it — a testable claim about the language's central bet, and
  the reason the rewrite stays on the roadmap. It moved *after* the
  checker because the claim is only testable once the features it names
  are actually switched on.
- **Two tiers** let overflow, backtraces, hygiene, and debug info each
  have the right answer. **Costs may differ; semantics may not** —
  which is why loop back-edge cancellation checks were rejected.
- **Debugging is staged**: a DAP server in the interpreter (a
  tree-walker is a debugger that has not been asked), `//line`
  directives plus preserved names in the transpiler era so delve works
  at `.gld` granularity, full DWARF natively. Plus the scope tree
  instead of a flat goroutine list, and deterministic-seed replay for
  races.
- **The interpreter survives as a shipping tier**, tracking the language
  rather than freezing — a statically-checked scripting language in one
  static binary, and the embedding library, with injectable stdlib shims
  giving capability-style sandboxing. The earlier freeze plan was
  dropped once the shared frontend delivered its goal outright.
- **Breaking changes are free** *because* canonical formatting makes
  `glide fix` produce zero-noise diffs. Toolchain pinning is the
  complement.

**Exercises**

1. **Test the central claim.** Write a small interpreter or type
   checker in Glide — an expression language with variables and let
   bindings is enough. Then write the same thing in Go. Count the lines
   spent on error handling and on dispatching over node kinds. The
   claim is that the Glide version is nicer; check it.

2. **Find a "forced by the interpreter" decision.** Read `DESIGN.md`
   for the phrase. Pick one and work out what the implementer must have
   hit. Then decide whether the resulting rule is a good language
   design or merely an implementation convenience that got ratified —
   the leading-dot continuation rule is the most interesting case.

3. **Design the generator lowering.** Take the three-line tree-walk
   generator from Chapter 24 and write out what a Go state machine
   implementing it would look like: what fields the state struct holds,
   how many states there are, and how `yield from` recursion is
   flattened. Then estimate whether goroutine pairs would be acceptable
   as a first transpiler cut. This is a real open question on the
   project's de-risk list, and your answer is worth as much as anyone's.
