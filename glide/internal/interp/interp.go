// Package interp is a tree-walking evaluator for the wordfreq subset
// of Glide. It is dynamically checked: type annotations are parsed
// and ignored, and rules the compiler will enforce statically (mut,
// shadowing, let-else divergence, the tail-value rule) are enforced
// at runtime here. Errors use Go panics internally, recovered at Run;
// Glide-level control flow (return, `?`) uses explicit signals.
package interp

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"glide/internal/ast"
)

type Interp struct {
	Stdout io.Writer
	Stderr io.Writer
	Args   []string // what os.args() returns

	fns      map[string]*ast.FuncDecl
	imports  map[string]bool
	types    map[string]*ast.TypeDecl
	methods  map[string]map[string]*ast.FuncDecl // type -> method -> decl
	variants map[string]variantInfo              // variant name -> owning type
	genCache map[*ast.FuncDecl]bool
	global   *Env
	exiting  bool // os.exit in flight: skip defers (Go's rule)

	traits     map[string]*ast.TraitDecl
	typeTraits map[string][]string // type -> traits it declares impls for
	constEval  bool                // inside a const initializer: pure exprs only
}

type variantInfo struct {
	typeName string
	arity    int
	fields   []string // named-field form; nil for positional/bare
}

// testFail aborts a test case; the runner reports it.
type testFail struct {
	line int
	msg  string
}

// sig carries Glide control flow up the evaluator: an early return —
// explicit (`return`) or from `?` propagating an Err — or a loop
// break/continue on its way to the nearest enclosing `for`. The zero
// kind is return, so `&sig{val: v}` reads as a return signal. The
// parser guarantees break/continue signals never escape a function:
// they only arise inside a loop body, and evalFor consumes them.
type sig struct {
	kind  sigKind
	val   Value
	label string // break/continue target; "" = nearest loop
}

type sigKind int

const (
	sigReturn sigKind = iota
	sigBreak
	sigContinue
)

// rtErr is a runtime error; exitPanic is os.exit in flight.
type rtErr struct {
	line int
	msg  string
}
type exitPanic struct{ code int }

// ExitError reports an os.exit so the CLI (not the interpreter)
// decides to terminate the process — tests intercept it instead.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

var knownModules = map[string]bool{"fs": true, "os": true}

func New() *Interp {
	return &Interp{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		fns:      map[string]*ast.FuncDecl{},
		imports:  map[string]bool{},
		types:    map[string]*ast.TypeDecl{},
		methods:  map[string]map[string]*ast.FuncDecl{},
		variants: map[string]variantInfo{},
		genCache: map[*ast.FuncDecl]bool{},

		traits:     map[string]*ast.TraitDecl{},
		typeTraits: map[string][]string{},
	}
}

// load registers a file's declarations. Shared by Run and RunTests.
func (in *Interp) load(f *ast.File) error {
	for _, im := range f.Imports {
		if !knownModules[im] {
			return fmt.Errorf("unknown module %q (this interpreter ships fs and os)", im)
		}
		in.imports[im] = true
	}
	for _, fn := range f.Funcs {
		if _, dup := in.fns[fn.Name]; dup {
			return fmt.Errorf("line %d: function %q declared twice", fn.Line, fn.Name)
		}
		if _, isBuiltin := builtins[fn.Name]; isBuiltin {
			return fmt.Errorf("line %d: %q is a builtin and cannot be redeclared", fn.Line, fn.Name)
		}
		in.fns[fn.Name] = fn
	}
	for _, td := range f.Types {
		if _, dup := in.types[td.Name]; dup {
			return fmt.Errorf("line %d: type %q declared twice", td.Line, td.Name)
		}
		in.types[td.Name] = td
		for _, v := range td.Variants {
			if prev, dup := in.variants[v.Name]; dup {
				return fmt.Errorf("line %d: variant %q already declared by type %s", td.Line, v.Name, prev.typeName)
			}
			vi := variantInfo{typeName: td.Name, arity: v.Arity}
			for _, fd := range v.Fields {
				vi.fields = append(vi.fields, fd.Name)
			}
			in.variants[v.Name] = vi
		}
	}
	for _, tr := range f.Traits {
		if _, dup := in.traits[tr.Name]; dup {
			return fmt.Errorf("line %d: trait %q declared twice", tr.Line, tr.Name)
		}
		in.traits[tr.Name] = tr
	}
	for _, im := range f.Impls {
		if _, known := in.types[im.Target]; !known {
			return fmt.Errorf("line %d: impl for unknown type %q", im.Line, im.Target)
		}
		if im.Trait != "" {
			in.typeTraits[im.Target] = append(in.typeTraits[im.Target], im.Trait)
		}
		ms := in.methods[im.Target]
		if ms == nil {
			ms = map[string]*ast.FuncDecl{}
			in.methods[im.Target] = ms
		}
		for _, fn := range im.Fns {
			if _, dup := ms[fn.Name]; dup {
				return fmt.Errorf("line %d: method %s.%s declared twice", fn.Line, im.Target, fn.Name)
			}
			ms[fn.Name] = fn
		}
	}
	in.global = newEnv(nil, true)
	// Consts: the only module-level state — evaluated once, in
	// declaration order, restricted to pure expressions (M2's
	// conservative comptime shim: it can loosen later, not tighten).
	for _, c := range f.Consts {
		if _, dup := in.global.vars[c.Name]; dup {
			return fmt.Errorf("line %d: const %q declared twice", c.Line, c.Name)
		}
		in.constEval = true
		v, sg := in.eval(c.E, in.global)
		in.constEval = false
		if sg != nil {
			return fmt.Errorf("line %d: a const initializer cannot return or propagate", c.Line)
		}
		in.global.declare(c.Name, v, false, c.Line)
	}
	return nil
}

func (in *Interp) Run(f *ast.File) (err error) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case rtErr:
			err = fmt.Errorf("line %d: %s", p.line, p.msg)
		case testFail:
			err = fmt.Errorf("line %d: %s", p.line, p.msg)
		case exitPanic:
			err = &ExitError{Code: p.code}
		default:
			panic(p)
		}
	}()
	if err := in.load(f); err != nil {
		return err
	}
	mainFn, ok := in.fns["main"]
	if !ok {
		return fmt.Errorf("no main function")
	}
	if len(mainFn.Params) != 0 {
		return fmt.Errorf("line %d: main takes no parameters (use os.args())", mainFn.Line)
	}
	ret := in.callFunc(mainFn, nil)
	if r, isRes := ret.(*ResultV); isRes && !r.Ok {
		return fmt.Errorf("%s", display(r.V))
	}
	return nil
}

// Calls

func (in *Interp) callFunc(decl *ast.FuncDecl, args []Value) Value {
	return in.callFuncSelf(decl, nil, args)
}

// callFuncSelf runs a function or method; self is non-nil for
// methods (already declared mut/immutable by the caller's check).
func (in *Interp) callFuncSelf(decl *ast.FuncDecl, self Value, args []Value) Value {
	return in.callFuncNamed(decl, self, args, nil, decl.Line)
}

// callFuncNamed binds a direct call site: positional prefix, then
// named arguments, then defaults for whatever is unfilled. Defaults
// evaluate per call, left to right, with earlier params in scope.
func (in *Interp) callFuncNamed(decl *ast.FuncDecl, self Value, args []Value, names []string, line int) Value {
	return in.callFuncNamedIn(in.global, decl, self, args, names, line)
}

