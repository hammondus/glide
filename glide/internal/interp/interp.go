// Package interp is a tree-walking evaluator for Glide, and one of
// the language's two shipping tiers.
//
// It does not check types: internal/check does that, for both tiers,
// and load() runs it before a single expression is evaluated. What
// remains here is evaluation plus a belt-and-braces layer of runtime
// assertions for rules the checker also enforces (mut, shadowing,
// let-else divergence, the tail-value rule, arity). Those are kept
// deliberately — a checker bug should surface as a loud, positioned
// interpreter error rather than as the evaluator quietly doing
// something undefined. Errors use Go panics internally, recovered at
// Run; Glide-level control flow (return, `?`) uses explicit signals.
package interp

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"glide/internal/ast"
	"glide/internal/check"
	"glide/internal/program"
	"glide/internal/source"
	"glide/internal/types"
)

type Interp struct {
	Stdout io.Writer
	Stderr io.Writer
	Args   []string // what os.args() returns

	// src is the file every Span indexes into, so a runtime error can
	// render itself with the same file:line:col and caret a compile
	// error gets. Set by load().
	src *source.File

	// prog is the declaration table, built by the same code the checker
	// uses (internal/program) so the two tiers cannot disagree about
	// what a name means.
	prog *program.Table

	// info is what the checker learned: types on expressions, and the
	// variant each `.Shorthand` resolved to. The evaluator does not
	// need it yet; it is the seam the checker-era rewrites of
	// `?`-conversion and shorthand resolution plug into.
	info *check.Info

	genCache map[*ast.FuncDecl]bool
	global   *Env
	exiting  bool // os.exit in flight: skip defers (Go's rule)

	constEval bool // inside a const initializer: pure exprs only

	// The runtime lock: exactly one goroutine interprets at a time;
	// blocking operations release it (concurrency.go). cur is the
	// interpreting goroutine's cancellation context, GIL-protected.
	gil sync.Mutex
	cur *taskCtx
}

// testFail aborts a test case; the runner reports it.
type testFail struct {
	at  source.Span
	msg string
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

// rtErr is a runtime error carrying the span of the construct that
// raised it; exitPanic is os.exit in flight. rtErr holds a span
// rather than a line number so a runtime error renders exactly like a
// compile error — same file:line:col, same caret — and so a checker
// diagnostic and a runtime one can never disagree about where
// something is.
type rtErr struct {
	at  source.Span
	msg string
}
type exitPanic struct{ code int }

// ExitError reports an os.exit so the CLI (not the interpreter)
// decides to terminate the process — tests intercept it instead.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

var knownModules = map[string]bool{
	"fs": true, "os": true, "time": true, "process": true,
	"json": true, "http": true, "sql": true,
}

func New() *Interp {
	return &Interp{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		genCache: map[*ast.FuncDecl]bool{},
	}
}

// errAt reports at a span, rendered against the loaded source file so
// runtime and compile diagnostics look identical.
func (in *Interp) errAt(at source.Span, format string, args ...any) error {
	d := source.Diagnostic{Span: at, Msg: fmt.Sprintf(format, args...)}
	if in.src == nil {
		return d
	}
	return &source.Error{File: in.src, Diags: []source.Diagnostic{d}}
}

// hostKnows is what a program may import and may not redeclare. It
// comes from the checker's own tables rather than from this package's
// `builtins` map, so the two tiers cannot disagree about which names
// are reserved — TestHostSurfaceMatchesRuntime asserts the tables and
// the implementations describe the same set.
func hostKnows() program.Known { return check.Host() }

// load indexes a file's declarations and evaluates its consts. Shared
// by Run and RunTests.
//
// The indexing half lives in internal/program because the checker
// needs exactly the same answers; only const *evaluation* is the
// interpreter's own business, and it stays here.
func (in *Interp) load(f *ast.File) error {
	in.src = f.Source
	prog, err := program.Load(f, hostKnows())
	if err != nil {
		return err
	}
	in.prog = prog

	// The checker runs before anything is evaluated, in both tiers,
	// always. There is no flag to skip it: an interpreter that can
	// run unchecked Glide makes unchecked Glide the real scripting
	// dialect and the annotations rot again (DESIGN.md).
	info, err := check.File(f, prog)
	if err != nil {
		return err
	}
	in.info = info

	in.global = newEnv(nil, true)
	// Consts: the only module-level state — evaluated once, in
	// declaration order, restricted to pure expressions (M2's
	// conservative comptime shim: it can loosen later, not tighten).
	// Iterating f.Consts rather than the table's map keeps the order
	// the program wrote; duplicates were already rejected above.
	for _, c := range f.Consts {
		in.constEval = true
		v, sg := in.eval(c.E, in.global)
		in.constEval = false
		if sg != nil {
			return in.errAt(c.Span, "a const initializer cannot return or propagate")
		}
		in.global.declare(c.Name, v, false, c.Span)
	}
	return nil
}

func (in *Interp) Run(f *ast.File) (err error) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case rtErr:
			err = in.errAt(p.at, "%s", p.msg)
		case testFail:
			err = in.errAt(p.at, "%s", p.msg)
		case exitPanic:
			err = &ExitError{Code: p.code}
		default:
			panic(p)
		}
	}()
	if err := in.load(f); err != nil {
		return err
	}
	mainFn, ok := in.prog.Fns["main"]
	if !ok {
		return fmt.Errorf("no main function")
	}
	if len(mainFn.Params) != 0 {
		return in.errAt(mainFn.Span, "main takes no parameters (use os.args())")
	}
	in.enterRoot()
	defer in.exitRoot()
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
	return in.callFuncNamed(decl, self, args, nil, decl.Span)
}

// callFuncNamed binds a direct call site: positional prefix, then
// named arguments, then defaults for whatever is unfilled. Defaults
// evaluate per call, left to right, with earlier params in scope.
func (in *Interp) callFuncNamed(decl *ast.FuncDecl, self Value, args []Value, names []string, at source.Span) Value {
	return in.callFuncNamedIn(in.global, decl, self, args, names, at)
}

