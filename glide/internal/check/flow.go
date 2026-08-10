package check

import (
	"glide/internal/ast"
	"glide/internal/types"
)

// Divergence: does control leave here without falling off the end?
//
// One rule needs this — `let … else` — and it needs it in the strict
// direction. The else block of a `let … else` must not fall through,
// because the binding the `let` promises does not exist on that path.
// The evaluator enforced it alone until M4d, and only when the pattern
// actually failed: an else block that forgot to return was a latent
// crash sitting on the branch nobody had taken yet.
//
// Everything here is *provable* divergence, and the check reports only
// where divergence is provably absent. Glide has no `panic` builtin and
// no `!` return type, so the primitives are a closed set: `return`,
// `break`, `continue`, and `os.exit`, which the universe types as
// Never. That closed set is what makes the analysis honest — there is
// no user-defined "this never returns" for it to miss.
//
// The consequence, stated rather than hidden: a helper that ends in
// `os.exit` is still typed Unit, so `else { die("no config") }` is
// rejected even though it would have run correctly. Rust rejects the
// same program for the same reason and answers it with `-> !`;
// DESIGN.md carries that as an open question.

// diverges reports whether a block always transfers control before its
// end. False means "not provably divergent", which includes every case
// the analysis does not model — so a false positive is possible only
// where the language offers no way to express the divergence at all.
func (c *checker) diverges(b *ast.Block) bool {
	if b == nil {
		return false
	}
	// Any statement diverging is enough: what follows it is dead.
	for _, s := range b.Stmts {
		if c.stmtDiverges(s) {
			return true
		}
	}
	return false
}

func (c *checker) stmtDiverges(s ast.Stmt) bool {
	switch st := s.(type) {
	case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.ExprStmt:
		return c.exprDiverges(st.E)
	case *ast.ForStmt:
		return loopDiverges(st)
	}
	// A `let` falls through by definition — that is what binding a name
	// for the following statements means. Its own else block diverging
	// says nothing about this path.
	return false
}

// loopDiverges reports whether control can never continue past a loop.
// Only the conditionless `for { … }` qualifies, and only when nothing
// in it breaks *out to here*.
//
// A condition is never evaluated: `for true { … }` is not treated as
// infinite. Go draws the line in the same place (a `for` with no
// condition is a terminating statement; `for true` is not) and so does
// Rust (`loop` has type `!`, `while true` does not). Reading the
// condition would mean constant-folding it, and then the rule's edge
// moves every time the folder gets cleverer — a bad property for a rule
// whose failure mode is rejecting working code.
func loopDiverges(st *ast.ForStmt) bool {
	if st.Iter != nil || st.Cond != nil {
		return false // may run zero times, or stop
	}
	return !breaksOut(st.Body, st.Label)
}

// breaksOut reports whether b contains a `break` that resumes control
// after the loop labelled `label` (or, for "", the loop this body
// belongs to).
//
// A break aimed at an *outer* loop does not count: it leaves this loop
// without resuming after it, so the block this loop sits in is still
// departed. Only a break landing right here brings control back.
func breaksOut(b *ast.Block, label string) bool {
	if b == nil {
		return false
	}
	found := false
	walkBlock(b, func(s ast.Stmt) {
		br, ok := s.(*ast.BreakStmt)
		if !ok || found {
			return
		}
		if br.Label != "" {
			found = br.Label == label
			return
		}
		// Unlabelled: it belongs to the nearest enclosing loop, so it
		// is only ours if no loop sits between it and us. walkBlock
		// descends through nested loops, so ask that question directly.
		found = !insideNestedLoop(b, br)
	})
	return found
}

// insideNestedLoop reports whether target sits inside a loop nested
// within b, rather than in b itself.
func insideNestedLoop(b *ast.Block, target *ast.BreakStmt) bool {
	nested := false
	walkBlock(b, func(s ast.Stmt) {
		inner, ok := s.(*ast.ForStmt)
		if !ok || nested {
			return
		}
		walkBlock(inner.Body, func(s2 ast.Stmt) {
			if bs, isBreak := s2.(*ast.BreakStmt); isBreak && bs == target {
				nested = true
			}
		})
	})
	return nested
}

func (c *checker) exprDiverges(e ast.Expr) bool {
	if e == nil {
		return false
	}
	// os.exit, and anything else the checker typed Never. Types are
	// recorded during checking, so this runs after the block has been
	// checked, never before.
	if t, ok := c.info.Types[e]; ok && types.IsNever(t) {
		return true
	}
	switch x := e.(type) {
	case *ast.BlockExpr:
		return c.diverges(x.Body)

	case *ast.If:
		// A one-armed `if` falls through when the condition is false,
		// whatever the arm does.
		return c.diverges(x.Then) && c.elseDiverges(x.ElseIf, x.ElseBlock)

	case *ast.IfLet:
		return c.diverges(x.Then) && c.elseDiverges(x.ElseIf, x.ElseBlock)

	case *ast.Match:
		// Exhaustiveness is checked separately and reported there, so a
		// match that reaches here covers its scrutinee: every arm
		// diverging means the match does.
		if len(x.Arms) == 0 {
			return false
		}
		for _, arm := range x.Arms {
			if !c.exprDiverges(arm.Body) {
				return false
			}
		}
		// Guards need no special case, and giving them one would be a
		// mistake in the expensive direction. A guard that fails falls
		// through to a later arm; exhaustiveness does not let a guarded
		// arm count as covering, so an unguarded arm always catches it.
		// Every arm diverging therefore means whichever arm runs
		// diverges. Treating a guard as "might fall out of the match"
		// would reject `match k { _ if hot => { return 1 } _ => { return 2 } }`,
		// which plainly cannot fall out.
		return true

	case *ast.CondMatch:
		// Subjectless match: without a `_` arm every condition can be
		// false, and control continues past the whole thing.
		total := false
		for _, arm := range x.Arms {
			if !c.exprDiverges(arm.Body) {
				return false
			}
			if arm.Cond == nil {
				total = true
			}
		}
		return total
	}
	return false
}

func (c *checker) elseDiverges(elseIf ast.Expr, elseBlock *ast.Block) bool {
	switch {
	case elseIf != nil:
		return c.exprDiverges(elseIf)
	case elseBlock != nil:
		return c.diverges(elseBlock)
	}
	return false
}
