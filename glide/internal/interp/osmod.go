package interp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"glide/internal/source"
)

// The fs and os host shims. Both are thin covers over Go's os package,
// and both are deliberately *stringly* about paths: a typed `path`
// module is designed (STDLIB-GOALS.md) but a String path is what a
// script actually has, and inventing the type before the module exists
// would mean converting at every boundary for no checking.
//
// Everything that can fail returns Result rather than trapping. A
// missing file is an ordinary outcome for a program that reads config,
// not a bug — which is the same reason `exists` returns a bare Bool: a
// Result there would be a Result you can only ever unwrap.

func okV(v Value) Value { return &ResultV{Ok: true, V: v} }
func errV(err error) Value {
	return &ResultV{Ok: false, V: &ErrV{Msg: err.Error()}}
}

// unitOr collapses the very common "Go call returning only error"
// shape into Result<(), Error>.
func unitOr(err error) Value {
	if err != nil {
		return errV(err)
	}
	return okV(UnitV{})
}

func (in *Interp) fsCall(name string, args []Value, at source.Span) Value {
	switch name {
	case "read_string":
		return readOrErr(pathArg("fs.read_string", args, at))
	case "write_string":
		path, data := twoStrings("fs.write_string", "path", "contents", args, at)
		// 0644 and truncate: the shell's `>`. A mode argument waits for
		// a real use — every script that needs one also needs the rest
		// of the permission surface, and that is the `fs` module's own
		// milestone.
		return unitOr(os.WriteFile(path, []byte(data), 0o644))
	case "append_string":
		path, data := twoStrings("fs.append_string", "path", "contents", args, at)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return errV(err)
		}
		if _, err := f.WriteString(data); err != nil {
			f.Close()
			return errV(err)
		}
		return unitOr(f.Close())
	case "exists":
		_, err := os.Stat(pathArg("fs.exists", args, at))
		return BoolV(err == nil)
	case "is_dir":
		st, err := os.Stat(pathArg("fs.is_dir", args, at))
		return BoolV(err == nil && st.IsDir())
	case "remove":
		return unitOr(os.Remove(pathArg("fs.remove", args, at)))
	case "remove_all":
		// Named for what it does. `fs.remove` refusing a non-empty
		// directory and `fs.remove_all` taking the tree is Go's split,
		// and the value of it is that the recursive one is never
		// reached by accident.
		return unitOr(os.RemoveAll(pathArg("fs.remove_all", args, at)))
	case "mkdir_all":
		return unitOr(os.MkdirAll(pathArg("fs.mkdir_all", args, at), 0o755))
	case "rename":
		from, to := twoStrings("fs.rename", "from", "to", args, at)
		return unitOr(os.Rename(from, to))
	case "list_dir":
		entries, err := os.ReadDir(pathArg("fs.list_dir", args, at))
		if err != nil {
			return errV(err)
		}
		// Names, not paths — Go's shape, and joining is the caller's
		// business. Sorted, because ReadDir sorts and a program that
		// iterates a directory must not have its output depend on the
		// filesystem's whim (the same reasoning that specified Map's
		// insertion order).
		l := &ListV{}
		for _, e := range entries {
			l.Elems = append(l.Elems, StrV(e.Name()))
		}
		sort.Slice(l.Elems, func(i, j int) bool {
			return l.Elems[i].(StrV) < l.Elems[j].(StrV)
		})
		return okV(l)
	case "join":
		l, ok := one("fs.join", args, at).(*ListV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("fs.join takes a List<String>, got %s", typeName(args[0]))})
		}
		parts := make([]string, len(l.Elems))
		for i, e := range l.Elems {
			s, ok := e.(StrV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("fs.join: segment %d is %s, not a String", i, typeName(e))})
			}
			parts[i] = string(s)
		}
		return StrV(filepath.Join(parts...))
	}
	panic(rtErr{at, fmt.Sprintf("module fs has no function %q", name)})
}

func readOrErr(path string) Value {
	data, err := os.ReadFile(path)
	if err != nil {
		return errV(err)
	}
	return okV(StrV(string(data)))
}

func (in *Interp) osCall(name string, args []Value, at source.Span) Value {
	switch name {
	case "args":
		if len(args) != 0 {
			panic(rtErr{at, "os.args takes no arguments"})
		}
		l := &ListV{}
		for _, a := range in.Args {
			l.Elems = append(l.Elems, StrV(a))
		}
		return l
	case "exit":
		code, ok := one("os.exit", args, at).(IntV)
		if !ok {
			panic(rtErr{at, "os.exit takes an Int"})
		}
		in.exiting = true
		panic(exitPanic{code: int(code)})
	case "env":
		// Option, not "" — an empty variable that is *set* is a
		// different thing from one that is not, and `?? "default"`
		// reads better than the Go dance.
		v, ok := os.LookupEnv(string(strArg("os.env", args, at)))
		if !ok {
			return NoneV{}
		}
		return some(StrV(v))
	case "set_env":
		k, v := twoStrings("os.set_env", "name", "value", args, at)
		return unitOr(os.Setenv(k, v))
	case "cwd":
		if len(args) != 0 {
			panic(rtErr{at, "os.cwd takes no arguments"})
		}
		dir, err := os.Getwd()
		if err != nil {
			return errV(err)
		}
		return okV(StrV(dir))
	case "chdir":
		// Process-global, and that is a real hazard under `spawn`: two
		// tasks calling chdir interleave, and a relative path resolved
		// by a third sees whichever won. Shipped anyway, because
		// without it there is no way at all to control where
		// `process.run` runs, and refusing the tool is worse than
		// documenting the edge. Single-threaded scripts — the case this
		// exists for — cannot hit it.
		return unitOr(os.Chdir(pathArg("os.chdir", args, at)))
	}
	panic(rtErr{at, fmt.Sprintf("module os has no function %q", name)})
}

// pathArg is the one-String-argument shape almost every fs call takes.
func pathArg(fn string, args []Value, at source.Span) string {
	s, ok := one(fn, args, at).(StrV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s takes a String path, got %s", fn, typeName(args[0]))})
	}
	return string(s)
}

func twoStrings(fn, a, b string, args []Value, at source.Span) (string, string) {
	if len(args) != 2 {
		panic(rtErr{at, fmt.Sprintf("%s takes (%s, %s)", fn, a, b)})
	}
	x, ok1 := args[0].(StrV)
	y, ok2 := args[1].(StrV)
	if !ok1 || !ok2 {
		panic(rtErr{at, fmt.Sprintf("%s takes two Strings (%s, %s)", fn, a, b)})
	}
	return string(x), string(y)
}
