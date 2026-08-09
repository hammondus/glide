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
	IntV int64

	// UintV is a u64 value, and only a u64: it exists because u64 is
	// the one integer type whose range an int64 cannot hold. A u64
	// never mixes with an Int: DESIGN.md forbids implicit numeric
	// conversion, the checker enforces it, and binop has no case for
	// the pair.
	UintV uint64

	// SizedV is an i8/i16/i32/u8/u16/u32 value. Those six carry their
	// own width at runtime because generics are type-erased: inside
	// `fn double<T>(v: T) -> T { v + v }` the static type is gone by
	// the time `+` runs, so the value itself is the only thing that
	// can say "trap at 8 bits". Deriving the width from the checker's
	// annotation on the operator node instead would be silently wrong
	// for exactly that call.
	//
	// One carrier with a width field rather than six Go types: six
	// would mean six more cases in typeName, render, eq, hashable,
	// binop, naturalLess, json and sql — the same switch written six
	// times. Int and u64 keep their own unboxed types because Int is
	// the default and hot, and u64 is the one width an int64 cannot
	// hold; the narrow six are exactly the ones that need to remember
	// something.
	//
	// V is sign-extended when Signed and zero-extended otherwise, so
	// it is always the value's true mathematical magnitude and `%d`
	// prints it correctly either way. SizedV is comparable, so it
	// works as a map key and under `==` with no extra cases.
	SizedV struct {
		Bits   int // 8, 16 or 32
		Signed bool
		V      int64
	}

	FloatV float64
	StrV   string
	BoolV  bool
	RuneV  rune
	UnitV  struct{}
	TupleV []Value

	ListV struct{ Elems []Value }

	// MapV preserves insertion order (keys slice). That is a
	// *specified* language property as of M4c, not an implementation
	// convenience: the compiled tier has to reproduce it, and cannot
	// emit a bare Go map. Deleting a key drops it from the order;
	// re-inserting appends.
	MapV struct {
		keys []Value
		m    map[Value]Value
	}

	// Option is BOXED: every T? value is either NoneV or a *SomeV, and
	// never a bare T. Unboxed until M4c, which cost three silent wrong
	// answers — a present-but-None map entry read as absent, a sent
	// None closed a channel, and Option<Option<T>> was unwritable.
	//
	// The price is that the implicit T -> T? coercion no longer falls
	// out for free: the checker records each site in Info.Wrap and the
	// evaluator wraps there. Every consumer below asserts canonical
	// form rather than assuming it, so a coercion site the checker
	// failed to record fails loudly instead of silently deciding a
	// value was Some when it was None.
	NoneV struct{}
	SomeV struct{ V Value }

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
	// Error is BOXED, on the same argument that boxed Option: every
	// value in an `Error` slot is an *ErrV, never the raw thing it was
	// built from. Erased until now, which cost three things — the type
	// could carry no methods (`e.message()` dispatched on the dynamic
	// String and failed), a concrete variant could be matched straight
	// back out of an `Error`, and a program-made error printed
	// `Err("msg")` where a host error printed `Err(msg)`.
	//
	// Held is the concrete error this boxes — the String, the user
	// variant, whatever `Err(…)` was handed. It is what `find` walks
	// the chain looking for, and it is the reason boxing does not
	// *lose* the typed error it wraps: erasure moves from the
	// representation to the API, where an escape hatch can be offered
	// deliberately.
	ErrV struct {
		Msg   string
		Cause Value // *ErrV or nil
		Held  Value // the concrete error, or nil when there was none
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
	case IntV, UintV, SizedV, StrV, BoolV, RuneV:
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
	case SizedV:
		return x.name()
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
	case NoneV, *SomeV:
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
	case *OutputV:
		return "Output"
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
	case SizedV:
		// V is kept sign- or zero-extended, so it is already the
		// value's true magnitude and needs no per-width formatting.
		return fmt.Sprintf("%d", x.V)
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
	case *SomeV:
		// Rendered as Some(x), not as x. None already prints as None,
		// so printing Some(1) as 1 would make an Option print as two
		// different shapes; and it is the only way Some(None) is
		// distinguishable from None in output.
		return "Some(" + render(x.V, true) + ")"
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
	case *OutputV:
		return fmt.Sprintf("<output status %d>", x.status)
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
	case IntV, UintV, SizedV, FloatV, StrV, BoolV, RuneV, UnitV, NoneV, DurationV:
		return a == b
	case InstantV:
		y, ok := b.(InstantV)
		return ok && x.T.Equal(y.T) // Go's ==-on-time.Time trap, dodged
	case RangeV:
		return a == b
	case *MapV:
		// **Insertion order is not part of a Map's identity.** It is a
		// specified *iteration* property (DESIGN.md) and stays one:
		// `for (k, v) in m` is ordered, `==` is not. A map is a set of
		// pairs, and two maps built by different routes to the same
		// pairs are the same map — Python, whose dicts have been
		// ordered since 3.7, draws the line in exactly this place.
		y, ok := b.(*MapV)
		if !ok || len(x.keys) != len(y.keys) {
			return false
		}
		for _, k := range x.keys {
			// Keys are restricted to comparable scalars by `hashable`,
			// so a direct lookup is the right membership test; only
			// the values need structural comparison.
			other, present := y.m[k]
			if !present || !eq(x.m[k], other, at) {
				return false
			}
		}
		return true
	case *ResultV:
		y, ok := b.(*ResultV)
		return ok && x.Ok == y.Ok && eq(x.V, y.V, at)
	case *ErrV:
		// Errors compare by message *and* cause, the whole chain:
		// `context` builds one, and ignoring it would make two errors
		// with different provenance compare equal. The boxed value is
		// deliberately *not* compared — it is a view of the same
		// failure the message already renders, and two errors whose
		// messages and chains agree while their payloads do not is a
		// state the boxing cannot produce.
		y, ok := b.(*ErrV)
		if !ok || x.Msg != y.Msg || (x.Cause == nil) != (y.Cause == nil) {
			return false
		}
		return x.Cause == nil || eq(x.Cause, y.Cause, at)
	case *SomeV:
		// Boxing Option in M4c left this case out, and `Some(1) ==
		// Some(1)` panicked with "Option values are not comparable" —
		// a regression, since before boxing a Some *was* its payload
		// and compared structurally like everything else. Equality is
		// specified universal and structural, so it has to reach
		// inside the box. (`None == None` never broke: NoneV is a
		// comparable struct and fell through the scalar case.)
		y, ok := b.(*SomeV)
		return ok && eq(x.V, y.V, at)
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