// callFuncNamedIn: base is the env the body resolves against — the
// global env, or a nested fn's private items env (never enclosing
// locals: nested fns do not capture).
func (in *Interp) callFuncNamedIn(base *Env, decl *ast.FuncDecl, self Value, args []Value, names []string, at source.Span) Value {
	if in.constEval {
		panic(rtErr{at, fmt.Sprintf("a const initializer cannot call %s — pure expressions only (comptime fn evaluation arrives later)", decl.Name)})
	}
	n := len(decl.Params)
	if len(args) > n {
		panic(rtErr{at, fmt.Sprintf("%s takes %d argument(s), got %d",
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
			panic(rtErr{at, fmt.Sprintf("%s has no parameter %q", decl.Name, names[i])})
		}
		if filled[idx] {
			panic(rtErr{at, fmt.Sprintf("%s: parameter %q given twice (positionally and by name)", decl.Name, names[i])})
		}
		slots[idx], filled[idx] = a, true
	}
	env := newEnv(base, true)
	env.retErr = resultErrType(decl.RetType)
	if self != nil {
		env.declare("self", self, decl.Self == ast.MutSelf, decl.Span)
	}
	for i, prm := range decl.Params {
		v := slots[i]
		if !filled[i] {
			if prm.Default == nil {
				panic(rtErr{at, fmt.Sprintf("%s is missing its %q argument", decl.Name, prm.Name)})
			}
			dv, sg := in.eval(prm.Default, env)
			if sg != nil {
				panic(rtErr{at, fmt.Sprintf("the default for %q cannot return or propagate an error", prm.Name)})
			}
			v = dv
		}
		env.declare(prm.Name, v, false, decl.Span)
	}
	if in.isGenerator(decl) {
		return in.runGenerator(decl.Body, env, decl.Span)
	}
	v, sg := in.evalBlock(decl.Body, env)
	if sg != nil {
		return sg.val
	}
	// The tail-value rule (DESIGN.md, Syntax): no declared return
	// type means a meaningful tail value is an error, not a silent
	// discard.
	if decl.RetType == nil {
		if _, isUnit := v.(UnitV); !isUnit {
			panic(rtErr{decl.Span, fmt.Sprintf(
				"%s declares no return value but its body ends with a %s; discard it with `_ = …` or declare `-> %s`",
				decl.Name, typeName(v), typeName(v))})
		}
	}
	return v
}

func (in *Interp) callValue(fnv Value, args []Value, at source.Span) Value {
	if in.constEval {
		if b, isB := fnv.(*BuiltinV); !isB || (b.Name != "Ok" && b.Name != "Err" && b.Name != "Some") {
			panic(rtErr{at, "a const initializer may only call pure constructors (Ok/Err/Some) and value methods — comptime fn evaluation arrives later"})
		}
	}
	switch f := fnv.(type) {
	case TypeV:
		// A primitive numeric type's name is its conversion: u8(n).
		if b, ok := types.Primitives[string(f)]; ok {
			if !types.Numeric(b) {
				panic(rtErr{at, fmt.Sprintf("%s is not a conversion (conversion is defined between numbers and Rune only)", b)})
			}
			return convert(b, one(string(f), args, at), at)
		}
		// A distinct type's name is its constructor: NoteId(7).
		// Explicit construction is the entire point of distinct.
		td := in.prog.Types[string(f)]
		if td != nil && td.Distinct != nil {
			v := one(string(f), args, at)
			base := td.Distinct.String()
			if got := typeName(v); got != base {
				panic(rtErr{at, fmt.Sprintf("%s wraps %s, got %s (no implicit conversion)", f, base, got)})
			}
			return &DistinctV{Type: string(f), V: v}
		}
		panic(rtErr{at, fmt.Sprintf("%s is not callable (structs use braces: %s{ … })", f, f)})
	case *FuncV:
		base := in.global
		if f.Items != nil {
			base = f.Items
		}
		return in.callFuncNamedIn(base, f.Decl, nil, args, nil, f.Decl.Span)
	case *BuiltinV:
		return f.Fn(in, args, at)
	case *ClosureV:
		if len(args) != len(f.Params) {
			panic(rtErr{at, fmt.Sprintf("closure takes %d argument(s), got %d", len(f.Params), len(args))})
		}
		env := newEnv(f.Env, true)
		for i, p := range f.Params {
			env.declare(p, args[i], false, at)
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
	panic(rtErr{at, fmt.Sprintf("%s is not callable", typeName(fnv))})
}

// findMethod resolves a self-method on a type: the type's own
// (inherent or trait-impl) methods win; otherwise a default from a
// trait the type declares. Two traits both providing an unoverridden
// default is ambiguous — an error naming both.
func (in *Interp) findMethod(typeName, method string, at source.Span) *ast.FuncDecl {
	if m := in.prog.Methods[typeName][method]; m != nil {
		return m
	}
	var found *ast.FuncDecl
	var from string
	for _, trName := range in.prog.TypeTraits[typeName] {
		tr := in.prog.Traits[trName]
		if tr == nil {
			continue // conformance asserted to an undeclared trait
		}
		for _, fn := range tr.Fns {
			if fn.Name == method && fn.Body != nil {
				if found != nil {
					panic(rtErr{at, fmt.Sprintf("%s.%s is ambiguous: both %s and %s provide a default (override it on %s)",
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

// resultErrType names E from a declared `Result<T, E>` return type;
// "" for any other shape (no conversion target).
//
// M1-M3 sliced this out of a display string, hand-counting angle
// brackets to find the top-level comma. With a real TypeExpr it is an
// index. Note it yields the *bare* name: `typeName()` and the method
// table are both keyed by name, so `Result<_, Foo<T>>` must look up
// "Foo" — the old string form produced "Foo<T>", which matched
// nothing and silently skipped the conversion.
func resultErrType(ret *ast.TypeExpr) string {
	if ret == nil || ret.Optional || ret.Kind != ast.TypeName {
		return ""
	}
	if ret.Name != "Result" || len(ret.Args) != 2 {
		return ""
	}
	return ret.Args[1].Name
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
			panic(rtErr{d.Span, "a defer block cannot return"})
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
			items.declare(fs.Decl.Name, fv, false, fs.Decl.Span)
			env.declare(fs.Decl.Name, fv, false, fs.Decl.Span)
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
				panic(rtErr{st.Span, fmt.Sprintf("let pattern does not match %s", render(v, true))})
			}
			_, esg := in.evalBlock(st.Else, newEnv(env, false))
			if esg != nil {
				return UnitV{}, esg
			}
			panic(rtErr{st.Span, "the else block of `let … else` must diverge (return or exit), but it ran off the end"})
		}
		for _, b := range binds {
			env.declare(b.name, b.val, b.mut, st.Span)
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
	panic(rtErr{source.Span{}, fmt.Sprintf("unhandled statement %T", s)})
}

func (in *Interp) evalAssign(st *ast.AssignStmt, env *Env) *sig {
	// `_ = expr`: evaluate, discard.
	if id, ok := st.Target.(*ast.IdentExpr); ok && id.Name == "_" {
		_, sg := in.eval(st.Value, env)
		return sg
	}
	in.requireMutRoot(st.Target, env, st.Span)

	rhs, sg := in.eval(st.Value, env)
	if sg != nil {
		return sg
	}

	switch t := st.Target.(type) {
	case *ast.IdentExpr:
		if st.Op != "=" {
			b := env.lookup(t.Name)
			if b == nil {
				panic(rtErr{st.Span, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", t.Name)})
			}
			rhs = in.binop(strings.TrimSuffix(st.Op, "="), b.v, rhs, st.Span)
		}
		env.assign(t.Name, rhs, st.Span)
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
			k := hashable(idx, st.Span)
			if st.Op != "=" {
				cur, ok := o.get(k)
				if !ok {
					panic(rtErr{st.Span, fmt.Sprintf("%s on a key that is not present; read with ?? or insert with = first", st.Op)})
				}
				rhs = in.binop(strings.TrimSuffix(st.Op, "="), cur, rhs, st.Span)
			}
			o.set(k, rhs)
			return nil
		case *ListV:
			i, ok := idx.(IntV)
			if !ok {
				panic(rtErr{st.Span, "list index must be an Int"})
			}
			if i < 0 || int(i) >= len(o.Elems) {
				panic(rtErr{st.Span, fmt.Sprintf("list index %d out of range (len %d)", i, len(o.Elems))})
			}
			if st.Op != "=" {
				rhs = in.binop(strings.TrimSuffix(st.Op, "="), o.Elems[i], rhs, st.Span)
			}
			o.Elems[i] = rhs
			return nil
		}
		panic(rtErr{st.Span, fmt.Sprintf("cannot index-assign into %s", typeName(obj))})
	case *ast.Field:
		obj, sg := in.eval(t.X, env)
		if sg != nil {
			return sg
		}
		sv, ok := obj.(*StructV)
		if !ok {
			panic(rtErr{st.Span, fmt.Sprintf("cannot assign a field on %s", typeName(obj))})
		}
		cur, ok := sv.Fields[t.Name]
		if !ok {
			panic(rtErr{st.Span, fmt.Sprintf("%s has no field %q", sv.Type, t.Name)})
		}
		if st.Op != "=" {
			rhs = in.binop(strings.TrimSuffix(st.Op, "="), cur, rhs, st.Span)
		}
		sv.Fields[t.Name] = rhs
		return nil
	}
	panic(rtErr{st.Span, "invalid assignment target"})
}

// requireMutRoot walks an assignment target to its root name and
// requires a mut binding — mutability is transitive through paths.
func (in *Interp) requireMutRoot(target ast.Expr, env *Env, at source.Span) {
	e := target
	for {
		switch t := e.(type) {
		case *ast.IdentExpr:
			b := env.lookup(t.Name)
			if b == nil {
				panic(rtErr{at, fmt.Sprintf("assignment to undeclared name %q (declare it with let)", t.Name)})
			}
			if !b.mut {
				panic(rtErr{at, fmt.Sprintf("cannot mutate through immutable binding %q (declare it with `let mut`)", t.Name)})
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
			panic(rtErr{at, "cannot mutate a temporary value (bind it with `let mut` first)"})
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
		next := in.iterate(it, st.Span)
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
				panic(rtErr{st.Span, fmt.Sprintf("for pattern does not match %s", render(v, true))})
			}
			for _, b := range binds {
				iterEnv.declare(b.name, b.val, b.mut, st.Span)
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
				panic(rtErr{st.Span, fmt.Sprintf("loop condition must be Bool, got %s", typeName(c))})
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
				panic(rtErr{pt.Span, fmt.Sprintf(
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
					panic(rtErr{pt.Span, fmt.Sprintf("%s has no field %q", pt.Type, f.Name)})
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
			panic(rtErr{pt.Span, fmt.Sprintf(
				"struct pattern %s{…} names %d of %d fields; mention them all, or end with `..` for a deliberate partial match",
				pt.Type, len(pt.Fields), len(sv.Fields))})
		}
		var all []bound
		for _, f := range pt.Fields {
			fv, ok := sv.Fields[f.Name]
			if !ok {
				panic(rtErr{pt.Span, fmt.Sprintf("%s has no field %q", pt.Type, f.Name)})
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
			_, isSome := v.(*SomeV)
			_, wasNone := v.(NoneV)
			return nil, wasNone && !isSome && len(pt.Args) == 0
		case "Some":
			inner, isSome := v.(*SomeV)
			if !isSome || len(pt.Args) != 1 {
				return nil, false
			}
			return match(pt.Args[0], inner.V)
		case "Ok", "Err":
			rv, isRes := v.(*ResultV)
			if !isRes || rv.Ok != (pt.Name == "Ok") || len(pt.Args) != 1 {
				return nil, false
			}
			return match(pt.Args[0], rv.V)
		}
		// Distinct pattern mirrors construction: NoteId(x) binds the
		// wrapped value.
		if dv, isDist := v.(*DistinctV); isDist {
			if dv.Type != pt.Name || len(pt.Args) != 1 {
				return nil, false
			}
			return match(pt.Args[0], dv.V)
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

// eval is evalRaw plus the implicit T -> T? coercion. Option is boxed,
// so a bare T flowing into a T? slot has to be wrapped, and the
// checker recorded exactly where. One chokepoint rather than a wrap at
// each of the dozen syntactic sites where the coercion is legal —
// missing one of those would be a silent wrong value, which is the
// bug class boxing exists to remove.
func (in *Interp) eval(e ast.Expr, env *Env) (Value, *sig) {
	v, sg := in.evalRaw(e, env)
	if sg != nil {
		return v, sg
	}
	return in.wrapIf(e, v), nil
}

func (in *Interp) evalRaw(e ast.Expr, env *Env) (Value, *sig) {
	switch ex := e.(type) {
	case *ast.IntLit:
		// A literal is a magnitude; the checker says what type it
		// landed in. Two types change the representation: Float,
		// because an integer literal in a float context is a float
		// (`let f: Float = 5` then `f / 2` is 2.5, not 2), and u64,
		// the one integer type whose values an i64 cannot all hold.
		// The narrower sized types are still IntV — the runtime gap
		// recorded in DESIGN-DECISIONS.md.
		return intLit(ex, in.landed(ex)), nil
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
				next := in.iterate(v, sp.Span)
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
			mv.set(hashable(k, source.Span{}), v)
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
			panic(rtErr{ex.Span, "range bounds must be Int"})
		}
		end := int64(h)
		if ex.Incl {
			// ..= desugars to the half-open range one past hi.
			if end == math.MaxInt64 {
				panic(rtErr{ex.Span, "..= cannot include the maximum Int"})
			}
			end++
		}
		return RangeV{Lo: int64(l), Hi: end}, nil
	case *ast.Unary:
		// `-<literal>` is one constant in the source, not a negation
		// applied to a value, so it is folded here rather than
		// evaluated. That is the only way to write i64's minimum: the
		// magnitude 2^63 has no positive Int to be negated *from*, and
		// evaluating it stepwise would trap on the way past.
		if lit, isLit := ex.X.(*ast.IntLit); isLit && ex.Op == "-" {
			return negate(intLit(lit, in.landed(ex))), nil
		}
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		switch ex.Op {
		case "-":
			if n, ok := negateChecked(v, ex.Span); ok {
				return n, nil
			}
			panic(rtErr{ex.Span, fmt.Sprintf("cannot negate %s", typeName(v))})
		case "!":
			b, ok := v.(BoolV)
			if !ok {
				panic(rtErr{ex.Span, fmt.Sprintf("! requires Bool, got %s", typeName(v))})
			}
			return !b, nil
		}
	case *ast.Binary:
		return in.evalBinary(ex, env)
	case *ast.BlockExpr:
		return in.evalBlock(ex.Body, newEnv(env, false))
	case *ast.ScopeExpr:
		return in.evalScope(ex, env)
	case *ast.SelectExpr:
		return in.evalSelect(ex, env)
	case *ast.If:
		return in.evalIf(ex, env)
	case *ast.Closure:
		// Capture the *bindings* visible right now, not the names:
		// a later `let x` redeclaration creates a new binding and
		// must not retarget closures that captured the old one.
		// Binding cells are shared, so mutation through a captured
		// `mut` variable stays visible both ways.
		// Annotations are the checker's business; the evaluator needs
		// only the names.
		names := make([]string, len(ex.Params))
		for i, prm := range ex.Params {
			names[i] = prm.Name
		}
		return &ClosureV{Params: names, BodyExpr: ex.BodyExpr, BodyBlock: ex.BodyBlock, Env: env.capture()}, nil
	case *ast.Try:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		r, ok := v.(*ResultV)
		if !ok {
			panic(rtErr{ex.Span, fmt.Sprintf("? requires a Result, got %s", typeName(v))})
		}
		if r.Ok {
			return r.V, nil
		}
		// Conversion fires at the propagation point (recorded): when
		// the enclosing fn declares Result<_, E> and the error isn't
		// already an E, E.from converts it — Rust's From, in the
		// trait-less associated-fn form until the checker era.
		if target := env.fnRetErr(); target != "" && typeName(r.V) != target {
			if m := in.prog.Methods[target]["from"]; m != nil && m.Self == ast.NoSelf {
				conv := in.callFuncNamed(m, nil, []Value{r.V}, nil, ex.Span)
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
			panic(rtErr{ex.Span, fmt.Sprintf(".%d requires a tuple, got %s", ex.N, typeName(v))})
		}
		if ex.N >= len(tv) {
			panic(rtErr{ex.Span, fmt.Sprintf("tuple has no field .%d (size %d)", ex.N, len(tv))})
		}
		return tv[ex.N], nil
	case *ast.Index:
		return in.evalIndex(ex, env)
	case *ast.Field:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		// Duration suffixes read as fields: 250.ms, 0.5.s (Kotlin's
		// extension properties, not calls).
		if unit, ok := durationUnits[ex.Name]; ok {
			switch n := v.(type) {
			case IntV:
				return DurationV(time.Duration(n) * unit), nil
			case FloatV:
				return DurationV(time.Duration(float64(n) * float64(unit))), nil
			}
		}
		if tv, isType := v.(TypeV); isType {
			// Namespaced variant: Color.Red, Shape.Circle (ctor).
			if vi, ok := in.prog.Variants[ex.Name]; ok && vi.Type == string(tv) {
				return in.variantValue(ex.Name, vi, ex.Span), nil
			}
			panic(rtErr{ex.Span, fmt.Sprintf("%s has no variant %q", tv, ex.Name)})
		}
		if vv, isVar := v.(*VariantV); isVar {
			for i, fn := range vv.FieldNames {
				if fn == ex.Name {
					return vv.Args[i], nil
				}
			}
			panic(rtErr{ex.Span, fmt.Sprintf("%s has no field %q", vv.Name, ex.Name)})
		}
		st, ok := v.(*StructV)
		if !ok {
			panic(rtErr{ex.Span, fmt.Sprintf("%s has no field %q (methods need call parens)", typeName(v), ex.Name)})
		}
		fv, ok := st.Fields[ex.Name]
		if !ok {
			panic(rtErr{ex.Span, fmt.Sprintf("%s has no field %q", st.Type, ex.Name)})
		}
		return fv, nil
	case *ast.StructLit:
		return in.evalStructLit(ex, env)
	case *ast.DotName:
		vi, ok := in.prog.Variants[ex.Name]
		if !ok {
			panic(rtErr{ex.Span, fmt.Sprintf(".%s: no type declares a variant %q", ex.Name, ex.Name)})
		}
		return in.variantValue(ex.Name, vi, ex.Span), nil
	case *ast.Match:
		return in.evalMatch(ex, env)
	case *ast.CondMatch:
		return in.evalCondMatch(ex, env)
	case *ast.IfLet:
		return in.evalIfLet(ex, env)
	case *ast.Call:
		return in.evalCall(ex, env)
	}
	panic(rtErr{source.Span{}, fmt.Sprintf("unhandled expression %T", e)})
}

// landed reports the type the checker settled an expression into, or
// nil when it did not record one (an untyped constant that nothing
// constrained, or a node the checker did not model).
func (in *Interp) landed(e ast.Expr) types.Type {
	if in.info == nil {
		return nil
	}
	return in.info.Types[e]
}

// intLit builds the value of an integer literal at the type it landed
// in. The default — nothing recorded, or a type with no separate
// representation — is Int.
//
// No range check here: the checker's FitsIn already proved the literal
// fits, and a negative literal arrives as its magnitude with the sign
// still to be applied by negate — so `-128` at i8 is momentarily the
// out-of-range 128 on its way to being in range.
func intLit(lit *ast.IntLit, t types.Type) Value {
	switch t {
	case types.U64:
		return UintV(lit.V)
	case types.Float, types.F32:
		return FloatV(lit.V)
	}
	if b, ok := t.(*types.Basic); ok {
		if s, sized := sizedZero(b); sized {
			s.V = int64(lit.V)
			return s
		}
	}
	return IntV(int64(lit.V))
}

// negate flips a folded literal. The Int case is written as an
// unsigned two's-complement negation because i64's minimum has no
// positive counterpart to subtract from.
func negate(v Value) Value {
	switch n := v.(type) {
	case IntV:
		return IntV(-int64(n))
	case FloatV:
		return -n
	case SizedV:
		n.V = -n.V
		return n
	}
	return v
}

func (in *Interp) evalIdent(ex *ast.IdentExpr, env *Env) (Value, *sig) {
	if ex.Name == "_" {
		panic(rtErr{ex.Span, "_ discards; it cannot be read"})
	}
	if b := env.lookup(ex.Name); b != nil {
		return b.v, nil
	}
	if fn, ok := in.prog.Fns[ex.Name]; ok {
		return &FuncV{Decl: fn}, nil
	}
	if in.prog.Imports[ex.Name] {
		return ModuleV(ex.Name), nil
	}
	if ex.Name == "None" {
		return NoneV{}, nil
	}
	if _, ok := in.prog.Types[ex.Name]; ok {
		return TypeV(ex.Name), nil
	}
	// A primitive type's name is a value, so that calling a numeric
	// one is a conversion: `u8(n)`. Reached after the local lookup, so
	// a `let u8 = 5` shadows it exactly as it shadows a predeclared
	// name in Go. Bool and String resolve too, and fail at the call
	// with what is actually wrong rather than "undefined name".
	if _, ok := types.Primitives[ex.Name]; ok {
		return TypeV(ex.Name), nil
	}
	if vi, ok := in.prog.Variants[ex.Name]; ok {
		panic(rtErr{ex.Span, fmt.Sprintf(
			"variants are namespaced: write .%s or %s.%s (bare variant names are pattern-only)",
			ex.Name, vi.Type, ex.Name)})
	}
	if b, ok := builtins[ex.Name]; ok {
		return b, nil
	}
	panic(rtErr{ex.Span, fmt.Sprintf("undefined name %q", ex.Name)})
}

func (in *Interp) evalStructLit(ex *ast.StructLit, env *Env) (Value, *sig) {
	td, ok := in.prog.Types[ex.Type]
	if !ok || td.Fields == nil {
		if vi, isVar := in.prog.Variants[ex.Type]; isVar && vi.Fields != nil {
			return in.evalVariantLit(ex, vi, env)
		}
		panic(rtErr{ex.Span, fmt.Sprintf("%q is not a struct type", ex.Type)})
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
			panic(rtErr{ex.Span, fmt.Sprintf("..base must be a %s, got %s", ex.Type, typeName(base))})
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
			panic(rtErr{ex.Span, fmt.Sprintf("%s has no field %q", ex.Type, name)})
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
			panic(rtErr{ex.Span, fmt.Sprintf("missing field %q in %s literal (no zero values)", fd.Name, ex.Type)})
		}
	}
	return sv, nil
}

// evalVariantLit constructs a named-field variant: NotFound{ id: 7 }.
// Every declared field, exactly once; ..base is a struct affair.
func (in *Interp) evalVariantLit(ex *ast.StructLit, vi program.Variant, env *Env) (Value, *sig) {
	if ex.Base != nil {
		panic(rtErr{ex.Span, fmt.Sprintf("..base is for structs; %s is a variant of %s", ex.Type, vi.Type)})
	}
	given := map[string]Value{}
	for i, name := range ex.Names {
		v, sg := in.eval(ex.Vals[i], env)
		if sg != nil {
			return UnitV{}, sg
		}
		given[name] = v
	}
	vv := &VariantV{Type: vi.Type, Name: ex.Type, FieldNames: vi.Fields}
	for _, f := range vi.Fields {
		v, ok := given[f]
		if !ok {
			panic(rtErr{ex.Span, fmt.Sprintf("%s is missing field %q (no zero values — every field is named)", ex.Type, f)})
		}
		vv.Args = append(vv.Args, v)
		delete(given, f)
	}
	for name := range given {
		panic(rtErr{ex.Span, fmt.Sprintf("%s has no field %q", ex.Type, name)})
	}
	return vv, nil
}

// variantValue resolves a variant reference: an arity-0 variant is
// its value; a positional-payload variant is its constructor; a
// named-field variant needs braces.
func (in *Interp) variantValue(name string, vi program.Variant, at source.Span) Value {
	if vi.Fields != nil {
		panic(rtErr{at, fmt.Sprintf("%s has named fields; construct it with %s{ … }", name, name)})
	}
	if vi.Arity == 0 {
		return &VariantV{Type: vi.Type, Name: name}
	}
	return &BuiltinV{Name: name, Fn: func(_ *Interp, args []Value, l source.Span) Value {
		if len(args) != vi.Arity {
			panic(rtErr{l, fmt.Sprintf("%s takes %d argument(s), got %d", name, vi.Arity, len(args))})
		}
		return &VariantV{Type: vi.Type, Name: name, Args: args}
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
			armEnv.declare(b.name, b.val, b.mut, arm.Span)
		}
		if arm.Guard != nil {
			g, sg := in.eval(arm.Guard, armEnv)
			if sg != nil {
				return UnitV{}, sg
			}
			gb, isBool := g.(BoolV)
			if !isBool {
				panic(rtErr{arm.Span, fmt.Sprintf("match guard must be Bool, got %s", typeName(g))})
			}
			if !gb {
				continue
			}
		}
		return in.eval(arm.Body, armEnv)
	}
	// The checker rejects a match it can prove incomplete, so reaching
	// here means one it could not judge — a literal arm set, a nested
	// pattern past the analysis depth. Kept as the belt to the
	// checker's braces, per DESIGN.md's open question on the
	// dynamically-enforced rules: the runtime keeps its copy as an
	// assertion so a gap surfaces loudly rather than as a wrong value.
	panic(rtErr{ex.Span, fmt.Sprintf("no match arm matched %s", render(x, true))})
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
				panic(rtErr{arm.Span, fmt.Sprintf("subjectless match arm must be Bool, got %s", typeName(c))})
			}
			if !cb {
				continue
			}
		}
		return in.eval(arm.Body, newEnv(env, false))
	}
	panic(rtErr{ex.Span, "no match arm was true (add a `_ =>` arm)"})
}

func (in *Interp) evalIfLet(ex *ast.IfLet, env *Env) (Value, *sig) {
	x, sg := in.eval(ex.X, env)
	if sg != nil {
		return UnitV{}, sg
	}
	if _, wasNone := x.(NoneV); wasNone {
		if ex.ElseIf != nil {
			return in.eval(ex.ElseIf, env)
		}
		if ex.ElseBlock != nil {
			return in.evalBlock(ex.ElseBlock, newEnv(env, false))
		}
		return UnitV{}, nil
	}
	// `if let` is a one-armed match, and the scrutinee is unwrapped
	// only for a plain binding: `if let root = self.root` takes the
	// value out of the Option, while `if let Err(Timeout) = r` matches
	// the pattern against the Result itself. A CtorPat therefore sees
	// the box, and Some(n) destructures it like any other constructor.
	scrut := x
	if boxed, isSome := x.(*SomeV); isSome {
		if _, plain := ex.Pat.(*ast.IdentPat); plain {
			scrut = boxed.V
		}
	}
	binds, ok := match(ex.Pat, scrut)
	if !ok {
		panic(rtErr{ex.Span, fmt.Sprintf("if let pattern does not match %s", render(x, true))})
	}
	thenEnv := newEnv(env, false)
	for _, b := range binds {
		thenEnv.declare(b.name, b.val, b.mut, ex.Span)
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
			s = formatSpec(v, part.Spec, part.Span)
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
func formatSpec(v Value, spec string, at source.Span) string {
	bad := func(msg string) string {
		panic(rtErr{at, fmt.Sprintf("format spec %q: %s", spec, msg)})
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
		if _, wasNone := l.(NoneV); wasNone {
			return in.eval(ex.R, env)
		}
		if boxed, isSome := l.(*SomeV); isSome {
			return boxed.V, nil // ?? unwraps a present Option
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
			panic(rtErr{ex.Span, fmt.Sprintf("%s requires Bool, got %s", ex.Op, typeName(l))})
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
			panic(rtErr{ex.Span, fmt.Sprintf("%s requires Bool, got %s", ex.Op, typeName(r))})
		}
		return rb, nil
	}
	r, sg := in.eval(ex.R, env)
	if sg != nil {
		return UnitV{}, sg
	}
	return in.binop(ex.Op, l, r, ex.Span), nil
}

func (in *Interp) binop(op string, l, r Value, at source.Span) Value {
	// Equality is structural for every comparable type; ordered
	// comparisons stay per-type below.
	switch op {
	case "==":
		// Equality stays structural and universal: no Eq trait, no
		// declaration, no way to redefine it (DESIGN.md). Ord governs
		// ordering only.
		return BoolV(eq(l, r, at))
	case "!=":
		return BoolV(!eq(l, r, at))
	}
	// Ordering a user type goes through its `cmp`, which the checker
	// has already established exists — `impl Ord for T` is verified,
	// and `<` on a type that never declared it is a compile error.
	if isOrderOp(op) {
		if n, ok := in.userCmp(l, r, at); ok {
			switch op {
			case "<":
				return BoolV(n < 0)
			case "<=":
				return BoolV(n <= 0)
			case ">":
				return BoolV(n > 0)
			}
			return BoolV(n >= 0)
		}
	}
	if li, ok := l.(IntV); ok {
		if ri, ok := r.(IntV); ok {
			// Overflow traps, in every tier and at every width. The
			// escape hatch is named at the point of failure because
			// the answer to "my checksum keeps trapping" is a
			// different operator, not a different build.
			switch op {
			case "+":
				c := li + ri
				if (c > li) != (ri > 0) {
					panic(rtErr{at, fmt.Sprintf("Int overflow: %d + %d (use wrapping_add for modular arithmetic)", li, ri)})
				}
				return c
			case "-":
				c := li - ri
				if (c < li) != (ri > 0) {
					panic(rtErr{at, fmt.Sprintf("Int overflow: %d - %d (use wrapping_sub for modular arithmetic)", li, ri)})
				}
				return c
			case "*":
				c := li * ri
				if li != 0 && (c/li != ri || (li == -1 && ri == math.MinInt64)) {
					panic(rtErr{at, fmt.Sprintf("Int overflow: %d * %d (use wrapping_mul for modular arithmetic)", li, ri)})
				}
				return c
			case "/":
				if ri == 0 {
					panic(rtErr{at, "division by zero"})
				}
				if li == math.MinInt64 && ri == -1 {
					panic(rtErr{at, fmt.Sprintf("Int overflow: %d / -1 (use wrapping_neg for modular arithmetic)", li)})
				}
				return li / ri
			case "%":
				if ri == 0 {
					panic(rtErr{at, "division by zero"})
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
	// The narrow widths, which carry their own size. Placed before the
	// u64 block only for reading order; the type switches are disjoint.
	if ls, ok := l.(SizedV); ok {
		if rs, ok := r.(SizedV); ok {
			return sizedBinop(op, ls, rs, at)
		}
	}
	// u64: unsigned arithmetic, trapping on overflow the way Int does.
	// There is deliberately no UintV/IntV case — mixing a u64 with an
	// Int is a compile error, so reaching this code with one would
	// mean the checker let it through.
	if lu, ok := l.(UintV); ok {
		if ru, ok := r.(UintV); ok {
			switch op {
			case "+":
				c := lu + ru
				if c < lu {
					panic(rtErr{at, fmt.Sprintf("u64 overflow: %d + %d (use wrapping_add for modular arithmetic)", lu, ru)})
				}
				return c
			case "-":
				if ru > lu {
					panic(rtErr{at, fmt.Sprintf("u64 overflow: %d - %d (use wrapping_sub for modular arithmetic)", lu, ru)})
				}
				return lu - ru
			case "*":
				c := lu * ru
				if lu != 0 && c/lu != ru {
					panic(rtErr{at, fmt.Sprintf("u64 overflow: %d * %d (use wrapping_mul for modular arithmetic)", lu, ru)})
				}
				return c
			case "/":
				if ru == 0 {
					panic(rtErr{at, "division by zero"})
				}
				return lu / ru
			case "%":
				if ru == 0 {
					panic(rtErr{at, "division by zero"})
				}
				return lu % ru
			case "<":
				return BoolV(lu < ru)
			case "<=":
				return BoolV(lu <= ru)
			case ">":
				return BoolV(lu > ru)
			case ">=":
				return BoolV(lu >= ru)
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
	// Time arithmetic — the ratified minimal set. No Instant+Instant
	// (meaningless), no Duration/Duration (Go's float-division wart).
	if ld, ok := l.(DurationV); ok {
		switch rv := r.(type) {
		case DurationV:
			switch op {
			case "+":
				return ld + rv
			case "-":
				return ld - rv
			case "<":
				return BoolV(ld < rv)
			case "<=":
				return BoolV(ld <= rv)
			case ">":
				return BoolV(ld > rv)
			case ">=":
				return BoolV(ld >= rv)
			}
		case IntV:
			switch op {
			case "*":
				return ld * DurationV(rv)
			case "/":
				if rv == 0 {
					panic(rtErr{at, "division by zero"})
				}
				return ld / DurationV(rv)
			}
		}
	}
	if li, ok := l.(IntV); ok {
		if rd, ok := r.(DurationV); ok && op == "*" {
			return DurationV(li) * rd // commutative convenience
		}
	}
	if lt, ok := l.(InstantV); ok {
		switch rv := r.(type) {
		case InstantV:
			switch op {
			case "-":
				return DurationV(lt.T.Sub(rv.T))
			case "<":
				return BoolV(lt.T.Before(rv.T))
			case "<=":
				return BoolV(!rv.T.Before(lt.T))
			case ">":
				return BoolV(lt.T.After(rv.T))
			case ">=":
				return BoolV(!lt.T.After(rv.T))
			}
		case DurationV:
			switch op {
			case "+":
				return InstantV{T: lt.T.Add(time.Duration(rv))}
			case "-":
				return InstantV{T: lt.T.Add(-time.Duration(rv))}
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
	panic(rtErr{at, fmt.Sprintf("operator %s not defined for %s and %s", op, typeName(l), typeName(r))})
}

func (in *Interp) evalIf(ex *ast.If, env *Env) (Value, *sig) {
	c, sg := in.eval(ex.Cond, env)
	if sg != nil {
		return UnitV{}, sg
	}
	b, ok := c.(BoolV)
	if !ok {
		panic(rtErr{ex.Span, fmt.Sprintf("if condition must be Bool, got %s", typeName(c))})
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
		// A map read is honest about absence: it returns an Option.
		// Boxed, so a key present holding None is distinguishable from
		// a key that is absent — the two read identically until M4c.
		v, ok := o.get(hashable(idx, ex.Span))
		if !ok {
			return NoneV{}, nil
		}
		return some(v), nil
	case *ListV:
		i, ok := idx.(IntV)
		if !ok {
			panic(rtErr{ex.Span, "list index must be an Int"})
		}
		if i < 0 || int(i) >= len(o.Elems) {
			panic(rtErr{ex.Span, fmt.Sprintf("list index %d out of range (len %d)", i, len(o.Elems))})
		}
		return o.Elems[i], nil
	}
	panic(rtErr{ex.Span, fmt.Sprintf("cannot index %s", typeName(obj))})
}

func (in *Interp) evalCall(ex *ast.Call, env *Env) (Value, *sig) {
	// expect(...) is a special form: it keeps the argument's AST so a
	// failed comparison can report both sides.
	if id, ok := ex.Fn.(*ast.IdentExpr); ok && id.Name == "expect" && env.lookup("expect") == nil {
		return in.evalExpect(ex, env)
	}
	// channel(cap: n) — the one builtin with a named parameter; the
	// name is validated here and the call proceeds positionally.
	if id, ok := ex.Fn.(*ast.IdentExpr); ok && id.Name == "channel" && ex.Names != nil {
		if len(ex.Names) != 1 || ex.Names[0] != "cap" {
			panic(rtErr{ex.Span, "channel takes no arguments or (cap: n)"})
		}
		ex = &ast.Call{Fn: ex.Fn, Args: ex.Args, Span: ex.Span}
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
			return in.moduleCall(string(mod), f.Name, args, ex.Span), nil
		}
		// Associated functions: Tree.new()
		if tv, isType := recv.(TypeV); isType {
			// Namespaced variant constructor: Shape.Circle(2).
			if vi, ok := in.prog.Variants[f.Name]; ok && vi.Type == string(tv) {
				rejectNamed(ex, "variant constructors")
				return in.callValue(in.variantValue(f.Name, vi, ex.Span), args, ex.Span), nil
			}
			m := in.prog.Methods[string(tv)][f.Name]
			if m == nil {
				panic(rtErr{ex.Span, fmt.Sprintf("type %s has no associated function %q", tv, f.Name)})
			}
			if m.Self != ast.NoSelf {
				panic(rtErr{ex.Span, fmt.Sprintf("%s.%s is a method; call it on a value", tv, f.Name)})
			}
			return in.callFuncNamed(m, nil, args, ex.Names, ex.Span), nil
		}
		// value() unwraps a distinct type — the one built-in escape
		// hatch (explicit, so the conversion is visible at the site).
		if dv, isDist := recv.(*DistinctV); isDist && f.Name == "value" {
			if len(args) != 0 {
				panic(rtErr{ex.Span, "value takes no arguments"})
			}
			return dv.V, nil
		}
		// User-defined methods on structs and variants; a trait
		// default fills in when the type doesn't override.
		if tn := userTypeName(recv); tn != "" {
			m := in.findMethod(tn, f.Name, ex.Span)
			if m == nil {
				panic(rtErr{ex.Span, fmt.Sprintf("%s has no method %q", tn, f.Name)})
			}
			if m.Self == ast.NoSelf {
				panic(rtErr{ex.Span, fmt.Sprintf("%s.%s is an associated function; call it as %s.%s(…)", tn, f.Name, tn, f.Name)})
			}
			// A `mut self` method is callable only through a mut
			// path — receiver marking happens at the declaration,
			// not the call site, but the path rule still holds.
			if m.Self == ast.MutSelf {
				in.requireMutRoot(f.X, env, ex.Span)
			}
			return in.callFuncNamed(m, recv, args, ex.Names, ex.Span), nil
		}
		if builtinMutMethods[typeName(recv)+"."+f.Name] {
			in.requireMutRoot(f.X, env, ex.Span)
		}
		rejectNamed(ex, "builtin methods")
		return in.methodCall(recv, f.Name, args, ex.Span), nil
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
				return in.callFuncNamedIn(base, fv.Decl, nil, args, ex.Names, ex.Span), nil
			}
		} else if fn, isFn := in.prog.Fns[id.Name]; isFn {
			args, sg := in.evalArgs(ex.Args, env)
			if sg != nil {
				return UnitV{}, sg
			}
			return in.callFuncNamed(fn, nil, args, ex.Names, ex.Span), nil
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
	return in.callValue(fnv, args, ex.Span), nil
}

// rejectNamed guards call paths where named arguments cannot bind:
// only a declared function's signature carries parameter names.
func rejectNamed(ex *ast.Call, what string) {
	if ex.Names != nil {
		panic(rtErr{ex.Span, fmt.Sprintf("named arguments work on declared functions and methods, not %s", what)})
	}
}

func userTypeName(v Value) string {
	switch x := v.(type) {
	case *StructV:
		return x.Type
	case *VariantV:
		return x.Type
	case *DistinctV:
		return x.Type // impl NoteId { … } works like any user type
	}
	return ""
}

// evalExpect: expect(a == b) reports both sides on failure; any
// other Bool expression reports generically.
func (in *Interp) evalExpect(ex *ast.Call, env *Env) (Value, *sig) {
	if len(ex.Args) != 1 {
		panic(rtErr{ex.Span, "expect takes exactly one expression"})
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
			res := in.binop(b.Op, l, r, b.Span)
			if res == BoolV(true) {
				return UnitV{}, nil
			}
			panic(testFail{ex.Span, fmt.Sprintf("expect failed: left %s right\n  left:  %s\n  right: %s",
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
	panic(testFail{ex.Span, fmt.Sprintf("expect failed (value: %s)", render(v, true))})
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
