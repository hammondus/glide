# Chapter 33: JSON

`DESIGN.md` opens its JSON section by calling Go's `encoding/json`
**five diseases**, and the list is worth reading before anything else,
because Glide's design is a point-by-point response:

1. Runtime reflection.
2. Stringly struct tags — `json:"nmae"` compiles and ships.
3. Missing field → silent zero value, so pointer fields get used to
   fake `Option`.
4. Case-insensitive field matching — a security-relevant surprise.
5. `map[string]interface{}` for anything dynamic.

Four of the five were already cured upstream by decisions in earlier
chapters. That is the interesting part: the JSON module is mostly a
*consequence* of no-null, mandatory initialisation, sum types, and
comptime.

The interpreter ships a dynamic shim (✓). The real design —
`derive Json` — is ○ and is Chapter 36's machinery.

---

### 1. Basic Usage

#### Encoding

```glide-run
import json

type P = struct {
    pub name: String
    pub age: Int
    pub tags: List<String>
}

fn main() {
    let p = P{ name: "ada", age: 36, tags: ["a", "b"] }
    println(json.encode(p))
    println(json.encode(["x": 1, "y": 2]))
    println(json.encode([1, 2, 3]))
    println(json.encode(None))
}
```

```
{"name":"ada","age":36,"tags":["a","b"]}
{"x":1,"y":2}
[1,2,3]
null
```

`json.encode(v)` does a **structural walk**:

| Glide | JSON |
|---|---|
| struct | object, in **field order** |
| `Map` with String keys | object, in **insertion order** |
| `List`, tuple | array |
| `None` | `null` |
| `distinct` | unwrapped |
| `Instant` | RFC 3339 string |
| variant, function | **error** — these wait for `derive` |

#### Decoding

```glide-run
import json

fn main() {
    match json.decode(`{"k": 1, "b": [1, 2], "s": "x", "n": null}`) {
        Ok(v)  => println("{v:?}")
        Err(e) => println("err: {e}")
    }
    match json.decode("not json") {
        Ok(v)  => println("{v:?}")
        Err(e) => println("err: {e}")
    }
}
```

```
["b": [1, 2], "k": 1, "n": None, "s": "x"]
err: invalid JSON: invalid character 'o' in literal null (expecting 'u')
```

`json.decode(s)` returns `Result<T, Error>` producing **dynamic
values**:

