package interp

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Free builtins: printing and the Result/Option constructors. Ok, Err
// and Some are ordinary constructors, not keywords.
var builtins = map[string]*BuiltinV{
	"println": {Name: "println", Fn: func(in *Interp, args []Value, line int) Value {
		fmt.Fprintln(in.Stdout, displayArg("println", args, line))
		return UnitV{}
	}},
	"eprintln": {Name: "eprintln", Fn: func(in *Interp, args []Value, line int) Value {
		fmt.Fprintln(in.Stderr, displayArg("eprintln", args, line))
		return UnitV{}
	}},
	"print": {Name: "print", Fn: func(in *Interp, args []Value, line int) Value {
		fmt.Fprint(in.Stdout, displayArg("print", args, line))
		return UnitV{}
	}},
	"eprint": {Name: "eprint", Fn: func(in *Interp, args []Value, line int) Value {
		fmt.Fprint(in.Stderr, displayArg("eprint", args, line))
		return UnitV{}
	}},
	"Ok": {Name: "Ok", Fn: func(in *Interp, args []Value, line int) Value {
		return &ResultV{Ok: true, V: one("Ok", args, line)}
	}},
	"Err": {Name: "Err", Fn: func(in *Interp, args []Value, line int) Value {
		return &ResultV{Ok: false, V: one("Err", args, line)}
	}},
	// Option is unboxed here, so Some is the identity function; it
	// exists so the spelling stays legal.
	"Some": {Name: "Some", Fn: func(in *Interp, args []Value, line int) Value {
		return one("Some", args, line)
	}},
	// channel() -> (tx, rx): rendezvous by default, channel(cap: n)
	// buffered. No unbounded variant (recorded). The named form is
	// translated to positional in evalCall.
	"channel": {Name: "channel", Fn: func(in *Interp, args []Value, line int) Value {
		capN := 0
		switch len(args) {
		case 0:
		case 1:
			n, ok := args[0].(IntV)
			if !ok || n < 0 {
				panic(rtErr{line, "channel cap must be a non-negative Int"})
			}
			capN = int(n)
		default:
			panic(rtErr{line, "channel takes no arguments or (cap: n)"})
		}
		tx, rx := newChannel(capN)
		return TupleV{tx, rx}
	}},
}

func one(name string, args []Value, line int) Value {
	if len(args) != 1 {
		panic(rtErr{line, fmt.Sprintf("%s takes exactly one argument, got %d", name, len(args))})
	}
	return args[0]
}

func strArg(name string, args []Value, line int) StrV {
	s, ok := one(name, args, line).(StrV)
	if !ok {
		panic(rtErr{line, fmt.Sprintf("%s takes a String, got %s", name, typeName(args[0]))})
	}
	return s
}

func displayArg(name string, args []Value, line int) string {
	return display(one(name, args, line))
}

// Module shims: Go code behind Glide interfaces (the recorded stdlib
// strategy for the interpreter tier).
func (in *Interp) moduleCall(mod, name string, args []Value, line int) Value {
	if in.constEval {
		panic(rtErr{line, fmt.Sprintf("a const initializer cannot call %s.%s (pure expressions only)", mod, name)})
	}
	switch mod + "." + name {
	case "os.args":
		if len(args) != 0 {
			panic(rtErr{line, "os.args takes no arguments"})
		}
		l := &ListV{}
		for _, a := range in.Args {
			l.Elems = append(l.Elems, StrV(a))
		}
		return l
	case "os.exit":
		code, ok := one("os.exit", args, line).(IntV)
		if !ok {
			panic(rtErr{line, "os.exit takes an Int"})
		}
		in.exiting = true
		panic(exitPanic{code: int(code)})
	case "time.now":
		if len(args) != 0 {
			panic(rtErr{line, "time.now takes no arguments"})
		}
		return InstantV{T: time.Now()}
	case "time.sleep":
		d, ok := one("time.sleep", args, line).(DurationV)
		if !ok {
			panic(rtErr{line, fmt.Sprintf("time.sleep takes a Duration (e.g. 100.ms), got %s", typeName(args[0]))})
		}
		cancel := in.cur.cancel
		timer := time.NewTimer(time.Duration(d))
		cancelled := false
		in.unblock(func() {
			select {
			case <-timer.C:
			case <-cancel:
				timer.Stop()
				cancelled = true
			}
		})
		if cancelled {
			panic(cancelUnwind{})
		}
		return UnitV{}
	case "time.after":
		d, ok := one("time.after", args, line).(DurationV)
		if !ok {
			panic(rtErr{line, fmt.Sprintf("time.after takes a Duration, got %s", typeName(args[0]))})
		}
		// Just a channel: the timeout arm in a select is an ordinary
		// recv case, nothing special in select itself (recorded).
		tx, rx := newChannel(1)
		go func() {
			time.Sleep(time.Duration(d))
			st := tx.(*SenderV).st
			st.ch <- UnitV{}
			st.closeOnce.Do(func() { close(st.ch) })
		}()
		return rx
	case "fs.read_string":
		path, ok := one("fs.read_string", args, line).(StrV)
		if !ok {
			panic(rtErr{line, "fs.read_string takes a String path"})
		}
		data, err := os.ReadFile(string(path))
		if err != nil {
			return &ResultV{Ok: false, V: &ErrV{Msg: err.Error()}}
		}
		return &ResultV{Ok: true, V: StrV(string(data))}
	}
	panic(rtErr{line, fmt.Sprintf("module %s has no function %q", mod, name)})
}