// callFuncNamedIn: base is the env the body resolves against — the
// global env, or a nested fn's private items env (never enclosing
// locals: nested fns do not capture).
func (in *Interp) callFuncNamedIn(base *Env, decl *ast.FuncDecl, self Value, args []Value, names []string, line int) Value {
	if in.constEval {
		panic(rtErr{line, fmt.Sprintf("a const initializer cannot call %s — pure expressions only (comptime fn evaluation arrives later)", decl.Name)})
	}
	n := len(decl.Params)
	if len(args) > n {
		panic(rtErr{line, fmt.Sprintf("%s takes %d argument(s), got %d",
			decl.Name, n, len(args))})
	}
	slots := make([]Value, n)
	filled := make([]bool, n)
	for i, a := range args {
		if names == nil || names[i] == "" {
			slots[i], filled[i] = a, true
			continue
		}
		idx := -1
		for j, prm := range decl.Params {
			if prm.Name == names[i] {
				idx = j
				break
			}
		}
		if idx < 0 {
			panic(rtErr{line, fmt.Sprintf("%s has no parameter %q", decl.Name, names[i])})
		}
		if filled[idx] {
			panic(rtErr{line, fmt.Sprintf("%s: parameter %q given twice (positionally and by name)", decl.Name, names[i])})
		}
		slots[idx], filled[idx] = a, true
	}
	env := newEnv(base, true)
	env.retErr = resultErrType(decl.RetType)
	if self != nil {
		env.declare("self", self, decl.Self == ast.MutSelf, decl.Line)
	}
	for i, prm := range decl.Params {
		v := slots[i]
		if !filled[i] {
			if prm.Default == nil {
				panic(rtErr{line, fmt.Sprintf("%s is missing its %q argument", decl.Name, prm.Name)})
			}
			dv, sg := in.eval(prm.Default, env)
			if sg != nil {
				panic(rtErr{line, fmt.Sprintf("the default for %q cannot return or propagate an error", prm.Name)})
			}
			v = dv
		}
		env.declare(prm.Name, v, false, decl.Line)
	}
	if in.isGenerator(decl) {
		return in.runGenerator(decl.Body, env, decl.Line)
	}
	v, sg := in.evalBlock(decl.Body, env)
	if sg != nil {
		return sg.val
	}
	// The tail-value rule (DESIGN.md, Syntax): no declared return
	// type means a meaningful tail value is an error, not a silent
	// discard.
	if decl.RetType == "" {
		if _, isUnit := v.(UnitV); !isUnit {
			panic(rtErr{decl.Line, fmt.Sprintf(
				"%s declares no return value but its body ends with a %s; discard it with `_ = …` or declare `-> %s`",
				decl.Name, typeName(v), typeName(v))})
		}
	}
	return v
}

func (in *Interp) callValue(fnv Value, args []Value, line int) Value {
	if in.constEval {
		if b, isB := fnv.(*BuiltinV); !isB || (b.Name != "Ok" && b.Name != "Err" && b.Name != "Some") {
			panic(rtErr{line, "a const initializer may only call pure constructors (Ok/Err/Some) and value methods — comptime fn evaluation arrives later"})
		}
	}
	switch f := fnv.(type) {
	case *FuncV:
		base := in.global
		if f.Items != nil {
			base = f.Items
		}
		return in.callFuncNamedIn(base, f.Decl, nil, args, nil, f.Decl.Line)
	case *BuiltinV:
		return f.Fn(in, args, line)
	case *ClosureV:
		if len(args) != len(f.Params) {
			panic(rtErr{line, fmt.Sprintf("closure takes %d argument(s), got %d", len(f.Params), len(args))})
		}
		env := newEnv(f.Env, true)
		for i, p := range f.Params {
			env.declare(p, args[i], false, line)
		}
		if f.BodyExpr != nil {
			v, sg := in.eval(f.BodyExpr, env)
			if sg != nil {
				return sg.val
			}
			return v
		}
		v, sg := in.evalBlock(f.BodyBlock, env)
		if sg != nil {
			return sg.val
		}
		return v
	}
	panic(rtErr{line, fmt.Sprintf("%s is not callable", typeName(fnv))})
}

// findMethod resolves a self-method on a type: the type's own
// (inherent or trait-impl) methods win; otherwise a default from a
// trait the type declares. Two traits both providing an unoverridden
// default is ambiguous — an error naming both.
func (in *Interp) findMethod(typeName, method string, line int) *ast.FuncDecl {
	if m := in.methods[typeName][method]; m != nil {
		return m
	}
	var found *ast.FuncDecl
	var from string
	for _, trName := range in.typeTraits[typeName] {
		tr := in.traits[trName]
		if tr == nil {
			continue // conformance asserted to an undeclared trait
		}
		for _, fn := range tr.Fns {
			if fn.Name == method && fn.Body != nil {
				if found != nil {
					panic(rtErr{line, fmt.Sprintf("%s.%s is ambiguous: both %s and %s provide a default (override it on %s)",
						typeName, method, from, trName, typeName)})
				}
				found, from = fn, trName
			}
		}
	}
	return found
}

// Statements

func (in *Interp) evalBlock(b *ast.Block, env *Env) (Value, *sig) {
	if b.HasFns {
		in.hoistFns(b, env)
	}
	if b.HasDefer {
		return in.evalBlockDeferred(b, env)
	}
	var last Value = UnitV{}
	for _, s := range b.Stmts {
		var sg *sig
		last, sg = in.evalStmt(s, env)
		if sg != nil {
			return UnitV{}, sg
		}
	}
	return last, nil
}

// evalBlockDeferred is evalBlock for blocks that register defers:
// they run LIFO at block exit — normal, signal (return/break/
// continue), or panic unwind. errdefer bodies run only on the error
// path: a return signal carrying an Err, or a panic. os.exit skips
// defers entirely (Go's rule).
func (in *Interp) evalBlockDeferred(b *ast.Block, env *Env) (val Value, sg *sig) {
	var defers []*ast.DeferStmt
	unwound := false
	defer func() {
		if unwound || in.exiting {
			return
		}
		// Still panicking: release-as-you-unwind, and a crash counts
		// as the error path (rollback must happen).
		in.runDefers(defers, env, true)
	}()
	var last Value = UnitV{}
	for _, s := range b.Stmts {
		if d, ok := s.(*ast.DeferStmt); ok {
			defers = append(defers, d)
			last = UnitV{}
			continue
		}
		var sg2 *sig
		last, sg2 = in.evalStmt(s, env)
		if sg2 != nil {
			unwound = true
			in.runDefers(defers, env, isErrSig(sg2))
			return UnitV{}, sg2
		}
	}
	unwound = true
	in.runDefers(defers, env, false)
	return last, nil
}

// resultErrType extracts E from a declared "Result<T, E>" return
// type; "" for any other shape (no conversion target).
func resultErrType(ret string) string {
	if !strings.HasPrefix(ret, "Result<") || !strings.HasSuffix(ret, ">") {
		return ""
	}
	inner := ret[len("Result<") : len(ret)-1]
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inner[i+1:])
			}
		}
	}
	return ""
}

// loopSig decides what a loop does with a signal from its body.
// done=true means the loop acts: out=nil consumes a break aimed here
// (or unlabeled); out=sg propagates a return or an outer loop's
// label. done=false means continue this loop's next iteration.
func loopSig(sg *sig, label string) (done bool, out *sig) {
	mine := sg.label == "" || sg.label == label
	switch {
	case sg.kind == sigBreak && mine:
		return true, nil
	case sg.kind == sigContinue && mine:
		return false, nil
	default:
		return true, sg
	}
}

// isErrSig reports an error-path exit: a return signal carrying an
// Err Result — what `?` propagates and `return Err(…)` produces.
func isErrSig(sg *sig) bool {
	if sg.kind != sigReturn {
		return false
	}
	r, ok := sg.val.(*ResultV)
	return ok && !r.Ok
}

