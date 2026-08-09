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
duplicate name), or the checker. The corpus states the contract the
same way for all three, because to a programmer they are one thing:
the program did not compile, and here is why.

Rules for adding cases:

- One theme per file, named after it. A file that fails should say
  what broke from its name alone.
- Prefer the smallest program that shows the rule. These are read as
  documentation of the language, not just executed.
- Put the expectation on the line the diagnostic points at. If that
  feels wrong, the diagnostic is probably pointing at the wrong place.
- An accept-case is as valuable as a reject-case: most checker bugs
  are false positives, and only accept-cases catch those.
