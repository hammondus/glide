# Book Notes

`docs/book/` — 41 chapters and five appendices, following the nine-section
chapter anatomy (Basic Usage / Under the Hood / Why This Design? /
Competing Approaches / Common Mistakes / Performance / Best Practices /
Examples / Summary & Exercises), with Chapter 1 exempt as an essay.

## Conventions

- **✓** runs in the current interpreter; **○** is designed and recorded
  in `DESIGN.md` but not implemented. Appendix D is the complete table.
- A code fence marked `glide-run` is a **complete program that the test
  suite executes** (`TestBookExamples` in `glide/doc_examples_test.go`).
  A plain `glide` fence is a fragment and is not run. The two render
  identically to a reader.
- Error output appears in two forms: verbatim (`file:line:col:` plus a
  source line and caret) in Chapter 19 and the chapters revised with it,
  and abbreviated (`error: line N: message`) elsewhere. The message text
  is real in both.

## What the book is anchored to

Nothing in the ✓ material is written from the design documents alone.
Every complete program was executed, and 122 of them are executed again
on every `make test`, so a ✓ example cannot silently stop working when
the language moves. That harness exists because the book *did* drift
once, between the M3 and M4 eras, and prose with no test behind it is
how that happened.

## Open items the book records rather than hides

- **Three rules are enforced by the evaluator rather than the checker**,
  so `glide check` misses them and they fire only on an executed path:
  the tail-value rule, the nested-shadow ban, and a generic bound whose
  type parameter appears only inside a container. All three
  under-approximate. Appendix D, *Known checker gaps*.
- **Normal scope exit joins children without cancelling them**, so a
  child blocked on `send` or `http.serve` deadlocks unless the body uses
  an early `return`. Not a bug — rule 1 — and Chapter 28's centrepiece.