func (in *Interp) runDefers(defers []*ast.DeferStmt, env *Env, errPath bool) {
	for i := len(defers) - 1; i >= 0; i-- {
		d := defers[i]
		if d.Err && !errPath {
			continue
		}
		if _, sg := in.evalBlock(d.Body, newEnv(env, false)); sg != nil {
			panic(rtErr{d.Line, "a defer block cannot return"})
		}
	}
}

// hoistFns declares a block's nested fns at block entry (Rust's
// item hoisting: helpers read fine below their callers). They share
// one private items env rooted at global, so siblings are mutually
// recursive while enclosing locals stay out of reach.
func (in *Interp) hoistFns(b *ast.Block, env *Env) {
	items := newEnv(in.global, true)
	for _, s := range b.Stmts {
		if fs, ok := s.(*ast.FnStmt); ok {
			fv := &FuncV{Decl: fs.Decl, Items: items}
			items.declare(fs.Decl.Name, fv, false, fs.Decl.Line)
			env.declare(fs.Decl.Name, fv, false, fs.Decl.Line)
		}
	}
}

// evalStmt returns the statement's value (unit for everything except
// expression statements — the tail-expression rule lives on this).
func (in *Interp) evalStmt(s ast.Stmt, env *Env) (Value, *sig) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		return in.eval(st.E, env)

	case *ast.LetStmt:
		v, sg := in.eval(st.Init, env)
		if sg != nil {
			return UnitV{}, sg
		}
		binds, ok := match(st.Pat, v)
		if !ok {
			if st.Else == nil {
				panic(rtErr{st.Line, fmt.Sprintf("let pattern does not match %s", render(v, true))})
			}
			_, esg := in.evalBlock(st.Else, newEnv(env, false))
			if esg != nil {
				return UnitV{}, esg
			}
			panic(rtErr{st.Line, "the else block of `let … else` must diverge (return or exit), but it ran off the end"})
		}
		for _, b := range binds {
			env.declare(b.name, b.val, b.mut, st.Line)
		}
		return UnitV{}, nil

	case *ast.AssignStmt:
		return UnitV{}, in.evalAssign(st, env)

	case *ast.ReturnStmt:
		if st.E == nil {
			return UnitV{}, &sig{val: UnitV{}}
		}
		v, sg := in.eval(st.E, env)
		if sg != nil {
			return UnitV{}, sg
		}
		return UnitV{}, &sig{val: v}

	case *ast.ForStmt:
		return UnitV{}, in.evalFor(st, env)

	case *ast.YieldStmt:
		return UnitV{}, in.evalYield(st, env)

	case *ast.FnStmt:
		return UnitV{}, nil // hoisted at block entry

	case *ast.BreakStmt:
		return UnitV{}, &sig{kind: sigBreak, val: UnitV{}, label: st.Label}

	case *ast.ContinueStmt:
		return UnitV{}, &sig{kind: sigContinue, val: UnitV{}, label: st.Label}
	}
	panic(rtErr{0, fmt.Sprintf("unhandled statement %T", s)})
}

