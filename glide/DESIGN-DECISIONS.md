# Interpreter implementation decisions

Language decisions live in `../DESIGN.md`. This file records how the
*interpreter* (bootstrap step 1) is built, and which corners are
deliberately cut because the real compiler makes them obsolete.

## Milestones

1. **M1 (done): run `wordfreq`** (GRAMMAR.md program 1) — the whole
   expression language, zero user-defined types.
2. **M2: run program 3 (Tree)** — `type`, sum types, `match`, `impl`,
   traits, generators. Generators early: DESIGN.md names them a
   top-three semantic risk.
3. **M3: run program 2 (HTTP + SQL)** — host shims + structured
   concurrency.

## Decisions

- **Dynamically checked.** Type annotations are parsed, kept as
  strings, and ignored. The interpreter proves *semantics*; the
  checker arrives with the Glide-written frontend (bootstrap step 2).
  Writing a static checker in Go now would be thrown-away work.
  Rules the compiler will enforce statically are enforced dynamically
  instead so programs still can't cheat: mut, nested-shadow ban,
  let-else divergence, the tail-value rule.
- **Go panics for runtime errors, explicit signals for Glide control
  flow.** `return` and `?` thread a `sig` value up the evaluator
  (they are semantics); runtime errors panic and are recovered once
  in `Run` (they are diagnostics). `os.exit` panics with a sentinel
  that becomes `ExitError`, so tests intercept exits instead of the
  test process dying.
- **The parser is the grammar.** Recursive descent + small Pratt
  core. EBNF gets extracted from the working parser later, not
  written ahead of it.
- **Maps are insertion-ordered** (keys slice + Go map). Makes
  programs and the golden test deterministic. Provisional language
  semantics — ratify or revisit when Map lands properly.
- **`sort_by` is stable**, comparator returns an Int in cmp order
  (<0, 0, >0). Whether Glide grows an `Ordering` type is an open
  stdlib question; Int is the M1 shim.
- **Collections are Go pointers** (`*ListV`, `*MapV`): reference
  semantics under GC, matching the recorded "mut is a path property"
  sacrifice. Mutability is checked at the *root binding* of an
  assignment path (`counts[k] = v` requires `mut counts`).
- **Receiver-mut on builtin methods is not enforced** (`xs.push`
  works through a `let` binding). The checker's job; noted so nobody
  mistakes it for a semantic decision.
- **Known lexer limitation**: nested tuple access `x.0.1` lexes
  `0.1` as a float. Rust special-cases this; we will when it matters.
  Same bucket: string literals inside interpolation are unsupported.

## Deliberately absent (M1)

`match`, user types, traits/impl, generics, generators, concurrency,
`defer`/`errdefer`, `or |e|` blocks, named arguments, parameter
defaults, `break`/`continue`, floats beyond literals/arithmetic,
method values as closures (`x.method` unapplied), struct literals.
