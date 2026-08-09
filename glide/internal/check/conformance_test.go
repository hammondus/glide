package check_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"glide/internal/check"
	"glide/internal/parser"
	"glide/internal/program"
	"glide/internal/source"
)

// The conformance corpus is a first-class artifact, not a Go table
// test that happens to live in files: every implementation of Glide's
// frontend has to pass it unchanged. See testdata/conformance/README.md
// for the format and the rules for adding cases.
const corpusDir = "../../testdata/conformance"

var errComment = regexp.MustCompile(`//\s*error:\s*(.*)$`)

func TestConformance(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.gld"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files found in %s (%v)", corpusDir, err)
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".gld"), func(t *testing.T) {
			runCorpusFile(t, path)
		})
	}
}

func runCorpusFile(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	want := expectedErrors(src)

	f, err := parser.ParseFile(path, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tab, err := program.Load(f, check.Host())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, checkErr := check.File(f, tab)

	got := map[int][]string{}
	var se *source.Error
	if checkErr != nil {
		if !errors.As(checkErr, &se) {
			t.Fatalf("checker returned a positionless error: %v", checkErr)
		}
		for _, d := range se.Diags {
			line, _ := se.File.LineCol(d.Pos)
			got[line] = append(got[line], d.Msg)
		}
	}

	for line, substr := range want {
		msgs := got[line]
		switch {
		case len(msgs) == 0:
			t.Errorf("line %d: expected a diagnostic containing %q, got none", line, substr)
		case len(msgs) > 1:
			t.Errorf("line %d: expected one diagnostic, got %d: %v", line, len(msgs), msgs)
		case !strings.Contains(msgs[0], substr):
			t.Errorf("line %d: diagnostic %q does not contain %q", line, msgs[0], substr)
		}
		delete(got, line)
	}
	for line, msgs := range got {
		for _, m := range msgs {
			t.Errorf("line %d: unexpected diagnostic %q", line, m)
		}
	}
}

// expectedErrors reads the `// error: …` contracts out of a corpus
// file, keyed by the line they sit on.
func expectedErrors(src string) map[int]string {
	want := map[int]string{}
	for i, line := range strings.Split(src, "\n") {
		if m := errComment.FindStringSubmatch(line); m != nil {
			want[i+1] = strings.TrimSpace(m[1])
		}
	}
	return want
}
