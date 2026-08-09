// Package ast defines the syntax tree for Glide.
//
// Nodes carry syntax and position, and nothing else: no types, no
// resolved names. What the checker learns attaches separately, in
// check.Info, keyed by node — go/types.Info's arrangement, and for
// its reasons (../../DESIGN-DECISIONS.md). Positions are the one
// exception, embedded directly, because a position is a property of
// the syntax rather than something derived from it.
package ast

import (
	"strings"

	"glide/internal/source"
)

// Import is `import name` — a span so an unknown module can be
// pointed at rather than merely named.
type Import struct {
	source.Span
	Name string
}

type File struct {
	// Source is what every node's Span indexes into: the checker and
	// the interpreter both need it to render a diagnostic, and hanging
	// it here means neither has to be handed it separately.
	Source  *source.File
	Imports []Import
	Funcs   []*FuncDecl
	Types   []*TypeDecl
	Impls   []*ImplBlock
	Traits  []*TraitDecl
	Consts  []*ConstDecl
	Tests   []*TestDecl
	Benches []*BenchDecl
}

type SelfMode int

const (
	NoSelf SelfMode = iota
	Self
	MutSelf
)

// TypeKind discriminates the three shapes a written type can take.
// It is an explicit tag rather than "exactly one field is set" because
// the checker switches on it constantly and a missed case should be a
// visible hole, not a silent zero value.
type TypeKind int

const (
	TypeName  TypeKind = iota // Int, List<Int>, Result<T, E>
	TypeTuple                 // (A, B)
	TypeUnit                  // ()
)

// TypeParam is one entry in a *declaration-site* `<...>` list:
// `<T>`, or `<T: Ord + Hash>`. Bounds are trait names exactly as
// written — resolving them is the checker's job.
//
// Declaration sites (fn, type, trait) bind parameters and may carry
// bounds. `impl` headers do not: `impl Iterable<T> for Tree<T>`
// mentions T twice and binds it once, so the parser records those as
// type *arguments* (TypeExpr) and leaves "which of these are binders"
// to the checker. That reading also survives a specialised impl like
// `impl Stack<Int>`, which the grammar does not currently forbid.
type TypeParam struct {
	Name   string
	Bounds []*TypeExpr // nil = unconstrained
	source.Span
}

// TypeExpr is a type as *written*, not a resolved type: `Foo` is a
// name the checker still has to look up, and two TypeExprs spelling
// the same type are distinct nodes. Resolution attaches separately,
// go/types.Info-style (see ../../DESIGN-DECISIONS.md).
//
// M1-M3 kept these as display strings. That made three things
// impossible: pointing a diagnostic *inside* a type, walking a type
// without re-parsing it, and telling `Result<A, B>` from a type whose
// name happens to contain a comma. String() reproduces the old display
// form exactly, so error text and `glide fmt` output are unchanged.
type TypeExpr struct {
	Kind     TypeKind
	Name     string      // TypeName only
	Args     []*TypeExpr // TypeName only: List<Int> -> [Int]
	Elems    []*TypeExpr // TypeTuple only, always >= 2
	Optional bool        // trailing `?`, any kind
	source.Span
}

func (t *TypeExpr) String() string {
	if t == nil {
		return ""
	}
	var s string
	switch t.Kind {
	case TypeUnit:
		s = "()"
	case TypeTuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = e.String()
		}
		s = "(" + strings.Join(parts, ", ") + ")"
	default:
		s = t.Name
		if len(t.Args) > 0 {
			parts := make([]string, len(t.Args))
			for i, a := range t.Args {
				parts[i] = a.String()
			}
			s += "<" + strings.Join(parts, ", ") + ">"
		}
	}
	if t.Optional {
		s += "?"
	}
	return s
}

type FuncDecl struct {
	Name       string
	TypeParams []TypeParam
	Self       SelfMode
	Params     []Param
	RetType    *TypeExpr // nil = declares no return value
	Body       *Block
	source.Span
}

type FieldDecl struct {
	source.Span
	Name string
	Type *TypeExpr
	Pub  bool
}

