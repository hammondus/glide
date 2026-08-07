package lexer

import "testing"

func kinds(t *testing.T, src string) []Kind {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	out := make([]Kind, len(toks))
	for i, tok := range toks {
		out[i] = tok.Kind
	}
	return out
}

func expectKinds(t *testing.T, src string, want ...Kind) {
	t.Helper()
	got := kinds(t, src)
	if len(got) != len(want) {
		t.Fatalf("Lex(%q): got %v, want %v", src, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Lex(%q): token %d = %v, want %v", src, i, got[i], want[i])
		}
	}
}

func TestNewlineTermination(t *testing.T) {
	// Newline after an expression-ending token terminates the
	// statement; after an operator it does not.
	expectKinds(t, "let x = 1\nx += 1\n",
		KwLet, Ident, Assign, Int, Semi,
		Ident, PlusEq, Int, Semi, EOF)
	expectKinds(t, "let x = 1 +\n2\n",
		KwLet, Ident, Assign, Int, Plus, Int, Semi, EOF)
}

func TestTupleAccessVsFloat(t *testing.T) {
	// b.1.cmp(a.1) is tuple access then a method call, not a float.
	expectKinds(t, "b.1.cmp(a.1)",
		Ident, Dot, Int, Dot, Ident, LParen, Ident, Dot, Int, RParen, Semi, EOF)
	expectKinds(t, "1.5", Float, Semi, EOF)
}

func TestRange(t *testing.T) {
	toks, err := Lex("0..10_000")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Kind != Int || toks[0].Int != 0 {
		t.Fatalf("token 0: %+v", toks[0])
	}
	if toks[1].Kind != DotDot {
		t.Fatalf("token 1: %+v", toks[1])
	}
	if toks[2].Kind != Int || toks[2].Int != 10000 {
		t.Fatalf("separators not stripped: %+v", toks[2])
	}
}

func TestInterpolation(t *testing.T) {
	toks, err := Lex(`"{n:6}  {word}"`)
	if err != nil {
		t.Fatal(err)
	}
	parts := toks[0].Parts
	if len(parts) != 3 {
		t.Fatalf("parts = %+v", parts)
	}
	if !parts[0].IsExpr || parts[0].S != "n" || parts[0].Spec != "6" {
		t.Fatalf("part 0 = %+v", parts[0])
	}
	if parts[1].IsExpr || parts[1].S != "  " {
		t.Fatalf("part 1 = %+v", parts[1])
	}
	if !parts[2].IsExpr || parts[2].S != "word" || parts[2].Spec != "" {
		t.Fatalf("part 2 = %+v", parts[2])
	}
}

func TestBraceEscape(t *testing.T) {
	toks, err := Lex(`"a \{literal\} b"`)
	if err != nil {
		t.Fatal(err)
	}
	parts := toks[0].Parts
	if len(parts) != 1 || parts[0].IsExpr || parts[0].S != "a {literal} b" {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestComments(t *testing.T) {
	expectKinds(t, "let x = 1 // List<(String, Int)>\n",
		KwLet, Ident, Assign, Int, Semi, EOF)
}
