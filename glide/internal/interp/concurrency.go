package interp

import (
	"fmt"
	"reflect"
	"sync"

	"glide/internal/ast"
)

// Structured concurrency (M3). Spawned tasks run on goroutines, but
// the interpreter itself is single-threaded by construction: a global
// runtime lock (in.gil) guards every eval, released only while a task
// is blocked (join; later channel ops and sleep). Tasks therefore
// interleave exactly at blocking operations — which is also the
// ratified cancellation-point rule, so the lock IS the semantics, not
// just a race guard. Release backends get real parallelism; programs
// can't tell the difference except in throughput.
//
// Cancellation is a Go panic carrying cancelUnwind: uncatchable by
// Glide code, and evalBlockDeferred's panic path already runs defers
// AND errdefers on the way out — exactly the ratified behavior
// ("values wait, bugs don't"; a cancelled transfer still rolls back).

// cancelUnwind unwinds a cancelled task. Only scope machinery
// recovers it.
type cancelUnwind struct{}

// taskCtx is the cancellation context of whatever the current
// goroutine is interpreting (main, a scope body, or a spawned task).
// It lives in in.cur, which is GIL-protected: every blocking site
// saves and restores it around the release.
type taskCtx struct {
	cancel <-chan struct{} // closed when this task's scope cancels; nil at top level
}

type scopeState struct {
	cancelled  chan struct{} // closed on cancel: early exit, child panic, outer cancel
	done       chan struct{} // closed when the scope has fully exited (frees the outer watcher)
	cancelOnce sync.Once
	doneOnce   sync.Once

	mu    sync.Mutex // guards tasks/panicVal (written by child goroutines)
	tasks []*TaskV
	pan   any // first child panic; the scope re-panics it at exit
}

func (st *scopeState) doCancel() { st.cancelOnce.Do(func() { close(st.cancelled) }) }

func (st *scopeState) recordPanic(p any) {
	st.mu.Lock()
	if st.pan == nil {
		st.pan = p
	}
	st.mu.Unlock()
}

// unblock releases the GIL, runs the (blocking) wait, and restores
// the goroutine's interpreter context on reacquire.
func (in *Interp) unblock(wait func()) {
	ctx := in.cur
	in.gil.Unlock()
	wait()
	in.gil.Lock()
	in.cur = ctx
}

// enterRoot/exitRoot bracket a top-level interpretation (Run, a test
// case): take the lock and install an uncancellable context.
func (in *Interp) enterRoot() {
	in.gil.Lock()
	in.cur = &taskCtx{}
}

func (in *Interp) exitRoot() { in.gil.Unlock() }

// evalScope: the four ratified rules live here.
//  1. Scope exit always joins every child; early exit cancels first.
//  2. A child's Err is a value in its handle until joined.
//  3. At normal exit an unjoined Err fails the scope (first spawned wins).
//  4. A child panic cancels siblings immediately; the scope re-panics.
func (in *Interp) evalScope(ex *ast.ScopeExpr, env *Env) (Value, *sig) {
	if ex.Timeout != nil || ex.Deadline != nil {
		panic(rtErr{ex.Line, "scope(timeout:/deadline:) arrives with the time types (in progress)"})
	}
	if in.constEval {
		panic(rtErr{ex.Line, "a const initializer cannot open a scope (pure expressions only)"})
	}
	st := &scopeState{cancelled: make(chan struct{}), done: make(chan struct{})}
	outer := in.cur
	if outer.cancel != nil {
		// Chain: an outer cancellation cancels this scope too. The
		// watcher dies with the scope.
		go func() {
			select {
			case <-outer.cancel:
				st.doCancel()
			case <-st.done:
			}
		}()
	}
	in.cur = &taskCtx{cancel: st.cancelled}
	scopeEnv := newEnv(env, false)
	if ex.Handle != "" {
		scopeEnv.declare(ex.Handle, &ScopeV{st: st}, false, ex.Line)
	}

	var val Value
	var sg *sig
	var bodyPanic any
	func() {
		defer func() { bodyPanic = recover() }()
		val, sg = in.evalBlock(ex.Body, scopeEnv)
	}()

	// Exit path: leaving early — signal or panic (cancelUnwind
	// included) — cancels the children before waiting for them.
	if sg != nil || bodyPanic != nil {
		st.doCancel()
	}
	// Join every child; children may legally spawn siblings while we
	// wait, so drain until the task list is stable.
	for i := 0; ; {
		st.mu.Lock()
		snapshot := st.tasks[i:]
		i = len(st.tasks)
		st.mu.Unlock()
		if len(snapshot) == 0 {
			break
		}
		in.unblock(func() {
			for _, t := range snapshot {
				<-t.done
			}
		})
	}
	st.doneOnce.Do(func() { close(st.done) })
	in.cur = outer

	// Aftermath, in precedence order: bugs first, then the body's own
	// exit, then unobserved child errors.
	if _, isCancel := bodyPanic.(cancelUnwind); isCancel && st.pan == nil {
		// Cancelled from outside: keep unwinding toward the scope
		// that started it.
		panic(bodyPanic)
	}
	if bodyPanic != nil {
		if _, isCancel := bodyPanic.(cancelUnwind); !isCancel {
			panic(bodyPanic) // the body's own bug
		}
	}
	if st.pan != nil {
		panic(st.pan) // rule 4: a child's panic resurfaces at scope exit
	}
	if sg != nil {
		return UnitV{}, sg // return/?/break/continue pass through
	}
	// Rule 3: errors can't vanish — first unjoined Err fails the
	// scope, as if the body ?'d it at the closing brace.
	st.mu.Lock()
	tasks := st.tasks
	st.mu.Unlock()
	for _, t := range tasks {
		if t.joined {
			continue
		}
		if r, isRes := t.result.(*ResultV); isRes && !r.Ok {
			return UnitV{}, in.propagateErr(r, env, ex.Line)
		}
	}
	return val, nil
}