// VariantDecl is one arm of a sum type. Payload holds the positional
// form's types (`Green(Int, Int)`); Fields the named form
// (`NotFound{ id: Int }`); both empty is a bare variant. M1-M3 kept
// only a payload *count* here and discarded the types, which is why
// `Some(x)` could be handed anything at all.
type VariantDecl struct {
	source.Span
	Name    string
	Payload []*TypeExpr
	Fields  []FieldDecl
}

// TypeDecl: exactly one of Fields (struct) / Variants (sum) /
// Distinct (nominal wrapper base type) is set.
type TypeDecl struct {
	Name       string
	TypeParams []TypeParam
	Fields     []FieldDecl
	Variants   []VariantDecl
	Distinct   *TypeExpr
	source.Span
}

// ConstDecl is module-level `const name = expr`: evaluated once at
// load, in declaration order, restricted to pure expressions (M2
// shim for comptime — conservative so comptime can only loosen it).
type ConstDecl struct {
	Name string
	E    Expr
	source.Span
}

// TraitDecl: methods with a Body are defaults, inherited by any type
// that declares `impl Trait for Type` and doesn't override; a nil
// Body is a required signature (unverified until the checker).
type TraitDecl struct {
	Name       string
	TypeParams []TypeParam
	Fns        []*FuncDecl
	source.Span
}

// ImplBlock: `impl Tree<T>` or `impl Iterable<T> for Tree<T>`.
// TargetArgs/TraitArgs hold the `<...>` lists as type *arguments* —
// see TypeParam for why an impl header is not a declaration site.
type ImplBlock struct {
	Target     string // type the methods attach to
	TargetArgs []*TypeExpr
	TraitArgs  []*TypeExpr
	Trait      string // "" for inherent impls
	Fns        []*FuncDecl
	source.Span
}

type TestDecl struct {
	Name   string
	Params []Param
	Body   *Block
	source.Span
}

type BenchDecl struct {
	Name string
	Body *Block
	source.Span
}

// Param: Default (nil = required) is re-evaluated per call, in scope
// of the params to its left — never Python's shared-once value.
type Param struct {
	source.Span
	Name    string
	Type    *TypeExpr
	Default Expr
}

// Block: HasDefer/HasFns are set at parse when a DeferStmt/FnStmt
// sits directly in Stmts, so evalBlock's hot path skips the
// bookkeeping entirely.
type Block struct {
	source.Span
	Stmts    []Stmt
	HasDefer bool
	HasFns   bool
}

// Statements

type Stmt interface{ stmt() }

type LetStmt struct {
	Pat  Pattern
	Type *TypeExpr // nil = no annotation; inferred from Init
	Init Expr
	Else *Block // nil unless `let … else`
	source.Span
}

type AssignStmt struct {
	Target Expr   // IdentExpr or Index; IdentExpr "_" discards
	Op     string // "=", "+=", "-="
	Value  Expr
	source.Span
}

type ExprStmt struct {
	E Expr
	source.Span
}

type ReturnStmt struct {
	E Expr // nil = return unit
	source.Span
}

type ForStmt struct {
	Pat   Pattern // for-in only
	Iter  Expr    // for-in only
	Cond  Expr    // conditional loop only; all nil = loop forever
	Body  *Block
	Label string // `search: for … { break search }`; "" = unlabeled
	source.Span
}

// YieldStmt makes the enclosing function a generator. From delegates
// to a sub-iterator (`yield from`).
type YieldStmt struct {
	E    Expr
	From bool
	source.Span
}

type BreakStmt struct {
	Label string // "" = nearest loop
	source.Span
}

type ContinueStmt struct {
	Label string // "" = nearest loop
	source.Span
}

// FnStmt is a nested fn: Rust's rule — a plain function that does
// NOT capture enclosing locals (capture is what closures are for).
// Hoisted to block entry, so helpers read fine below their callers
// and siblings can be mutually recursive.
type FnStmt struct {
	source.Span
	Decl *FuncDecl
}

// DeferStmt: Err marks `errdefer` — runs only when the enclosing
// block exits on the error path.
type DeferStmt struct {
	Body *Block
	Err  bool
	source.Span
}

