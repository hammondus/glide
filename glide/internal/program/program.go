// Package program builds the declaration table for a Glide file: what
// it declares, and what those declarations collide with.
//
// This exists so the checker and the evaluator cannot disagree. Both
// tiers have to answer "is `Red` a variant, and of what type?" and
// "does `Tree` have a method `insert`?" identically, and the surest way
// to guarantee that is one implementation rather than two that are
// meant to match. The rules enforced here — nothing declared twice,
// no shadowing a builtin, no impl for an unknown type, no unknown
// import — are the ones that need no type information at all, so they
// belong before the checker rather than inside it.
//
// Deliberately *not* here: evaluating `const` initializers. That is
// interpretation, and it produces values, not declarations. The table
// records that a const exists and where; the evaluator decides what it
// is worth.
package program

import (
	"fmt"

	"glide/internal/ast"
	"glide/internal/source"
)

// Known is what the host provides. Both tiers must be given the same
// Known — a program that redeclares `println`, or imports a module the
// compiler ships and the interpreter does not, has to fail the same way
// in both or the tiers have drifted.
type Known struct {
	Builtins map[string]bool // names a program may not redeclare
	Modules  map[string]bool // modules a program may import
}

// Variant is a sum-type variant, indexed by its own name because
// that is how programs refer to it (`Red`, `.NotFound{…}`).
type Variant struct {
	Type   string   // the sum type that declares it
	Arity  int      // positional payload count; 0 = bare
	Fields []string // named-field form; nil for positional or bare
}

// Table is everything a file declares. Every map is keyed by the name
// a program writes.
type Table struct {
	File *source.File

	Imports    map[string]bool
	Fns        map[string]*ast.FuncDecl
	Types      map[string]*ast.TypeDecl
	Traits     map[string]*ast.TraitDecl
	Consts     map[string]*ast.ConstDecl
	Methods    map[string]map[string]*ast.FuncDecl // type -> method -> decl
	Variants   map[string]Variant                  // variant name -> owner
	TypeTraits map[string][]string                 // type -> traits it declares
}

// Load indexes f's declarations, reporting every collision it finds
// rather than stopping at the first. A returned error is a
// *source.Error carrying all of them in source order; the table is
// still returned and is safe to inspect, so a caller that wants to
// keep going (an editor, the checker) can.
func Load(f *ast.File, known Known) (*Table, error) {
	t := &Table{
		File:       f.Source,
		Imports:    map[string]bool{},
		Fns:        map[string]*ast.FuncDecl{},
		Types:      map[string]*ast.TypeDecl{},
		Traits:     map[string]*ast.TraitDecl{},
		Consts:     map[string]*ast.ConstDecl{},
		Methods:    map[string]map[string]*ast.FuncDecl{},
		Variants:   map[string]Variant{},
		TypeTraits: map[string][]string{},
	}
	bag := &source.Bag{File: f.Source}

	for _, im := range f.Imports {
		if !known.Modules[im.Name] {
			bag.Add(im.Span, "unknown module %q", im.Name)
			continue
		}
		t.Imports[im.Name] = true
	}

	for _, fn := range f.Funcs {
		if prev, dup := t.Fns[fn.Name]; dup {
			bag.Add(fn.Span, "function %q declared twice (first at %s)", fn.Name, t.where(prev.Span))
			continue
		}
		if known.Builtins[fn.Name] {
			bag.Add(fn.Span, "%q is a builtin and cannot be redeclared", fn.Name)
			continue
		}
		t.Fns[fn.Name] = fn
	}

	// Types before impls and traits: an impl names a type, so the type
	// index has to be complete before any impl is resolved. Declaration
	// order in the file is irrelevant — module-level declarations are
	// order-independent.
	for _, td := range f.Types {
		if prev, dup := t.Types[td.Name]; dup {
			bag.Add(td.Span, "type %q declared twice (first at %s)", td.Name, t.where(prev.Span))
			continue
		}
		t.Types[td.Name] = td
		for _, v := range td.Variants {
			if prev, dup := t.Variants[v.Name]; dup {
				bag.Add(td.Span, "variant %q already declared by type %s", v.Name, prev.Type)
				continue
			}
			vi := Variant{Type: td.Name, Arity: v.Arity}
			for _, fd := range v.Fields {
				vi.Fields = append(vi.Fields, fd.Name)
			}
			t.Variants[v.Name] = vi
		}
	}

	for _, tr := range f.Traits {
		if prev, dup := t.Traits[tr.Name]; dup {
			bag.Add(tr.Span, "trait %q declared twice (first at %s)", tr.Name, t.where(prev.Span))
			continue
		}
		t.Traits[tr.Name] = tr
	}

	for _, im := range f.Impls {
		if _, known := t.Types[im.Target]; !known {
			bag.Add(im.Span, "impl for unknown type %q", im.Target)
			continue
		}
		if im.Trait != "" {
			t.TypeTraits[im.Target] = append(t.TypeTraits[im.Target], im.Trait)
		}
		ms := t.Methods[im.Target]
		if ms == nil {
			ms = map[string]*ast.FuncDecl{}
			t.Methods[im.Target] = ms
		}
		for _, fn := range im.Fns {
			if prev, dup := ms[fn.Name]; dup {
				bag.Add(fn.Span, "method %s.%s declared twice (first at %s)",
					im.Target, fn.Name, t.where(prev.Span))
				continue
			}
			ms[fn.Name] = fn
		}
	}

	for _, c := range f.Consts {
		if prev, dup := t.Consts[c.Name]; dup {
			bag.Add(c.Span, "const %q declared twice (first at %s)", c.Name, t.where(prev.Span))
			continue
		}
		t.Consts[c.Name] = c
	}

	return t, bag.Err()
}

// where formats a cross-reference to another declaration's position,
// so "declared twice" can say where the first one was.
func (t *Table) where(sp source.Span) string {
	if t.File == nil || !sp.IsValid() {
		return "?"
	}
	line, col := t.File.LineCol(sp.Pos)
	return fmt.Sprintf("%d:%d", line, col)
}
