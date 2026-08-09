// Package types is Glide's semantic type universe: what a type *is*,
// as opposed to ast.TypeExpr, which is what a type was *written* as.
// `Int`, `i64` and the element type of a `List<Int>` are three
// different pieces of syntax and one type; this package is where they
// become one thing.
//
// Both tiers share it. The checker produces these, the tree-walker
// consumes them for `?`-conversion and shorthand resolution, and the
// eventual code generator consumes them for layout. Nothing here knows
// how a value is represented at runtime — that is deliberately the
// backend's business, so a second backend cannot be forced to copy the
// first one's choices.
//
// # The Unknown type
//
// Unknown means "the checker cannot tell", and it is compatible with
// everything in both directions. It is not an error type and reaching
// it never reports anything. This is what lets the checker be
// mandatory from its first commit: every construct it does not yet
// understand yields Unknown, so coverage grows monotonically and a
// half-finished checker can never reject a valid program. The rule the
// whole design leans on is: **report only when certain.** An
// under-approximation of errors is a feature that gets better; an
// over-approximation is a language that rejects working code.
package types

import (
	"fmt"
	"strings"

	"glide/internal/ast"
)

// Type is a Glide type. The set is closed — every implementation is in
// this file — because the checker switches on it exhaustively and a
// type declared elsewhere would be a silent hole.
type Type interface {
	String() string
	isType()
}

// Basic is a primitive: no structure, no parameters, one singleton per
// type so identity is pointer comparison.
type Basic struct {
	name  string
	flags basicFlag
	bits  int // integer/float width; 0 where meaningless
}

type basicFlag uint

const (
	isBool basicFlag = 1 << iota
	isInteger
	isUnsigned
	isFloat
	isRune
	isString
	isUnit
	isUntyped
	isUnknown
	isNever
)

// The primitives. `Int` is i64 on every target and `i64` is an
// accepted spelling of it, not a separate type — DESIGN.md's "Int is
// i64 on every target, not platform-sized" is a statement about
// identity, not just about width. Likewise `Float` is f64 and has no
// second spelling.
//
// i128/u128 are ratified in DESIGN.md but deliberately absent here;
// see DESIGN-DECISIONS.md for why they are deferred past M4 rather
// than emulated.
var (
	Unknown = &Basic{name: "?", flags: isUnknown}

	// Never is the type of an expression that does not produce a value
	// because control leaves: `os.exit(1)`, `return`, `panic`. It is
	// assignable to everything, which is what lets `let x = f() else {
	// os.exit(1) }` typecheck without a special case per diverging
	// construct. Programs cannot write it; DESIGN.md spells it `!` in
	// signatures and that is how it prints.
	Never = &Basic{name: "!", flags: isNever}

	Bool   = &Basic{name: "Bool", flags: isBool}
	Int    = &Basic{name: "Int", flags: isInteger, bits: 64}
	I8     = &Basic{name: "i8", flags: isInteger, bits: 8}
	I16    = &Basic{name: "i16", flags: isInteger, bits: 16}
	I32    = &Basic{name: "i32", flags: isInteger, bits: 32}
	U8     = &Basic{name: "u8", flags: isInteger | isUnsigned, bits: 8}
	U16    = &Basic{name: "u16", flags: isInteger | isUnsigned, bits: 16}
	U32    = &Basic{name: "u32", flags: isInteger | isUnsigned, bits: 32}
	U64    = &Basic{name: "u64", flags: isInteger | isUnsigned, bits: 64}
	Float  = &Basic{name: "Float", flags: isFloat, bits: 64}
	F32    = &Basic{name: "f32", flags: isFloat, bits: 32}
	Rune   = &Basic{name: "Rune", flags: isRune, bits: 32}
	String = &Basic{name: "String", flags: isString}
	Unit   = &Basic{name: "()", flags: isUnit}

	// Untyped constants: a literal is arbitrary-precision until it
	// lands in a type (DESIGN.md, Go's untyped constants / Zig's
	// comptime_int). These never appear in a declared signature; they
	// exist only between a literal and the type it flows into.
	UntypedInt   = &Basic{name: "untyped integer", flags: isInteger | isUntyped}
	UntypedFloat = &Basic{name: "untyped float", flags: isFloat | isUntyped}
)