func (in *Interp) evalAssign(st *ast.AssignStmt, env *Env) *sig {
	// `_ = expr`: evaluate, discard.
	if id, ok := st.Target.(*ast.IdentExpr); ok && id.Name == "_" {
		_, sg := in.eval(st.Value, env)
		return sg
	}
	in.requireMutRoot(st.Target, env, st.Line)

	rhs, sg := in.eval(st.Value, env)
	if sg != nil {
		return sg
	}

	switch t := st.Target.(type) {
	case *ast.IdentExpr:
		if st.Op != "=" {
			b := env.lookup(t.Name)
			if b == nil {
				panic(rtErr{st.Line, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", t.Name)})
			}
			rhs = binop(strings.TrimSuffix(st.Op, "="), b.v, rhs, st.Line)
		}
		env.assign(t.Name, rhs, st.Line)
		return nil
	case *ast.Index:
		obj, sg := in.eval(t.X, env)
		if sg != nil {
			return sg
		}
		idx, sg := in.eval(t.I, env)
		if sg != nil {
			return sg
		}
		switch o := obj.(type) {
		case *MapV:
			k := hashable(idx, st.Line)
			if st.Op != "=" {
				cur, ok := o.get(k)
				if !ok {
					panic(rtErr{st.Line, fmt.Sprintf("%s on a key that is not present; read with ?? or insert with = first", st.Op)})
				}
				rhs = binop(strings.TrimSuffix(st.Op, "="), cur, rhs, st.Line)
			}
			o.set(k, rhs)
			return nil
		case *ListV:
			i, ok := idx.(IntV)
			if !ok {
				panic(rtErr{st.Line, "list index must be an Int"})
			}
			if i < 0 || int(i) >= len(o.Elems) {
				panic(rtErr{st.Line, fmt.Sprintf("list index %d out of range (len %d)", i, len(o.Elems))})
			}
			if st.Op != "=" {
				rhs = binop(strings.TrimSuffix(st.Op, "="), o.Elems[i], rhs, st.Line)
			}
			o.Elems[i] = rhs
			return nil
		}
		panic(rtErr{st.Line, fmt.Sprintf("cannot index-assign into %s", typeName(obj))})
	case *ast.Field:
		obj, sg := in.eval(t.X, env)
		if sg != nil {
			return sg
		}
		sv, ok := obj.(*StructV)
		if !ok {
			panic(rtErr{st.Line, fmt.Sprintf("cannot assign a field on %s", typeName(obj))})
		}
		cur, ok := sv.Fields[t.Name]
		if !ok {
			panic(rtErr{st.Line, fmt.Sprintf("%s has no field %q", sv.Type, t.Name)})
		}
		if st.Op != "=" {
			rhs = binop(strings.TrimSuffix(st.Op, "="), cur, rhs, st.Line)
		}
		sv.Fields[t.Name] = rhs
		return nil
	}
	panic(rtErr{st.Line, "invalid assignment target"})
}

// requireMutRoot walks an assignment target to its root name and
// requires a mut binding — mutability is transitive through paths.
func (in *Interp) requireMutRoot(target ast.Expr, env *Env, line int) {
	e := target
	for {
		switch t := e.(type) {
		case *ast.IdentExpr:
			b := env.lookup(t.Name)
			if b == nil {
				panic(rtErr{line, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", t.Name)})
			}
			if !b.mut {
				panic(rtErr{line, fmt.Sprintf("cannot mutate through immutable binding %q (declare it with `let mut`)", t.Name)})
			}
			return
		case *ast.Index:
			e = t.X
		case *ast.Field:
			e = t.X
		case *ast.TupleIndex:
			e = t.X
		default:
			// Covers assignment targets and mut-method receivers alike:
			// a temporary is not a path, so it has no mut path.
			panic(rtErr{line, "cannot mutate a temporary value (bind it with `let mut` first)"})
		}
	}
}

func (in *Interp) evalFor(st *ast.ForStmt, env *Env) *sig {
	switch {
	case st.Iter != nil:
		it, sg := in.eval(st.Iter, env)
		if sg != nil {
			return sg
		}
		next := in.iterate(it, st.Line)
		for {
			v, ok := next()
			if !ok {
				return nil
			}
			// Fresh binding every iteration (recorded: Go's 1.22 fix,
			// ours from day one).
			iterEnv := newEnv(env, false)
			binds, ok2 := match(st.Pat, v)
			if !ok2 {
				panic(rtErr{st.Line, fmt.Sprintf("for pattern does not match %s", render(v, true))})
			}
			for _, b := range binds {
				iterEnv.declare(b.name, b.val, b.mut, st.Line)
			}
			if _, sg := in.evalBlock(st.Body, iterEnv); sg != nil {
				if done, out := loopSig(sg, st.Label); done {
					return out
				}
			}
		}
	case st.Cond != nil:
		for {
			c, sg := in.eval(st.Cond, env)
			if sg != nil {
				return sg
			}
			b, ok := c.(BoolV)
			if !ok {
				panic(rtErr{st.Line, fmt.Sprintf("loop condition must be Bool, got %s", typeName(c))})
			}
			if !b {
				return nil
			}
			if _, sg := in.evalBlock(st.Body, newEnv(env, false)); sg != nil {
				if done, out := loopSig(sg, st.Label); done {
					return out
				}
			}
		}
	default:
		for {
			if _, sg := in.evalBlock(st.Body, newEnv(env, false)); sg != nil {
				if done, out := loopSig(sg, st.Label); done {
					return out
				}
			}
		}
	}
}

// Pattern matching

type bound struct {
	name string
	mut  bool
	val  Value
}

func match(p ast.Pattern, v Value) ([]bound, bool) {
	switch pt := p.(type) {
	case *ast.WildPat:
		return nil, true
	case *ast.IdentPat:
		return []bound{{name: pt.Name, mut: pt.Mut, val: v}}, true
	case *ast.IntPat:
		iv, ok := v.(IntV)
		return nil, ok && int64(iv) == pt.V
	case *ast.StrPat:
		sv, ok := v.(StrV)
		return nil, ok && string(sv) == pt.V
	case *ast.BoolPat:
		bv, ok := v.(BoolV)
		return nil, ok && bool(bv) == pt.V
	case *ast.RangePat:
		iv, ok := v.(IntV)
		in := ok && pt.Lo <= int64(iv) &&
			(int64(iv) < pt.Hi || (pt.Incl && int64(iv) == pt.Hi))
		return nil, in
	case *ast.RunePat:
		rv, ok := v.(RuneV)
		return nil, ok && rune(rv) == pt.V
	case *ast.RuneRangePat:
		rv, ok := v.(RuneV)
		in := ok && pt.Lo <= rune(rv) &&
			(rune(rv) < pt.Hi || (pt.Incl && rune(rv) == pt.Hi))
		return nil, in
	case *ast.TuplePat:
		tv, ok := v.(TupleV)
		if !ok || len(tv) != len(pt.Elems) {
			return nil, false
		}
		var all []bound
		for i, el := range pt.Elems {
			bs, ok := match(el, tv[i])
			if !ok {
				return nil, false
			}
			all = append(all, bs...)
		}
		return all, true
	case *ast.StructPat:
		// Named-field variant: NotFound{ id } — same field rules.
		if vv, isVar := v.(*VariantV); isVar {
			if vv.Name != pt.Type || vv.FieldNames == nil {
				return nil, false
			}
			if !pt.Rest && len(pt.Fields) != len(vv.FieldNames) {
				panic(rtErr{pt.Line, fmt.Sprintf(
					"variant pattern %s{…} names %d of %d fields; mention them all, or end with `..` for a deliberate partial match",
					pt.Type, len(pt.Fields), len(vv.FieldNames))})
			}
			var all []bound
			for _, f := range pt.Fields {
				idx := -1
				for i, fn := range vv.FieldNames {
					if fn == f.Name {
						idx = i
						break
					}
				}
				if idx < 0 {
					panic(rtErr{pt.Line, fmt.Sprintf("%s has no field %q", pt.Type, f.Name)})
				}
				bs, ok := match(f.Pat, vv.Args[idx])
				if !ok {
					return nil, false
				}
				all = append(all, bs...)
			}
			return all, true
		}
		sv, ok := v.(*StructV)
		if !ok || sv.Type != pt.Type {
			return nil, false
		}
		// Partial matches must say so: without `..` every field is
		// mentioned, so new fields break match sites (the point).
		if !pt.Rest && len(pt.Fields) != len(sv.Fields) {
			panic(rtErr{pt.Line, fmt.Sprintf(
				"struct pattern %s{…} names %d of %d fields; mention them all, or end with `..` for a deliberate partial match",
				pt.Type, len(pt.Fields), len(sv.Fields))})
		}
		var all []bound
		for _, f := range pt.Fields {
			fv, ok := sv.Fields[f.Name]
			if !ok {
				panic(rtErr{pt.Line, fmt.Sprintf("%s has no field %q", pt.Type, f.Name)})
			}
			bs, ok2 := match(f.Pat, fv)
			if !ok2 {
				return nil, false
			}
			all = append(all, bs...)
		}
		return all, true
	case *ast.CtorPat:
		switch pt.Name {
		case "None":
			_, isNone := v.(NoneV)
			return nil, isNone && len(pt.Args) == 0
		case "Some":
			// Unboxed Option: Some(p) matches any non-None value.
			if _, isNone := v.(NoneV); isNone {
				return nil, false
			}
			if len(pt.Args) != 1 {
				return nil, false
			}
			return match(pt.Args[0], v)
		case "Ok", "Err":
			rv, isRes := v.(*ResultV)
			if !isRes || rv.Ok != (pt.Name == "Ok") || len(pt.Args) != 1 {
				return nil, false
			}
			return match(pt.Args[0], rv.V)
		}
		vv, isVar := v.(*VariantV)
		if !isVar || vv.Name != pt.Name || len(pt.Args) != len(vv.Args) {
			return nil, false
		}
		var all []bound
		for i, ap := range pt.Args {
			bs, ok := match(ap, vv.Args[i])
			if !ok {
				return nil, false
			}
			all = append(all, bs...)
		}
		return all, true
	case *ast.ListPat:
		lv, ok := v.(*ListV)
		if !ok {
			return nil, false
		}
		el := lv.Elems
		if pt.Rest < 0 {
			if len(el) != len(pt.Elems) {
				return nil, false
			}
			var all []bound
			for i, ep := range pt.Elems {
				bs, ok := match(ep, el[i])
				if !ok {
					return nil, false
				}
				all = append(all, bs...)
			}
			return all, true
		}
		if len(el) < len(pt.Elems) {
			return nil, false
		}
		var all []bound
		front := pt.Elems[:pt.Rest]
		back := pt.Elems[pt.Rest:]
		for i, ep := range front {
			bs, ok := match(ep, el[i])
			if !ok {
				return nil, false
			}
			all = append(all, bs...)
		}
		for i, ep := range back {
			bs, ok := match(ep, el[len(el)-len(back)+i])
			if !ok {
				return nil, false
			}
			all = append(all, bs...)
		}
		if pt.RestName != "_" {
			mid := el[len(front) : len(el)-len(back)]
			rest := &ListV{Elems: append([]Value{}, mid...)} // honestly a copy
			all = append(all, bound{name: pt.RestName, val: rest})
		}
		return all, true
	}
	return nil, false
}

// Expressions

func (in *Interp) eval(e ast.Expr, env *Env) (Value, *sig) {
	switch ex := e.(type) {
	case *ast.IntLit:
		return IntV(ex.V), nil
	case *ast.FloatLit:
		return FloatV(ex.V), nil
	case *ast.BoolLit:
		return BoolV(ex.V), nil
	case *ast.RuneLit:
		return RuneV(ex.V), nil
	case *ast.UnitLit:
		return UnitV{}, nil
	case *ast.StrLit:
		return in.evalStr(ex, env)
	case *ast.IdentExpr:
		return in.evalIdent(ex, env)
	case *ast.TupleLit:
		tv := make(TupleV, 0, len(ex.Elems))
		for _, el := range ex.Elems {
			v, sg := in.eval(el, env)
			if sg != nil {
				return UnitV{}, sg
			}
			tv = append(tv, v)
		}
		return tv, nil
	case *ast.ListLit:
		lv := &ListV{}
		for _, el := range ex.Elems {
			if sp, isSpread := el.(*ast.Spread); isSpread {
				v, sg := in.eval(sp.E, env)
				if sg != nil {
					return UnitV{}, sg
				}
				next := in.iterate(v, sp.Line)
				for {
					e, ok := next()
					if !ok {
						break
					}
					lv.Elems = append(lv.Elems, e)
				}
				continue
			}
			v, sg := in.eval(el, env)
			if sg != nil {
				return UnitV{}, sg
			}
			lv.Elems = append(lv.Elems, v)
		}
		return lv, nil
	case *ast.MapLit:
		mv := newMap()
		for i, ke := range ex.Keys {
			k, sg := in.eval(ke, env)
			if sg != nil {
				return UnitV{}, sg
			}
			v, sg := in.eval(ex.Vals[i], env)
			if sg != nil {
				return UnitV{}, sg
			}
			mv.set(hashable(k, 0), v)
		}
		return mv, nil
	case *ast.RangeExpr:
		lo, sg := in.eval(ex.Lo, env)
		if sg != nil {
			return UnitV{}, sg
		}
		hi, sg := in.eval(ex.Hi, env)
		if sg != nil {
			return UnitV{}, sg
		}
		l, ok1 := lo.(IntV)
		h, ok2 := hi.(IntV)
		if !ok1 || !ok2 {
			panic(rtErr{ex.Line, "range bounds must be Int"})
		}
		end := int64(h)
		if ex.Incl {
			// ..= desugars to the half-open range one past hi.
			if end == math.MaxInt64 {
				panic(rtErr{ex.Line, "..= cannot include the maximum Int"})
			}
			end++
		}
		return RangeV{Lo: int64(l), Hi: end}, nil
	case *ast.Unary:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		switch ex.Op {
		case "-":
			switch n := v.(type) {
			case IntV:
				if n == math.MinInt64 {
					panic(rtErr{ex.Line, "Int overflow: negating the minimum Int (dev builds trap; release wraps)"})
				}
				return -n, nil
			case FloatV:
				return -n, nil
			}
			panic(rtErr{ex.Line, fmt.Sprintf("cannot negate %s", typeName(v))})
		case "!":
			b, ok := v.(BoolV)
			if !ok {
				panic(rtErr{ex.Line, fmt.Sprintf("! requires Bool, got %s", typeName(v))})
			}
			return !b, nil
		}
	case *ast.Binary:
		return in.evalBinary(ex, env)
	case *ast.BlockExpr:
		return in.evalBlock(ex.Body, newEnv(env, false))
	case *ast.If:
		return in.evalIf(ex, env)
	case *ast.Closure:
		// Capture the *bindings* visible right now, not the names:
		// a later `let x` redeclaration creates a new binding and
		// must not retarget closures that captured the old one.
		// Binding cells are shared, so mutation through a captured
		// `mut` variable stays visible both ways.
		return &ClosureV{Params: ex.Params, BodyExpr: ex.BodyExpr, BodyBlock: ex.BodyBlock, Env: env.capture()}, nil
	case *ast.Try:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		r, ok := v.(*ResultV)
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("? requires a Result, got %s", typeName(v))})
		}
		if r.Ok {
			return r.V, nil
		}
		// Conversion fires at the propagation point (recorded): when
		// the enclosing fn declares Result<_, E> and the error isn't
		// already an E, E.from converts it — Rust's From, in the
		// trait-less associated-fn form until the checker era.
		if target := env.fnRetErr(); target != "" && typeName(r.V) != target {
			if m := in.methods[target]["from"]; m != nil && m.Self == ast.NoSelf {
				conv := in.callFuncNamed(m, nil, []Value{r.V}, nil, ex.Line)
				return UnitV{}, &sig{val: &ResultV{Ok: false, V: conv}}
			}
		}
		return UnitV{}, &sig{val: r} // propagate the Err to the caller
	case *ast.TupleIndex:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		tv, ok := v.(TupleV)
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf(".%d requires a tuple, got %s", ex.N, typeName(v))})
		}
		if ex.N >= len(tv) {
			panic(rtErr{ex.Line, fmt.Sprintf("tuple has no field .%d (size %d)", ex.N, len(tv))})
		}
		return tv[ex.N], nil
	case *ast.Index:
		return in.evalIndex(ex, env)
	case *ast.Field:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		if tv, isType := v.(TypeV); isType {
			// Namespaced variant: Color.Red, Shape.Circle (ctor).
			if vi, ok := in.variants[ex.Name]; ok && vi.typeName == string(tv) {
				return in.variantValue(ex.Name, vi, ex.Line), nil
			}
			panic(rtErr{ex.Line, fmt.Sprintf("%s has no variant %q", tv, ex.Name)})
		}
		if vv, isVar := v.(*VariantV); isVar {
			for i, fn := range vv.FieldNames {
				if fn == ex.Name {
					return vv.Args[i], nil
				}
			}
			panic(rtErr{ex.Line, fmt.Sprintf("%s has no field %q", vv.Name, ex.Name)})
		}
		st, ok := v.(*StructV)
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("%s has no field %q (methods need call parens)", typeName(v), ex.Name)})
		}
		fv, ok := st.Fields[ex.Name]
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("%s has no field %q", st.Type, ex.Name)})
		}
		return fv, nil
	case *ast.StructLit:
		return in.evalStructLit(ex, env)
	case *ast.DotName:
		vi, ok := in.variants[ex.Name]
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf(".%s: no type declares a variant %q", ex.Name, ex.Name)})
		}
		return in.variantValue(ex.Name, vi, ex.Line), nil
	case *ast.Match:
		return in.evalMatch(ex, env)
	case *ast.CondMatch:
		return in.evalCondMatch(ex, env)
	case *ast.IfLet:
		return in.evalIfLet(ex, env)
	case *ast.Call:
		return in.evalCall(ex, env)
	}
	panic(rtErr{0, fmt.Sprintf("unhandled expression %T", e)})
}

