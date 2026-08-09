package source

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLineCol(t *testing.T) {
	f := NewFile("t.gld", "abc\ndef\n\nghi")
	for _, tc := range []struct {
		pos       int
		line, col int
	}{
		{0, 1, 1},
		{2, 1, 3},
		{3, 1, 4},  // the '\n' itself belongs to line 1
		{4, 2, 1},  // first byte of line 2
		{8, 3, 1},  // the empty line
		{9, 4, 1},  // 'g'
		{12, 4, 4}, // one past the end
	} {
		if line, col := f.LineCol(tc.pos); line != tc.line || col != tc.col {
			t.Errorf("LineCol(%d) = %d:%d, want %d:%d", tc.pos, line, col, tc.line, tc.col)
		}
	}
}

// Columns count runes, so a multi-byte rune earlier in the line must
// not push the reported column (or the caret) to the right.
func TestLineColCountsRunes(t *testing.T) {
	src := `let s = "héllo wörld"` + "\n"
	pos := strings.Index(src, "wörld")
	line, col := NewFile("t.gld", src).LineCol(pos)
	// 15 runes precede "wörld" but 16 bytes do, so byte-counting
	// would report 17 here. That off-by-one is the bug this catches.
	want := utf8.RuneCountInString(src[:pos]) + 1
	if line != 1 || col != want {
		t.Errorf("LineCol = %d:%d, want 1:%d", line, col, want)
	}
}

func TestRenderCaret(t *testing.T) {
	src := "fn main() {\n    let x = 1 + \n}\n"
	f := NewFile("t.gld", src)
	pos := strings.Index(src, "+")
	got := f.Render(Diagnostic{Span: Span{Pos: pos, End: pos + 1}, Msg: "expected an operand"})
	want := "t.gld:2:15: expected an operand\n" +
		" 2 |     let x = 1 + \n" +
		"   |               ^"
	if got != want {
		t.Errorf("Render:\n%s\nwant:\n%s", got, want)
	}
}

// A span wider than one character underlines all of it — the checker
// wants to point at a whole type annotation, not its first byte.
func TestRenderUnderlinesWholeSpan(t *testing.T) {
	src := "fn f(x: List<Int>) { }\n"
	f := NewFile("t.gld", src)
	pos := strings.Index(src, "List<Int>")
	got := f.Render(Diagnostic{Span: Span{Pos: pos, End: pos + len("List<Int>")}, Msg: "no such type"})
	if !strings.HasSuffix(got, "\n   |         ^^^^^^^^^") {
		t.Errorf("Render:\n%s", got)
	}
}

// Tabs are one column but many screen columns; the run-up copies them
// verbatim so the caret still lands under the right character.
func TestRenderPreservesTabs(t *testing.T) {
	src := "fn main() {\n\t\tlet x = ?\n}\n"
	f := NewFile("t.gld", src)
	pos := strings.Index(src, "?")
	got := f.Render(Diagnostic{Span: At(pos), Msg: "hole"})
	line := got[strings.LastIndex(got, "\n")+1:]
	if !strings.HasPrefix(line, "   | \t\t") {
		t.Errorf("caret run-up did not preserve tabs: %q", line)
	}
}

func TestRenderNoPosition(t *testing.T) {
	f := NewFile("t.gld", "x")
	if got := f.Render(Diagnostic{Msg: "bare"}); got != "bare" {
		t.Errorf("Render with zero span = %q, want %q", got, "bare")
	}
}

func TestBagSortsBySourceOrder(t *testing.T) {
	f := NewFile("t.gld", "aaa\nbbb\nccc\n")
	b := &Bag{File: f}
	b.Add(At(8), "third")
	b.Add(At(0), "first")
	b.Add(At(4), "second")
	if b.Len() != 3 || b.Empty() {
		t.Fatalf("bag len = %d", b.Len())
	}
	err := b.Err()
	if err == nil {
		t.Fatal("Err() = nil for a non-empty bag")
	}
	got := err.Error()
	if i, j := strings.Index(got, "first"), strings.Index(got, "second"); i > j {
		t.Errorf("diagnostics not in source order:\n%s", got)
	}
	if strings.Index(got, "second") > strings.Index(got, "third") {
		t.Errorf("diagnostics not in source order:\n%s", got)
	}
	if (&Bag{File: f}).Err() != nil {
		t.Error("empty bag returned a non-nil error")
	}
}
