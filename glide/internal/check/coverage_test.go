package check_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"glide/internal/check"
	"glide/internal/parser"
	"glide/internal/program"
)

// The conformance corpus is the device that will prove a Glide-written
// frontend faithful to this Go one (DESIGN.md, bootstrap step 3). A
// diagnostic the corpus never triggers is a rule the replacement can
// get wrong without anything noticing — so this test measures the
// coverage rather than leaving it to be assumed.
//
// It reads the checker's own source for every `c.errf` format string,
// runs every corpus program, and asserts that each format is matched
// by something the corpus actually produced. Reading the source is
// unusual and deliberate: the alternative is a hand-maintained list of
// diagnostics, which is a second thing to keep in step with the first.
//
// Adding a diagnostic without a corpus case now fails here, which is
// the point. If a diagnostic is genuinely unreachable from source —
// an internal assertion — it belongs in `exempt` with the reason,
// not in the corpus.
var exempt = map[string]string{
	"operator %s is not defined for %s and %s": "reached only via sizedBinop's width mismatch, which the checker prevents",
	"%s": "the methodHints passthrough; the hints themselves are covered",
}

func TestCorpusCoversEveryDiagnostic(t *testing.T) {
	formats := append(checkerFormats(t), declarationFormats(t)...)
	fired := corpusDiagnostics(t)

	var missing []string
	for _, f := range formats {
		if _, ok := exempt[f]; ok {
			continue
		}
		if !matchedBy(f, fired) {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d of %d frontend diagnostics are never triggered by testdata/conformance.\n"+
			"Add a case, or record it in `exempt` with the reason:\n  %s",
			len(missing), len(formats), strings.Join(missing, "\n  "))
	}
}

// checkerFormats extracts every c.errf format string from the checker's
// own source. Concatenated literals are joined, since a message split
// across Go string literals is still one message.
func checkerFormats(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	call := regexp.MustCompile(`c\.errf\([^,]+,\s*((?:"(?:[^"\\]|\\.)*"\s*\+?\s*)+)`)
	lit := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			var b strings.Builder
			for _, piece := range lit.FindAllStringSubmatch(m[1], -1) {
				b.WriteString(strings.ReplaceAll(piece[1], `\"`, `"`))
			}
			if f := b.String(); f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	if len(out) < 50 {
		t.Fatalf("only found %d diagnostics in the checker source — the extractor is broken", len(out))
	}
	return out
}

// declarationFormats extracts the declaration table's diagnostics.
// Its rules — nothing declared twice, no shadowing a builtin, no impl
// for an unknown type — are frontend decisions too, and a Glide
// reimplementation has to reproduce them exactly as it does the
// checker's.
func declarationFormats(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../program/program.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`bag\.Add\([^,]+,\s*"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(string(src), -1) {
		out = append(out, strings.ReplaceAll(m[1], `\"`, `"`))
	}
	if len(out) < 8 {
		t.Fatalf("only found %d declaration diagnostics — the extractor is broken", len(out))
	}
	return out
}

// corpusDiagnostics runs every corpus program and returns the messages
// the frontend produced, deduplicated. Declaration-stage failures
// count: to a programmer "unknown module" and "expected Int" are the
// same event, and only the stage differs.
func corpusDiagnostics(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../testdata/conformance/*.gld")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus files: %v", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(path, string(src))
		if err != nil {
			continue // a parse-stage case; it pins no checker diagnostic
		}
		tab, loadErr := program.Load(f, check.Host())
		if loadErr != nil {
			collect(loadErr.Error(), seen, &out)
			continue
		}
		_, checkErr := check.File(f, tab)
		if checkErr == nil {
			continue
		}
		collect(checkErr.Error(), seen, &out)
	}
	return out
}

// collect splits a rendered error into its messages, stripping the
// file:line:col prefix each one carries.
func collect(rendered string, seen map[string]bool, out *[]string) {
	for _, line := range strings.Split(rendered, "\n") {
		msg := strings.TrimSpace(line)
		if i := strings.Index(msg, ".gld:"); i >= 0 {
			if j := strings.Index(msg[i:], ": "); j >= 0 {
				msg = msg[i+j+2:]
			}
		}
		if msg != "" && !seen[msg] {
			seen[msg] = true
			*out = append(*out, msg)
		}
	}
}

// matchedBy reports whether some produced message could have come from
// this format string: the literal parts must appear, in order.
func matchedBy(format string, fired []string) bool {
	parts := regexp.MustCompile(`%[-#+ 0-9.]*[a-zA-Z]`).Split(format, -1)
	var b strings.Builder
	b.WriteString("^")
	for i, p := range parts {
		if i > 0 {
			b.WriteString("(?s).*")
		}
		b.WriteString(regexp.QuoteMeta(p))
	}
	rx, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	for _, f := range fired {
		if rx.MatchString(f) {
			return true
		}
	}
	return false
}
