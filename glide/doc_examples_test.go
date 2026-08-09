// Runs fenced code blocks from the prose docs through the interpreter,
// so they can't drift from the language. Trailing comment lines at the
// end of a block are its contract: "// error: …" means the program
// must fail with exactly that error; any other trailing comments are
// the expected stdout, line for line. Blocks with no trailing comments
// must simply run cleanly.
//
// Two sources, two conventions:
//
//   - GLIDE-BY-EXAMPLE.md — *every* block runs. The document exists to
//     be executable, so there is nothing to opt into.
//   - docs/book/*.md — only blocks fenced ```glide-run. The book is
//     mostly fragments (a lone signature, three lines of a body), which
//     is the right way to teach and the wrong thing to execute, so
//     complete programs opt in. An unknown info string renders exactly
//     like ```glide, so the marker is invisible to a reader.
package glide_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"glide/internal/interp"
	"glide/internal/parser"
	"glide/internal/source"
)

type docBlock struct {
	line int // doc line of the opening fence, for test names
	src  string
	want []string // trailing "// " comments, prefix stripped
}

// docBlocks returns every fenced block in the file. fence, when
// non-empty, restricts the result to blocks whose info string matches
// it exactly.
func docBlocks(t *testing.T, path string, fence string) []docBlock {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []docBlock
	var cur *docBlock
	var skip bool
	for i, line := range strings.Split(string(data), "\n") {
		switch {
		case cur == nil && strings.HasPrefix(line, "```"):
			cur = &docBlock{line: i + 1}
			skip = fence != "" && strings.TrimSpace(line) != "```"+fence
		case cur != nil && strings.TrimSpace(line) == "```":
			cur.src = strings.TrimSuffix(cur.src, "\n") + "\n"
			if !skip {
				blocks = append(blocks, *cur)
			}
			cur = nil
		case cur != nil:
			cur.src += line + "\n"
		}
	}
	if cur != nil {
		t.Fatalf("%s: unclosed fence at line %d", path, cur.line)
	}
	return blocks
}

// expectations strips the trailing comment lines off a block. They are
// valid Glide comments, so the source runs unmodified; they are only
// read back as the contract.
func expectations(src string) []string {
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	n := len(lines)
	for n > 0 && strings.HasPrefix(strings.TrimSpace(lines[n-1]), "//") {
		n--
	}
	var want []string
	for _, l := range lines[n:] {
		want = append(want, strings.TrimPrefix(strings.TrimSpace(l), "// "))
	}
	return want
}

func TestDocExamples(t *testing.T) {
	blocks := docBlocks(t, "../GLIDE-BY-EXAMPLE.md", "")
	if len(blocks) == 0 {
		t.Fatal("no fenced blocks found")
	}
	runBlocks(t, blocks)
}

// TestBookExamples runs the book's opted-in programs. The book is
// allowed to contain fragments; a ```glide-run block is a promise that
// the block is a whole program, and this is what holds the promise.
func TestBookExamples(t *testing.T) {
	paths, err := filepath.Glob("../docs/book/*.md")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no book chapters found")
	}
	total := 0
	for _, path := range paths {
		blocks := docBlocks(t, path, "glide-run")
		total += len(blocks)
		if len(blocks) == 0 {
			continue
		}
		t.Run(strings.TrimSuffix(filepath.Base(path), ".md"), func(t *testing.T) {
			runBlocks(t, blocks)
		})
	}
	if total == 0 {
		t.Fatal("no ```glide-run blocks found in docs/book")
	}
}

func runBlocks(t *testing.T, blocks []docBlock) {
	t.Helper()
	for _, b := range blocks {
		t.Run(fmt.Sprintf("L%d", b.line), func(t *testing.T) {
			want := expectations(b.src)
			wantErr := ""
			if len(want) > 0 && strings.HasPrefix(want[0], "error: ") {
				wantErr = strings.TrimPrefix(want[0], "error: ")
			}

			run := func() (string, error) {
				file, err := parser.ParseFile("example.gld", b.src)
				if err != nil {
					return "", err
				}
				var out bytes.Buffer
				in := interp.New()
				in.Stdout = &out
				in.Stderr = &out
				err = in.Run(file)
				return out.String(), err
			}
			out, err := run()

			if wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, ran fine", wantErr)
				}
				if got := docErrText(err); got != wantErr {
					t.Fatalf("error mismatch:\n doc:  %s\n got:  %s\n\nfull:\n%v", wantErr, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("example does not run: %v", err)
			}
			if len(want) > 0 {
				expected := strings.Join(want, "\n") + "\n"
				if out != expected {
					t.Fatalf("output mismatch:\n doc:  %q\n got:  %q", expected, out)
				}
			}
		})
	}
}

// docErrText spells an error the way GLIDE-BY-EXAMPLE.md's "// error:"
// contracts do: "line N: message". Errors now render with a filename,
// a column and a source snippet, none of which belongs in a doc
// contract — the doc asserts *what* went wrong and on which line, and
// stays readable.
func docErrText(err error) string {
	var se *source.Error
	if errors.As(err, &se) && len(se.Diags) > 0 {
		d := se.Diags[0]
		line, _ := se.File.LineCol(d.Pos)
		return fmt.Sprintf("line %d: %s", line, d.Msg)
	}
	return err.Error()
}
