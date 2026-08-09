package program_test

import (
	"strings"
	"testing"

	"glide/internal/parser"
	"glide/internal/program"
)

func load(t *testing.T, src string) (*program.Table, error) {
	t.Helper()
	f, err := parser.ParseFile("t.gld", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return program.Load(f, program.Known{
		Builtins: map[string]bool{"println": true},
		Modules:  map[string]bool{"fs": true, "os": true},
	})
}

func TestIndexesDeclarations(t *testing.T) {
	tab, err := load(t, `
import fs

const limit = 10

type Colour = Red | Green(Int) | Shade{ pct: Int }
type Tree = struct { root: Int }

trait Iterable { fn iter(self) -> Int }

impl Tree { fn depth(self) -> Int { 0 } }
impl Iterable for Tree { fn iter(self) -> Int { 0 } }

fn main() { }
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !tab.Imports["fs"] || len(tab.Imports) != 1 {
		t.Errorf("imports = %v", tab.Imports)
	}
	if tab.Fns["main"] == nil || tab.Types["Tree"] == nil ||
		tab.Traits["Iterable"] == nil || tab.Consts["limit"] == nil {
		t.Error("a top-level declaration is missing from the table")
	}
	// Variants are indexed by their own name, with arity and any
	// named fields, because that is how programs refer to them.
	for name, want := range map[string]program.Variant{
		"Red":   {Type: "Colour", Arity: 0},
		"Green": {Type: "Colour", Arity: 1},
		"Shade": {Type: "Colour", Fields: []string{"pct"}},
	} {
		got := tab.Variants[name]
		if got.Type != want.Type || got.Arity != want.Arity ||
			strings.Join(got.Fields, ",") != strings.Join(want.Fields, ",") {
			t.Errorf("variant %s = %+v, want %+v", name, got, want)
		}
	}
	// Both impl blocks contribute to one method set for the type.
	if ms := tab.Methods["Tree"]; ms["depth"] == nil || ms["iter"] == nil {
		t.Errorf("Tree methods = %v", ms)
	}
	if got := tab.TypeTraits["Tree"]; len(got) != 1 || got[0] != "Iterable" {
		t.Errorf("Tree traits = %v", got)
	}
}

// Load reports every collision rather than stopping at the first —
// this is what the checker needs, and it is why the parser's
// fail-fast behaviour does not extend here.
func TestReportsEveryCollision(t *testing.T) {
	_, err := load(t, `
import nosuch
fn helper() { }
fn helper() { }
type Colour = Red
type Colour = Blue
fn println() { }
impl Missing { fn f(self) { } }
const k = 1
const k = 2
fn main() { }
`)
	if err == nil {
		t.Fatal("Load accepted a file full of collisions")
	}
	got := err.Error()
	for _, want := range []string{
		`unknown module "nosuch"`,
		`function "helper" declared twice`,
		`type "Colour" declared twice`,
		`"println" is a builtin and cannot be redeclared`,
		`impl for unknown type "Missing"`,
		`const "k" declared twice`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing diagnostic %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "t.gld:"); n != 6 {
		t.Errorf("want 6 positioned diagnostics, got %d:\n%s", n, got)
	}
	// Each duplicate names where the first declaration was, so the
	// reader does not have to search for it.
	if !strings.Contains(got, "(first at 3:1)") {
		t.Errorf("duplicate should cross-reference the original:\n%s", got)
	}
}

func TestVariantCollisionAcrossTypes(t *testing.T) {
	_, err := load(t, "type A = Red | Blue\ntype B = Red\nfn main() { }")
	if err == nil || !strings.Contains(err.Error(), `variant "Red" already declared by type A`) {
		t.Fatalf("cross-type variant collision: %v", err)
	}
}

// A table is returned even when Load fails, so a caller that wants to
// keep working with a broken file (the checker, an editor) can.
func TestTableUsableAfterErrors(t *testing.T) {
	tab, err := load(t, "fn dup() { }\nfn dup() { }\nfn main() { }")
	if err == nil {
		t.Fatal("expected an error")
	}
	if tab == nil || tab.Fns["main"] == nil || tab.Fns["dup"] == nil {
		t.Error("table should still hold the declarations it did index")
	}
}
