# Glide in Depth: From Philosophy to Production

A book about the Glide programming language, written for engineers who
already write software and want to know *why* Glide is shaped the way
it is — not just how to spell a `for` loop.

## Who this is for

You know how to program. You have opinions about error handling. You
have shipped something. This book does not explain what a variable is,
what an `Int` is, or the difference between a compiler and an
interpreter.

It **does** explain, from first principles and assuming no prior
exposure: closures, sum types, pattern matching, traits and interfaces,
generics, structured concurrency, generators, comptime, iterators,
option types, and every other concept Glide leans on. If a feature is
common in the ML/Rust/Swift world but rare in the C/Java/Go world, this
book assumes you have never met it and teaches it properly.

Where a Go comparison illuminates something, the book makes it — Glide
is a Go-tradition language and Go instincts mostly transfer — but
knowing Go is not a prerequisite.

## Status markers — read this before you type anything

Glide's design runs ahead of its implementation, though by less than it
used to. The language is specified on paper; a tree-walking interpreter
written in Go runs a large subset today, and **every program it runs is
type-checked first**. Every code example in this book is marked:

| Marker | Meaning |
|---|---|
| **✓** | Runs in the current interpreter. Every ✓ example in this book was executed against `glide run` or `glide test` and its output pasted back in. |
| **○** | Designed and recorded in `DESIGN.md`, not yet implemented. Typing it today is a parse or runtime error. |

**The ✓ examples are executed by the test suite.** A code block whose
fence says `glide-run` rather than `glide` is a complete program that
`go test` runs on every build — 122 of them, across 33 chapters — so a
✓ example cannot silently stop working. The plain `glide` fence marks a
fragment (a lone signature, three lines of a body), which is the right
way to teach and the wrong thing to execute. The two render
identically; the marker exists only for the harness.

**Two forms of error output appear.** Chapter 19 and the chapters
revised alongside it quote the diagnostic verbatim —
`file:line:col: message`, then the source line and a caret under the
offending sub-expression, which is exactly what the tool prints.
Elsewhere the book abbreviates to `error: line N: message`, dropping
the filename, column and snippet as noise when the point is only
*which rule fired*. The message text is the real one in both cases.

Chapters that cover ○ material say so in their opening paragraph. This
is not hedging: a book that silently mixes shipped behaviour with
aspiration is worse than no book, and stale documentation is the one
sin this project's `CLAUDE.md` names explicitly.

**Where the implementation stands.** Milestones M1–M4 are complete.
M4 — the checker era — was defined by a property rather than a program:
*every annotation is checked, and no remaining case computes the wrong
value*. It delivered mandatory type checking in every tier, generic
bounds, trait conformance, `Ord`, sized numerics with explicit
conversion, boxed `Option`, boxed `Error`, and static match
exhaustiveness. The current work is bootstrap step 3 — the compiler
frontend rewritten in Glide, running on the interpreter (Chapter 37).

**Where the future is described.** Every chapter's *Under the Hood* and
*Why This Design?* sections describe the designed end state alongside
what runs, and each such passage carries a ○. Two places gather it up:
**Appendix D** is the complete feature table with a status against
every row, and **Chapter 37** is the roadmap narrative — the five
bootstrap steps, what each buys, and what is deliberately absent.

**The checker reports only what it is certain of.** Anything it cannot
model passes in silence and is caught at runtime instead. That is a
deliberate under-approximation — it means closing a gap can never break
a program that works — and the two known gaps are listed in Appendix D
and explained in Chapter 19.

## The chapter anatomy

Every chapter except Chapter 1 has the same nine sections. This is
deliberate: it lets you read the book cover to cover, or skip to
"Common Mistakes" for a feature you already half-know.

1. **Basic Usage** — the "how". Runnable snippets, building slowly.
2. **Under the Hood** — the "engine". What the interpreter actually
   does, and what the designed compiler will do instead.
3. **Why This Design?** — the "philosophy". Why *this* and not the
   obvious alternative.
4. **Competing Approaches** — how Go, Rust, Swift, Zig, Python, Java
   and friends solve the same problem.
5. **Common Mistakes** — the gotchas, with the error message you will
   actually see.
6. **Performance Considerations** — the cost, honestly, at both tiers.
7. **Best Practices** — the idiomatic way, with bad examples beside
   good ones and an explanation of the difference.
8. **Examples** — complete programs that crystallise the chapter.
9. **Summary & Exercises** — recap plus engineering challenges.

## Table of contents

### Part I — The Glide Mindset

- **[Chapter 1: Why Glide Exists](chapter-01.md)**
  The problem with picking a language in 2026 · Nothing here is new,
  and that is the point · What Glide keeps from Go and refuses from Go
  · The three pillars · Why this book marks every example
- **[Chapter 2: The Toolchain](chapter-02.md)**
  Building the interpreter · `glide run`, `glide test`, `glide check` ·
  Script mode and shebangs · The designed one-binary toolchain · Editor
  setup · Why the toolchain is part of the language