// Duration suffix constructors on Int and Float: 250.ms, 0.5.s.
// `.mins` not `.min` (min is the obvious future math method); stops
// at hours — days are calendar arithmetic (recorded).
var durationUnits = map[string]time.Duration{
	"ns": time.Nanosecond, "us": time.Microsecond, "ms": time.Millisecond,
	"s": time.Second, "mins": time.Minute, "h": time.Hour,
}

// Builtin methods that mutate their receiver, keyed "Type.method".
// These obey the same rule as user `mut self` methods: callable only
// through a mut path (checked in evalCall, where the receiver
// expression is still available).
var builtinMutMethods = map[string]bool{
	"List.push":    true,
	"List.sort_by": true,
}

// Methods, dispatched on receiver type.
func (in *Interp) methodCall(recv Value, name string, args []Value, line int) Value {
	switch r := recv.(type) {
	case StrV:
		switch name {
		case "split_whitespace":
			if len(args) != 0 {
				panic(rtErr{line, "split_whitespace takes no arguments"})
			}
			l := &ListV{}
			for _, w := range strings.Fields(string(r)) {
				l.Elems = append(l.Elems, StrV(w))
			}
			return l
		case "len":
			return IntV(len(r))
		case "trim":
			return StrV(strings.TrimSpace(string(r)))
		case "cmp":
			s, ok := one("cmp", args, line).(StrV)
			if !ok {
				panic(rtErr{line, "String.cmp compares against another String"})
			}
			return IntV(strings.Compare(string(r), string(s)))
		case "contains":
			return BoolV(strings.Contains(string(r), string(strArg(name, args, line))))
		case "starts_with":
			return BoolV(strings.HasPrefix(string(r), string(strArg(name, args, line))))
		case "ends_with":
			return BoolV(strings.HasSuffix(string(r), string(strArg(name, args, line))))
		case "split":
			sep := strArg(name, args, line)
			if sep == "" {
				panic(rtErr{line, "split separator must be non-empty (use runes() for per-character iteration)"})
			}
			l := &ListV{}
			for _, part := range strings.Split(string(r), string(sep)) {
				l.Elems = append(l.Elems, StrV(part))
			}
			return l
		case "lines":
			if len(args) != 0 {
				panic(rtErr{line, "lines takes no arguments"})
			}
			// Rust's semantics: split on \n, strip a trailing \r from
			// each line, and a final newline yields no empty last line.
			l := &ListV{}
			s := string(r)
			for len(s) > 0 {
				ln := s
				if i := strings.IndexByte(s, '\n'); i >= 0 {
					ln, s = s[:i], s[i+1:]
				} else {
					s = ""
				}
				l.Elems = append(l.Elems, StrV(strings.TrimSuffix(ln, "\r")))
			}
			return l
		case "replace":
			if len(args) != 2 {
				panic(rtErr{line, "replace takes (old, new)"})
			}
			old, ok1 := args[0].(StrV)
			new_, ok2 := args[1].(StrV)
			if !ok1 || !ok2 {
				panic(rtErr{line, "replace takes two Strings"})
			}
			return StrV(strings.ReplaceAll(string(r), string(old), string(new_)))
		case "to_upper":
			if len(args) != 0 {
				panic(rtErr{line, "to_upper takes no arguments"})
			}
			return StrV(strings.ToUpper(string(r)))
		case "to_lower":
			if len(args) != 0 {
				panic(rtErr{line, "to_lower takes no arguments"})
			}
			return StrV(strings.ToLower(string(r)))
		case "runes":
			if len(args) != 0 {
				panic(rtErr{line, "runes takes no arguments"})
			}
			// Invalid UTF-8 yields U+FFFD per byte (recorded).
			s := string(r)
			i := 0
			return &IterV{Next: func() (Value, bool) {
				if i >= len(s) {
					return nil, false
				}
				ru, size := utf8.DecodeRuneInString(s[i:])
				i += size
				return RuneV(ru), true
			}}
		case "bytes":
			if len(args) != 0 {
				panic(rtErr{line, "bytes takes no arguments"})
			}
			s := string(r)
			i := 0
			return &IterV{Next: func() (Value, bool) {
				if i >= len(s) {
					return nil, false
				}
				b := s[i]
				i++
				return IntV(b), true
			}}
		case "trim_prefix":
			return StrV(strings.TrimPrefix(string(r), string(strArg(name, args, line))))
		case "trim_suffix":
			return StrV(strings.TrimSuffix(string(r), string(strArg(name, args, line))))
		case "repeat":
			k, ok := one("repeat", args, line).(IntV)
			if !ok {
				panic(rtErr{line, "repeat takes an Int count"})
			}
			if k < 0 {
				panic(rtErr{line, fmt.Sprintf("repeat count must be >= 0, got %d", k)})
			}
			if len(r) > 0 && int(k) > math.MaxInt/len(r) {
				panic(rtErr{line, fmt.Sprintf("repeat(%d) of a %d-byte string is too large", k, len(r))})
			}
			return StrV(strings.Repeat(string(r), int(k)))
		}
	case IntV:
		switch name {
		case "cmp":
			o, ok := one("cmp", args, line).(IntV)
			if !ok {
				panic(rtErr{line, "Int.cmp compares against another Int"})
			}
			switch {
			case r < o:
				return IntV(-1)
			case r > o:
				return IntV(1)
			}
			return IntV(0)
		}
	case *ListV:
		switch name {
		case "len":
			return IntV(len(r.Elems))
		case "sorted":
			if len(args) != 0 {
				panic(rtErr{line, "sorted takes no arguments"})
			}
			out := &ListV{Elems: append([]Value{}, r.Elems...)}
			var sortErr any
			func() {
				defer func() { sortErr = recover() }()
				sort.SliceStable(out.Elems, func(i, j int) bool {
					return naturalLess(out.Elems[i], out.Elems[j], line)
				})
			}()
			if sortErr != nil {
				panic(sortErr)
			}
			return out
		case "push":
			r.Elems = append(r.Elems, one("push", args, line))
			return UnitV{}
		case "join":
			sep := strArg(name, args, line)
			var b strings.Builder
			for i, e := range r.Elems {
				s, ok := e.(StrV)
				if !ok {
					panic(rtErr{line, fmt.Sprintf("join needs a List<String>; element %d is %s", i, typeName(e))})
				}
				if i > 0 {
					b.WriteString(string(sep))
				}
				b.WriteString(string(s))
			}
			return StrV(b.String())
		case "repeat":
			arg := one("repeat", args, line)
			k, ok := arg.(IntV)
			if !ok {
				panic(rtErr{line, fmt.Sprintf("repeat takes an Int count, got %s", typeName(arg))})
			}
			if k < 0 {
				panic(rtErr{line, fmt.Sprintf("repeat count must be >= 0, got %d", k)})
			}
			// Shallow: the same element values appear k times. With a
			// reference element ([[]].repeat(2)) that is two slots
			// sharing one list — documented in stdlib.md.
			if len(r.Elems) > 0 && int(k) > math.MaxInt/len(r.Elems) {
				panic(rtErr{line, fmt.Sprintf("repeat(%d) of a %d-element list is too large", k, len(r.Elems))})
			}
			out := &ListV{Elems: make([]Value, 0, len(r.Elems)*int(k))}
			for range int(k) {
				out.Elems = append(out.Elems, r.Elems...)
			}
			return out
		case "iter":
			if len(args) != 0 {
				panic(rtErr{line, "iter takes no arguments"})
			}
			i := 0
			return &IterV{Next: func() (Value, bool) {
				if i >= len(r.Elems) {
					return nil, false
				}
				v := r.Elems[i]
				i++
				return v, true
			}}
		case "sort_by":
			f := one("sort_by", args, line)
			// Stable, so ties keep first-seen order — deterministic
			// programs without a tiebreak dance.
			var sortErr any
			func() {
				defer func() { sortErr = recover() }()
				sort.SliceStable(r.Elems, func(i, j int) bool {
					c := in.callValue(f, []Value{r.Elems[i], r.Elems[j]}, line)
					n, ok := c.(IntV)
					if !ok {
						panic(rtErr{line, fmt.Sprintf("sort_by comparator must return an Int (cmp order), got %s", typeName(c))})
					}
					return n < 0
				})
			}()
			if sortErr != nil {
				panic(sortErr)
			}
			return UnitV{}
		}
	case RangeV:
		switch name {
		case "iter":
			if len(args) != 0 {
				panic(rtErr{line, "iter takes no arguments"})
			}
			return &IterV{Next: in.iterate(r, line)}
		}
	case *MapV:
		switch name {
		case "iter":
			if len(args) != 0 {
				panic(rtErr{line, "iter takes no arguments"})
			}
			return &IterV{Next: in.iterate(r, line)}
		case "len":
			return IntV(len(r.keys))
		case "entries":
			if len(args) != 0 {
				panic(rtErr{line, "entries takes no arguments"})
			}
			l := &ListV{}
			for _, k := range r.keys {
				l.Elems = append(l.Elems, TupleV{k, r.m[k]})
			}
			return l
		}
	case *SenderV:
		switch name {
		case "send":
			in.chanSend(r, one("send", args, line), line)
			return UnitV{}
		case "close":
			if len(args) != 0 {
				panic(rtErr{line, "close takes no arguments"})
			}
			// Idempotent by design: with cloned senders and no
			// deterministic drop, close must be safe from racing defers.
			r.st.closeOnce.Do(func() { close(r.st.ch) })
			return UnitV{}
		}
	case *ReceiverV:
		switch name {
		case "recv":
			if len(args) != 0 {
				panic(rtErr{line, "recv takes no arguments"})
			}
			return in.chanRecv(r)
		case "close":
			panic(rtErr{line, "only the sender half closes a channel"})
		}
	case *ScopeV:
		switch name {
		case "spawn":
			return in.spawnTask(r, one("spawn", args, line), line)
		case "deadline":
			if len(args) != 0 {
				panic(rtErr{line, "deadline takes no arguments"})
			}
			if r.st.deadline.IsZero() {
				return NoneV{}
			}
			return InstantV{T: r.st.deadline}
		}
	case *TaskV:
		switch name {
		case "join":
			if len(args) != 0 {
				panic(rtErr{line, "join takes no arguments"})
			}
			return in.joinTask(r, line)
		}
	case *ResultV:
		switch name {
		case "context":
			msg, ok := one("context", args, line).(StrV)
			if !ok {
				panic(rtErr{line, "context takes a String"})
			}
			if r.Ok {
				return r
			}
			return &ResultV{Ok: false, V: &ErrV{Msg: string(msg), Cause: r.V}}
		}
	case *IterV:
		switch name {
		case "take":
			n, ok := one("take", args, line).(IntV)
			if !ok {
				panic(rtErr{line, "take takes an Int"})
			}
			left := int(n)
			return &IterV{Next: func() (Value, bool) {
				if left <= 0 {
					return nil, false
				}
				left--
				return r.Next()
			}}
		case "collect":
			if len(args) != 0 {
				panic(rtErr{line, "collect takes no arguments"})
			}
			l := &ListV{}
			for {
				v, ok := r.Next()
				if !ok {
					return l
				}
				l.Elems = append(l.Elems, v)
			}
		case "map":
			f := one("map", args, line)
			return &IterV{Next: func() (Value, bool) {
				v, ok := r.Next()
				if !ok {
					return nil, false
				}
				return in.callValue(f, []Value{v}, line), true
			}}
		case "filter":
			f := one("filter", args, line)
			return &IterV{Next: func() (Value, bool) {
				for {
					v, ok := r.Next()
					if !ok {
						return nil, false
					}
					keep, isBool := in.callValue(f, []Value{v}, line).(BoolV)
					if !isBool {
						panic(rtErr{line, "filter predicate must return Bool"})
					}
					if bool(keep) {
						return v, true
					}
				}
			}}
		case "enumerate":
			if len(args) != 0 {
				panic(rtErr{line, "enumerate takes no arguments"})
			}
			i := int64(0)
			return &IterV{Next: func() (Value, bool) {
				v, ok := r.Next()
				if !ok {
					return nil, false
				}
				t := TupleV{IntV(i), v}
				i++
				return t, true
			}}
		case "zip":
			other := in.iterate(one("zip", args, line), line)
			return &IterV{Next: func() (Value, bool) {
				a, ok := r.Next()
				if !ok {
					return nil, false
				}
				b, ok := other()
				if !ok {
					return nil, false
				}
				return TupleV{a, b}, true
			}}
		case "count":
			if len(args) != 0 {
				panic(rtErr{line, "count takes no arguments"})
			}
			n := 0
			for {
				if _, ok := r.Next(); !ok {
					return IntV(n)
				}
				n++
			}
		case "sum":
			if len(args) != 0 {
				panic(rtErr{line, "sum takes no arguments"})
			}
			// Fold `+` from the first element, so Int, Float and
			// String all work; empty sums to Int 0.
			acc, ok := r.Next()
			if !ok {
				return IntV(0)
			}
			for {
				v, ok := r.Next()
				if !ok {
					return acc
				}
				acc = binop("+", acc, v, line)
			}
		}
	}
	panic(rtErr{line, fmt.Sprintf("%s has no method %q", typeName(recv), name)})
}

