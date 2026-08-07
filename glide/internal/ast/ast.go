// Package ast defines the syntax tree for the wordfreq subset of
// Glide. Type annotations are carried as raw text: the interpreter is
// dynamically checked and the real checker arrives with the
// Glide-written frontend (bootstrap step 2).
package ast

type File struct {
	Imports []string
	Funcs   []*FuncDecl
}

type FuncDecl struct {
	Name    string
	Params  []Param
	RetType string // "" = declares no return value
	Body    *Block
	Line    int
}

type Param struct{ Name, Type string }

type Block struct{ Stmts []Stmt }

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

func (*LetStmt) stmt()    {}
func (*AssignStmt) stmt() {}
func (*ExprStmt) stmt()   {}
func (*ReturnStmt) stmt() {}
func (*ForStmt) stmt()    {}

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

func (*IdentPat) pat() {}
func (*WildPat) pat()  {}
func (*TuplePat) pat() {}
func (*ListPat) pat()  {}

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
type StrLit struct{ Parts []StrPart }

type IdentExpr struct {
	Name string
	Line int
}

type TupleLit struct{ Elems []Expr }
type ListLit struct{ Elems []Expr }
type MapLit struct{ Keys, Vals []Expr }

type Binary struct {
	Op   string
	L, R Expr
	Line int
}
type Unary struct {
	Op string
	X  Expr
}
type RangeExpr struct{ Lo, Hi Expr }

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
	X Expr
	N int
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
	ElseIf    *If
	ElseBlock *Block
	Line      int
}

func (*IntLit) expr()     {}
func (*FloatLit) expr()   {}
func (*BoolLit) expr()    {}
func (*UnitLit) expr()    {}
func (*StrLit) expr()     {}
func (*IdentExpr) expr()  {}
func (*TupleLit) expr()   {}
func (*ListLit) expr()    {}
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