// propagateErr builds the return signal `?` would: converting the
// error via E.from when the enclosing fn declares a different
// Result error type.
func (in *Interp) propagateErr(r *ResultV, env *Env, line int) *sig {
	if target := env.fnRetErr(); target != "" && typeName(r.V) != target {
		if m := in.methods[target]["from"]; m != nil && m.Self == ast.NoSelf {
			conv := in.callFuncNamed(m, nil, []Value{r.V}, nil, line)
			return &sig{val: &ResultV{Ok: false, V: conv}}
		}
	}
	return &sig{val: r}
}

// spawnTask starts f() on its own goroutine as a child of the scope.
func (in *Interp) spawnTask(s *ScopeV, f Value, line int) Value {
	switch f.(type) {
	case *ClosureV, *FuncV:
	default:
		panic(rtErr{line, fmt.Sprintf("spawn takes a function or closure, got %s", typeName(f))})
	}
	st := s.st
	select {
	case <-st.done:
		panic(rtErr{line, "spawn on a scope that has already ended"})
	default:
	}
	t := &TaskV{done: make(chan struct{})}
	st.mu.Lock()
	st.tasks = append(st.tasks, t)
	st.mu.Unlock()

	go func() {
		in.gil.Lock()
		in.cur = &taskCtx{cancel: st.cancelled}
		defer func() {
			if p := recover(); p != nil {
				if _, isCancel := p.(cancelUnwind); isCancel {
					t.cancelled = true
				} else {
					// Rule 4: bugs don't wait to be observed.
					t.pan = p
					st.recordPanic(p)
					st.doCancel()
				}
			}
			close(t.done)
			in.gil.Unlock()
		}()
		t.result = in.callValue(f, nil, line)
	}()
	return t
}

// Channels. The Go channel provides the entire engine: mpmc,
// buffered/rendezvous, and closed-and-drained recv semantics match
// the ratified design one-for-one. Send-on-closed is Go's runtime
// panic, recovered into a Glide panic with a line number — it IS a
// bug in the ratified semantics, just a diagnosable one.
type chanState struct {
	ch        chan Value
	closeOnce sync.Once
}

func newChannel(cap int) (Value, Value) {
	st := &chanState{ch: make(chan Value, cap)}
	return &SenderV{st: st}, &ReceiverV{st: st}
}

// chanSend blocks until delivered; a cancellation unwinds, a close
// (before or during) panics — a sender coordination bug.
func (in *Interp) chanSend(s *SenderV, v Value, line int) {
	cancel := in.cur.cancel
	var cancelled, wasClosed bool
	in.unblock(func() {
		defer func() {
			if p := recover(); p != nil {
				wasClosed = true // Go: "send on closed channel"
			}
		}()
		select {
		case s.st.ch <- v:
		case <-cancel:
			cancelled = true
		}
	})
	if cancelled {
		panic(cancelUnwind{})
	}
	if wasClosed {
		panic(rtErr{line, "send on a closed channel (only senders close — coordinate them)"})
	}
}

// chanRecv blocks for a value; closed-and-drained is None.
func (in *Interp) chanRecv(r *ReceiverV) Value {
	cancel := in.cur.cancel
	var v Value
	var ok, cancelled bool
	in.unblock(func() {
		select {
		case v, ok = <-r.st.ch:
		case <-cancel:
			cancelled = true
		}
	})
	if cancelled {
		panic(cancelUnwind{})
	}
	if !ok {
		return NoneV{}
	}
	return v
}

