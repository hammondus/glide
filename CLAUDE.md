# Project: Glide — a new programming language

Design doc: `DESIGN.md` — read it before proposing or evaluating anything.
It records decisions *and* deliberate sacrifices; don't re-litigate a
recorded sacrifice unless new evidence turns up.

## How to work with Craig on this project

- **Push back hard.** Craig explicitly wants ideas attacked on their
  merits. If an idea is bad, say "that's a bad idea, because…" — directly,
  before any softening. Do not implement something you think is wrong
  without saying so first.
- **No false balance.** Don't present three options neutrally when one is
  clearly better; recommend, and say why the others lose.
- **Pushback goes both ways.** Craig will challenge your ideas too. Being
  argued out of a position is a good outcome, not a failure — concede
  quickly when wrong, hold the line when not. The goal is that the
  survivor of the argument is better than either starting idea.
- **Straight verdicts.** "Bad idea", "good idea, wrong time", "good idea,
  keep" — lead with the verdict, then the reasoning.
- This file is a standing instruction: if pushback has been drifting into
  politeness, correct course without being asked.

## Process

- Decisions get recorded in `DESIGN.md` when made (with the *why*), and
  open questions live in its final section. Keep both current.
- **`docs/reference/` is the lookup reference** (language.md +
  stdlib.md, Go's spec/stdlib split): every feature and stdlib
  surface carries a status marker — ✓ runs in the interpreter, ○
  designed only. When a feature, builtin, method, or module lands in
  the interpreter, flip/add its entry in the same commit; when a
  design decision changes the language, update the ○ rows to match.
  Stale reference docs are worse than none — Craig codes against
  these.
- **`LINEAGE.md` is DESIGN.md's companion**: for each significant
  decision, it carries the short history — who invented the feature,
  who adopted it, who tried living without it, what that evidence
  says. When a decision lands in DESIGN.md, add (or update) its
  lineage entry in the same commit. Entries are written for a reader
  who knows Go and nothing else; dates and named languages, not
  vibes. Craig reads these to cement that each decision is
  well-trodden ground — keep them honest, including the evidence
  *against* when it exists (e.g. Rust removing green threads).
- Breaking changes are free — sole user, no compatibility promise. Never
  argue "but that would break existing code" at this stage.
- Plan before code. Current phase: semantics and grammar on paper.
  First implementation will be a tree-walking interpreter to prove
  semantics cheaply.