func (in *Interp) evalIdent(ex *ast.IdentExpr, env *Env) (Value, *sig) {
	if ex.Name == "_" {
		panic(rtErr{ex.Line, "_ discards; it cannot be read"})
	}
	if b := env.lookup(ex.Name); b != nil {
		return b.v, nil
	}
	if fn, ok := in.fns[ex.Name]; ok {
		return &FuncV{Decl: fn}, nil
	}
	if in.imports[ex.Name] {
		return ModuleV(ex.Name), nil
	}
	if ex.Name == "None" {
		return NoneV{}, nil
	}
	if _, ok := in.types[ex.Name]; ok {
		return TypeV(ex.Name), nil
	}
	if vi, ok := in.variants[ex.Name]; ok {
		panic(rtErr{ex.Line, fmt.Sprintf(
			"variants are namespaced: write .%s or %s.%s (bare variant names are pattern-only)",
			ex.Name, vi.typeName, ex.Name)})
	}
	if b, ok := builtins[ex.Name]; ok {
		return b, nil
	}
	panic(rtErr{ex.Line, fmt.Sprintf("undefined name %q", ex.Name)})
}

func (in *Interp) evalStructLit(ex *ast.StructLit, env *Env) (Value, *sig) {
	td, ok := in.types[ex.Type]
	if !ok || td.Fields == nil {
		if vi, isVar := in.variants[ex.Type]; isVar && vi.fields != nil {
			return in.evalVariantLit(ex, vi, env)
		}
		panic(rtErr{ex.Line, fmt.Sprintf("%q is not a struct type", ex.Type)})
	}
	sv := &StructV{Type: ex.Type, Fields: map[string]Value{}}
	for _, fd := range td.Fields {
		sv.Order = append(sv.Order, fd.Name)
	}
	if ex.Base != nil {
		base, sg := in.eval(ex.Base, env)
		if sg != nil {
			return UnitV{}, sg
		}
		bs, ok := base.(*StructV)
		if !ok || bs.Type != ex.Type {
			panic(rtErr{ex.Line, fmt.Sprintf("..base must be a %s, got %s", ex.Type, typeName(base))})
		}
		for f, v := range bs.Fields {
			sv.Fields[f] = v // copy-with-changes; base object untouched
		}
	}
	for i, name := range ex.Names {
		found := false
		for _, fd := range td.Fields {
			if fd.Name == name {
				found = true
				break
			}
		}
		if !found {
			panic(rtErr{ex.Line, fmt.Sprintf("%s has no field %q", ex.Type, name)})
		}
		v, sg := in.eval(ex.Vals[i], env)
		if sg != nil {
			return UnitV{}, sg
		}
		sv.Fields[name] = v
	}
	// Mandatory initialisation: every field accounted for, by the
	// literal or by ..base.
	for _, fd := range td.Fields {
		if _, ok := sv.Fields[fd.Name]; !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("missing field %q in %s literal (no zero values)", fd.Name, ex.Type)})
		}
	}
	return sv, nil
}