// Primitives maps every accepted spelling of a primitive to its type.
// `i64` and `Int` are the same entry on purpose.
var Primitives = map[string]*Basic{
	"Bool": Bool,
	"Int":  Int, "i64": Int,
	"i8": I8, "i16": I16, "i32": I32,
	"u8": U8, "u16": U16, "u32": U32, "u64": U64,
	"Float": Float, "f32": F32,
	"Rune": Rune, "String": String,
}

func (b *Basic) String() string { return b.name }
func (b *Basic) isType()        {}

func (b *Basic) IsUnknown() bool  { return b.flags&isUnknown != 0 }
func (b *Basic) IsNever() bool    { return b.flags&isNever != 0 }
func (b *Basic) IsUntyped() bool  { return b.flags&isUntyped != 0 }
func (b *Basic) IsInteger() bool  { return b.flags&isInteger != 0 }
func (b *Basic) IsUnsigned() bool { return b.flags&isUnsigned != 0 }
func (b *Basic) IsFloat() bool    { return b.flags&isFloat != 0 }
func (b *Basic) IsRune() bool     { return b.flags&isRune != 0 }
func (b *Basic) IsNumeric() bool  { return b.flags&(isInteger|isFloat) != 0 }
func (b *Basic) IsOrdered() bool  { return b.flags&(isInteger|isFloat|isRune|isString) != 0 }

// Bits is the width of an integer or float type, 0 for anything else
// and for untyped constants (which have no width until they land).
func (b *Basic) Bits() int {
	if b.IsUntyped() {
		return 0
	}
	return b.bits
}

// Ctor names a built-in type constructor. These are the types the
// language provides that have structure or parameters; a user cannot
// declare one, and the spellings are reserved.
type Ctor int

// Set is deliberately absent: the runtime has no set type, and a
// checker that accepts `Set<Int>` for a program that cannot run it
// would be inventing language surface. Constructors land here when the
// runtime gains them, not before.
const (
	List     Ctor = iota // List<T>
	Map                  // Map<K, V>
	Option               // Option<T>, also written T?
	Result               // Result<T, E>
	Sender               // Sender<T>
	Receiver             // Receiver<T>
	Iterator             // Iterator<T>
	Task                 // Task<T>
	Range                // Range
	Duration             // Duration
	Instant              // Instant
	Scope                // Scope
	Error                // Error
	Router               // Router
	Request              // Request
	Response             // Response
	Db                   // Db
)

var ctors = [...]struct {
	name  string
	arity int
}{
	List:     {"List", 1},
	Map:      {"Map", 2},
	Option:   {"Option", 1},
	Result:   {"Result", 2},
	Sender:   {"Sender", 1},
	Receiver: {"Receiver", 1},
	Iterator: {"Iterator", 1},
	Task:     {"Task", 1},
	Range:    {"Range", 0},
	Duration: {"Duration", 0},
	Instant:  {"Instant", 0},
	Scope:    {"Scope", 0},
	Error:    {"Error", 0},
	Router:   {"Router", 0},
	Request:  {"Request", 0},
	Response: {"Response", 0},
	Db:       {"Db", 0},
}

// Builtins maps each reserved constructor spelling to its Ctor. The
// resolver consults this before the program's own type table, which is
// what makes the names reserved.
var Builtins = func() map[string]Ctor {
	m := make(map[string]Ctor, len(ctors))
	for c, info := range ctors {
		m[info.name] = Ctor(c)
	}
	return m
}()

func (c Ctor) String() string { return ctors[c].name }
func (c Ctor) Arity() int     { return ctors[c].arity }

// App is a built-in constructor applied to its arguments. A
// zero-arity constructor (Duration, Router) is an App with no args
// rather than a Basic, because it is nominal — nothing else is
// assignable to it.
type App struct {
	C    Ctor
	Args []Type
}

// Apply builds an App. Arity is the caller's responsibility; the
// resolver checks it against Ctor.Arity and reports a diagnostic,
// because a wrong arity is a program error, not a compiler bug.
func Apply(c Ctor, args ...Type) *App { return &App{C: c, Args: args} }