func naturalLess(a, b Value, line int) bool {
	if x, ok := a.(IntV); ok {
		if y, ok := b.(IntV); ok {
			return x < y
		}
	}
	if x, ok := a.(StrV); ok {
		if y, ok := b.(StrV); ok {
			return x < y
		}
	}
	if x, ok := a.(FloatV); ok {
		if y, ok := b.(FloatV); ok {
			return x < y
		}
	}
	if x, ok := a.(RuneV); ok {
		if y, ok := b.(RuneV); ok {
			return x < y
		}
	}
	panic(rtErr{line, fmt.Sprintf("cannot order %s against %s", typeName(a), typeName(b))})
}

// iterate adapts any iterable to a next() function for `for … in`.
func (in *Interp) iterate(v Value, line int) func() (Value, bool) {
	switch it := v.(type) {
	case *IterV:
		return it.Next
	case *StructV:
		// Anything with an iter() method is iterable.
		if m := in.findMethod(it.Type, "iter", line); m != nil {
			res := in.callFuncSelf(m, it, nil)
			iv, ok := res.(*IterV)
			if !ok {
				panic(rtErr{line, fmt.Sprintf("%s.iter() must return an Iterator, got %s", it.Type, typeName(res))})
			}
			return iv.Next
		}
	case *ListV:
		i := 0
		return func() (Value, bool) {
			if i >= len(it.Elems) {
				return nil, false
			}
			e := it.Elems[i]
			i++
			return e, true
		}
	case RangeV:
		n := it.Lo
		return func() (Value, bool) {
			if n >= it.Hi {
				return nil, false
			}
			v := IntV(n)
			n++
			return v, true
		}
	case *MapV:
		i := 0
		return func() (Value, bool) {
			if i >= len(it.keys) {
				return nil, false
			}
			k := it.keys[i]
			i++
			return TupleV{k, it.m[k]}, true
		}
	case *ReceiverV:
		// `for v in rx` consumes until closed-and-drained — blocking
		// recv per element, each a cancellation point.
		return func() (Value, bool) {
			v := in.chanRecv(it)
			if _, isNone := v.(NoneV); isNone {
				return nil, false
			}
			return v, true
		}
	}
	panic(rtErr{line, fmt.Sprintf("%s is not iterable", typeName(v))})
}
