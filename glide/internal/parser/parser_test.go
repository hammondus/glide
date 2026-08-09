package parser

import (
	"os"
	"strings"
	"testing"

	"glide/internal/ast"
)

func TestParseWordfreq(t *testing.T) {
	src, err := os.ReadFile("../../examples/wordfreq.gld")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFile("test.gld", string(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Imports) != 2 || f.Imports[0].Name != "fs" || f.Imports[1].Name != "os" {
		t.Fatalf("imports = %v", f.Imports)
	}
	if len(f.Funcs) != 1 || f.Funcs[0].Name != "main" {
		t.Fatalf("funcs = %+v", f.Funcs)
	}
	if got := f.Funcs[0].RetType.String(); got != "Result<(), Error>" {
		t.Fatalf("main return type = %q", got)
	}
}

// M1-M3 skipped `<...>` lists wholesale, so nothing here could be
// asserted: type parameters never reached the AST.
func TestTypeParamsReachAST(t *testing.T) {
	f, err := ParseFile("test.gld", `
type Pair<A, B> = struct { first: A, second: B }
trait Container<T> { fn get(self) -> T }
impl Iterable<T> for Tree<T> { fn iter(self) -> Int { 0 } }
impl Stack<Int> { fn depth(self) -> Int { 0 } }
fn insert_node<T: Ord + Hash, U>(at: T, v: U) -> T { at }
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Types[0].TypeParams; len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Errorf("type Pair params = %+v", got)
	}
	if got := f.Traits[0].TypeParams; len(got) != 1 || got[0].Name != "T" {
		t.Errorf("trait Container params = %+v", got)
	}
	// `impl Trait<..> for Target<..>`: the first list belongs to the
	// trait, the second to the target.
	im := f.Impls[0]
	if im.Trait != "Iterable" || im.Target != "Tree" ||
		len(im.TraitArgs) != 1 || im.TraitArgs[0].String() != "T" ||
		len(im.TargetArgs) != 1 || im.TargetArgs[0].String() != "T" {
		t.Errorf("impl Iterable for Tree: trait %q %v, target %q %v",
			im.Trait, im.TraitArgs, im.Target, im.TargetArgs)
	}
	// An impl header holds type *arguments*, so a concrete one
	// round-trips rather than being mistaken for a binder named Int.
	if sp := f.Impls[1]; sp.Trait != "" || sp.Target != "Stack" ||
		len(sp.TargetArgs) != 1 || sp.TargetArgs[0].String() != "Int" {
		t.Errorf("impl Stack<Int> = %+v", sp)
	}
	fn := f.Funcs[0]
	if len(fn.TypeParams) != 2 {
		t.Fatalf("insert_node params = %+v", fn.TypeParams)
	}
	if b := fn.TypeParams[0].Bounds; len(b) != 2 || b[0].String() != "Ord" || b[1].String() != "Hash" {
		t.Errorf("T bounds = %v", b)
	}
	if b := fn.TypeParams[1].Bounds; b != nil {
		t.Errorf("U should be unconstrained, got %v", b)
	}
}

func TestTypeParamErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"fn f<t>(x: Int) { }", "type parameter names are capitalised"},
		{"fn f<>(x: Int) { }", "type parameter list"},
		{"fn f<T(x: Int) { }", "type parameter list"},
		{"fn f(x: (Int)) { }", "is not a type"},
	} {
		_, err := ParseFile("test.gld", tc.src)
		if err == nil {
			t.Errorf("%q parsed, want error %q", tc.src, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error = %q, want it to contain %q", tc.src, err, tc.want)
		}
	}
}

func TestLetElse(t *testing.T) {
	f, err := ParseFile("test.gld", "fn main() {\n let [_, p, ..rest] = xs else { return }\n}")
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
	if _, err := ParseFile("test.gld", "fn main() {\n let x = 1\n for i in 0..3 { let x = 2 }\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestAssignTargets(t *testing.T) {
	if _, err := ParseFile("test.gld", "fn main() {\n f() = 3\n}"); err == nil {
		t.Fatal("assignment to a call should not parse")
	}
	if _, err := ParseFile("test.gld", "fn main() {\n m[k] = 3\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestClosureForms(t *testing.T) {
	f, err := ParseFile("test.gld", "fn main() {\n xs.sort_by(|a, b| b.1.cmp(a.1))\n spawn(|| tick())\n}")
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
		if _, err := ParseFile("test.gld", src); err == nil {
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
		if _, err := ParseFile("test.gld", src); err != nil {
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
		if _, err := ParseFile("test.gld", src); err == nil {
			t.Errorf("should not parse:\n%s", src)
		}
	}
	// Binding-free constructor args are fine.
	if _, err := ParseFile("test.gld", "fn main() {\n _ = match x {\n  Some(1), None => 1\n  _ => 0\n }\n}"); err != nil {
		t.Fatal(err)
	}
}

func TestStringPatternNoInterpolation(t *testing.T) {
	if _, err := ParseFile("test.gld", "fn main() {\n _ = match s {\n  \"{x}\" => 1\n  _ => 0\n }\n}"); err == nil {
		t.Fatal("interpolated string pattern should not parse")
	}
}

func TestLeadingDotContinuesLine(t *testing.T) {
	f, err := ParseFile("test.gld", "fn main() {\n let x = [1, 2].iter()\n  .map(|n| n)\n  .collect()\n _ = x\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Funcs[0].Body.Stmts) != 2 {
		t.Fatalf("chain must parse as one statement; got %d", len(f.Funcs[0].Body.Stmts))
	}
	// `..` at line start is a range token, not a continuation.
	if _, err := ParseFile("test.gld", "fn main() {\n let x = 1\n ..3\n}"); err == nil {
		t.Fatal("leading .. must not continue the line")
	}
}
