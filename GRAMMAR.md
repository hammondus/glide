# Glide grammar sketch — examples before EBNF

Three complete programs written as if Glide exists. Purpose: find the
ugly corners by *reading*, before formalising. Every construct follows
DESIGN.md where a decision exists; syntax invented here is listed in
**Decisions this sketch forces** at the end — ratify or fight.

---

## Program 1 — `wordfreq`, a small CLI

```glide
// wordfreq counts word frequencies in a file.
import fs
import os

fn main() -> Result<(), Error> {
    let [_, path] = os.args() else {
        eprintln("usage: wordfreq <file>")
        os.exit(2)
    }

    let text = fs.read_string(path).context("reading input")?

    let mut counts: Map<String, Int> = [:]
    for word in text.split_whitespace() {
        counts[word] = (counts[word] ?? 0) + 1
    }

    let mut entries = counts.entries()          // List<(String, Int)>
    entries.sort_by(|a, b| b.1.cmp(a.1))

    for (word, n) in entries.iter().take(20) {
        println("{n:6}  {word}")
    }
    Ok(())
}
```

Notes: list pattern + `let else` for argv; `os.exit` diverges so the
else-body rule is satisfied; map read returns `Int?` so `?? 0` is the
counting idiom; write-through-index inserts; `for` header destructures
the tuple.

---

## Program 2 — `notes`, an HTTP + SQL service

```glide
import http
import json
import log
import sql
import time

type NoteId = distinct i64

type Note = struct {
    pub id: NoteId
    pub title: String
    pub body: String
    pub created: Time
} derive(Json, Row, Debug)

type ApiError =
    NotFound(id: NoteId)
    | BadInput(msg: String)
    | Db(cause: Error)

fn main() -> Result<(), Error> {
    let db = sql.open("sqlite:notes.db")?
    defer { _ = db.close() }

    let mut r = http.Router.new()
    r.get("/notes/{id}", |req| get_note(db, req))
    r.post("/notes", |req| create_note(db, req))

    scope s {
        s.spawn(|| sweeper(db))          // cancelled if serve fails
        log.info("listening", { port: 8080 })
        http.serve(addr: ":8080", handler: r)
    }
}

fn get_note(db: sql.Db, req: http.Request) -> Result<http.Response, ApiError> {
    let id = req.path_param("id").parse<i64>() else {
        return Ok(http.bad_request("bad id"))
    }

    let found = db.query_one<Note>(
        "select id, title, body, created from notes where id = :id",
        { id: NoteId(id) },
    ) or |e| { return Err(.Db(e)) }

    match found {
        Some(n) => Ok(http.json(n))
        None    => Err(.NotFound(NoteId(id)))
    }
}

fn create_note(db: sql.Db, req: http.Request) -> Result<http.Response, ApiError> {
    let input = req.json<Note>() or |e| { return Err(.BadInput("{e}")) }

    db.exec(
        "insert into notes (title, body, created) values (:title, :body, :now)",
        { title: input.title, body: input.body, now: time.now() },
    ) or |e| { return Err(.Db(e)) }

    Ok(http.created())
}

fn sweeper(db: sql.Db) {
    for {
        time.sleep(1.min)                // cancellation point
        let n = db.exec("delete from notes where created < :cutoff",
                        { cutoff: time.now() - 30.d }) ?? 0
        if n > 0 {
            log.info("swept old notes", { count: n })
        }
    }
}
```

Notes: `distinct` needs an explicit construction (`NoteId(id)`); the
scope ties the sweeper's life to the server's; sweeper errors are
deliberately absorbed here (`?? 0`) — a real design question in
miniature.

---

## Program 3 — a library module with generators, traits, tests

```glide
// package tree: an ordered binary tree.

pub type Tree<T: Ord> = struct {
    root: Node<T>?                       // private field
}

type Node<T> = struct {
    value: T
    left: Node<T>?
    right: Node<T>?
}

impl Tree<T> {
    pub fn new() -> Tree<T> {
        Tree{ root: None }
    }

    pub fn insert(mut self, value: T) {
        self.root = insert_node(self.root, value)
    }
}

impl Iterable<T> for Tree<T> {
    fn iter(self) -> Iterator<T> {
        if let root = self.root {
            yield from walk(root)        // generator: body contains yield
        }
    }
}

fn walk<T>(n: Node<T>) -> Iterator<T> {
    if let l = n.left  { yield from walk(l) }
    yield n.value
    if let r = n.right { yield from walk(r) }
}

fn insert_node<T: Ord>(at: Node<T>?, value: T) -> Node<T> {
    match at {
        None => Node{ value: value, left: None, right: None }
        Some(n) if value < n.value =>
            Node{ left: insert_node(n.left, value), ..n }
        Some(n) =>
            Node{ right: insert_node(n.right, value), ..n }
    }
}

test "in-order traversal is sorted" (xs: List<Int>) {
    let mut t = Tree.new()
    for x in xs { t.insert(x) }
    expect(t.iter().collect() == xs.sorted())
}

bench "insert 10k" {
    let mut t = Tree.new()
    for x in 0..10_000 { t.insert(x) }
}
```

Notes: property test (generated `xs`, shrinking on failure); struct
update in the recursive insert keeps untouched subtrees shared; the
generator body reads like the traversal it is.

---

## Decisions this sketch forces (ratify or fight)

1. **Uniform type declarations**: one keyword — `type X = struct {…}`,
   `type C = A | B`, `type Id = distinct Int`, `type H = fn(A) -> B`.
   No separate `struct`/`enum` keywords.
2. **Imports**: bare names for stdlib (`import http`), quoted URLs for
   external (`import "github.com/x/y" as y`). One import per line.
3. **`impl` blocks** hold methods (inherent: `impl Tree<T>`; trait:
   `impl Iterable<T> for Tree<T>`). Associated functions called
   `Type.new(…)`.
4. **`derive(…)` trails the type declaration.**
5. **Map literals**: `["a": 1]`, empty `[:]` (Swift style — bracket
   family with lists). Anonymous-struct literals `{ port: 8080 }` stay
   distinct (identifier keys, brace family — log fields, named
   bundles).
6. **Tuple access**: `.0`, `.1` (Rust style).
7. **Patterns in `for` headers** (irrefutable only): `for (k, v) in m`.
   (Params stay flat — recorded; `for` headers are `let`-like, not
   signatures.)
8. **`or |e| { … }` error-handling form** — fought and declined
   (DESIGN.md, Errors): `?`-conversion covers wrap-and-propagate,
   `??` on Result covers fallback, `match` the residue. Deferred
   with a count-the-residue test in DESIGN.md's open questions.
9. **`yield from`** delegates to a sub-generator.
10. **`main` may return `Result<(), Error>`** (error prints + exit 1)
    or `()`.
11. **Number literal separators**: `10_000`.
12. **Duration/date literal family**: `1.min`, `30.d` — method-call
    literals on numbers, stdlib-defined, no language magic.

Not yet exercised (next sketch): channels + `select` syntax, `errdefer`
in anger, `supervise` scopes, comptime blocks, `unsafe`, embed,
`match` on strings/ranges, labeled break.