// evalSelect: operands and guards evaluate once at entry, in arm
// order; one reflect case per distinct (channel, direction); uniform
// random among ready (reflect.Select is Go's select); a ready op's
// arms are tried in declaration order — no pattern matching is a
// runtime error, match's dynamic-exhaustiveness bargain. Blocking
// select is a cancellation point. There is no ctx.Done arm: the
// scope cancels a blocked select.
func (in *Interp) evalSelect(ex *ast.SelectExpr, env *Env) (Value, *sig) {
	type group struct {
		st   *chanState
		recv bool
		arms []int // indices into ex.Arms, declaration order
		send Value // first send arm's value (one case per group)
	}
	var groups []*group
	var elseArm = -1
	for i := range ex.Arms {
		arm := &ex.Arms[i]
		if arm.Else {
			elseArm = i
			continue
		}
		if arm.Guard != nil {
			g, sg := in.eval(arm.Guard, env)
			if sg != nil {
				return UnitV{}, sg
			}
			gb, isBool := g.(BoolV)
			if !isBool {
				panic(rtErr{arm.Line, fmt.Sprintf("select arm guard must be Bool, got %s", typeName(g))})
			}
			if !bool(gb) {
				continue // disabled: out of the ready set entirely
			}
		}
		ch, sg := in.eval(arm.Ch, env)
		if sg != nil {
			return UnitV{}, sg
		}
		var st *chanState
		isRecv := arm.Pat != nil
		if isRecv {
			rx, ok := ch.(*ReceiverV)
			if !ok {
				panic(rtErr{arm.Line, fmt.Sprintf("recv arm needs a Receiver, got %s", typeName(ch))})
			}
			st = rx.st
		} else {
			tx, ok := ch.(*SenderV)
			if !ok {
				panic(rtErr{arm.Line, fmt.Sprintf("send arm needs a Sender, got %s", typeName(ch))})
			}
			st = tx.st
		}
		var sendVal Value
		if !isRecv {
			v, sg := in.eval(arm.SendVal, env)
			if sg != nil {
				return UnitV{}, sg
			}
			sendVal = v
		}
		// Recv arms on the same channel share one case (the delivered
		// value is tried against their patterns in order). Send arms
		// never merge — each sends its own value.
		grouped := false
		if isRecv {
			for _, g := range groups {
				if g.st == st && g.recv {
					g.arms = append(g.arms, i)
					grouped = true
					break
				}
			}
		}
		if !grouped {
			groups = append(groups, &group{st: st, recv: isRecv, arms: []int{i}, send: sendVal})
		}
	}
	if len(groups) == 0 {
		if elseArm >= 0 {
			return in.eval(ex.Arms[elseArm].Body, newEnv(env, false))
		}
		panic(rtErr{ex.Line, "select has no enabled arms and no else — it would block forever"})
	}

	cases := make([]reflect.SelectCase, 0, len(groups)+2)
	for _, g := range groups {
		if g.recv {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(g.st.ch)})
		} else {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(g.st.ch), Send: reflect.ValueOf(&g.send).Elem()})
		}
	}
	cancelIdx := -1
	if c := in.cur.cancel; c != nil {
		cancelIdx = len(cases)
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c)})
	}
	if elseArm >= 0 {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
	}

	var chosen int
	var recvVal reflect.Value
	var recvOK, wasClosed bool
	in.unblock(func() {
		defer func() {
			if p := recover(); p != nil {
				wasClosed = true // reflect.Select: "send on closed channel"
			}
		}()
		chosen, recvVal, recvOK = reflect.Select(cases)
	})
	if wasClosed {
		panic(rtErr{ex.Line, "send on a closed channel in select (only senders close — coordinate them)"})
	}
	if cancelIdx >= 0 && chosen == cancelIdx {
		panic(cancelUnwind{})
	}
	if chosen >= len(groups) {
		return in.eval(ex.Arms[elseArm].Body, newEnv(env, false))
	}

	g := groups[chosen]
	if !g.recv {
		first := &ex.Arms[g.arms[0]]
		return in.eval(first.Body, newEnv(env, false))
	}
	var v Value = NoneV{}
	if recvOK {
		v = recvVal.Interface().(Value)
	}
	for _, ai := range g.arms {
		arm := &ex.Arms[ai]
		binds, ok := match(arm.Pat, v)
		if !ok {
			continue
		}
		armEnv := newEnv(env, false)
		for _, b := range binds {
			armEnv.declare(b.name, b.val, b.mut, arm.Line)
		}
		return in.eval(arm.Body, armEnv)
	}
	panic(rtErr{ex.Line, fmt.Sprintf("select received %s but no arm pattern matches it (the value is consumed — cover the closed case with `None = rx.recv()`)", render(v, true))})
}

// joinTask blocks until the child finishes and returns exactly what
// its closure returned. A cancellation arriving first unwinds the
// joiner; a panicked child unwinds it too (the scope is already
// going down and will re-panic the real bug at exit).
func (in *Interp) joinTask(t *TaskV, line int) Value {
	cancel := in.cur.cancel
	cancelled := false
	in.unblock(func() {
		select {
		case <-t.done:
		case <-cancel:
			cancelled = true
		}
	})
	if cancelled || t.pan != nil || t.cancelled {
		// Either we were cancelled, or the child failed in a way that
		// is already taking the whole scope down. Unwind; the scope
		// machinery reports the real cause.
		panic(cancelUnwind{})
	}
	t.joined = true
	return t.result
}
