// Package source carries positions and diagnostics: the shared
// vocabulary the lexer, parser, checker and interpreter use to point
// at a place in a program.
//
// Positions are byte offsets, not line/column pairs. Offsets are what
// a scanner naturally produces, they compare and subtract for free,
// and line/column is a *rendering* concern — computed once, when a
// diagnostic is actually printed. M1-M3 carried a bare line number on
// some nodes and nothing at all on others (Param, FieldDecl, Closure,
// every literal and pattern), which put a floor under how good a
// diagnostic could ever be: "line 42" cannot point at one field of
// three on that line.
package source

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Span is a half-open byte range [Pos, End). The zero Span means
// "no position known" and renders without a location.
type Span struct {
	Pos int
	End int
}

// At builds a span covering a single position.
func At(pos int) Span { return Span{Pos: pos, End: pos} }

// To extends s to cover through the end of other. Used to widen a
// node's span to its full extent as the parser learns where it ends.
func (s Span) To(other Span) Span {
	if !s.IsValid() {
		return other
	}
	if !other.IsValid() {
		return s
	}
	return Span{Pos: min(s.Pos, other.Pos), End: max(s.End, other.End)}
}

func (s Span) IsValid() bool { return s.End >= s.Pos && (s.Pos > 0 || s.End > 0) }

// File is a source file plus the line index needed to turn offsets
// into human positions and rendered snippets.
type File struct {
	Name string
	Src  string

	lineStarts []int // byte offset of the first byte of each line
}

func NewFile(name, src string) *File {
	f := &File{Name: name, Src: src, lineStarts: []int{0}}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			f.lineStarts = append(f.lineStarts, i+1)
		}
	}
	return f
}

// LineCol reports the 1-based line and column of a byte offset.
//
// Columns count *runes*, not bytes: the column exists to be read by a
// human and to line a caret up under the right character, and a
// multi-byte rune earlier in the line would break both.
func (f *File) LineCol(pos int) (line, col int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(f.Src) {
		pos = len(f.Src)
	}
	// The first line start strictly greater than pos, minus one, is
	// the line containing pos.
	line = sort.SearchInts(f.lineStarts, pos+1) - 1
	col = utf8.RuneCountInString(f.Src[f.lineStarts[line]:pos]) + 1
	return line + 1, col
}

// lineText returns the text of 1-based line n, without its newline.
func (f *File) lineText(n int) string {
	if n < 1 || n > len(f.lineStarts) {
		return ""
	}
	start := f.lineStarts[n-1]
	end := len(f.Src)
	if n < len(f.lineStarts) {
		end = f.lineStarts[n] - 1 // drop the '\n'
	}
	return strings.TrimRight(f.Src[start:end], "\r")
}

// Diagnostic is one message about one place in the program.
type Diagnostic struct {
	Span
	Msg string
}

func (d Diagnostic) Error() string { return d.Msg }

// Render formats a diagnostic with its location and a caret snippet:
//
//	notes.gld:12:18: expected ':' in parameter list, found ','
//	   12 | fn process(items, limit) {
//	      |                 ^
//
// A span covering more than one character underlines all of it. A span
// running past the end of its first line is truncated there — a caret
// spanning lines is noise, and the start is what matters.
func (f *File) Render(d Diagnostic) string {
	if f == nil || !d.IsValid() {
		return d.Msg
	}
	line, col := f.LineCol(d.Pos)
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d:%d: %s", f.Name, line, col, d.Msg)

	text := f.lineText(line)
	if text == "" {
		return b.String()
	}
	gutter := fmt.Sprintf("%d", line)
	pad := strings.Repeat(" ", len(gutter))
	fmt.Fprintf(&b, "\n %s | %s\n %s | ", gutter, text, pad)

	// Rebuild the run-up from the source itself rather than emitting
	// col-1 spaces: a tab is one column but many screen columns, and
	// copying it verbatim is the only thing that keeps the caret under
	// the right character.
	runUp := []rune(text)
	if col-1 < len(runUp) {
		runUp = runUp[:col-1]
	}
	for _, r := range runUp {
		if r == '\t' {
			b.WriteByte('\t')
		} else {
			b.WriteByte(' ')
		}
	}

	width := utf8.RuneCountInString(f.Src[d.Pos:min(d.End, len(f.Src))])
	if lineEnd := f.lineStarts[line-1] + len(text); d.End > lineEnd {
		width = utf8.RuneCountInString(f.Src[d.Pos:lineEnd])
	}
	b.WriteString(strings.Repeat("^", max(width, 1)))
	return b.String()
}

// Bag collects diagnostics. The checker reports every problem it can
// find in one pass (DESIGN.md's typed holes require checking to
// continue past an error), so the bag — not a returned error — is the
// reporting channel.
type Bag struct {
	File  *File
	diags []Diagnostic
}

func (b *Bag) Add(sp Span, format string, args ...any) {
	b.diags = append(b.diags, Diagnostic{Span: sp, Msg: fmt.Sprintf(format, args...)})
}

func (b *Bag) Len() int          { return len(b.diags) }
func (b *Bag) All() []Diagnostic { return b.diags }
func (b *Bag) Empty() bool       { return len(b.diags) == 0 }

// Err returns all collected diagnostics as one error, or nil if the
// bag is empty. Diagnostics come out in source order, whatever order
// they were found in.
func (b *Bag) Err() error {
	if len(b.diags) == 0 {
		return nil
	}
	ds := append([]Diagnostic(nil), b.diags...)
	sort.SliceStable(ds, func(i, j int) bool { return ds[i].Pos < ds[j].Pos })
	return &Error{File: b.File, Diags: ds}
}

// Error is one or more diagnostics that know how to render themselves.
type Error struct {
	File  *File
	Diags []Diagnostic
}

func (e *Error) Error() string {
	parts := make([]string, len(e.Diags))
	for i, d := range e.Diags {
		parts[i] = e.File.Render(d)
	}
	return strings.Join(parts, "\n")
}