// Opt is the T? / Option<T> constructor, spelled out because the
// checker builds and unwraps Options constantly.
func Opt(t Type) *App { return &App{C: Option, Args: []Type{t}} }

func (a *App) isType() {}

func (a *App) String() string {
	// T? is the spelling programs write; Option<T> is the same type
	// and prints the short way so diagnostics match the source.
	if a.C == Option && len(a.Args) == 1 {
		// Two spellings print long-form: Option<Option<T>>, which `?`
		// cannot express unambiguously, and Option<?>, which would
		// otherwise render as the unreadable `??`.
		if inner, nested := a.Args[0].(*App); nested && inner.C == Option {
			return "Option<" + inner.String() + ">"
		}
		if IsUnknown(a.Args[0]) {
			return "Option<?>"
		}
		return a.Args[0].String() + "?"
	}
	if len(a.Args) == 0 {
		return a.C.String()
	}
	parts := make([]string, len(a.Args))
	for i, t := range a.Args {
		parts[i] = t.String()
	}
	return a.C.String() + "<" + strings.Join(parts, ", ") + ">"
}

// Elem is the single argument of a one-parameter constructor, or
// Unknown when the App is malformed (a resolver error already
// reported). Callers use it instead of indexing Args so a bad arity
// degrades to Unknown rather than panicking.
func (a *App) Elem() Type {
	if len(a.Args) == 0 {
		return Unknown
	}
	return a.Args[0]
}

// Arg is Args[i], or Unknown if absent — same rationale as Elem.
func (a *App) Arg(i int) Type {
	if i >= len(a.Args) {
		return Unknown
	}
	return a.Args[i]
}

// Field is a struct field or a named-field variant payload.
type Field struct {
	Name string
	Type Type
	Pub  bool
}

// Variant is one arm of a sum type. Args is the positional payload;
// Fields is the named-field form. Both empty is a bare variant.
type Variant struct {
	Name   string
	Owner  *Named
	Args   []Type
	Fields []Field
}

// Named is a user-declared type: struct, sum, or distinct. Exactly one
// of Fields / Variants / Base is populated.
//
// A generic type instantiated at different arguments produces distinct
// *Named values sharing one Decl. Identity is therefore structural
// (name plus arguments), never pointer equality — see Identical.
//
// Fields/Variants/Base are resolved once, on the uninstantiated form,
// with the declaration's own Vars still in place; Instantiate only
// swaps Args, and substitution happens when a field or variant is
// looked up. That is what makes a recursive generic (`type Stack<T> =
// Empty | Push(T, Stack<T>)`) terminate: nothing ever expands a type
// that nobody asked about.
type Named struct {
	Name   string
	Decl   *ast.TypeDecl
	Params []*Var // declared type parameters
	Args   []Type // instantiation; nil for the uninstantiated form

	Fields   []Field    // struct
	Variants []*Variant // sum
	Base     Type       // distinct
}

// Instantiate binds the declaration's type parameters. The result
// shares Fields/Variants with n and substitutes on lookup.
func (n *Named) Instantiate(args []Type) *Named {
	out := *n
	out.Args = args
	return &out
}

// binding maps this instance's type parameters to its arguments; nil
// when there is nothing to substitute.
func (n *Named) binding() map[string]Type {
	if len(n.Params) == 0 || len(n.Args) != len(n.Params) {
		return nil
	}
	m := make(map[string]Type, len(n.Params))
	for i, v := range n.Params {
		m[v.Name] = n.Args[i]
	}
	return m
}

func (n *Named) isType() {}

func (n *Named) String() string {
	if len(n.Args) == 0 {
		return n.Name
	}
	parts := make([]string, len(n.Args))
	for i, t := range n.Args {
		parts[i] = t.String()
	}
	return n.Name + "<" + strings.Join(parts, ", ") + ">"
}

// IsDistinct reports whether n is a nominal wrapper. Distinct types
// share no operators with their base and never convert implicitly —
// that is the whole point of declaring one.
func (n *Named) IsDistinct() bool { return n.Base != nil }

