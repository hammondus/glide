# The conformance corpus

Every implementation of Glide's frontend must pass these programs
unchanged: this Go checker, the Glide port that replaces it, and both
backends. It is the anti-drift device the two-tier design depends on —
"the interpreter and the compiler agree" is a claim, and this is the
evidence.

Each `.gld` file is a whole program. A file with no `// error:`
comments must be **accepted**. A `// error: <text>` comment on a line
declares that exactly one diagnostic is reported on that line, and
that its message contains `<text>`. Any unexpected diagnostic, any
missing one, and any diagnostic on a line without a comment is a
failure.

A rejection counts whichever stage produces it — the lexer (an
integer literal too large for any type), the declaration table (a
duplicate name), or the checker. Note that the stages differ in how
much they report: the lexer and parser stop at the first error, so a
parse-stage case needs its own file, while the declaration table and
the checker report everything they find. The corpus states the contract the
same way for all three, because to a programmer they are one thing:
the program did not compile, and here is why.

## Coverage is measured, not assumed

`internal/check/coverage_test.go` reads every `c.errf` format string
out of the checker's source and every `bag.Add` out of the declaration
table's, runs every program here, and asserts that each diagnostic is
actually triggered by something in this directory. Adding a diagnostic
without a case fails that test.

That is the difference between a corpus that *looks* thorough and one
that is. A diagnostic nothing here triggers is a rule the Glide
frontend can get wrong with nothing to notice — and this corpus is the
only thing that will notice.

Reading the source rather than keeping a hand-written list is
deliberate: a list is a second thing to keep in step with the first,
and it would rot exactly the way stale reference docs do.

A diagnostic genuinely unreachable from Glide source — an internal
assertion — goes in that test's `exempt` map with its reason. Two are
there today. Trying to write a case for one is also how the checker's
dead `yield`-with-no-value branch was found and deleted: the parser
requires an expression, so the condition could not arise.

**Not yet measured**: the parser's own diagnostics. A parse error
aborts at the first one, so each needs its own file, and 46 files of
one line each would buy less than it costs to read. Worth revisiting
if the Glide frontend's parser turns out to disagree.

## Rules for adding cases:

- One theme per file, named after it. A file that fails should say
  what broke from its name alone.
- Prefer the smallest program that shows the rule. These are read as
  documentation of the language, not just executed.
- Put the expectation on the line the diagnostic points at. If that
  feels wrong, the diagnostic is probably pointing at the wrong place.
- An accept-case is as valuable as a reject-case: most checker bugs
  are false positives, and only accept-cases catch those.