// evalVariantLit constructs a named-field variant: NotFound{ id: 7 }.
// Every declared field, exactly once; ..base is a struct affair.
func (in *Interp) evalVariantLit(ex *ast.StructLit, vi variantInfo, env *Env) (Value, *sig) {
	if ex.Base != nil {
		panic(rtErr{ex.Line, fmt.Sprintf("..base is for structs; %s is a variant of %s", ex.Type, vi.typeName)})
	}
	given := map[string]Value{}
	for i, name := range ex.Names {
		v, sg := in.eval(ex.Vals[i], env)
		if sg != nil {
			return UnitV{}, sg
		}
		given[name] = v
	}
	vv := &VariantV{Type: vi.typeName, Name: ex.Type, FieldNames: vi.fields}
	for _, f := range vi.fields {
		v, ok := given[f]
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("%s is missing field %q (no zero values — every field is named)", ex.Type, f)})
		}
		vv.Args = append(vv.Args, v)
		delete(given, f)
	}
	for name := range given {
		panic(rtErr{ex.Line, fmt.Sprintf("%s has no field %q", ex.Type, name)})
	}
	return vv, nil
}

// variantValue resolves a variant reference: an arity-0 variant is
// its value; a positional-payload variant is its constructor; a
// named-field variant needs braces.
func (in *Interp) variantValue(name string, vi variantInfo, line int) Value {
	if vi.fields != nil {
		panic(rtErr{line, fmt.Sprintf("%s has named fields; construct it with %s{ … }", name, name)})
	}
	if vi.arity == 0 {
		return &VariantV{Type: vi.typeName, Name: name}
	}
	return &BuiltinV{Name: name, Fn: func(_ *Interp, args []Value, l int) Value {
		if len(args) != vi.arity {
			panic(rtErr{l, fmt.Sprintf("%s takes %d argument(s), got %d", name, vi.arity, len(args))})
		}
		return &VariantV{Type: vi.typeName, Name: name, Args: args}
	}}
}

func (in *Interp) evalMatch(ex *ast.Match, env *Env) (Value, *sig) {
	x, sg := in.eval(ex.X, env)
	if sg != nil {
		return UnitV{}, sg
	}
	for i := range ex.Arms {
		arm := &ex.Arms[i]
		var binds []bound
		ok := false
		for _, q := range arm.Pats {
			if bs, matched := match(q, x); matched {
				binds, ok = bs, true
				break
			}
		}
		if !ok {
			continue
		}
		armEnv := newEnv(env, false)
		for _, b := range binds {
			armEnv.declare(b.name, b.val, b.mut, arm.Line)
		}
		if arm.Guard != nil {
			g, sg := in.eval(arm.Guard, armEnv)
			if sg != nil {
				return UnitV{}, sg
			}
			gb, isBool := g.(BoolV)
			if !isBool {
				panic(rtErr{arm.Line, fmt.Sprintf("match guard must be Bool, got %s", typeName(g))})
			}
			if !gb {
				continue
			}
		}
		return in.eval(arm.Body, armEnv)
	}
	panic(rtErr{ex.Line, fmt.Sprintf("no match arm matched %s (exhaustiveness checking arrives with the compiler)", render(x, true))})
}

func (in *Interp) evalCondMatch(ex *ast.CondMatch, env *Env) (Value, *sig) {
	for i := range ex.Arms {
		arm := &ex.Arms[i]
		if arm.Cond != nil {
			c, sg := in.eval(arm.Cond, env)
			if sg != nil {
				return UnitV{}, sg
			}
			cb, isBool := c.(BoolV)
			if !isBool {
				panic(rtErr{arm.Line, fmt.Sprintf("subjectless match arm must be Bool, got %s", typeName(c))})
			}
			if !cb {
				continue
			}
		}
		return in.eval(arm.Body, newEnv(env, false))
	}
	panic(rtErr{ex.Line, "no match arm was true (add a `_ =>` arm)"})
}

func (in *Interp) evalIfLet(ex *ast.IfLet, env *Env) (Value, *sig) {
	x, sg := in.eval(ex.X, env)
	if sg != nil {
		return UnitV{}, sg
	}
	if _, isNone := x.(NoneV); isNone {
		if ex.ElseIf != nil {
			return in.eval(ex.ElseIf, env)
		}
		if ex.ElseBlock != nil {
			return in.evalBlock(ex.ElseBlock, newEnv(env, false))
		}
		return UnitV{}, nil
	}
	binds, ok := match(ex.Pat, x)
	if !ok {
		panic(rtErr{ex.Line, fmt.Sprintf("if let pattern does not match %s", render(x, true))})
	}
	thenEnv := newEnv(env, false)
	for _, b := range binds {
		thenEnv.declare(b.name, b.val, b.mut, ex.Line)
	}
	return in.evalBlock(ex.Then, thenEnv)
}

func (in *Interp) evalStr(ex *ast.StrLit, env *Env) (Value, *sig) {
	var sb strings.Builder
	for _, part := range ex.Parts {
		if !part.IsExpr {
			sb.WriteString(part.Lit)
			continue
		}
		v, sg := in.eval(part.E, env)
		if sg != nil {
			return UnitV{}, sg
		}
		s := display(v)
		if part.Spec != "" {
			s = formatSpec(v, part.Spec, ex.Line)
		}
		sb.WriteString(s)
	}
	return StrV(sb.String()), nil
}

