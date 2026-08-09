package interp

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"glide/internal/source"
)

// The process host shim: run another program, wait, and read back what
// it wrote and how it exited.
//
// Three decisions shape the whole surface, and each is the opposite of
// what a naive port of the shell would give you:
//
//  1. **A non-zero exit is not an error.** `Err` means the program
//     could not be run at all — not on PATH, not executable, killed by
//     the scope. A program that ran and exited 1 produced an *answer*:
//     `grep` says "no match" that way, `diff` says "they differ", and
//     `test` says "false". Folding that into Err would make `?`
//     propagate a normal result, and every caller would have to
//     un-propagate it. So the status is a field of the Ok value.
//
//  2. **No shell.** The command is an executable and a list of
//     arguments, never a string to be word-split. There is no quoting
//     to get wrong and no injection to audit, which is most of what
//     makes shell scripts fragile. A shell is still available when it
//     is genuinely wanted, and then it is visible at the call site:
//     process.run("sh", ["-c", "a | b > c"]).
//
//  3. **It is a cancellation point.** The child dies with its enclosing
//     scope, through the same bridged context http.get uses. Without
//     that, `scope(timeout: 5.s)` would time out and leave the process
//     running, which is the bug the whole scope design exists to
//     prevent.

// OutputV is a finished child process: what it wrote, and how it left.
// Immutable — it is a record of something that already happened.
type OutputV struct {
	status         int
	stdout, stderr string
}

func (in *Interp) processCall(name string, args []Value, at source.Span) Value {
	switch name {
	case "run":
		cmd, argv := commandArgs("process.run", args, at)
		return in.runProcess(cmd, argv)
	}
	panic(rtErr{at, fmt.Sprintf("module process has no function %q", name)})
}

// commandArgs reads the (cmd) or (cmd, args) shape. The argument list
// is optional because `process.run("date")` should not have to write
// an empty list, and it is a List<String> because that is the argv the
// operating system actually takes.
func commandArgs(fn string, args []Value, at source.Span) (string, []string) {
	if len(args) == 0 || len(args) > 2 {
		panic(rtErr{at, fn + " takes (cmd) or (cmd, args)"})
	}
	cmd, ok := args[0].(StrV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s: the command is a String, got %s", fn, typeName(args[0]))})
	}
	if cmd == "" {
		panic(rtErr{at, fn + ": the command is empty"})
	}
	var argv []string
	if len(args) == 2 {
		l, ok := args[1].(*ListV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("%s: the arguments are a List<String>, got %s", fn, typeName(args[1]))})
		}
		for i, a := range l.Elems {
			s, ok := a.(StrV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("%s: argument %d is %s, not a String", fn, i, typeName(a))})
			}
			argv = append(argv, string(s))
		}
	}
	return string(cmd), argv
}

// runProcess starts the child under the task's cancellation context,
// releases the GIL while it runs (a subprocess is exactly the kind of
// wait other green threads should not sit behind), and maps the two
// failure modes apart: could-not-run is Err, ran-and-exited is Ok.
func (in *Interp) runProcess(name string, argv []string) Value {
	ctx, release := in.hostCtx()
	var out OutputV
	var failure string
	cancelled := false
	in.unblock(func() {
		defer release()
		cmd := exec.CommandContext(ctx, name, argv...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		out.stdout, out.stderr = stdout.String(), stderr.String()
		var exit *exec.ExitError
		switch {
		case err == nil:
			out.status = 0
		case errors.As(err, &exit):
			// It ran. Even a signal death is an outcome the caller can
			// see: Go reports that as a negative-ish ExitCode(), and -1
			// specifically means "killed by a signal, no code".
			if ctx.Err() != nil {
				cancelled = true
				return
			}
			out.status = exit.ExitCode()
		default:
			// Never started: not found, not executable, bad directory.
			if ctx.Err() != nil {
				cancelled = true
				return
			}
			failure = err.Error()
		}
	})
	if cancelled {
		panic(cancelUnwind{})
	}
	if failure != "" {
		return errV(errors.New(failure))
	}
	return okV(&out)
}

func (o *OutputV) method(name string, args []Value, at source.Span) (Value, bool) {
	switch name {
	case "status":
		nilArgs("status", args, at)
		return IntV(o.status), true
	case "stdout":
		nilArgs("stdout", args, at)
		return StrV(o.stdout), true
	case "stderr":
		nilArgs("stderr", args, at)
		return StrV(o.stderr), true
	case "ok":
		// `out.ok()` rather than making the caller write
		// `out.status() == 0` — the exit-zero convention is the one
		// piece of shell lore worth keeping, and spelling it out at
		// every call site invites getting the sense backwards.
		nilArgs("ok", args, at)
		return BoolV(o.status == 0), true
	}
	return nil, false
}

func nilArgs(name string, args []Value, at source.Span) {
	if len(args) != 0 {
		panic(rtErr{at, name + " takes no arguments"})
	}
}
