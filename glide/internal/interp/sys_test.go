package interp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fs round-trips through a real temp directory. The point is not that
// os.WriteFile works — it is that the Result shape is right at every
// step, that a missing file is an Err rather than a trap, and that the
// predicates answer without one.
func TestFilesystemRoundTrip(t *testing.T) {
	dir := t.TempDir()
	out, err := runProg(t, `
import fs

fn main() -> Result<(), Error> {
    let dir = "`+dir+`"
    let sub = fs.join([dir, "a", "b"])
    fs.mkdir_all(sub)?
    println("made={fs.is_dir(sub)}")

    let p = fs.join([sub, "note.txt"])
    println("before={fs.exists(p)}")
    fs.write_string(p, "one\n")?
    fs.append_string(p, "two\n")?
    println("after={fs.exists(p)} dir?={fs.is_dir(p)}")
    println("body={fs.read_string(p)?.lines()}")

    // write_string truncates, like the shell's >.
    fs.write_string(p, "fresh\n")?
    println("truncated={fs.read_string(p)?.trim()}")

    let other = fs.join([sub, "moved.txt"])
    fs.rename(p, other)?
    println("moved={fs.exists(p) == false} to={fs.exists(other)}")
    println("listing={fs.list_dir(sub)?}")

    fs.remove(other)?
    println("removed={fs.exists(other) == false}")

    // remove refuses a non-empty tree; remove_all takes it.
    match fs.remove(fs.join([dir, "a"])) {
        Ok(_) => println("remove ate a non-empty directory")
        Err(_) => println("remove refused the tree")
    }
    fs.remove_all(fs.join([dir, "a"]))?
    println("swept={fs.exists(sub) == false}")

    match fs.read_string(fs.join([dir, "nope.txt"])) {
        Ok(_) => println("read a file that is not there")
        Err(e) => println("missing file is Err")
    }
    Ok(())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"made=true",
		"before=false",
		"after=true dir?=false",
		`body=["one", "two"]`,
		"truncated=fresh",
		"moved=true to=true",
		`listing=["moved.txt"]`,
		"removed=true",
		"remove refused the tree",
		"swept=true",
		"missing file is Err",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// os.env distinguishes unset from set-and-empty, which is the whole
// reason it returns an Option instead of "".
func TestOsEnvIsOptional(t *testing.T) {
	t.Setenv("GLIDE_TEST_EMPTY", "")
	os.Unsetenv("GLIDE_TEST_ABSENT")
	out, err := runProg(t, `
import os

fn main() {
    println("empty={os.env("GLIDE_TEST_EMPTY")}")
    println("absent={os.env("GLIDE_TEST_ABSENT")}")
    println("default={os.env("GLIDE_TEST_ABSENT") ?? "fallback"}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "empty=Some(\"\")\nabsent=None\ndefault=fallback\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestOsCwdAndChdir(t *testing.T) {
	dir := t.TempDir()
	start, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(start) })
	// macOS hands out /var/... symlinked to /private/var/..., and
	// Getwd resolves it — so compare against the resolved form rather
	// than the string t.TempDir handed back.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runProg(t, `
import os

fn main() -> Result<(), Error> {
    os.chdir("`+dir+`")?
    println(os.cwd()?)
    match os.chdir("`+filepath.Join(dir, "nowhere")+`") {
        Ok(_) => println("chdir into a missing directory succeeded")
        Err(_) => println("chdir to a missing directory is Err")
    }
    Ok(())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := real + "\nchdir to a missing directory is Err\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// The central process decision, asserted: exiting non-zero is an Ok
// with a status, and only failing to *start* is an Err. If these two
// ever collapse into one, `?` starts propagating grep's "no match".
func TestProcessRunSplitsExitFromFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	out, err := runProg(t, `
import process

fn main() {
    match process.run("sh", ["-c", "printf out; printf err >&2; exit 0"]) {
        Ok(o) => println("ok status={o.status()} ok={o.ok()} out={o.stdout()} err={o.stderr()}")
        Err(e) => println("unexpected Err {e}")
    }
    match process.run("sh", ["-c", "exit 7"]) {
        Ok(o) => println("nonzero status={o.status()} ok={o.ok()}")
        Err(e) => println("unexpected Err {e}")
    }
    match process.run("glide-no-such-binary-9f3a") {
        Ok(o) => println("unexpected Ok {o.status()}")
        Err(e) => println("missing binary is Err")
    }
    // Arguments are argv entries, never re-split: a single argument
    // containing a space stays one argument, which is the whole point
    // of not going through a shell.
    match process.run("echo", ["a b", "c"]) {
        Ok(o) => println("argv={o.stdout().trim()}")
        Err(e) => println("unexpected Err {e}")
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"ok status=0 ok=true out=out err=err",
		"nonzero status=7 ok=false",
		"missing binary is Err",
		"argv=a b c",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A child must not outlive the scope that started it. Without the
// bridged context the timeout would fire, the scope would return, and
// `sleep 30` would still be running — which is exactly the leak
// structured concurrency exists to prevent.
func TestProcessDiesWithItsScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep(1)")
	}
	out, err := runProg(t, `
import process
import time

fn main() {
    let start = time.now()
    scope(timeout: 200.ms) s {
        let t = s.spawn(|| process.run("sleep", ["30"]))
        println("never printed: {t.join()}")
    }
    println("scope ended early={time.now() - start < 5.s}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "scope ended early=true\n" {
		t.Fatalf("got %q", out)
	}
}

// The List and Map additions, at runtime. The checker has its own
// corpus case; this asserts the values, including the two edges that
// are easy to get wrong: extending a list with itself, and where a
// re-inserted map key lands in the iteration order.
func TestCollectionMethods(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let mut xs = [3, 1, 2]
    println("{xs.contains(1)} {xs.contains(9)} {xs.index_of(2)} {xs.index_of(9)}")
    println("{xs.first()} {xs.last()} {xs.reversed()} {xs.slice(1, 3)} {xs.slice(1, 1)}")

    xs.insert(0, 0)
    xs.insert(4, 4)
    println("inserted={xs}")
    println("removed={xs.remove(0)} left={xs}")
    xs.extend([8, 9])
    println("extended={xs}")
    println("popped={xs.pop()} left={xs}")

    let mut self_ext = [1, 2]
    self_ext.extend(self_ext)
    println("doubled={self_ext}")

    let mut empty: List<Int> = []
    println("{empty.first()} {empty.last()} {empty.pop()} {empty.contains(1)}")

    // reversed and slice copy; the original is untouched.
    let orig = [1, 2, 3]
    let rev = orig.reversed()
    println("copies={orig} {rev}")

    let mut m = ["a": 1, "b": 2, "c": 3]
    println("{m.keys()} {m.values()} {m.contains_key("b")} {m.contains_key("z")}")
    println("removed={m.remove("b")} missing={m.remove("z")} now={m}")
    m["b"] = 20
    println("reinserted appends={m}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"true false Some(2) None",
		"Some(3) Some(2) [2, 1, 3] [1, 2] []",
		"inserted=[0, 3, 1, 2, 4]",
		"removed=0 left=[3, 1, 2, 4]",
		"extended=[3, 1, 2, 4, 8, 9]",
		"popped=Some(9) left=[3, 1, 2, 4, 8]",
		"doubled=[1, 2, 1, 2]",
		"None None None false",
		"copies=[1, 2, 3] [3, 2, 1]",
		`["a", "b", "c"] [1, 2, 3] true false`,
		`removed=Some(2) missing=None now=["a": 1, "c": 3]`,
		`reinserted appends=["a": 1, "c": 3, "b": 20]`,
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Out-of-range is a positioned trap, not a clamp — the caller named a
// slot that is not there, and silently returning a shorter list is how
// an off-by-one survives to production.
func TestCollectionIndexTraps(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"xs.remove(5)", "remove: index 5 out of range"},
		{"xs.remove(0 - 1)", "remove: index -1 out of range"},
		{"xs.insert(9, 1)", "insert: index 9 out of range"},
		{"xs.slice(0, 9)", "slice: index 9 out of range"},
		{"xs.slice(2, 1)", "slice(2, 1): lo is past hi"},
	} {
		_, err := runProg(t, "fn main() {\n    let mut xs = [1, 2, 3]\n    println("+tc.expr+")\n}")
		if err == nil {
			t.Errorf("%s: expected a trap", tc.expr)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.expr, err, tc.want)
		}
	}
}

// Equality is specified structural and universal (DESIGN.md), and the
// evaluator's switch had four holes in it: Map, Result, Error and
// Range all panicked with "not comparable" while List and tuples
// worked. The Map case carries the one real decision — insertion
// order is an *iteration* property, not part of a map's identity, so
// two maps with the same pairs are equal however they were built.
// Python draws the line in exactly the same place, and its dicts have
// been ordered since 3.7.
func TestStructuralEqualityHasNoHoles(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    // Map: order-independent, including after a delete-and-reinsert
    // moves a key to the end of the iteration order.
    let mut a = ["x": 1, "y": 2]
    let b = ["y": 2, "x": 1]
    println("{a == b} {a.keys()} {b.keys()}")
    let _ = a.remove("x")
    a["x"] = 1
    println("{a == b} {a.keys()}")

    println("{["x": 1] == ["x": 2]} {["x": 1] == ["x": 1, "y": 2]} {["x": 1] == ["y": 1]}")
    println("{["x": [1, 2]] == ["x": [1, 2]]} {["x": [1, 2]] == ["x": [2, 1]]}")
    let e1: Map<String, Int> = [:]
    let e2: Map<String, Int> = [:]
    println("{e1 == e2} {e1 == ["x": 1]}")

    // Result, both sides.
    let l: Result<Int, String> = Ok(1)
    let r: Result<Int, String> = Err("boom")
    println("{Ok(1) == Ok(1)} {Ok(1) == Ok(2)} {l == r} {r == Err("boom")} {r == Err("other")}")

    // Range.
    println("{(0..3) == (0..3)} {(0..3) == (0..4)} {(1..3) == (0..3)}")

    // Nested through Map, Result and the Option box at once.
    println("{["k": Ok(Some(1))] == ["k": Ok(Some(1))]} {["k": Ok(Some(1))] == ["k": Ok(None)]}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		`true ["x", "y"] ["y", "x"]`,
		`true ["y", "x"]`,
		"false false false",
		"true false",
		"true false",
		"true false false true false",
		"true false false",
		"true false",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Errors compare by message and by the whole cause chain: `context`
// builds one, and ignoring it would make two failures with different
// provenance compare equal.
func TestErrorEquality(t *testing.T) {
	dir := t.TempDir()
	out, err := runProg(t, `
import fs

fn main() {
    let a = fs.read_string("`+dir+`/nope")
    let b = fs.read_string("`+dir+`/nope")
    let c = fs.read_string("`+dir+`/other")
    println("{a == b} {a == c}")
    println("{a.context("loading") == b.context("loading")}")
    println("{a.context("loading") == b.context("saving")}")
    println("{a.context("loading") == b}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "true false\ntrue\nfalse\nfalse\n"; out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// Boxing Option in M4c dropped *SomeV from the structural-equality
// switch, so `Some(1) == Some(1)` panicked with "Option values are
// not comparable" — in a language whose equality is specified
// universal and structural. Regression test, including through a
// container and a nested Option, since the box has to be transparent
// at every depth.
func TestOptionEquality(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let a: Int? = Some(1)
    let b: Int? = Some(1)
    let c: Int? = Some(2)
    let n: Int? = None
    println("{a == b} {a == c} {a == n} {n == None} {a == Some(1)}")

    // Through a container, and nested: Some(None) is not None, which
    // is the property boxing existed to give in the first place.
    println("{[a, n] == [b, None]} {[a, n] == [c, None]}")
    println("{Some(a) == Some(b)} {Some(n) == Some(n)} {Some(n) == None}")

    // Distinct payloads still compare by their own rule.
    println("{Some("x") == Some("x")} {Some("x") == Some("y")}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "true false false true true\n" +
		"true false\n" +
		"true true false\n" +
		"true false\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The arithmetic methods. Two things are worth asserting beyond the
// values: that Self really is the receiver's own width (a u8's max is
// a u8, not an Int), and that abs and pow trap where the type runs
// out rather than wrapping.
func TestArithmeticMethods(t *testing.T) {
	out, err := runProg(t, `
import math

fn main() {
    println("{(0 - 7).abs()} {7.abs()} {0.abs()}")
    println("{5.min(3)} {5.max(3)} {2.pow(10)} {2.pow(0)} {2.pow(1)}")

    // Widths carry: Self binds to the receiver, so these stay i8/u8.
    let a: i8 = -100
    let b: i8 = 4
    println("{a.abs()} {a.min(b)} {a.max(b)} {b.pow(2)}")
    let u: u8 = 200
    println("{u.min(3)} {u.max(3)} {u.pow(1)}")

    let f = 0.0 - 2.5
    println("{f.abs()} {math.floor(f)} {math.ceil(f)} {math.round(f)} {math.trunc(f)}")
    println("{math.round(2.5)} {math.round(3.5)} {math.round(0.0 - 2.5)}")
    println("{math.sqrt(9.0)} {(2.0).pow(10.0)} {(2.0).pow(0.5)}")

    // Classification, and the total order min/max inherits from cmp:
    // NaN sorts after every number, so it loses min and wins max.
    println("{math.is_nan(math.nan)} {math.is_infinite(math.inf)} {math.is_finite(1.0)} {math.is_finite(math.nan)}")
    println("{math.nan.min(1.0)} {math.nan.max(1.0)}")

    // Constants, and the one thing a module could not hold before.
    println("{math.pi} {math.e} {math.nan == math.nan}")
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"7 7 0",
		"3 5 1024 1 2",
		"100 -100 4 16",
		"3 200 200",
		"2.5 -3 -2 -3 -2",
		"3 4 -3", // half away from zero, not banker's rounding
		"3 1024 1.4142135623730951",
		"true true true false",
		"1 NaN",
		"3.141592653589793 2.718281828459045 false",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// abs and pow trap at the edges rather than wrapping, like every other
// arithmetic operation in the language.
func TestArithmeticTraps(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"let n: i8 = -128\n    println(n.abs())", "i8 overflow: abs of -128"},
		{"println((0 - 9223372036854775807 - 1).abs())", "abs of the minimum Int"},
		{"println(2.pow(64))", "Int overflow"},
		{"let n: u8 = 16\n    println(n.pow(3))", "u8 overflow"},
		{"println(2.pow(0 - 1))", "exponent -1 is negative"},
	} {
		_, err := runProg(t, "fn main() {\n    "+tc.src+"\n}")
		if err == nil {
			t.Errorf("%s: expected a trap", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
}
