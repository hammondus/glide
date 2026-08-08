// Runs every fenced code block in GLIDE-BY-EXAMPLE.md through the
// interpreter, so the doc can't drift from the language. Trailing
// comment lines at the end of a block are its contract: "// error: …"
// means the program must fail with exactly that error; any other
// trailing comments are the expected stdout, line for line. Blocks
// with no trailing comments must simply run cleanly.
package glide_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"glide/internal/interp"
	"glide/internal/parser"
)

type docBlock struct {
	line int // doc line of the opening fence, for test names
	src  string
	want []string // trailing "// " comments, prefix stripped
}

func docBlocks(t *testing.T, path string) []docBlock {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []docBlock
	var cur *docBlock
	for i, line := range strings.Split(string(data), "\n") {
		switch {
		case cur == nil && strings.HasPrefix(line, "```"):
			cur = &docBlock{line: i + 1}
		case cur != nil && strings.TrimSpace(line) == "```":
			cur.src = strings.TrimSuffix(cur.src, "\n") + "\n"
			blocks = append(blocks, *cur)
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
	blocks := docBlocks(t, "../GLIDE-BY-EXAMPLE.md")
	if len(blocks) == 0 {
		t.Fatal("no fenced blocks found")
	}
	for _, b := range blocks {
		t.Run(fmt.Sprintf("L%d", b.line), func(t *testing.T) {
			want := expectations(b.src)
			wantErr := ""
			if len(want) > 0 && strings.HasPrefix(want[0], "error: ") {
				wantErr = strings.TrimPrefix(want[0], "error: ")
			}

			run := func() (string, error) {
				file, err := parser.ParseFile(b.src)
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
				if err.Error() != wantErr {
					t.Fatalf("error mismatch:\n doc:  %s\n got:  %s", wantErr, err)
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
