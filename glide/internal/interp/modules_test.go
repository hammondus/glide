package interp

import (
	"strings"
	"testing"
)

// json: structural encode (structs → objects, insertion order), and
// decode to dynamic values (null → None, whole numbers → Int).
func TestJSONEncodeDecode(t *testing.T) {
	out, err := runProg(t, `
import json

type Note = struct {
    pub title: String
    pub stars: Int
}
type NoteId = distinct Int

fn main() {
    println(json.encode(Note{ title: "hi \"you\"", stars: 5 }))
    // Heterogeneous literals stopped being writable when the checker
    // landed — [1, true, None] has no element type — so the three
    // scalar encodings are exercised separately. Mixed-type documents
    // come back with typed encoding (derive Json).
    println(json.encode(["b": 2, "c": 3]))
    println(json.encode(["a": [1, 2, 3]]))
    println(json.encode([true, false]))
    println(json.encode(["z": None]))
    println(json.encode(NoteId(9)))
    match json.decode(`+"`"+`{"x": 1.5, "n": 3, "z": null, "l": ["s"]}`+"`"+`) {
        Ok(v) => println(v)
        Err(e) => println("bad: {e}")
    }
    match json.decode("\{oops") {
        Ok(_) => println("impossible")
        Err(_) => println("rejected")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"title":"hi \"you\"","stars":5}` + "\n" +
		`{"b":2,"c":3}` + "\n" +
		`{"a":[1,2,3]}` + "\n" +
		`[true,false]` + "\n" +
		`{"z":null}` + "\n" +
		"9\n" +
		`["l": ["s"], "n": 3, "x": 1.5, "z": None]` + "\n" +
		"rejected\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

// http: a Glide program serves and queries itself inside one scope;
// returning from the body cancels the server — no shutdown code.
func TestHTTPServeAndClient(t *testing.T) {
	out, err := runProg(t, `
import http
import time

fn run() -> String {
    let mut r = http.router()
    r.get(`+"`"+`/hello/{name}`+"`"+`, |req| http.text("hi " + (req.path_param("name") ?? "?")))
    r.post("/echo", |req| http.text(req.body()))
    scope s {
        _ = s.spawn(|| http.serve("127.0.0.1:17641", r))
        time.sleep(80.ms)
        let g = match http.get("http://127.0.0.1:17641/hello/craig") {
            Ok(resp) => "{resp.status()} {resp.body()}"
            Err(e) => "get failed: {e}"
        }
        let p = match http.post("http://127.0.0.1:17641/echo", `+"`"+`{"k":1}`+"`"+`) {
            Ok(resp) => "{resp.status()} {resp.body()}"
            Err(e) => "post failed: {e}"
        }
        let missing = match http.get("http://127.0.0.1:17641/nope") {
            Ok(resp) => "{resp.status()}"
            Err(e) => "get failed: {e}"
        }
        return "{g} | {p} | {missing}"
    }
}
fn main() {
    println(run())
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "200 hi craig | 200 {\"k\":1} | 404\n" {
		t.Fatalf("got %q", out)
	}
}

// http: an Err from a handler is the one default error mapping (500).
func TestHTTPHandlerError(t *testing.T) {
	out, err := runProg(t, `
import http
import time

fn run() -> String {
    let mut r = http.router()
    r.get("/boom", |req| Err("kaput"))
    scope s {
        _ = s.spawn(|| http.serve("127.0.0.1:17642", r))
        time.sleep(80.ms)
        return match http.get("http://127.0.0.1:17642/boom") {
            Ok(resp) => "{resp.status()} {resp.body().trim()}"
            Err(e) => "failed: {e}"
        }
    }
}
fn main() {
    println(run())
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "500 kaput\n" {
		t.Fatalf("got %q", out)
	}
}

// sql: open/exec/query/query_one against in-memory sqlite; named
// params verified both directions; NULL is None both directions.
func TestSQLRoundTrip(t *testing.T) {
	out, err := runProg(t, `
import sql

fn run() -> Result<String, Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec("create table notes (id integer primary key, title text not null, body text)")?
    _ = db.exec("insert into notes (title, body) values (:t, :b)", ["t": "first", "b": "hello"])?
    _ = db.exec("insert into notes (title, body) values (:t, :b)", ["t": "second", "b": None])?
    let rows = db.query("select id, title, body from notes order by id")?
    let one = db.query_one("select title from notes where id = :id", ["id": 1])?
    let missing = db.query_one("select title from notes where id = :id", ["id": 99])?
    let miss_str = match missing {
        Some(_) => "impossible"
        None => "no row"
    }
    Ok("{rows} | {one ?? [:]} | {miss_str}")
}
fn main() {
    match run() {
        Ok(s) => println(s)
        Err(e) => println("failed: {e}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[["id": 1, "title": "first", "body": "hello"], ["id": 2, "title": "second", "body": None]] | ["title": "first"] | no row` + "\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

// sql: named-parameter verification is loud in both directions.
func TestSQLNamedParamErrors(t *testing.T) {
	out, err := runProg(t, `
import sql

fn run() -> Result<(), Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec("create table t (a text)")?
    match db.exec("insert into t (a) values (:a)", [:]) {
        Ok(_) => println("impossible")
        Err(e) => println("missing: {e}")
    }
    match db.exec("insert into t (a) values (:a)", ["a": "x", "ghost": 1]) {
        Ok(_) => println("impossible")
        Err(e) => println("extra: {e}")
    }
    Ok(())
}
fn main() {
    _ = run()
}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "missing: query names :a") || !strings.Contains(out, `extra: params supply "ghost"`) {
		t.Fatalf("got %q", out)
	}
}

// sql: distinct values bind by unwrapping — the codec is the
// explicit conversion boundary.
func TestSQLDistinctParam(t *testing.T) {
	out, err := runProg(t, `
import sql

type NoteId = distinct Int

fn run() -> Result<String, Error> {
    let db = sql.open("sqlite::memory:")?
    defer { _ = db.close() }
    _ = db.exec("create table n (id integer, title text)")?
    _ = db.exec("insert into n values (:id, :t)", ["id": NoteId(7), "t": "x"])?
    let row = db.query_one("select title from n where id = :id", ["id": NoteId(7)])?
    Ok("{row ?? [:]}")
}
fn main() {
    match run() {
        Ok(s) => println(s)
        Err(e) => println("failed: {e}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != `["title": "x"]`+"\n" {
		t.Fatalf("got %q", out)
	}
}