// formatSpec applies the format-spec set (DESIGN.md: deliberately
// small — printf must not grow back). Standalone: `?` (Debug render)
// and `hex` (Int, lowercase). Numeric: [`-`|`0`] [width] [`,`]
// [`.`prec] — right-aligned width, `-` left-aligns, leading `0`
// zero-pads, `,` groups thousands, `.prec` fixes decimal places.
// A spec that doesn't fit the value's type is an error, never noise.
func formatSpec(v Value, spec string, line int) string {
	bad := func(msg string) string {
		panic(rtErr{line, fmt.Sprintf("format spec %q: %s", spec, msg)})
	}
	switch spec {
	case "?":
		return render(v, true)
	case "hex":
		iv, ok := v.(IntV)
		if !ok {
			return bad(fmt.Sprintf("hex needs an Int, got %s", typeName(v)))
		}
		return strconv.FormatInt(int64(iv), 16)
	}

	// Parse [`-`|`0`] [width] [`,`] [`.`prec]; anything left over is
	// an unsupported spec.
	rest := spec
	left, zero := false, false
	if strings.HasPrefix(rest, "-") {
		left, rest = true, rest[1:]
	} else if len(rest) > 1 && rest[0] == '0' && rest[1] >= '0' && rest[1] <= '9' {
		zero, rest = true, rest[1:]
	}
	width := 0
	for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
		width = width*10 + int(rest[0]-'0')
		rest = rest[1:]
	}
	group := false
	if strings.HasPrefix(rest, ",") {
		group, rest = true, rest[1:]
	}
	prec := -1
	if strings.HasPrefix(rest, ".") {
		rest = rest[1:]
		prec = 0
		n := 0
		for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			prec = prec*10 + int(rest[0]-'0')
			rest = rest[1:]
			n++
		}
		if n == 0 {
			return bad("`.` needs digits, e.g. {x:.2}")
		}
	}
	if rest != "" || spec == "" || (left && width == 0) {
		return bad("unsupported (the set: {x:6}, {x:-6}, {x:04}, {x:.2}, {x:8.2}, {n:,}, {n:hex}, {v:?})")
	}

	_, isInt := v.(IntV)
	fv, isFloat := v.(FloatV)

	var s string
	switch {
	case prec >= 0:
		if !isFloat {
			return bad(fmt.Sprintf(".%d needs a Float, got %s", prec, typeName(v)))
		}
		s = strconv.FormatFloat(float64(fv), 'f', prec, 64)
	default:
		s = display(v)
	}
	if group {
		if !isInt && !(isFloat && prec >= 0) {
			return bad(fmt.Sprintf("`,` groups Int, or Float with a precision (e.g. {x:,.2}); got %s", typeName(v)))
		}
		s = groupThousands(s)
	}
	if zero {
		if !isInt && !isFloat {
			return bad(fmt.Sprintf("zero-padding needs a number, got %s", typeName(v)))
		}
		if len(s) < width {
			pad := strings.Repeat("0", width-len(s))
			if strings.HasPrefix(s, "-") {
				return "-" + pad + s[1:]
			}
			return pad + s
		}
		return s
	}
	if left {
		return fmt.Sprintf("%-*s", width, s)
	}
	return fmt.Sprintf("%*s", width, s)
}

// groupThousands inserts commas into the integer digits of a plain
// decimal rendering ("-1234567.89" -> "-1,234,567.89").
func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, tail := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, tail = s[:i], s[i:]
	}
	if len(intPart) <= 3 {
		return sign + intPart + tail
	}
	var b strings.Builder
	lead := len(intPart) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(intPart[:lead])
	for i := lead; i < len(intPart); i += 3 {
		b.WriteByte(',')
		b.WriteString(intPart[i : i+3])
	}
	return sign + b.String() + tail
}

func (in *Interp) evalBinary(ex *ast.Binary, env *Env) (Value, *sig) {
	l, sg := in.eval(ex.L, env)
	if sg != nil {
		return UnitV{}, sg
	}
	switch ex.Op {
	case "??":
		// Unboxed Option: None takes the default, anything else is
		// already the value. On a Result, ?? unwraps Ok and takes
		// the default on Err — the error is discarded deliberately
		// (the sketch's `db.exec(…) ?? 0`; ratified with the or-block
		// decision).
		if _, isNone := l.(NoneV); isNone {
			return in.eval(ex.R, env)
		}
		if r, isRes := l.(*ResultV); isRes {
			if r.Ok {
				return r.V, nil
			}
			return in.eval(ex.R, env)
		}
		return l, nil
	case "&&", "||":
		lb, ok := l.(BoolV)
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("%s requires Bool, got %s", ex.Op, typeName(l))})
		}
		if (ex.Op == "&&" && !lb) || (ex.Op == "||" && lb) {
			return lb, nil
		}
		r, sg := in.eval(ex.R, env)
		if sg != nil {
			return UnitV{}, sg
		}
		rb, ok := r.(BoolV)
		if !ok {
			panic(rtErr{ex.Line, fmt.Sprintf("%s requires Bool, got %s", ex.Op, typeName(r))})
		}
		return rb, nil
	}
	r, sg := in.eval(ex.R, env)
	if sg != nil {
		return UnitV{}, sg
	}
	return binop(ex.Op, l, r, ex.Line), nil
}

func binop(op string, l, r Value, line int) Value {
	// Equality is structural for every comparable type; ordered
	// comparisons stay per-type below.
	switch op {
	case "==":
		return BoolV(eq(l, r, line))
	case "!=":
		return BoolV(!eq(l, r, line))
	}
	if li, ok := l.(IntV); ok {
		if ri, ok := r.(IntV); ok {
			// The interpreter is the dev tier: overflow traps here,
			// wraps in release builds (recorded trade, Zig's).
			switch op {
			case "+":
				c := li + ri
				if (c > li) != (ri > 0) {
					panic(rtErr{line, fmt.Sprintf("Int overflow: %d + %d (dev builds trap; release wraps)", li, ri)})
				}
				return c
			case "-":
				c := li - ri
				if (c < li) != (ri > 0) {
					panic(rtErr{line, fmt.Sprintf("Int overflow: %d - %d (dev builds trap; release wraps)", li, ri)})
				}
				return c
			case "*":
				c := li * ri
				if li != 0 && (c/li != ri || (li == -1 && ri == math.MinInt64)) {
					panic(rtErr{line, fmt.Sprintf("Int overflow: %d * %d (dev builds trap; release wraps)", li, ri)})
				}
				return c
			case "/":
				if ri == 0 {
					panic(rtErr{line, "division by zero"})
				}
				if li == math.MinInt64 && ri == -1 {
					panic(rtErr{line, fmt.Sprintf("Int overflow: %d / -1 (dev builds trap; release wraps)", li)})
				}
				return li / ri
			case "%":
				if ri == 0 {
					panic(rtErr{line, "division by zero"})
				}
				if li == math.MinInt64 && ri == -1 {
					return IntV(0)
				}
				return li % ri
			case "==":
				return BoolV(li == ri)
			case "!=":
				return BoolV(li != ri)
			case "<":
				return BoolV(li < ri)
			case "<=":
				return BoolV(li <= ri)
			case ">":
				return BoolV(li > ri)
			case ">=":
				return BoolV(li >= ri)
			}
		}
	}
	if lr, ok := l.(RuneV); ok {
		if rr, ok := r.(RuneV); ok {
			switch op {
			case "<":
				return BoolV(lr < rr)
			case "<=":
				return BoolV(lr <= rr)
			case ">":
				return BoolV(lr > rr)
			case ">=":
				return BoolV(lr >= rr)
			}
		}
	}
	if lf, ok := l.(FloatV); ok {
		if rf, ok := r.(FloatV); ok {
			switch op {
			case "+":
				return lf + rf
			case "-":
				return lf - rf
			case "*":
				return lf * rf
			case "/":
				return lf / rf
			case "==":
				return BoolV(lf == rf)
			case "!=":
				return BoolV(lf != rf)
			case "<":
				return BoolV(lf < rf)
			case "<=":
				return BoolV(lf <= rf)
			case ">":
				return BoolV(lf > rf)
			case ">=":
				return BoolV(lf >= rf)
			}
		}
	}
	if ls, ok := l.(StrV); ok {
		if rs, ok := r.(StrV); ok {
			switch op {
			case "+":
				return ls + rs
			case "==":
				return BoolV(ls == rs)
			case "!=":
				return BoolV(ls != rs)
			case "<":
				return BoolV(ls < rs)
			case "<=":
				return BoolV(ls <= rs)
			case ">":
				return BoolV(ls > rs)
			case ">=":
				return BoolV(ls >= rs)
			}
		}
	}
	if lb, ok := l.(BoolV); ok {
		if rb, ok := r.(BoolV); ok {
			switch op {
			case "==":
				return BoolV(lb == rb)
			case "!=":
				return BoolV(lb != rb)
			}
		}
	}
	panic(rtErr{line, fmt.Sprintf("operator %s not defined for %s and %s", op, typeName(l), typeName(r))})
}

