package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glide/internal/ast"
	"glide/internal/check"
	"glide/internal/parser"
	"glide/internal/program"
	"glide/internal/types"
)

func checkSrc(t *testing.T, src string) (*check.Info, error) {
	t.Helper()
	f, err := parser.ParseFile("t.gld", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tab, err := program.Load(f, check.Host())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return check.File(f, tab)
}

// The real programs are the regression suite that matters: a checker
// that rejects working Glide is worse than no checker at all, and
// these six are the largest Glide that exists.
func TestExamplesCheckClean(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.gld")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found (%v)", err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(path, string(src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			tab, err := program.Load(f, check.Host())
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if _, err := check.File(f, tab); err != nil {
				t.Errorf("%s does not check clean:\n%v", path, err)
			}
		})
	}
}

// Info is the seam the evaluator and the eventual code generator read.
// These assert it is actually populated, not merely returned.
func TestInfoRecordsShorthandResolution(t *testing.T) {
	info, err := checkSrc(t, `
type Colour = Red | Green(Int)
fn paint(c: Colour) -> String { "x" }
fn main() { println(paint(.Green(1))) }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Shorthand) != 1 {
		t.Fatalf("want one resolved shorthand, got %d", len(info.Shorthand))
	}
	for _, v := range info.Shorthand {
		if v.Name != "Green" || v.Owner.Name != "Colour" {
			t.Errorf("resolved to %s of %s", v.Name, v.Owner.Name)
		}
		if len(v.Args) != 1 || !types.Identical(v.Args[0], types.Int) {
			t.Errorf("payload types = %v", v.Args)
		}
	}
}

func TestInfoRecordsExpressionTypes(t *testing.T) {
	src := "fn main() {\n    let xs = [\"a\"]\n    println(xs.len())\n}\n"
	f, err := parser.ParseFile("t.gld", src)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := program.Load(f, check.Host())
	if err != nil {
		t.Fatal(err)
	}
	info, err := check.File(f, tab)
	if err != nil {
		t.Fatal(err)
	}
	var sawList, sawLen bool
	for e, ty := range info.Types {
		switch e.(type) {
		case *ast.ListLit:
			sawList = ty.String() == "List<String>"
		case *ast.Call:
			if ty == types.Int {
				sawLen = true
			}
		}
	}
	if !sawList {
		t.Error("the list literal's type was not recorded as List<String>")
	}
	if !sawLen {
		t.Error("xs.len()'s type was not recorded as Int")
	}
}

// The checker is under-approximating by construction: anything it
// cannot model is Unknown, and Unknown never produces a diagnostic.
// This is what makes it safe to have it mandatory while it is still
// growing, so it gets a test of its own.
func TestUnknownNeverReports(t *testing.T) {
	// A generic body: T is opaque, so nothing about `a < b`,
	// `a.whatever()` or `a + b` is knowable yet.
	if _, err := checkSrc(t, `
fn compare<T: Ord>(a: T, b: T) -> Bool { a < b }
fn main() { println(compare(1, 2)) }
`); err != nil {
		t.Errorf("operations on a type parameter must not be reported: %v", err)
	}
	// A dynamically-shaped value from json.decode. The JSON document
	// is a raw string, which is what a program with braces in it has
	// to use — an ordinary string always interpolates.
	dyn := "import json\n" +
		"fn main() {\n" +
		"    match json.decode(`{}`) {\n" +
		"        Ok(v)  => println(v.anything)\n" +
		"        Err(e) => println(\"{e}\")\n" +
		"    }\n" +
		"}\n"
	if _, err := checkSrc(t, dyn); err != nil {
		t.Errorf("members of an unknown value must not be reported: %v", err)
	}
}

func TestMultipleDiagnosticsAreReported(t *testing.T) {
	_, err := checkSrc(t, `
fn f(n: Int) -> Int { n }
fn main() {
    println(f("a"))
    println(f("b"))
    println(f("c"))
}
`)
	if err == nil {
		t.Fatal("expected diagnostics")
	}
	// Check-and-continue, not fail-fast: the parser stops at the first
	// error because a broken parse makes everything after it noise;
	// the checker has a complete tree and no such excuse.
	if n := strings.Count(err.Error(), "expected Int, found String"); n != 3 {
		t.Errorf("want 3 diagnostics, got %d:\n%v", n, err)
	}
}