func (*LetStmt) stmt()      {}
func (*AssignStmt) stmt()   {}
func (*ExprStmt) stmt()     {}
func (*ReturnStmt) stmt()   {}
func (*ForStmt) stmt()      {}
func (*YieldStmt) stmt()    {}
func (*BreakStmt) stmt()    {}
func (*ContinueStmt) stmt() {}
func (*DeferStmt) stmt()    {}
func (*FnStmt) stmt()       {}

// Patterns

type Pattern interface{ pat() }

type IdentPat struct {
	source.Span
	Name string
	Mut  bool
}
type WildPat struct{ source.Span }
type TuplePat struct {
	source.Span
	Elems []Pattern
}

// ListPat: Elems holds the non-rest element patterns; Rest is the
// index in Elems where a `..rest` sits (-1 for exact-arity), RestName
// its binding name ("_" discards).
type ListPat struct {
	source.Span
	Elems    []Pattern
	Rest     int
	RestName string
}

// CtorPat matches a constructor: None, Some(x), or a sum-type
// variant. In patterns, case is load-bearing: capitalised names are
// constructors, lowercase names bind.
type CtorPat struct {
	source.Span
	Name string
	Args []Pattern
}

// StructPat matches a struct by type and fields. Shorthand `name`
// binds the field; `field: pat` nests. Rest (`..`) is required for a
// partial match (Rust's rule: silent omission would mean new fields
// change nothing at match sites) — without it every field must be
// mentioned, enforced when the pattern is applied.
type FieldPat struct {
	source.Span
	Name string
	Pat  Pattern
}
type StructPat struct {
	Type   string
	Fields []FieldPat
	Rest   bool
	source.Span
}

// Literal patterns match by equality; range patterns by half-open
// containment — the same meaning `..` has everywhere else.
type IntPat struct {
	source.Span
	V int64
}
type StrPat struct {
	source.Span
	V string
}
type BoolPat struct {
	source.Span
	V bool
}
type RangePat struct {
	source.Span
	Lo, Hi int64
	Incl   bool // `..=`
}
type RunePat struct {
	source.Span
	V rune
}
type RuneRangePat struct {
	source.Span
	Lo, Hi rune
	Incl   bool // `..=`
}

func (*IdentPat) pat()     {}
func (*WildPat) pat()      {}
func (*TuplePat) pat()     {}
func (*ListPat) pat()      {}
func (*CtorPat) pat()      {}
func (*StructPat) pat()    {}
func (*IntPat) pat()       {}
func (*StrPat) pat()       {}
func (*BoolPat) pat()      {}
func (*RangePat) pat()     {}
func (*RunePat) pat()      {}
func (*RuneRangePat) pat() {}

// Expressions

type Expr interface{ expr() }

type IntLit struct {
	source.Span
	V int64
}
type FloatLit struct {
	source.Span
	V float64
}
type BoolLit struct {
	source.Span
	V bool
}
type RuneLit struct {
	source.Span
	V rune
}
type UnitLit struct{ source.Span }

type StrPart struct {
	source.Span
	IsExpr bool
	Lit    string
	E      Expr
	Spec   string
}
type StrLit struct {
	Parts []StrPart
	source.Span
}

// BlockExpr is a bare { … } in expression or statement position: a
// scope whose tail expression is its value.
type BlockExpr struct {
	Body *Block
	source.Span
}

// ScopeExpr is `scope [(config)] [handle] { body }` — a structured-
// concurrency scope. Handle is "" when the body doesn't spawn;
// Timeout/Deadline are nil when absent (a timeout scope evaluates to
// Result<T, Timeout>).
type ScopeExpr struct {
	Handle   string
	Timeout  Expr
	Deadline Expr
	Body     *Block
	source.Span
}

// SelectExpr waits on multiple channel operations; arms wear match's
// syntax. Exactly one of the arm shapes holds per arm:
// recv (Pat + Ch), send (Ch + SendVal), or else (Else).
type SelectExpr struct {
	Arms []SelectArm
	source.Span
}
type SelectArm struct {
	Pat     Pattern // recv arms: pattern over the Option<T>
	Ch      Expr    // the channel-half expression (nil for else)
	SendVal Expr    // send arms: the value expression
	Else    bool
	Guard   Expr // nil = always enabled; evaluated once at entry
	Body    Expr
	source.Span
}