| JSON | Glide |
|---|---|
| object | `Map` — **keys sorted** (Go's decoder loses order) |
| array | `List` |
| whole number | `Int` |
| other number | `Float` |
| `null` | `None` |

Trailing garbage is an error. **Typed decode arrives with
`derive Json`.**

Note the asymmetry: encoding preserves order, decoding sorts. That is a
shim limitation — Go's decoder does not preserve key order — and the
designed module fixes it with an insertion-ordered map.

#### JSON literals want raw strings

```glide
// Bad — {"title"...} tries to interpolate
let body = "{\"title\": \"first\"}"

// Good
let body = `{"title": "first", "body": "hello"}`
```

This is the most common mistake in the chapter and it is recorded in
`glide/DESIGN-DECISIONS.md` as discovered the hour the HTTP shim
landed. In an always-interpolating string (Chapter 6), `{` opens an
interpolation. Backticks are what raw strings were ratified for.

#### Working with decoded values

Because decode produces a `Map`, indexing returns an `Option`:

```glide-run
import json

fn main() {
    let Ok(v) = json.decode(`{"title": "hello", "count": 3}`) else {
        eprintln("bad json")
        return
    }
    let title = v["title"] ?? ""
    let count = v["count"] ?? 0
    println("{title} x{count}")
}
```

```
hello x3
```

`??` supplies the default for a missing key — the same idiom as any
map (Chapter 11).

#### `derive Json` ○

The real design:

```glide
type Note = struct {
    pub id: NoteId
    pub title: String
    pub created: Instant
    pub body: String?
} derive(Json)
```

```glide
let note: Note = json.decode(text)?      // ○ typed decode
let text = json.encode(note)             // ○ generated encoder
```

The encoder and decoder are **generated as plain code at compile
time** — serde-class speed with no proc-macro second compiler and no
runtime reflection.

Options are **typed comptime arguments**, never string tags:

```glide
type Note = struct { … } derive(Json(rename_all: camel))     // ○
```

A typo'd option is a compile error. Compare `json:"nmae"`, which
compiles, ships, and silently produces a field nobody reads.

#### Dynamic JSON is a sum type ○

```glide
type Json =
    Null
    | Bool(Bool)
    | Number(JsonNumber)
    | Str(String)
    | Array(List<Json>)
    | Object(Map<String, Json>)
```

Exhaustive `match` over shapes replaces the type-assertion ladder that
`map[string]interface{}` forces:

```glide
fn render(v: Json) -> String {              // ○
    match v {
        Null       => "null"
        Bool(b)    => "{b}"
        Number(n)  => n.text()
        Str(s)     => "\"{s}\""
        Array(xs)  => …
        Object(m)  => …
    }
}
```

This is sum types' best demonstration, and Chapter 13 built a working
version of exactly this shape.

---

### 2. Under the Hood

#### The shim does dynamically what derive will do statically

`glide/DESIGN-DECISIONS.md` is explicit: `json.encode` walks values
structurally; `json.decode` produces dynamic maps and lists. **None of
this survives into the compiled tier** — `derive Json` generates the
typed versions.

The shim exists to prove the *surfaces*, not the mechanism. So the
things it cannot do — encode a variant, decode into a type — are not
gaps to work around; they are the parts that are waiting for the right
implementation.

#### Why `derive` is not reflection

`derive Json` is an **ordinary comptime function** that walks the
type's structure (fields, names, types) at compile time and emits a
plain encoder — the code you would have hand-written.

Contrast Go, where `json.Marshal` runs an interpretive loop over
`reflect.Type` on **every call**: read the struct tag string, parse it,
switch on the field kind, box the value. That is a permanent runtime
tax and the biggest hole in Go's auditability story.

Contrast Rust's `serde`, which gets the performance right via proc
macros — a second compiler, slow, and opaque to tooling.

Comptime derive is the third option: generated at build time, visible
to the same tools as any other code, no runtime cost.

#### Required-by-default falls out of earlier decisions

This is the elegant part, and it is worth spelling out because it is
four chapters paying off at once.

Because there are **no zero values** (Chapter 12) and **absence is a
type** (Chapter 14):

```glide
type Note = struct {
    pub title: String        // required — absent input is a decode error
    pub body: String?        // may be absent or null
}
```

No tag is needed to say "required". A missing `title` cannot decode
into a `Note`, because there is no zero `String` to put there. A
missing `body` decodes to `None`, because that is what `String?` means.

**Absent-versus-zero is unrepresentable.** In Go you need `*string` to
distinguish "absent" from "empty", which reintroduces nil to fake
`Option`, and then `omitempty` interacts with it confusingly.

`T?` also collapses JSON's missing-versus-null tri-state into two,
which is what every API actually wants. The rare protocol that genuinely
distinguishes them gets an explicit wrapper.

#### Numbers keep their lexical form ○

Go's decoder turns every number into `float64`, which **silently
corrupts int64 IDs past 2⁵³**. A Twitter-scale ID round-trips wrong,
and nothing tells you.

The designed answer: `JsonNumber` holds the digits, with
`as_int() -> Int?` and `as_float()` converting where you choose. Plus
optional string-encoding of big integers for JavaScript-facing APIs.

#### Exact-case matching, always

Go's decoder matches field names case-insensitively, so a JSON field
`ADMIN` populates a Go field `Admin`. That has been a
security-relevant surprise in real systems.

Glide matches exactly, always. Unknown fields are ignored by default —
the right default for evolving APIs — with a `strict` opt-in for
config files, where "you typo'd `porrt`" beats silence.

---

### 3. Why This Design?

#### Why comptime derive rather than reflection

Three costs of reflection, all avoided:

**Performance.** An interpretive loop per call, per field. `serde` is
often an order of magnitude faster than `encoding/json` for exactly
this reason.

**Auditability.** `DESIGN.md` calls runtime reflection "the biggest
hole in Go's auditability story". You cannot tell by reading a
function what it will do to a value, because it decides at runtime.

**The tag language.** Reflection needs metadata, and metadata in Go is
a string in a struct tag — unchecked, untyped, and silently wrong when
misspelled.

The cost of banning reflection is named honestly: **no deserialising
into a type named by a runtime string.** The rare dynamic case
hand-rolls a registry, visibly.

#### Why options are typed arguments, not tags

```go
// Go — a string. `json:"nmae,omitemty"` compiles.
Name string `json:"name,omitempty"`
```

```glide
// Glide — checked at compile time                       ○
type User = struct { … } derive(Json(rename_all: camel))
```

A struct tag is a program written in a string, parsed at runtime,
checked by nothing. `json:"nmae"` ships. `json:"name,omitemty"` ships
and silently does not omit.

Typed comptime arguments are just arguments — the compiler checks the
name and the type.

#### Why dynamic JSON is a sum type

Because `map[string]interface{}` is the shape you get when a language
has a top type and no sum types, and working with it is a ladder of
type assertions with no coverage check:

```go
switch v := val.(type) {
case map[string]interface{}: …
case []interface{}: …
case string: …
case float64: …           // and int64 has already been lost
case bool: …
case nil: …
}
```

Nothing checks that you handled all six, and `float64` for every number
is disease four.

A six-variant sum type gets exhaustiveness for free, keeps numbers in
their lexical form, and reads as what it is.

#### Why insertion-ordered maps matter here

Chapter 11's map ordering pays off at the JSON boundary: **key order
round-trips.** Decode an object, modify one field, re-encode, and the
output is not gratuitously reordered — which matters for
human-readable config files under version control.

Go's decoder loses order (its maps are randomised), so a
decode-modify-encode cycle produces a diff touching every line.

#### Why serde-style format genericity was rejected

`serde` abstracts over formats: one derive, and your type works with
JSON, YAML, MessagePack, and CBOR.

It is elegant, and `DESIGN.md` declines it: the thirty-method trait
dance is "the enterprise abstraction we keep declining." Each format
gets its own comptime derive over the same reflection API, and if
duplication across four formats ever hurts, a shared-walk refactor fits
behind the derives **without touching user code**.

That last clause is the reason it is safe to decline: the decision is
reversible.

---

### 4. Competing Approaches

**Go.** `encoding/json` with reflection and struct tags — the five
diseases. Also `json.Number` as an opt-in escape from the float64
problem, which most people do not know about. Go's v2 `encoding/json`
proposal fixes several of these, a decade in.

**Rust.** `serde` — the performance target. Proc-macro derive, format
genericity, and excellent ergonomics, at the cost of a second compiler
in your build and macro-expanded code your tooling cannot see through.

**Python.** `json` producing `dict`/`list`/`None`, with `pydantic` as
the near-universal typed layer. `pydantic`'s popularity is the evidence
that dynamic decoding is not enough.

**Java.** Jackson and Gson — annotation-driven runtime reflection, with
all of Go's problems plus configuration surface. `@JsonProperty`,
`@JsonIgnore`, and a `ObjectMapper` with dozens of feature flags.

**TypeScript.** `JSON.parse` returning `any`, with `zod` or `io-ts` as
the runtime-validation layer everyone adds. Same story as `pydantic`:
the type system cannot help at the boundary, so a library re-does it.

**Zig.** `std.json` with comptime-driven parsing into typed structs —
architecturally the closest to Glide's design, and evidence that
comptime derive works.

---

### 5. Common Mistakes

**Using a regular string for a JSON literal.**

```glide
// Bad — does not even lex
let body = "{\"title\": \"first\"}"

// Good
let body = `{"title": "first"}`
```

**Expecting typed decode.** `json.decode` produces dynamic maps and
lists today. Reach into them with `??`, or wait for `derive Json`.

**Expecting decoded key order to be preserved.** Encoding preserves
order; decoding sorts. A shim limitation.

**Encoding a variant.** It errors — variants wait for `derive`. Encode
a struct or a map instead:

```glide
// Bad today
json.encode(Status.Active)

// Good today
json.encode(["status": "active"])
```

**Assuming a missing key gives a zero.** It gives `None`:

```glide
let count = v["count"] ?? 0        // the ?? is not optional
```

**Round-tripping large integers through a JavaScript client.** Numbers
past 2⁵³ lose precision in JavaScript regardless of what Glide does.
The designed answer is optional string-encoding of big integers; today,
send them as strings yourself.

**Reaching for `omitempty` habits.** There is no `omitempty` and there
does not need to be: `String?` with `None` encodes as `null`, and the
designed derive has a typed option for omitting rather than
null-encoding.

**Trusting case-insensitive matching.** There is none. Field names
match exactly.

---

### 6. Performance Considerations

**The shim walks values structurally at runtime.** That is reflection
by another name, and it is a tree-walker cost. Do not benchmark it.

**`derive Json` generates plain code** (○) — a straight-line encoder
per type, optimised like anything you would hand-write. That is the
serde cost model, and it is typically several times faster than
reflection-based encoding.

**Decoding allocates a map per object and a list per array.** Typed
decode (○) allocates the struct directly and skips the intermediate.
That is the biggest performance difference between the shim and the
designed module.

**Key ordering costs a keys slice per map** (Chapter 11) — one pointer
per entry, in exchange for stable output.

**Numbers in lexical form cost a string** (○) until converted, in
exchange for not corrupting int64s.

**Exact-case matching is a plain comparison.** Go's case-insensitive
fallback costs a second pass.

---

### 7. Best Practices

**Use raw strings for every JSON literal.** No exceptions. If you find
yourself typing `\"`, stop.

**Parse into a type at the boundary, once.** This is Chapter 11's
"maps at the boundary, structs inside", and JSON is where it matters
most:

```glide
// Good — the dynamic map lives for three lines
fn parse_note(body: String) -> Result<Note, ApiError> {
    let Ok(v) = json.decode(body) else {
        return Err(.BadInput{ msg: "invalid JSON" })
    }
    let title = v["title"] ?? ""
    if title == "" {
        return Err(.BadInput{ msg: "title is required" })
    }
    Ok(Note{ title: title, body: v["body"] ?? "" })
}
```

Downstream code gets a `Note`, not a map that might contain anything.
When `derive Json` lands this becomes one line, and the *shape* is the
same.

**Let `T?` express optionality; do not invent sentinels.**

```glide
// Good
type Note = struct {
    pub title: String        // required
    pub body: String?        // optional
}
```

**Decide unknown-field policy deliberately.** Ignore for evolving APIs
(the default); `strict` for config files, where a typo'd key should be
an error rather than a silent default.

**Do not send large integers to JavaScript clients as numbers.**
Anything above 2⁵³ needs string encoding, and that is a protocol
decision, not a serialisation one.

**Keep the wire type separate from the domain type when they diverge.**

```glide
// Good, once shapes differ
type NoteWire = struct { pub title: String, pub body: String? }
type Note = struct { pub id: NoteId, pub title: String, pub words: Int }

fn to_domain(w: NoteWire, id: NoteId) -> Note { … }
```

A `derive(Json)` on your domain type couples your API to your internal
model. Sometimes that is fine; when it stops being fine, the fix is a
second type and a conversion, not a pile of tag options.

---

### 8. Examples

**Encoding, the full structural walk:**

```glide-run
import json

type P = struct {
    pub name: String
    pub age: Int
    pub tags: List<String>
}

fn main() {
    let p = P{ name: "ada", age: 36, tags: ["a", "b"] }
    println(json.encode(p))
    println(json.encode(["x": 1, "y": 2]))
    println(json.encode([1, 2, 3]))
    println(json.encode(None))
}
```

```
{"name":"ada","age":36,"tags":["a","b"]}
{"x":1,"y":2}
[1,2,3]
null
```

Struct field order and map insertion order are both preserved.

**Decoding and validating at the boundary:**

```glide-run
import json

type Note = struct {
    pub title: String
    pub body: String
}

type ApiError = BadInput{ msg: String }

fn parse_note(body: String) -> Result<Note, ApiError> {
    let Ok(v) = json.decode(body) else {
        return Err(.BadInput{ msg: "invalid JSON" })
    }
    let title = v["title"] ?? ""
    if title == "" {
        return Err(.BadInput{ msg: "title is required" })
    }
    Ok(Note{ title: title, body: v["body"] ?? "" })
}

fn main() {
    for input in [
        `{"title": "first", "body": "hello"}`,
        `{"title": "second"}`,
        `{"body": "no title"}`,
        `not json`,
    ] {
        match parse_note(input) {
            Ok(n)                => println("ok: {n:?}")
            Err(BadInput{ msg }) => println("rejected: {msg}")
        }
    }
}
```

```
ok: Note{ title: "first", body: "hello" }
ok: Note{ title: "second", body: "" }
rejected: title is required
rejected: invalid JSON
```

Four inputs, four distinct outcomes, and every failure is a typed value
the caller can match on. Note the shape: `json.decode` produces a
dynamic map, the function validates and constructs a `Note`, and
nothing downstream ever sees the map.

**The round-trip:**

```glide-run
import json

fn main() {
    let original = `{"name":"ada","age":36,"active":true,"tags":["x","y"]}`

    let Ok(v) = json.decode(original) else {
        println("decode failed")
        return
    }
    println("{v:?}")
    println(json.encode(v))
}
```

```
["active": true, "age": 36, "name": "ada", "tags": ["x", "y"]]
{"active":true,"age":36,"name":"ada","tags":["x","y"]}
```

The keys came back sorted rather than in input order — the shim
limitation. The designed module preserves order through the round trip,
which is what makes decode-modify-encode produce a clean diff.

**Bad versus good: the stringly-typed pipeline**

```glide
// Bad — the map escapes into the program
fn handle(body: String) -> String {
    let Ok(v) = json.decode(body) else { return "bad json" }
    process(v)                       // takes a Map
}

fn process(v: Map<String, Json>) -> String {
    let title = v["title"] ?? ""     // every access might be absent
    let count = v["count"] ?? 0      // every value needs a default
    let nested = v["meta"] ?? [:]    // and this one is a map too
    …
}
```

Every function downstream has to know the schema, restate the defaults,
and handle absence. Two call sites can disagree about what the default
for `count` is, and nothing notices.

```glide
// Good — parse once, then it is a type
fn handle(body: String) -> String {
    match parse_note(body) {
        Ok(n)  => process(n)         // takes a Note
        Err(e) => "rejected: {e:?}"
    }
}

fn process(n: Note) -> String {
    // n.title is a String. Not a maybe-String. A String.
    …
}
```

This is the same argument as Chapter 12's parse-don't-validate, and
JSON is where it earns the most, because JSON is where untrusted data
enters.

---

### 9. Summary & Exercises

**Summary**

- Go's `encoding/json` has **five diseases**: runtime reflection,
  stringly struct tags, missing-field-becomes-zero, case-insensitive
  matching, and `map[string]interface{}` for dynamic data. Glide's
  design is a point-by-point response, and four of the five were cured
  by earlier decisions.
- **Today (✓):** `json.encode(v)` does a structural walk preserving
  struct field order and map insertion order; `json.decode(s)` produces
  dynamic maps and lists with **sorted** keys. Variants and functions
  cannot be encoded — they wait for derive.
- **JSON literals want raw strings.** `` `{"k": 1}` `` — in an
  always-interpolating string, `{` opens an interpolation.
- **`derive Json` (○)** generates encoder and decoder as plain code at
  compile time — serde-class speed, no proc-macro second compiler, no
  runtime reflection. Options are **typed comptime arguments**, so a
  typo is a compile error rather than a shipped bug.
- **Required-by-default falls out of no-zero-values plus `Option`.** A
  missing `String` field is a decode error; a missing `String?` field
  is `None`. Absent-versus-zero is unrepresentable, and no tag is
  needed.
- **Dynamic JSON is a sum type** (○) — `Null | Bool | Number | Str |
  Array | Object` — so exhaustive `match` replaces the type-assertion
  ladder.
- **Numbers keep their lexical form** (○), so int64 IDs past 2⁵³ do not
  silently corrupt the way Go's decode-to-float64 does.
- **Exact-case matching, always.** Unknown fields ignored by default,
  `strict` opt-in for config files.
- **serde-style format genericity was rejected** as an enterprise
  abstraction, and the decision is reversible — a shared-walk refactor
  fits behind the derives without touching user code.
- Insertion-ordered maps (Chapter 11) mean key order round-trips, so
  decode-modify-encode produces a clean diff.

**Exercises**

1. **Find the silent zero.** In a Go service, find a struct decoded
   from JSON with a non-pointer `int` or `string` field that is
   logically optional. Determine what happens when the field is absent
   versus present-and-zero. Then write the Glide type and note that the
   distinction is forced into the declaration rather than into a
   comment.

2. **Break a big integer.** Encode `9007199254740993` (2⁵³ + 1) as
   JSON, decode it in a JavaScript environment, and re-encode. Note
   what you get back. Then decide, for an API you maintain, whether any
   ID could reach that magnitude — and whether you would notice if one
   did.

3. **Design the strict/lax boundary.** For a service you know, list
   every JSON input: request bodies, config files, webhook payloads,
   cached documents. For each, decide whether unknown fields should be
   ignored or rejected, and write down why. You will find the answer
   splits cleanly along "does a human write this?" — which is exactly
   why `DESIGN.md` makes it an opt-in rather than a global setting.
