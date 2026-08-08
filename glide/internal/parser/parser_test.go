package parser

import (
	"os"
	"testing"

	"glide/internal/ast"
)

func TestParseWordfreq(t *testing.T) {
	src, err := os.ReadFile("../../examples/wordfreq.gld")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFile(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Imports) != 2 || f.Imports[0] != "fs" || f.Imports[1] != "os" {
		t.Fatalf("imports = %v", f.Imports)
	}
	if len(f.Funcs) != 1 || f.Funcs[0].Name != "main" {
		t.Fatalf("funcs = %+v", f.Funcs)
	}
	if f.Funcs[0].RetType != "Result<(), Error>" {
		t.Fatalf("main return type = %q", f.Funcs[0].RetType)
	}
}

func TestLetElse(t *testing.T) {
	f, err := ParseFile("fn main() {\n let [_, p, ..rest] = xs else { return }\n}")
	if err != nil {
		t.Fatal(err)
	}
	let := f.Funcs[0].Body.Stmts[0].(*ast.LetStmt)
	if let.Else == nil {
		t.Fatal("else block not parsed")
	}
	lp := let.Pat.(*ast.ListPat)
	if lp.Rest != 2 || lp.RestName != "rest" || len(lp.Elems) != 2 {
		t.Fatalf("list pattern = %+v", lp)
	}
}

func TestNestedShadowIsRuntimeNotParse(t *testing.T) {
	// The shadow ban is enforced in eval, not the parser.
	if _, err := ParseFile("fn main() {\n let x = 1\n for i in 0..3 { let x = 2 }\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestAssignTargets(t *testing.T) {
	if _, err := ParseFile("fn main() {\n f() = 3\n}"); err == nil {
		t.Fatal("assignment to a call should not parse")
	}
	if _, err := ParseFile("fn main() {\n m[k] = 3\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestClosureForms(t *testing.T) {
	f, err := ParseFile("fn main() {\n xs.sort_by(|a, b| b.1.cmp(a.1))\n spawn(|| tick())\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Funcs[0].Body.Stmts) != 2 {
		t.Fatalf("stmts = %d", len(f.Funcs[0].Body.Stmts))
	}
}

func TestBreakContinueOnlyInLoops(t *testing.T) {
	bad := []string{
		"fn main() {\n break\n}",
		"fn main() {\n continue\n}",
		"fn main() {\n if true { break }\n}",
		// A closure body is its own function; the enclosing loop is
		// out of reach.
		"fn main() {\n for i in 0..3 { spawn(|| { break }) }\n}",
	}
	for _, src := range bad {
		if _, err := ParseFile(src); err == nil {
			t.Errorf("should not parse:\n%s", src)
		}
	}
	good := []string{
		"fn main() {\n for { break }\n}",
		"fn main() {\n for i in 0..3 { continue }\n}",
		"fn main() {\n for true { if x { break } }\n}",
		// A loop inside the closure makes break legal again.
		"fn main() {\n for i in 0..3 { spawn(|| { for { break } }) }\n}",
	}
	for _, src := range good {
		if _, err := ParseFile(src); err != nil {
			t.Errorf("should parse: %v\n%s", err, src)
		}
	}
}

func TestMultiPatternArmsBindNothing(t *testing.T) {
	bad := []string{
		"fn main() {\n _ = match x {\n  1, n => n\n  _ => 0\n }\n}",
		"fn main() {\n _ = match x {\n  Some(v), None => 1\n  _ => 0\n }\n}",
	}
	for _, src := range bad {
		if _, err := ParseFile(src); err == nil {
			t.Errorf("should not parse:\n%s", src)
		}
	}
	// Binding-free constructor args are fine.
	if _, err := ParseFile("fn main() {\n _ = match x {\n  Some(1), None => 1\n  _ => 0\n }\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestStringPatternNoInterpolation(t *testing.T) {
	if _, err := ParseFile("fn main() {\n _ = match s {\n  \"{x}\" => 1\n  _ => 0\n }\n}"); err == nil {
		t.Fatal("interpolated string pattern should not parse")
	}
}
