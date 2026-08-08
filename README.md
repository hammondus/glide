<p align="center">
  <img src="assets/logo/glide-wordmark.svg" alt="Glide" width="360">
</p>

# The Glide Programming Language

Glide is a compiled, statically typed language in the Go tradition —
garbage collected, green-threaded, one binary, boring on purpose — with
the type system of the ML family: sum types, pattern matching, no null.
Effortless motion, no visible struggle, real speed. Files end in `.gld`.

```glide
fn main() {
    let mut primes = [2, 3, 5]
    primes.push(7)
    for p in primes {
        println("{p} is prime")
    }
}
```

## Status

**Early and moving fast.** The language is designed on paper ahead of its
implementation; a tree-walking interpreter (milestone M2) runs the core
today: bindings, functions, closures, lists/maps/tuples, structs and
methods, sum types with `match`, `Result` + `?`, generators, and built-in
testing with property-based tests. The compiler comes later.

One user, no compatibility promise — breaking changes are free until
further notice, deliberately.

## Try it

Requires a Go toolchain to build the interpreter:

```bash
cd glide
make build                                      # → bin/glide
bin/glide run examples/wordfreq.gld testdata/sample.txt
bin/glide run yourfile.gld                      # any .gld file
bin/glide test yourfile.gld                     # runs its test blocks
```

## Reading order

- [`DESIGN.md`](DESIGN.md) — the design document: every decision and its
  *why*, including the deliberate sacrifices.
- [`LINEAGE.md`](LINEAGE.md) — the history behind each decision: who
  invented it, who adopted it, who tried living without it.
- [`docs/reference/language.md`](docs/reference/language.md) and
  [`docs/reference/stdlib.md`](docs/reference/stdlib.md) — terse lookup
  references, Go's spec/stdlib split. ✓ marks what runs today, ○ what is
  designed but not yet implemented.
- [`docs/book/`](docs/book/) — the book: teaches the language from
  hello-world up, assuming Go instincts and nothing else.
- [`GRAMMAR.md`](GRAMMAR.md) — the grammar, including the open fights.
