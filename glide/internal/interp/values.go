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
	RuneV  rune
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

	// Option is UNBOXED in the interpreter: a T? is "the value, or
	// NoneV". Implicit T -> T? coercion falls out for free; the cost
	// is that Option<Option<T>> is unrepresentable here (a checker-
	// era concern, recorded in DESIGN-DECISIONS.md).
	NoneV struct{}

	StructV struct {
		Type   string
		Order  []string // field order for display
		Fields map[string]Value
	}
	VariantV struct {
		Type       string
		Name       string
		Args       []Value
		FieldNames []string // named-field variants; parallel to Args
	}
	TypeV string // a type name as a value: Tree.new()

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
	// FuncV: Items is non-nil for nested fns — a private env of the
	// sibling nested fns (rooted at global), so recursion and mutual
	// recursion work while enclosing locals stay invisible.
	FuncV struct {
		Decl  *ast.FuncDecl
		Items *Env
	}
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
	case IntV, StrV, BoolV, RuneV:
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
	switch x := v.(type) {
	case IntV:
		return "Int"
	case FloatV:
		return "Float"
	case StrV:
		return "String"
	case BoolV:
		return "Bool"
	case RuneV:
		return "Rune"
	case UnitV:
		return "()"
	case TupleV:
		return "tuple"
	case *ListV:
		return "List"
	case *MapV:
		return "Map"
	case NoneV:
		return "Option"
	case *StructV:
		return x.Type
	case *VariantV:
		return x.Type
	case TypeV:
		return "type"
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
	case RuneV:
		if quoted {
			return fmt.Sprintf("%q", rune(x))
		}
		return string(rune(x))
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
	case NoneV:
		return "None"
	case *StructV:
		parts := make([]string, len(x.Order))
		for i, f := range x.Order {
			parts[i] = f + ": " + render(x.Fields[f], true)
		}
		return x.Type + "{ " + strings.Join(parts, ", ") + " }"
	case *VariantV:
		if len(x.Args) == 0 {
			return x.Name
		}
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = render(a, true)
			if x.FieldNames != nil {
				parts[i] = x.FieldNames[i] + ": " + parts[i]
			}
		}
		if x.FieldNames != nil {
			return x.Name + "{ " + strings.Join(parts, ", ") + " }"
		}
		return x.Name + "(" + strings.Join(parts, ", ") + ")"
	case TypeV:
		return "<type " + string(x) + ">"
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

// eq is deep structural equality; functions and iterators are not
// comparable (runtime error at the call site's line).
func eq(a, b Value, line int) bool {
	switch x := a.(type) {
	case IntV, FloatV, StrV, BoolV, RuneV, UnitV, NoneV:
		return a == b
	case TupleV:
		y, ok := b.(TupleV)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !eq(x[i], y[i], line) {
				return false
			}
		}
		return true
	case *ListV:
		y, ok := b.(*ListV)
		if !ok || len(x.Elems) != len(y.Elems) {
			return false
		}
		for i := range x.Elems {
			if !eq(x.Elems[i], y.Elems[i], line) {
				return false
			}
		}
		return true
	case *VariantV:
		y, ok := b.(*VariantV)
		if !ok || x.Type != y.Type || x.Name != y.Name || len(x.Args) != len(y.Args) {
			return false
		}
		for i := range x.Args {
			if !eq(x.Args[i], y.Args[i], line) {
				return false
			}
		}
		return true
	case *StructV:
		y, ok := b.(*StructV)
		if !ok || x.Type != y.Type {
			return false
		}
		for f, v := range x.Fields {
			if !eq(v, y.Fields[f], line) {
				return false
			}
		}
		return true
	}
	panic(rtErr{line, fmt.Sprintf("%s values are not comparable with ==", typeName(a))})
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

// capture flattens the visible bindings into one env for a closure:
// capture-by-reference to binding cells, resolved at closure creation.
// Inner scopes win, matching lookup order.
func (e *Env) capture() *Env {
	flat := newEnv(nil, false)
	for env := e; env != nil; env = env.parent {
		for name, b := range env.vars {
			if _, seen := flat.vars[name]; !seen {
				flat.vars[name] = b
			}
		}
	}
	return flat
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
	// The free builtins are reserved outright: the set is tiny and
	// fixed, and no program legitimately needs a local named println.
	// (Imports are deliberately not reserved — that conflict is the
	// checker era's two-live-meanings rule; see DESIGN.md Shadowing.)
	if _, isBuiltin := builtins[name]; isBuiltin {
		panic(rtErr{line, fmt.Sprintf("%q is a builtin and cannot be used as a binding name", name)})
	}
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