- **[Chapter 3: Your First Glide Program](chapter-03.md)**
  Hello world, dissected · `main` and its two signatures · Imports ·
  The newline rule · Case as grammar · Why every Glide file looks
  the same

### Part II — Core Language Fundamentals

- **[Chapter 4: Bindings, Mutability, and Shadowing](chapter-04.md)**
  `let` and `mut` · Why there is no `:=` · Sequential redeclaration as
  a refinement pipeline · The nested-shadow ban · Blocks as
  expressions · Explicit discards
- **[Chapter 5: Primitive Types, Literals, and Operators](chapter-05.md)**
  `Int` is i64 everywhere · Sized numerics and explicit conversion · No
  truthiness · Overflow traps · `abs`/`min`/`max`/`pow` and the `math`
  module · Float's total order · Structural `==` · Ranges · Precedence
  · Why no `++`
- **[Chapter 6: Strings, Runes, and Interpolation](chapter-06.md)**
  Three delimiters, three meanings · Always-on interpolation · Format
  specs · Why `s[i]` does not exist · Runes and UTF-8 · The four
  print builtins
- **[Chapter 7: Functions](chapter-07.md)**
  Signatures as documentation · The tail-expression rule · Default
  parameters and named arguments · Nested functions that do not
  capture · Why no overloading and no variadics
- **[Chapter 8: Closures](chapter-08.md)**
  What a closure *is*, from scratch · Capture by reference, capture of
  bindings · One function type · Loop variables · Closures versus
  nested `fn`
- **[Chapter 9: Control Flow](chapter-09.md)**
  One loop · Expression-`if` and the missing ternary · Labeled
  break/continue · No goto · Blocks, tails, and divergence
- **[Chapter 10: Pattern Matching](chapter-10.md)**
  Patterns are construction run backwards · Every pattern form ·
  `match`, guards, exhaustiveness · `if let` and `let … else` ·
  Subjectless `match`

### Part III — Data Modelling

- **[Chapter 11: Lists, Maps, and Tuples](chapter-11.md)**
  The three built-in collections · Reference semantics · The full
  method surface · Map indexing returns an Option · Map equality
  ignores order · Spread · Tuples are for transport
- **[Chapter 12: Structs](chapter-12.md)**
  Mandatory initialisation · Why there are no zero values · Field
  visibility · Struct update · Structs as the boring default
- **[Chapter 13: Sum Types](chapter-13.md)**
  The star feature, taught from zero · Making illegal states
  unrepresentable · Payloads, named fields, dot shorthand · Enums as
  the degenerate case
- **[Chapter 14: Option — There Is No Null](chapter-14.md)**
  `T?` from first principles · `??`, `if let`, `let … else` · Why the
  billion-dollar mistake is a type error here · Boxing, and what it
  closed
- **[Chapter 15: Distinct Types](chapter-15.md)**
  Ten characters that kill a bug class · No inherited operators ·
  Where distinct pays and where it annoys

### Part IV — Abstraction & The Type System

- **[Chapter 16: Methods and `impl`](chapter-16.md)**
  Methods live apart from data · Receiver mutability · Associated
  functions · Mutability as a path property
- **[Chapter 17: Traits](chapter-17.md)**
  Interfaces from scratch · Declared conformance, structural
  satisfaction · Default methods and interface evolution · The orphan
  rule · `any Trait` and the cost of boxing
- **[Chapter 18: Generics](chapter-18.md)**
  Type parameters explained from zero · Trait bounds · Monomorphisation
  · Why angle brackets and no turbofish
- **[Chapter 19: The Type Checker](chapter-19.md)**
  Mandatory in every tier, with no way to skip it · Reading a
  diagnostic · Bidirectional checking · `Unknown`, and reporting only
  when certain · The side tables that make the checker load-bearing ·
  The conformance corpus · Where the checker still stops

### Part V — Errors & Reliability

- **[Chapter 20: Errors as Values](chapter-20.md)**
  `Result` · `?` and its conversion rule · Sum-type errors versus the
  dynamic `Error` · `??` on a Result · The or-block that was declined
- **[Chapter 21: Panics](chapter-21.md)**
  What panics are for · No `recover`, ever · The three unwinds
- **[Chapter 22: `defer` and `errdefer`](chapter-22.md)**
  Block scope, not function scope · Blocks, not calls · The discarded
  error problem · Why not RAII
- **[Chapter 23: Testing](chapter-23.md)**
  Tests as a language construct · One assertion, compiler-known ·
  Property-based testing and shrinking, from scratch · Benchmarks

### Part VI — Iteration

- **[Chapter 24: Iterators](chapter-24.md)**
  Lazy sequences explained · Adapters · `Iterable` versus `Iterator` ·
  Why channels are not the iteration protocol
