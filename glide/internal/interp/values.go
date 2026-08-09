package interp

import (
	"fmt"
	"strings"
	"time"

	"glide/internal/ast"
	"glide/internal/source"
)

// Values. Collections are pointer types: Glide has reference
// semantics under GC, and `mut` is a path property, not an object
// guarantee (DESIGN.md, recorded sacrifice).
type Value any

type (
	IntV   int64

	// UintV is a u64 value, and only a u64: it exists because u64 is
	// the one integer type whose range an int64 cannot hold. The
	// narrower sized types (i8-i32, u8-u32) still live in an IntV,
	// which is why they do not yet wrap at their own width — the
	// stated gap in DESIGN-DECISIONS.md. A u64 never mixes with an
	// Int: DESIGN.md forbids implicit numeric conversion, the checker
	// enforces it, and binop has no case for the pair.
	UintV uint64

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
		Fn   func(in *Interp, args []Value, at source.Span) Value
	}
	ModuleV string

	IterV  struct{ Next func() (Value, bool) }
	RangeV struct{ Lo, Hi int64 }

	// ScopeV is the handle bound by `scope s { … }`; TaskV is what
	// spawn returns. Both are meaningful only while their scope runs.
	ScopeV struct{ st *scopeState }

	// Channel halves: channel() returns the pair, and the split is
	// structural — no whole-channel value exists. Copies share state
	// (mpmc); only the sender half can close.
	SenderV   struct{ st *chanState }
	ReceiverV struct{ st *chanState }

	// Time: Duration and Instant are distinct, never conflated.
	// Wrapping Go's types buys the dual wall/monotonic clock for free.
	DurationV time.Duration
	InstantV  struct{ T time.Time }

	// DistinctV is a `type NoteId = distinct Int` value: a nominal
	// wrapper. No implicit conversion, no inherited operators —
	// mixing with the base type is a loud error, which is the point.
	DistinctV struct {
		Type string
		V    Value
	}
	TaskV struct {
		done      chan struct{} // closed after result/pan are set
		result    Value
		pan       any  // child panic; the scope re-panics it at exit
		cancelled bool // child unwound by cancellation; result never existed
		joined    bool // GIL-protected; unjoined Err fails the scope
	}
)

func newMap() *MapV { return &MapV{m: map[Value]Value{}} }

func hashable(k Value, at source.Span) Value {
	switch k.(type) {
	case IntV, UintV, StrV, BoolV, RuneV:
		return k
	}
	panic(rtErr{at, fmt.Sprintf("%s cannot be a map key", typeName(k))})
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
	case UintV:
		return "u64"
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
	case *ScopeV:
		return "Scope"
	case *TaskV:
		return "Task"
	case *SenderV:
		return "Sender"
	case *ReceiverV:
		return "Receiver"
	case DurationV:
		return "Duration"
	case InstantV:
		return "Instant"
	case *DistinctV:
		return x.Type
	case *RouterV:
		return "Router"
	case *RequestV:
		return "Request"
	case *ResponseV:
		return "Response"
	case *DbV:
		return "Db"
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
	case UintV:
		return fmt.Sprintf("%d", uint64(x))
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
	case *ScopeV:
		return "<scope>"
	case *TaskV:
		return "<task>"
	case *SenderV:
		return "<sender>"
	case *ReceiverV:
		return "<receiver>"
	case *DistinctV:
		return x.Type + "(" + render(x.V, true) + ")"
	case *RouterV:
		return "<router>"
	case *RequestV:
		return "<request>"
	case *ResponseV:
		return fmt.Sprintf("<response %d>", x.status)
	case *DbV:
		return "<db>"
	case DurationV:
		return time.Duration(x).String()
	case InstantV:
		return x.T.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return fmt.Sprintf("%v", v)
}

// eq is deep structural equality; functions and iterators are not
// comparable (runtime error at the call site's at).
func eq(a, b Value, at source.Span) bool {
	switch x := a.(type) {
	case IntV, UintV, FloatV, StrV, BoolV, RuneV, UnitV, NoneV, DurationV:
		return a == b
	case InstantV:
		y, ok := b.(InstantV)
		return ok && x.T.Equal(y.T) // Go's ==-on-time.Time trap, dodged
	case *DistinctV:
		y, ok := b.(*DistinctV)
		return ok && x.Type == y.Type && eq(x.V, y.V, at)
	case TupleV:
		y, ok := b.(TupleV)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !eq(x[i], y[i], at) {
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
			if !eq(x.Elems[i], y.Elems[i], at) {
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
			if !eq(x.Args[i], y.Args[i], at) {
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
			if !eq(v, y.Fields[f], at) {
				return false
			}
		}
		return true
	}
	panic(rtErr{at, fmt.Sprintf("%s values are not comparable with ==", typeName(a))})
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
	retErr     string // enclosing fn's Result error type; ? converts to it
}

// fnRetErr walks to the nearest function boundary and reports its
// declared Result error type ("" = none; closures never convert).
func (e *Env) fnRetErr() string {
	for env := e; env != nil; env = env.parent {
		if env.fnBoundary {
			return env.retErr
		}
	}
	return ""
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
func (e *Env) declare(name string, v Value, mut bool, at source.Span) {
	// The free builtins are reserved outright: the set is tiny and
	// fixed, and no program legitimately needs a local named println.
	// (Imports are deliberately not reserved — that conflict is the
	// checker era's two-live-meanings rule; see DESIGN.md Shadowing.)
	if _, isBuiltin := builtins[name]; isBuiltin {
		panic(rtErr{at, fmt.Sprintf("%q is a builtin and cannot be used as a binding name", name)})
	}
	if !e.fnBoundary {
		for p := e.parent; p != nil; p = p.parent {
			if _, ok := p.vars[name]; ok {
				panic(rtErr{at, fmt.Sprintf(
					"cannot shadow %q from an enclosing block (redeclaring in the same scope is fine; nested shadowing is not)", name)})
			}
			if p.fnBoundary {
				break
			}
		}
	}
	e.vars[name] = &binding{v: v, mut: mut}
}

func (e *Env) assign(name string, v Value, at source.Span) {
	b := e.lookup(name)
	if b == nil {
		panic(rtErr{at, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", name)})
	}
	if !b.mut {
		panic(rtErr{at, fmt.Sprintf("cannot assign to immutable binding %q (declare it with `let mut`)", name)})
	}
	b.v = v
}