// Field looks up a struct field by name, with this instance's type
// arguments substituted in.
func (n *Named) Field(name string) (Field, bool) {
	for _, f := range n.Fields {
		if f.Name == name {
			f.Type = Subst(f.Type, n.binding())
			return f, true
		}
	}
	return Field{}, false
}

// Variant looks up a sum-type arm by name, substituted for this
// instance.
func (n *Named) Variant(name string) (*Variant, bool) {
	for _, v := range n.Variants {
		if v.Name != name {
			continue
		}
		m := n.binding()
		if m == nil {
			return v, true
		}
		out := *v
		out.Args = make([]Type, len(v.Args))
		for i, a := range v.Args {
			out.Args[i] = Subst(a, m)
		}
		out.Fields = make([]Field, len(v.Fields))
		for i, f := range v.Fields {
			f.Type = Subst(f.Type, m)
			out.Fields[i] = f
		}
		return &out, true
	}
	return nil, false
}

// Tuple is (A, B, ...) — always two or more elements; `()` is Unit and
// a one-element tuple does not exist (it is just a parenthesised
// expression).
type Tuple struct{ Elems []Type }

func (t *Tuple) isType() {}

func (t *Tuple) String() string {
	parts := make([]string, len(t.Elems))
	for i, e := range t.Elems {
		parts[i] = e.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// Param is one parameter of a function type. HasDefault matters to the
// checker because it decides whether an argument may be omitted; the
// default *expression* stays in the AST, since it is re-evaluated per
// call.
type Param struct {
	Name       string
	Type       Type
	HasDefault bool
}

// Func is a function's type. Self records whether it is a method and
// whether it needs a mutable receiver — the checker enforces the
// receiver-mut rule the evaluator currently enforces at runtime.
type Func struct {
	Name       string
	Self       ast.SelfMode
	TypeParams []*Var
	Params     []Param
	Ret        Type
	Variadic   bool          // builtins only: println and friends
	Decl       *ast.FuncDecl // nil for builtins
}

func (f *Func) isType() {}

func (f *Func) String() string {
	parts := make([]string, 0, len(f.Params))
	if f.Self != ast.NoSelf {
		if f.Self == ast.MutSelf {
			parts = append(parts, "mut self")
		} else {
			parts = append(parts, "self")
		}
	}
	for _, p := range f.Params {
		s := p.Type.String()
		if p.Name != "" {
			s = p.Name + ": " + s
		}
		if p.HasDefault {
			s += " = …"
		}
		parts = append(parts, s)
	}
	if f.Variadic {
		parts = append(parts, "…")
	}
	s := "fn(" + strings.Join(parts, ", ") + ")"
	if f.Ret != nil && f.Ret != Unit {
		s += " -> " + f.Ret.String()
	}
	return s
}

// Arity reports the minimum and maximum argument counts, excluding
// self. Max is -1 for a variadic builtin.
func (f *Func) Arity() (min, max int) {
	for _, p := range f.Params {
		if !p.HasDefault {
			min++
		}
	}
	if f.Variadic {
		return min, -1
	}
	return min, len(f.Params)
}

// Var is a type parameter — `T` inside the body of a generic
// declaration. The interpreter runs generics type-erased, so a Var
// never reaches the runtime; it exists so the checker can verify a
// generic body once against its bounds rather than per instantiation
// (declaration-site checking, DESIGN.md).
type Var struct {
	Name   string
	Bounds []string // trait names, as written
}

func (v *Var) isType()        {}
func (v *Var) String() string { return v.Name }

// Module is the type of an imported module handle: `fs` in `fs.read`.
// A module is not a value that can be stored or passed — the checker
// only ever sees this type on the left of a `.`.
type Module struct{ Name string }

func (m *Module) isType()        {}
func (m *Module) String() string { return "module " + m.Name }

// Meta is the type of a type used in value position: `Tree` in
// `Tree.new()`. It exists so `Tree.new` resolves against the type's
// associated functions rather than a struct field.
type Meta struct{ T Type }

func (m *Meta) isType()        {}
func (m *Meta) String() string { return fmt.Sprintf("type %s", m.T) }