func (in *Interp) evalIf(ex *ast.If, env *Env) (Value, *sig) {
	c, sg := in.eval(ex.Cond, env)
	if sg != nil {
		return UnitV{}, sg
	}
	b, ok := c.(BoolV)
	if !ok {
		panic(rtErr{ex.Line, fmt.Sprintf("if condition must be Bool, got %s", typeName(c))})
	}
	if b {
		return in.evalBlock(ex.Then, newEnv(env, false))
	}
	if ex.ElseIf != nil {
		return in.eval(ex.ElseIf, env)
	}
	if ex.ElseBlock != nil {
		return in.evalBlock(ex.ElseBlock, newEnv(env, false))
	}
	return UnitV{}, nil
}

func (in *Interp) evalIndex(ex *ast.Index, env *Env) (Value, *sig) {
	obj, sg := in.eval(ex.X, env)
	if sg != nil {
		return UnitV{}, sg
	}
	idx, sg := in.eval(ex.I, env)
	if sg != nil {
		return UnitV{}, sg
	}
	switch o := obj.(type) {
	case *MapV:
		// A map read is honest about absence: it returns an Option
		// (unboxed: the value, or None).
		v, ok := o.get(hashable(idx, ex.Line))
		if !ok {
			return NoneV{}, nil
		}
		return v, nil
	case *ListV:
		i, ok := idx.(IntV)
		if !ok {
			panic(rtErr{ex.Line, "list index must be an Int"})
		}
		if i < 0 || int(i) >= len(o.Elems) {
			panic(rtErr{ex.Line, fmt.Sprintf("list index %d out of range (len %d)", i, len(o.Elems))})
		}
		return o.Elems[i], nil
	}
	panic(rtErr{ex.Line, fmt.Sprintf("cannot index %s", typeName(obj))})
}

func (in *Interp) evalCall(ex *ast.Call, env *Env) (Value, *sig) {
	// expect(...) is a special form: it keeps the argument's AST so a
	// failed comparison can report both sides.
	if id, ok := ex.Fn.(*ast.IdentExpr); ok && id.Name == "expect" && env.lookup("expect") == nil {
		return in.evalExpect(ex, env)
	}
	// Method / module calls: f.X.name(args)
	if f, ok := ex.Fn.(*ast.Field); ok {
		recv, sg := in.eval(f.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		args, sg := in.evalArgs(ex.Args, env)
		if sg != nil {
			return UnitV{}, sg
		}
		if mod, isMod := recv.(ModuleV); isMod {
			rejectNamed(ex, "module functions")
			return in.moduleCall(string(mod), f.Name, args, ex.Line), nil
		}
		// Associated functions: Tree.new()
		if tv, isType := recv.(TypeV); isType {
			// Namespaced variant constructor: Shape.Circle(2).
			if vi, ok := in.variants[f.Name]; ok && vi.typeName == string(tv) {
				rejectNamed(ex, "variant constructors")
				return in.callValue(in.variantValue(f.Name, vi, ex.Line), args, ex.Line), nil
			}
			m := in.methods[string(tv)][f.Name]
			if m == nil {
				panic(rtErr{ex.Line, fmt.Sprintf("type %s has no associated function %q", tv, f.Name)})
			}
			if m.Self != ast.NoSelf {
				panic(rtErr{ex.Line, fmt.Sprintf("%s.%s is a method; call it on a value", tv, f.Name)})
			}
			return in.callFuncNamed(m, nil, args, ex.Names, ex.Line), nil
		}
		// User-defined methods on structs and variants; a trait
		// default fills in when the type doesn't override.
		if tn := userTypeName(recv); tn != "" {
			m := in.findMethod(tn, f.Name, ex.Line)
			if m == nil {
				panic(rtErr{ex.Line, fmt.Sprintf("%s has no method %q", tn, f.Name)})
			}
			if m.Self == ast.NoSelf {
				panic(rtErr{ex.Line, fmt.Sprintf("%s.%s is an associated function; call it as %s.%s(…)", tn, f.Name, tn, f.Name)})
			}
			// A `mut self` method is callable only through a mut
			// path — receiver marking happens at the declaration,
			// not the call site, but the path rule still holds.
			if m.Self == ast.MutSelf {
				in.requireMutRoot(f.X, env, ex.Line)
			}
			return in.callFuncNamed(m, recv, args, ex.Names, ex.Line), nil
		}
		if builtinMutMethods[typeName(recv)+"."+f.Name] {
			in.requireMutRoot(f.X, env, ex.Line)
		}
		rejectNamed(ex, "builtin methods")
		return in.methodCall(recv, f.Name, args, ex.Line), nil
	}
	// Direct call of a declared function by name: the one place
	// defaults and named arguments apply (function *values* keep
	// full positional arity — DESIGN.md: defaults are declaration
	// sugar, not type).
	if id, ok := ex.Fn.(*ast.IdentExpr); ok {
		if b := env.lookup(id.Name); b != nil {
			if fv, isFn := b.v.(*FuncV); isFn {
				args, sg := in.evalArgs(ex.Args, env)
				if sg != nil {
					return UnitV{}, sg
				}
				base := in.global
				if fv.Items != nil {
					base = fv.Items
				}
				return in.callFuncNamedIn(base, fv.Decl, nil, args, ex.Names, ex.Line), nil
			}
		} else if fn, isFn := in.fns[id.Name]; isFn {
			args, sg := in.evalArgs(ex.Args, env)
			if sg != nil {
				return UnitV{}, sg
			}
			return in.callFuncNamed(fn, nil, args, ex.Names, ex.Line), nil
		}
	}
	fnv, sg := in.eval(ex.Fn, env)
	if sg != nil {
		return UnitV{}, sg
	}
	args, sg := in.evalArgs(ex.Args, env)
	if sg != nil {
		return UnitV{}, sg
	}
	rejectNamed(ex, "closures, builtins, and function values")
	return in.callValue(fnv, args, ex.Line), nil
}

// rejectNamed guards call paths where named arguments cannot bind:
// only a declared function's signature carries parameter names.
func rejectNamed(ex *ast.Call, what string) {
	if ex.Names != nil {
		panic(rtErr{ex.Line, fmt.Sprintf("named arguments work on declared functions and methods, not %s", what)})
	}
}

func userTypeName(v Value) string {
	switch x := v.(type) {
	case *StructV:
		return x.Type
	case *VariantV:
		return x.Type
	}
	return ""
}

// evalExpect: expect(a == b) reports both sides on failure; any
// other Bool expression reports generically.
func (in *Interp) evalExpect(ex *ast.Call, env *Env) (Value, *sig) {
	if len(ex.Args) != 1 {
		panic(rtErr{ex.Line, "expect takes exactly one expression"})
	}
	if b, ok := ex.Args[0].(*ast.Binary); ok {
		switch b.Op {
		case "==", "!=", "<", "<=", ">", ">=":
			l, sg := in.eval(b.L, env)
			if sg != nil {
				return UnitV{}, sg
			}
			r, sg := in.eval(b.R, env)
			if sg != nil {
				return UnitV{}, sg
			}
			res := binop(b.Op, l, r, b.Line)
			if res == BoolV(true) {
				return UnitV{}, nil
			}
			panic(testFail{ex.Line, fmt.Sprintf("expect failed: left %s right\n  left:  %s\n  right: %s",
				b.Op, render(l, true), render(r, true))})
		}
	}
	v, sg := in.eval(ex.Args[0], env)
	if sg != nil {
		return UnitV{}, sg
	}
	if v == BoolV(true) {
		return UnitV{}, nil
	}
	panic(testFail{ex.Line, fmt.Sprintf("expect failed (value: %s)", render(v, true))})
}

func (in *Interp) evalArgs(exprs []ast.Expr, env *Env) ([]Value, *sig) {
	args := make([]Value, 0, len(exprs))
	for _, a := range exprs {
		v, sg := in.eval(a, env)
		if sg != nil {
			return nil, sg
		}
		args = append(args, v)
	}
	return args, nil
}
