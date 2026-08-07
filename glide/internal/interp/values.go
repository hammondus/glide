package interp

import (
	"fmt"
	"strings"

	"glide/internal/ast"
)

// Values. Collections are pointer types: Glide has reference
// semantics under GC, and `mut` is a path property, not an object
// guarantee (DESIGN.md, recorded sacrifice).
type Value any

type (
	IntV   int64
	FloatV float64
	StrV   string
	BoolV  bool
	UnitV  struct{}
	TupleV []Value

	ListV struct{ Elems []Value }

	// MapV preserves insertion order (keys slice) so programs and the
	// test suite are deterministic. Provisional semantics — see
	// DESIGN-DECISIONS.md.
	MapV struct {
		keys []Value
		m    map[Value]Value
	}

	SomeV struct{ V Value }
	NoneV struct{}

	ResultV struct {
		Ok bool
		V  Value // inner value, or *ErrV
	}
	ErrV struct {
		Msg   string
		Cause Value // *ErrV or nil
	}

	ClosureV struct {
		Params    []string
		BodyExpr  ast.Expr
		BodyBlock *ast.Block
		Env       *Env
	}
	FuncV struct{ Decl *ast.FuncDecl }
	BuiltinV struct {
		Name string
		Fn   func(in *Interp, args []Value, line int) Value
	}
	ModuleV string

	IterV  struct{ Next func() (Value, bool) }
	RangeV struct{ Lo, Hi int64 }
)

func newMap() *MapV { return &MapV{m: map[Value]Value{}} }

func hashable(k Value, line int) Value {
	switch k.(type) {
	case IntV, StrV, BoolV:
		return k
	}
	panic(rtErr{line, fmt.Sprintf("%s cannot be a map key", typeName(k))})
}

func (m *MapV) get(k Value) (Value, bool) {
	v, ok := m.m[k]
	return v, ok
}

func (m *MapV) set(k, v Value) {
	if _, ok := m.m[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.m[k] = v
}

func typeName(v Value) string {
	switch v.(type) {
	case IntV:
		return "Int"
	case FloatV:
		return "Float"
	case StrV:
		return "String"
	case BoolV:
		return "Bool"
	case UnitV:
		return "()"
	case TupleV:
		return "tuple"
	case *ListV:
		return "List"
	case *MapV:
		return "Map"
	case SomeV, NoneV:
		return "Option"
	case *ResultV:
		return "Result"
	case *ErrV:
		return "Error"
	case *ClosureV, *FuncV, *BuiltinV:
		return "function"
	case ModuleV:
		return "module"
	case *IterV:
		return "Iterator"
	case RangeV:
		return "Range"
	}
	return fmt.Sprintf("%T", v)
}

// display renders a value for println/interpolation: strings bare at
// the top level, quoted inside containers.
func display(v Value) string { return render(v, false) }

func render(v Value, quoted bool) string {
	switch x := v.(type) {
	case IntV:
		return fmt.Sprintf("%d", int64(x))
	case FloatV:
		return fmt.Sprintf("%g", float64(x))
	case StrV:
		if quoted {
			return fmt.Sprintf("%q", string(x))
		}
		return string(x)
	case BoolV:
		return fmt.Sprintf("%t", bool(x))
	case UnitV:
		return "()"
	case TupleV:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = render(e, true)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *ListV:
		parts := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			parts[i] = render(e, true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *MapV:
		if len(x.keys) == 0 {
			return "[:]"
		}
		parts := make([]string, len(x.keys))
		for i, k := range x.keys {
			parts[i] = render(k, true) + ": " + render(x.m[k], true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case SomeV:
		return "Some(" + render(x.V, true) + ")"
	case NoneV:
		return "None"
	case *ResultV:
		if x.Ok {
			return "Ok(" + render(x.V, true) + ")"
		}
		return "Err(" + render(x.V, true) + ")"
	case *ErrV:
		if x.Cause != nil {
			return x.Msg + ": " + render(x.Cause, false)
		}
		return x.Msg
	case *ClosureV, *FuncV, *BuiltinV:
		return "<fn>"
	case ModuleV:
		return "<module " + string(x) + ">"
	case *IterV:
		return "<iterator>"
	case RangeV:
		return fmt.Sprintf("%d..%d", x.Lo, x.Hi)
	}
	return fmt.Sprintf("%v", v)
}

// Environments

type binding struct {
	v   Value
	mut bool
}

type Env struct {
	vars       map[string]*binding
	parent     *Env
	fnBoundary bool
}

func newEnv(parent *Env, fnBoundary bool) *Env {
	return &Env{vars: map[string]*binding{}, parent: parent, fnBoundary: fnBoundary}
}

func (e *Env) lookup(name string) *binding {
	for env := e; env != nil; env = env.parent {
		if b, ok := env.vars[name]; ok {
			return b
		}
	}
	return nil
}

// declare enforces the shadowing rule dynamically: redeclaring in the
// same scope is idiomatic; shadowing a live name from an enclosing
// block within the same function is an error. Function boundaries
// reset the rule (a closure may reuse outer names for its own locals).
func (e *Env) declare(name string, v Value, mut bool, line int) {
	if !e.fnBoundary {
		for p := e.parent; p != nil; p = p.parent {
			if _, ok := p.vars[name]; ok {
				panic(rtErr{line, fmt.Sprintf(
					"cannot shadow %q from an enclosing block (redeclaring in the same scope is fine; nested shadowing is not)", name)})
			}
			if p.fnBoundary {
				break
			}
		}
	}
	e.vars[name] = &binding{v: v, mut: mut}
}

func (e *Env) assign(name string, v Value, line int) {
	b := e.lookup(name)
	if b == nil {
		panic(rtErr{line, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", name)})
	}
	if !b.mut {
		panic(rtErr{line, fmt.Sprintf("cannot assign to immutable binding %q (declare it with `let mut`)", name)})
	}
	b.v = v
}
