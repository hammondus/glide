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
	"os"
	"strconv"
	"strings"

	"glide/internal/ast"
)

type Interp struct {
	Stdout io.Writer
	Stderr io.Writer
	Args   []string // what os.args() returns

	fns     map[string]*ast.FuncDecl
	imports map[string]bool
	global  *Env
}

// sig carries Glide control flow up the evaluator: an early return,
// either explicit (`return`) or from `?` propagating an Err.
type sig struct{ val Value }

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
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		fns:     map[string]*ast.FuncDecl{},
		imports: map[string]bool{},
	}
}

func (in *Interp) Run(f *ast.File) (err error) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case rtErr:
			err = fmt.Errorf("line %d: %s", p.line, p.msg)
		case exitPanic:
			err = &ExitError{Code: p.code}
		default:
			panic(p)
		}
	}()
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
		in.fns[fn.Name] = fn
	}
	mainFn, ok := in.fns["main"]
	if !ok {
		return fmt.Errorf("no main function")
	}
	if len(mainFn.Params) != 0 {
		return fmt.Errorf("line %d: main takes no parameters (use os.args())", mainFn.Line)
	}
	in.global = newEnv(nil, true)
	ret := in.callFunc(mainFn, nil)
	if r, isRes := ret.(*ResultV); isRes && !r.Ok {
		return fmt.Errorf("%s", display(r.V))
	}
	return nil
}

// Calls

func (in *Interp) callFunc(decl *ast.FuncDecl, args []Value) Value {
	if len(args) != len(decl.Params) {
		panic(rtErr{decl.Line, fmt.Sprintf("%s takes %d argument(s), got %d",
			decl.Name, len(decl.Params), len(args))})
	}
	env := newEnv(in.global, true)
	for i, p := range decl.Params {
		env.declare(p.Name, args[i], false, decl.Line)
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
	switch f := fnv.(type) {
	case *FuncV:
		return in.callFunc(f.Decl, args)
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

// Statements

func (in *Interp) evalBlock(b *ast.Block, env *Env) (Value, *sig) {
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
			panic(rtErr{line, "cannot assign through a temporary value"})
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
				return sg
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
				return sg
			}
		}
	default:
		for {
			if _, sg := in.evalBlock(st.Body, newEnv(env, false)); sg != nil {
				return sg
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
			panic(rtErr{0, "range bounds must be Int"})
		}
		return RangeV{Lo: int64(l), Hi: int64(h)}, nil
	case *ast.Unary:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		switch ex.Op {
		case "-":
			switch n := v.(type) {
			case IntV:
				return -n, nil
			case FloatV:
				return -n, nil
			}
			panic(rtErr{0, fmt.Sprintf("cannot negate %s", typeName(v))})
		case "!":
			b, ok := v.(BoolV)
			if !ok {
				panic(rtErr{0, fmt.Sprintf("! requires Bool, got %s", typeName(v))})
			}
			return !b, nil
		}
	case *ast.Binary:
		return in.evalBinary(ex, env)
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
		return UnitV{}, &sig{val: r} // propagate the Err to the caller
	case *ast.TupleIndex:
		v, sg := in.eval(ex.X, env)
		if sg != nil {
			return UnitV{}, sg
		}
		tv, ok := v.(TupleV)
		if !ok {
			panic(rtErr{0, fmt.Sprintf(".%d requires a tuple, got %s", ex.N, typeName(v))})
		}
		if ex.N >= len(tv) {
			panic(rtErr{0, fmt.Sprintf("tuple has no field .%d (size %d)", ex.N, len(tv))})
		}
		return tv[ex.N], nil
	case *ast.Index:
		return in.evalIndex(ex, env)
	case *ast.Field:
		panic(rtErr{ex.Line, fmt.Sprintf("%q is not callable here — field access on values arrives with structs (M2); did you forget ()?", ex.Name)})
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
	if b, ok := builtins[ex.Name]; ok {
		return b, nil
	}
	panic(rtErr{ex.Line, fmt.Sprintf("undefined name %q", ex.Name)})
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
			w, err := strconv.Atoi(part.Spec)
			if err != nil {
				panic(rtErr{0, fmt.Sprintf("unsupported format spec %q (only a width, e.g. {x:6})", part.Spec)})
			}
			s = fmt.Sprintf("%*s", w, s)
		}
		sb.WriteString(s)
	}
	return StrV(sb.String()), nil
}

func (in *Interp) evalBinary(ex *ast.Binary, env *Env) (Value, *sig) {
	l, sg := in.eval(ex.L, env)
	if sg != nil {
		return UnitV{}, sg
	}
	switch ex.Op {
	case "??":
		switch o := l.(type) {
		case SomeV:
			return o.V, nil
		case NoneV:
			return in.eval(ex.R, env)
		}
		panic(rtErr{ex.Line, fmt.Sprintf("?? requires an Option on the left, got %s", typeName(l))})
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
	if li, ok := l.(IntV); ok {
		if ri, ok := r.(IntV); ok {
			switch op {
			case "+":
				return li + ri
			case "-":
				return li - ri
			case "*":
				return li * ri
			case "/":
				if ri == 0 {
					panic(rtErr{line, "division by zero"})
				}
				return li / ri
			case "%":
				if ri == 0 {
					panic(rtErr{line, "division by zero"})
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
		return in.evalIf(ex.ElseIf, env)
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
		v, ok := o.get(hashable(idx, ex.Line))
		if !ok {
			return NoneV{}, nil
		}
		return SomeV{V: v}, nil
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
			return in.moduleCall(string(mod), f.Name, args, ex.Line), nil
		}
		return in.methodCall(recv, f.Name, args, ex.Line), nil
	}
	fnv, sg := in.eval(ex.Fn, env)
	if sg != nil {
		return UnitV{}, sg
	}
	args, sg := in.evalArgs(ex.Args, env)
	if sg != nil {
		return UnitV{}, sg
	}
	return in.callValue(fnv, args, ex.Line), nil
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
