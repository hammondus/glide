# Book Notes



Followed the style guide in other_books/README.md exactly — the same nine-section chapter anatomy (Basic Usage / Under the Hood / Why This Design? / Competing Approaches / Common Mistakes / Performance / Best Practices / Examples / Summary & Exercises), with Chapter 1 exempt as an essay.

Per your instruction about the audience: I explained closures, sum types, pattern matching, traits, generics, iterators, generators, structured concurrency, Option types, and comptime from scratch, while assuming variables/functions/ints need no introduction. Bad examples appear beside good ones throughout with the reasoning spelled out — including a full ten-anti-pattern "Go-in-Glide" translation in Chapter 37 and a Version 1 → Version 2 refactor in Chapter 36.

The thing worth knowing

I built the interpreter and executed every ✓ example, pasting real output back in. That caught a lot: {ratio * 100} fails because Float * Int isn't defined; match arms are newline-separated; e => e in a match hits the nested-shadow ban; {a:8} right-aligns; a string literal can't sit inside an interpolation. All of those are now taught rather than silently wrong.

○ marks designed-but-unimplemented material and is used honestly throughout — Appendix D is the complete status table.

Two findings for you

A real interpreter bug. A driver-level SQL failure panics with cancelUnwind instead of returning Err. In glide/internal/interp/sqlmod.go, release() is deferred inside the in.unblock closure, so by the time line ~114 checks cancelled() the context has already been cancelled by release() — every failure looks like a cancellation. bindNamed errors are unaffected. Documented in Chapters 33 and 39 with a workaround; it looks like a one-line fix but I didn't touch the interpreter.

Not a bug, but sharp. Normal scope exit joins children without cancelling — only early exit cancels (rule 1). So a child blocked on send or http.serve deadlocks unless the body uses return. My first draft of a Chapter 27 example hit this; I turned it into the chapter's centrepiece, contrasting Go's silent leak with Glide's loud deadlock.

I also added glide/examples/links.gld — the Chapter 39 case study, a URL shortener that serves, drives its own API over HTTP, and passes its property tests. Nothing existing was modified.