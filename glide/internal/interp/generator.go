package interp

import (
	"runtime"
	"sync"

	"glide/internal/ast"
)

// Generators: a function whose body contains `yield` returns an
// Iterator when called. The interpreter runs the body on its own
// goroutine and hands values across a channel — the cheapest correct
// lazy implementation for a tree-walker. (The transpiler will need
// CPS or a state machine; DESIGN.md records that as the lowering to
// prototype early. This is not that prototype — it proves semantics,
// not compilation.)
//
// Lifecycle: a consumer that drops the iterator early (take, or just
// walking away) would strand the goroutine on its next send, so the
// yield send also listens on a stop channel closed by a GC cleanup
// hook attached to the returned IterV.

// yieldKey is the env slot the generator's yield function lives in.
// No user identifier can collide: "yield" is a keyword.
const yieldKey = "yield"

type genStop struct{}

func (in *Interp) isGenerator(decl *ast.FuncDecl) bool {
	if v, ok := in.genCache[decl]; ok {
		return v
	}
	v := blockYields(decl.Body)
	in.genCache[decl] = v
	return v
}

func blockYields(b *ast.Block) bool {
	for _, s := range b.Stmts {
		if stmtYields(s) {
			return true
		}
	}
	return false
}

func stmtYields(s ast.Stmt) bool {
	switch st := s.(type) {
	case *ast.YieldStmt:
		return true
	case *ast.ForStmt:
		return blockYields(st.Body)
	case *ast.ExprStmt:
		return exprYields(st.E)
	case *ast.LetStmt:
		return st.Else != nil && blockYields(st.Else)
	}
	return false
}

// exprYields looks inside block-bearing expressions (if/match), but
// deliberately not into closures: a yield belongs to the function
// whose body it sits in, and a closure is a different function.
func exprYields(e ast.Expr) bool {
	switch ex := e.(type) {
	case *ast.If:
		if blockYields(ex.Then) {
			return true
		}
		if ex.ElseIf != nil && exprYields(ex.ElseIf) {
			return true
		}
		return ex.ElseBlock != nil && blockYields(ex.ElseBlock)
	case *ast.IfLet:
		if blockYields(ex.Then) {
			return true
		}
		return ex.ElseBlock != nil && blockYields(ex.ElseBlock)
	}
	return false
}

// runGenerator starts the body in a goroutine; env must already hold
// self/params. Returns the consumer-facing iterator.
func (in *Interp) runGenerator(body *ast.Block, env *Env, line int) *IterV {
	ch := make(chan Value)
	stop := make(chan struct{})
	var stopOnce sync.Once
	halt := func() { stopOnce.Do(func() { close(stop) }) }

	var crash any // rtErr etc. from the generator body, re-thrown at Next

	yield := &BuiltinV{Name: "yield", Fn: func(_ *Interp, args []Value, yline int) Value {
		select {
		case ch <- args[0]:
			return UnitV{}
		case <-stop:
			panic(genStop{})
		}
	}}
	env.vars[yieldKey] = &binding{v: yield}

	go func() {
		defer close(ch)
		defer func() {
			switch r := recover().(type) {
			case nil, genStop:
			default:
				crash = r // surfaced by Next; close(ch) orders the write
			}
		}()
		in.evalBlock(body, env)
	}()

	it := &IterV{}
	it.Next = func() (Value, bool) {
		v, ok := <-ch
		if !ok {
			if crash != nil {
				panic(crash)
			}
			return nil, false
		}
		return v, true
	}
	// If the consumer abandons the iterator, unblock the producer.
	runtime.AddCleanup(it, func(struct{}) { halt() }, struct{}{})
	return it
}

// evalYield handles both `yield v` and `yield from iterable`.
func (in *Interp) evalYield(st *ast.YieldStmt, env *Env) *sig {
	b := env.lookup(yieldKey)
	if b == nil {
		panic(rtErr{st.Line, "yield outside a generator"})
	}
	yield := b.v.(*BuiltinV)
	v, sg := in.eval(st.E, env)
	if sg != nil {
		return sg
	}
	if !st.From {
		yield.Fn(in, []Value{v}, st.Line)
		return nil
	}
	next := in.iterate(v, st.Line)
	for {
		item, ok := next()
		if !ok {
			return nil
		}
		yield.Fn(in, []Value{item}, st.Line)
	}
}
