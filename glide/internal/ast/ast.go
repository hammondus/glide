// Package ast defines the syntax tree for the wordfreq subset of
// Glide. Type annotations are carried as raw text: the interpreter is
// dynamically checked and the real checker arrives with the
// Glide-written frontend (bootstrap step 2).
package ast

type File struct {
	Imports []string
	Funcs   []*FuncDecl
	Types   []*TypeDecl
	Impls   []*ImplBlock
	Tests   []*TestDecl
	Benches []*BenchDecl
}

type SelfMode int

const (
	NoSelf SelfMode = iota
	Self
	MutSelf
)

type FuncDecl struct {
	Name    string
	Self    SelfMode
	Params  []Param
	RetType string // "" = declares no return value
	Body    *Block
	Line    int
}

type FieldDecl struct {
	Name string
	Type string
	Pub  bool
}

type VariantDecl struct {
	Name  string
	Arity int // positional payload count; 0 = bare variant
}

// TypeDecl: exactly one of Fields (struct) / Variants (sum) is set.
type TypeDecl struct {
	Name     string
	Fields   []FieldDecl
	Variants []VariantDecl
	Line     int
}

type ImplBlock struct {
	Target string // type the methods attach to
	Trait  string // "" for inherent impls
	Fns    []*FuncDecl
	Line   int
}

type TestDecl struct {
	Name   string
	Params []Param
	Body   *Block
	Line   int
}

type BenchDecl struct {
	Name string
	Body *Block
	Line int
}

type Param struct{ Name, Type string }

// Block: HasDefer is set at parse when a DeferStmt sits directly in
// Stmts, so evalBlock's hot path skips defer bookkeeping entirely.
type Block struct {
	Stmts    []Stmt
	HasDefer bool
}

// Statements

type Stmt interface{ stmt() }

type LetStmt struct {
	Pat  Pattern
	Type string
	Init Expr
	Else *Block // nil unless `let … else`
	Line int
}

type AssignStmt struct {
	Target Expr   // IdentExpr or Index; IdentExpr "_" discards
	Op     string // "=", "+=", "-="
	Value  Expr
	Line   int
}

type ExprStmt struct {
	E    Expr
	Line int
}

type ReturnStmt struct {
	E    Expr // nil = return unit
	Line int
}

type ForStmt struct {
	Pat  Pattern // for-in only
	Iter Expr    // for-in only
	Cond Expr    // conditional loop only; all nil = loop forever
	Body *Block
	Line int
}

// YieldStmt makes the enclosing function a generator. From delegates
// to a sub-iterator (`yield from`).
type YieldStmt struct {
	E    Expr
	From bool
	Line int
}

type BreakStmt struct{ Line int }

type ContinueStmt struct{ Line int }

