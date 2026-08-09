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

Glide's design runs ahead of its implementation. The language is
specified on paper; a tree-walking interpreter written in Go runs a
large subset today. Every code example in this book is marked:

| Marker | Meaning |
|---|---|
| **✓** | Runs in the current interpreter. Every ✓ example in this book was executed against `glide run` or `glide test` and its output pasted back in. |
| **○** | Designed and recorded in `DESIGN.md`, not yet implemented. Typing it today is a parse or runtime error. |

Chapters that cover ○ material say so in their opening paragraph. This
is not hedging: a book that silently mixes shipped behaviour with
aspiration is worse than no book, and stale documentation is the one
sin this project's `CLAUDE.md` names explicitly.

Type annotations are *checked* as of M4b: every program is
type-checked before it runs, in every tier, and there is no flag to
skip it. M4c added generic bounds, trait conformance, `Ord`, sized
numerics, explicit numeric conversion, boxed `Option` and match
exhaustiveness. The checker still reports only what it is certain of,
so what it does not yet cover passes in silence and is caught at
runtime instead; chapters written before a rule landed may still
describe it as runtime-only.

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
  Building the interpreter · `glide run`, `glide test` · Script mode
  and shebangs · The designed one-binary toolchain · Editor setup ·
  Why the toolchain is part of the language
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
  `Int` is i64 everywhere · No truthiness · No implicit conversions ·
  Overflow traps · Ranges · Precedence · Why no `++`
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
  The three built-in collections · Reference semantics · Map indexing
  returns an Option · Spread · Tuples are for transport
- **[Chapter 12: Structs](chapter-12.md)**
  Mandatory initialisation · Why there are no zero values · Field
  visibility · Struct update · Structs as the boring default
- **[Chapter 13: Sum Types](chapter-13.md)**
  The star feature, taught from zero · Making illegal states
  unrepresentable · Payloads, named fields, dot shorthand · Enums as
  the degenerate case
- **[Chapter 14: Option — There Is No Null](chapter-14.md)**
  `T?` from first principles · `??`, `if let`, `let … else` · Why the
  billion-dollar mistake is a type error here · The unboxing wart
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

### Part V — Errors & Reliability

- **[Chapter 19: Errors as Values](chapter-19.md)**
  `Result` · `?` and its conversion rule · Sum-type errors versus the
  dynamic `Error` · `??` on a Result · The or-block that was declined
- **[Chapter 20: Panics](chapter-20.md)**
  What panics are for · No `recover`, ever · The three unwinds
- **[Chapter 21: `defer` and `errdefer`](chapter-21.md)**
  Block scope, not function scope · Blocks, not calls · The discarded
  error problem · Why not RAII
- **[Chapter 22: Testing](chapter-22.md)**
  Tests as a language construct · One assertion, compiler-known ·
  Property-based testing and shrinking, from scratch · Benchmarks

### Part VI — Iteration

- **[Chapter 23: Iterators](chapter-23.md)**
  Lazy sequences explained · Adapters · `Iterable` versus `Iterator` ·
  Why channels are not the iteration protocol
- **[Chapter 24: Generators](chapter-24.md)**
  `yield` and the suspended function · `yield from` · Writing an
  iterator that reads like a traversal · Infinite sequences

### Part VII — Concurrency

- **[Chapter 25: Structured Concurrency](chapter-25.md)**
  Green threads · Scopes and nurseries · `spawn`/`join` · Values wait,
  bugs don't · Why not async/await
- **[Chapter 26: Cancellation, Timeouts, and Deadlines](chapter-26.md)**
  The third unwind · Cancellation points · `scope(timeout:)` · Why
  `context.Context` is function colouring by hand
- **[Chapter 27: Channels](chapter-27.md)**
  Rendezvous and buffered · Split halves · Ownership transfer · Go's
  three channel panics, dispatched
- **[Chapter 28: `select`](chapter-28.md)**
  Match's clothes, Go's engine · Arm guards · The missing `ctx.Done`
  arm · Fan-in patterns

### Part VIII — Building Real Software

- **[Chapter 29: Modules, Imports, and Visibility](chapter-29.md)**
  Directory is module · Inert imports · Two visibility levels · No
  life before `main`
- **[Chapter 30: Files, Processes, and CLI Programs](chapter-30.md)**
  `os` and `fs` · Argument parsing without a framework · Exit codes ·
  stdout versus stderr discipline
- **[Chapter 31: JSON](chapter-31.md)**
  The shim today, `derive Json` tomorrow · Dynamic JSON as a sum type
  · Why raw strings for JSON literals
- **[Chapter 32: HTTP](chapter-32.md)**
  Handlers that return values · Routing · The client with real
  defaults · Cancellation for free
- **[Chapter 33: SQL and Databases](chapter-33.md)**
  Named parameters only · NULL is `Option` · No ORM, ever ·
  Transactions as closures

### Part IX — Ahead of the Interpreter

- **[Chapter 34: Comptime, `derive`, and Metaprogramming](chapter-34.md)**
  Compile-time execution instead of macros · The comptime-is-not-
  generics fence · `derive Json`/`Row`/`Debug` · No runtime reflection
- **[Chapter 35: The Implementation Path](chapter-35.md)**
  Tree-walker → Glide-written frontend → Glide→Go transpiler → maybe
  LLVM · Tiered backends · What differs between dev and release

### Part X — Writing Idiomatic Glide

- **[Chapter 36: Effective Glide](chapter-36.md)**
  Naming · Signature design · When to reach for which construct ·
  Reading a diff
- **[Chapter 37: Common Anti-Patterns](chapter-37.md)**
  Go-in-Glide · Rust-in-Glide · Sum-type abuse · Premature traits ·
  `mut` creep
- **[Chapter 38: Architecture and Project Structure](chapter-38.md)**
  Small programs · Module boundaries · Dependency injection without a
  framework · When structure becomes ceremony
- **[Chapter 39: Case Study — A Complete Service](chapter-39.md)**
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
- **`docs/book/01-introduction.md`** — a single-chapter whirlwind tour
  of the whole language, written before this book. Read it in an hour
  if you want the shape before the detail.