type IdentExpr struct {
	Name string
	source.Span
}

type TupleLit struct {
	source.Span
	Elems []Expr
}
type ListLit struct {
	source.Span
	Elems []Expr
}
type MapLit struct {
	source.Span
	Keys, Vals []Expr
}

// Spread is `..xs` inside a list literal — the only place the parser
// creates one. The spread value may be any iterable.
type Spread struct {
	E Expr
	source.Span
}

// DotName is `.Variant` — Swift's dot shorthand. M2 resolves it in
// the global variant namespace (variant names are file-unique); the
// checker era resolves it in the expected type instead.
type DotName struct {
	Name string
	source.Span
}

type Binary struct {
	Op   string
	L, R Expr
	source.Span
}
type Unary struct {
	Op string
	X  Expr
	source.Span
}
type RangeExpr struct {
	Lo, Hi Expr
	Incl   bool // `..=` — hi included
	source.Span
}

// Call: Names parallels Args ("" = positional; nil = all positional).
// The parser guarantees positionals precede named arguments.
type Call struct {
	Fn    Expr
	Args  []Expr
	Names []string
	source.Span
}
type Index struct {
	X, I Expr
	source.Span
}
type Field struct {
	X    Expr
	Name string
	source.Span
}
type TupleIndex struct {
	X Expr
	N int
	source.Span
}
type Try struct {
	X Expr
	source.Span
}

type Closure struct {
	source.Span
	Params    []string
	BodyExpr  Expr // one of BodyExpr / BodyBlock is set
	BodyBlock *Block
}

type If struct {
	Cond      Expr
	Then      *Block
	ElseIf    Expr // *If or *IfLet — either form may chain
	ElseBlock *Block
	source.Span
}

// IfLet unwraps an Option: pattern binds the inner value when the
// scrutinee is not None.
type IfLet struct {
	Pat       Pattern
	X         Expr
	Then      *Block
	ElseIf    Expr // *If or *IfLet — either form may chain
	ElseBlock *Block
	source.Span
}

// StructLit: Type{ name: expr, ..Base }. Base (may be nil) supplies
// the unmentioned fields — copy-with-changes.
type StructLit struct {
	Type  string
	Names []string
	Vals  []Expr
	Base  Expr
	source.Span
}

// MatchArm: Pats are comma alternatives (`1, 2 =>`, Go-style); with
// more than one, none may bind a name (enforced at parse).
type MatchArm struct {
	Pats  []Pattern
	Guard Expr // nil = unguarded
	Body  Expr
	source.Span
}

type Match struct {
	X    Expr
	Arms []MatchArm
	source.Span
}

// CondMatch is subjectless `match { cond => … }`: Go's expressionless
// switch. First true arm wins; a `_` arm (nil Cond) is always true.
type CondArm struct {
	Cond Expr // nil = the `_` arm
	Body Expr
	source.Span
}
type CondMatch struct {
	Arms []CondArm
	source.Span
}

func (*IntLit) expr()     {}
func (*RuneLit) expr()    {}
func (*FloatLit) expr()   {}
func (*BoolLit) expr()    {}
func (*UnitLit) expr()    {}
func (*StrLit) expr()     {}
func (*BlockExpr) expr()  {}
func (*ScopeExpr) expr()  {}
func (*SelectExpr) expr() {}
func (*IdentExpr) expr()  {}
func (*TupleLit) expr()   {}
func (*ListLit) expr()    {}
func (*Spread) expr()     {}
func (*DotName) expr()    {}
func (*MapLit) expr()     {}
func (*Binary) expr()     {}
func (*Unary) expr()      {}
func (*RangeExpr) expr()  {}
func (*Call) expr()       {}
func (*Index) expr()      {}
func (*Field) expr()      {}
func (*TupleIndex) expr() {}
func (*Try) expr()        {}
func (*Closure) expr()    {}
func (*If) expr()         {}
func (*IfLet) expr()      {}
func (*StructLit) expr()  {}
func (*Match) expr()      {}
func (*CondMatch) expr()  {}