// DeferStmt: Err marks `errdefer` — runs only when the enclosing
// block exits on the error path.
type DeferStmt struct {
	Body *Block
	Err  bool
	Line int
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

// Patterns

type Pattern interface{ pat() }

type IdentPat struct {
	Name string
	Mut  bool
}
type WildPat struct{}
type TuplePat struct{ Elems []Pattern }

// ListPat: Elems holds the non-rest element patterns; Rest is the
// index in Elems where a `..rest` sits (-1 for exact-arity), RestName
// its binding name ("_" discards).
type ListPat struct {
	Elems    []Pattern
	Rest     int
	RestName string
}

// CtorPat matches a constructor: None, Some(x), or a sum-type
// variant. In patterns, case is load-bearing: capitalised names are
// constructors, lowercase names bind.
type CtorPat struct {
	Name string
	Args []Pattern
}

// StructPat matches a struct by type and fields. Shorthand `name`
// binds the field; `field: pat` nests. Rest (`..`) is required for a
// partial match (Rust's rule: silent omission would mean new fields
// change nothing at match sites) — without it every field must be
// mentioned, enforced when the pattern is applied.
type FieldPat struct {
	Name string
	Pat  Pattern
}
type StructPat struct {
	Type   string
	Fields []FieldPat
	Rest   bool
	Line   int
}

// Literal patterns match by equality; RangePat by half-open
// containment — the same meaning `..` has everywhere else.
type IntPat struct{ V int64 }
type StrPat struct{ V string }
type BoolPat struct{ V bool }
type RangePat struct{ Lo, Hi int64 }

func (*IdentPat) pat() {}
func (*WildPat) pat()  {}
func (*TuplePat) pat() {}
func (*ListPat) pat()  {}
func (*CtorPat) pat()  {}
func (*StructPat) pat() {}
func (*IntPat) pat()    {}
func (*StrPat) pat()   {}
func (*BoolPat) pat()  {}
func (*RangePat) pat() {}

// Expressions

type Expr interface{ expr() }

type IntLit struct{ V int64 }
type FloatLit struct{ V float64 }
type BoolLit struct{ V bool }
type UnitLit struct{}

type StrPart struct {
	IsExpr bool
	Lit    string
	E      Expr
	Spec   string
}
type StrLit struct {
	Parts []StrPart
	Line  int
}

// BlockExpr is a bare { … } in expression or statement position: a
// scope whose tail expression is its value.
type BlockExpr struct {
	Body *Block
	Line int
}

type IdentExpr struct {
	Name string
	Line int
}

type TupleLit struct{ Elems []Expr }
type ListLit struct{ Elems []Expr }
type MapLit struct{ Keys, Vals []Expr }

// Spread is `..xs` inside a list literal — the only place the parser
// creates one. The spread value may be any iterable.
type Spread struct {
	E    Expr
	Line int
}

type Binary struct {
	Op   string
	L, R Expr
	Line int
}
type Unary struct {
	Op   string
	X    Expr
	Line int
}
type RangeExpr struct {
	Lo, Hi Expr
	Line   int
}

type Call struct {
	Fn   Expr
	Args []Expr
	Line int
}
type Index struct {
	X, I Expr
	Line int
}
type Field struct {
	X    Expr
	Name string
	Line int
}
type TupleIndex struct {
	X    Expr
	N    int
	Line int
}
type Try struct {
	X    Expr
	Line int
}

type Closure struct {
	Params    []string
	BodyExpr  Expr   // one of BodyExpr / BodyBlock is set
	BodyBlock *Block
}

type If struct {
	Cond      Expr
	Then      *Block
	ElseIf    Expr // *If or *IfLet — either form may chain
	ElseBlock *Block
	Line      int
}

// IfLet unwraps an Option: pattern binds the inner value when the
// scrutinee is not None.
type IfLet struct {
	Pat       Pattern
	X         Expr
	Then      *Block
	ElseIf    Expr // *If or *IfLet — either form may chain
	ElseBlock *Block
	Line      int
}

// StructLit: Type{ name: expr, ..Base }. Base (may be nil) supplies
// the unmentioned fields — copy-with-changes.
type StructLit struct {
	Type  string
	Names []string
	Vals  []Expr
	Base  Expr
	Line  int
}

// MatchArm: Pats are comma alternatives (`1, 2 =>`, Go-style); with
// more than one, none may bind a name (enforced at parse).
type MatchArm struct {
	Pats  []Pattern
	Guard Expr // nil = unguarded
	Body  Expr
	Line  int
}

type Match struct {
	X    Expr
	Arms []MatchArm
	Line int
}

// CondMatch is subjectless `match { cond => … }`: Go's expressionless
// switch. First true arm wins; a `_` arm (nil Cond) is always true.
type CondArm struct {
	Cond Expr // nil = the `_` arm
	Body Expr
	Line int
}
type CondMatch struct {
	Arms []CondArm
	Line int
}

func (*IntLit) expr()     {}
func (*FloatLit) expr()   {}
func (*BoolLit) expr()    {}
func (*UnitLit) expr()    {}
func (*StrLit) expr()     {}
func (*BlockExpr) expr()  {}
func (*IdentExpr) expr()  {}
func (*TupleLit) expr()   {}
func (*ListLit) expr()    {}
func (*Spread) expr()     {}
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
