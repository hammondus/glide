# Stdlib & x/ — long-term goals

Aspirational inventory, informed by what Go/Python/Rust/Java ecosystems
proved people need. One line each, no designs. Tiers per DESIGN.md:
**core** ships with the toolchain and is the audited surface; **x/** is
first-party, separately versioned, faster-churning; **never** is
policy. Committed items from DESIGN.md marked ✓.

## Core — committed ✓

- `http` (server + client, router, HTTP/2) ✓
- `tls` ✓ · `crypto` (sealed misuse-resistant API) ✓
- `json` (derive-based) ✓
- `time` (Time/Date/TimeOfDay/ZonedTime, Duration/Period, tzdata) ✓
- `fs`, `os`, `process` ✓ · `flag` (CLI args) ✓
- `testing` (expect/require, property tests, deterministic scheduler,
  bench, fuzz-eventually) ✓
- `regex` (RE2 semantics) ✓ · `log` (structured, scope-inherited) ✓
- `template` (HTML, contextual auto-escaping) ✓
- `sql` (interface only; drivers outside) ✓
- `rand` (secure by default) ✓ · base64/hex ✓ · compression ✓
- persistent collections `PList`/`PMap` ✓ · `Mutex<T>` ✓

## Core — wanted long-term

- `net` — TCP/UDP/Unix sockets, DNS resolution (underpins http)
- `io` — Reader/Writer/Seeker traits, copy, pipes, buffered adapters
- `url` — parsing/building (http's dependency, separately usable)
- `path` — cross-platform path manipulation (typed, not stringly)
- `strings`/`bytes` — the workhorse function sets + builders
- `math` — the usual surface + correct rounding modes
- `Decimal` — money-grade fixed-point (operator traits' second customer)
- `uuid` — it's a u128; too small and too common to farm out
- `csv` — business reality; small, stable, eternal
- `toml` — config file reading (apps need one blessed config format)
- `archive` — zip, tar · compression grows zstd alongside gzip
- crypto growth: password hashing (argon2, misuse-resistant API),
  HMAC, key derivation, x509 handling for tls
- `mime` — type tables, multipart (http adjacency)
- `unicode` — normalization; segmentation/graphemes may be x/ (tables
  churn with Unicode releases)
- sync extras: RwLock, Once, atomics, object Pool
- containers: deque, heap/priority queue, sorted (btree) map, bitset
- http extras: SSE (cheap), cookie handling, `httptest`-equivalent
  (Go's most-loved testing utility)
- `signal` — graceful shutdown wiring into scopes
- `term` (minimal) — TTY detect, ANSI colors (log already needs both)
- `fsnotify`-equivalent — file watching (dev tooling wants it)

## x/ — the porch

- `x/mail` + `x/smtp` ✓ (committed; MIME construction is the real 90%)
- `x/sqlpg` — Postgres driver (pure Glide, wire protocol)
- `x/sqlite` — SQLite driver (FFI's first customer; host-shim in
  interpreter era)
- `x/websocket` — common enough to be first-party, churns too much for
  core
- `x/yaml` — reading other people's configs (the format is a horror;
  quarantined on the porch)
- `x/markdown` — the general-purpose library (CommonMark/GFM-class:
  big spec, extension churn, HTML-sanitization surface — porch
  cadence; Go's Blackfriday→Goldmark ecosystem migration is what
  freezing a dialect into core would have prevented). Distinct from
  `glide doc`'s renderer, which is toolchain-*internal* on purpose:
  the doc-comment subset is tiny and frozen, versions with the
  toolchain, and must never grow a dialect — different masters, two
  artifacts.
- `x/term` + `x/tui` — raw mode, events, widgets: the TUI toolkit
  (cross-compiled TUIs are a first-class Glide use case)
- `x/oauth2` / `x/oidc` — client side; every backend eventually needs it
- `x/jose` — JWT verify/sign, misuse-resistant subset only
- `x/ssh` — client (ops tooling; Go's x/crypto/ssh proved the demand)
- `x/html` — parsing/scraping (spec-correct parser is a big ship)
- `x/image` — decode/resize/encode basics (web apps make thumbnails;
  out of core per policy, porch is the right distance)
- `x/msgpack` or `x/cbor` — one blessed compact binary format via the
  same derive machinery (not both; pick when needed)
- `x/i18n` — message catalogs, plural rules (low priority until users
  exist)

## Never (policy, recorded)

- ORM · GUI toolkit · ML/tensors · heavy XML stack (minimal parser at
  most) · SOAP/WSDL anything · DI frameworks · plugin systems ·
  protobuf/gRPC (revisit only under real interop pressure — it drags
  a codegen ecosystem behind it)
- GUI rationale, since it's the non-obvious one: bigger than the rest
  of this file combined (text shaping + accessibility are careers);
  four OSes' churn vs stdlib cadence (net/smtp rot ×100); Java shipped
  it three times, all museums; main-thread affinity fights the green
  scheduler; and Glide's niches answer it with web-served-by-the-binary
  or x/tui. "Never" ≠ forbidden — a community toolkit is an ordinary
  package; the language's credibility just isn't mortgaged on one.

## Sequencing reality

Interpreter era: everything above is a Go host shim behind a Glide
interface — cheap to stand up, disposable, lets the *interfaces* be
designed against real use before any of it is rewritten in Glide.
Priority order follows the dogfood: what the compiler + one web service
+ one TUI actually touch, in that order.