- **[Chapter 25: Generators](chapter-25.md)**
  `yield` and the suspended function · `yield from` · Writing an
  iterator that reads like a traversal · Infinite sequences

### Part VII — Concurrency

- **[Chapter 26: Structured Concurrency](chapter-26.md)**
  Green threads · Scopes and nurseries · `spawn`/`join` · Values wait,
  bugs don't · Why not async/await
- **[Chapter 27: Cancellation, Timeouts, and Deadlines](chapter-27.md)**
  The third unwind · Cancellation points · `scope(timeout:)` · Why
  `context.Context` is function colouring by hand
- **[Chapter 28: Channels](chapter-28.md)**
  Rendezvous and buffered · Split halves · Ownership transfer · Go's
  three channel panics, dispatched
- **[Chapter 29: `select`](chapter-29.md)**
  Match's clothes, Go's engine · Arm guards · The missing `ctx.Done`
  arm · Fan-in patterns

### Part VIII — Building Real Software

- **[Chapter 30: Modules, Imports, and Visibility](chapter-30.md)**
  Directory is module · Inert imports · Two visibility levels · No
  life before `main`
- **[Chapter 31: Files, Processes, and the Environment](chapter-31.md)**
  `os`, `fs` and `process` · Why a non-zero exit is not an error · Why
  there is no shell · Argument parsing without a framework · Exit codes
  · stdout versus stderr discipline
- **[Chapter 32: Case Study — Replacing a Shell Script](chapter-32.md)**
  One release script, in bash and in Glide · Six defects the type
  system deletes · `errdefer` versus `trap … EXIT` · When a sum-type
  error earns its keep
- **[Chapter 33: JSON](chapter-33.md)**
  The shim today, `derive Json` tomorrow · Dynamic JSON as a sum type
  · Why raw strings for JSON literals
- **[Chapter 34: HTTP](chapter-34.md)**
  Handlers that return values · Routing · The client with real
  defaults · Cancellation for free
- **[Chapter 35: SQL and Databases](chapter-35.md)**
  Named parameters only · NULL is `Option` · No ORM, ever ·
  Transactions as closures

### Part IX — Ahead of the Interpreter

- **[Chapter 36: Comptime, `derive`, and Metaprogramming](chapter-36.md)**
  Compile-time execution instead of macros · The comptime-is-not-
  generics fence · `derive Json`/`Row`/`Debug` · No runtime reflection
- **[Chapter 37: The Implementation Path](chapter-37.md)**
  Tree-walker → Glide-written frontend → Glide→Go transpiler → maybe
  LLVM · Tiered backends · What differs between dev and release

### Part X — Writing Idiomatic Glide

- **[Chapter 38: Effective Glide](chapter-38.md)**
  Naming · Signature design · When to reach for which construct ·
  Reading a diff
- **[Chapter 39: Common Anti-Patterns](chapter-39.md)**
  Go-in-Glide · Rust-in-Glide · Sum-type abuse · Premature traits ·
  `mut` creep
- **[Chapter 40: Architecture and Project Structure](chapter-40.md)**
  Small programs · Module boundaries · Dependency injection without a
  framework · When structure becomes ceremony
- **[Chapter 41: Case Study — A Complete Service](chapter-41.md)**
  Requirements → design → implementation → tests, in one program that
  runs

### Appendices

- **[Appendix A: Command Reference](zAppendix-A.md)** — every `glide`
  subcommand, shipped and designed
- **[Appendix B: Glide for Go Programmers](zAppendix-B.md)** —
  translation table and the ten things that will trip you
- **[Appendix C: Glide for Rust, Swift, and Python Programmers](zAppendix-C.md)**
- **[Appendix D: Implementation Status](zAppendix-D.md)** — the full
  ✓/○ table in one place
- **[Appendix E: Further Reading](zAppendix-E.md)** — the repo's own
  documents, and the papers and languages Glide borrows from

## Related documents in this repository

This book teaches. Four other documents exist and are worth knowing
about:

- **`DESIGN.md`** — every decision and its *why*, including the
  deliberate sacrifices. The book cites it constantly; it is the
  recommended second read.
- **`LINEAGE.md`** — the history behind each decision: who invented a
  feature, who adopted it, who tried living without it.
- **`docs/reference/language.md`** and **`docs/reference/stdlib.md`** —
  terse lookup references. The book teaches; those files remind.
- **`GLIDE-BY-EXAMPLE.md`** — the whole language in runnable snippets,
  in an hour. Every fenced block in it is executed by `go test`, so it
  cannot drift; read it if you want the shape before the detail.

(A pre-book whirlwind tour used to live at `docs/book/01-introduction.md`.
It was removed: it predated several reversals — it taught the declined
`or |e|` construct as though it were the language — and an unverified
prose tour beside an executable one is exactly the drift this project
treats as worse than no document. `git log` has it if you want it.)
