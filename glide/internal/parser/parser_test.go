package parser

import (
	"os"
	"testing"

	"glide/internal/ast"
)

func TestParseWordfreq(t *testing.T) {
	src, err := os.ReadFile("../../examples/wordfreq.gl")
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
