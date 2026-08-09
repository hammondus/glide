package interp

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"glide/internal/source"
)

// Free builtins: printing and the Result/Option constructors. Ok, Err
// and Some are ordinary constructors, not keywords.
var builtins = map[string]*BuiltinV{
	"println": {Name: "println", Fn: func(in *Interp, args []Value, at source.Span) Value {
		fmt.Fprintln(in.Stdout, displayArg("println", args, at))
		return UnitV{}
	}},
	"eprintln": {Name: "eprintln", Fn: func(in *Interp, args []Value, at source.Span) Value {
		fmt.Fprintln(in.Stderr, displayArg("eprintln", args, at))
		return UnitV{}
	}},
	"print": {Name: "print", Fn: func(in *Interp, args []Value, at source.Span) Value {
		fmt.Fprint(in.Stdout, displayArg("print", args, at))
		return UnitV{}
	}},
	"eprint": {Name: "eprint", Fn: func(in *Interp, args []Value, at source.Span) Value {
		fmt.Fprint(in.Stderr, displayArg("eprint", args, at))
		return UnitV{}
	}},
	"Ok": {Name: "Ok", Fn: func(in *Interp, args []Value, at source.Span) Value {
		return &ResultV{Ok: true, V: one("Ok", args, at)}
	}},
	"Err": {Name: "Err", Fn: func(in *Interp, args []Value, at source.Span) Value {
		return &ResultV{Ok: false, V: one("Err", args, at)}
	}},
	"Some": {Name: "Some", Fn: func(in *Interp, args []Value, at source.Span) Value {
		return some(one("Some", args, at))
	}},
	// channel() -> (tx, rx): rendezvous by default, channel(cap: n)
	// buffered. No unbounded variant (recorded). The named form is
	// translated to positional in evalCall.
	"channel": {Name: "channel", Fn: func(in *Interp, args []Value, at source.Span) Value {
		capN := 0
		switch len(args) {
		case 0:
		case 1:
			n, ok := args[0].(IntV)
			if !ok || n < 0 {
				panic(rtErr{at, "channel cap must be a non-negative Int"})
			}
			capN = int(n)
		default:
			panic(rtErr{at, "channel takes no arguments or (cap: n)"})
		}
		tx, rx := newChannel(capN)
		return TupleV{tx, rx}
	}},
}

func one(name string, args []Value, at source.Span) Value {
	if len(args) != 1 {
		panic(rtErr{at, fmt.Sprintf("%s takes exactly one argument, got %d", name, len(args))})
	}
	return args[0]
}

func strArg(name string, args []Value, at source.Span) StrV {
	s, ok := one(name, args, at).(StrV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s takes a String, got %s", name, typeName(args[0]))})
	}
	return s
}

func displayArg(name string, args []Value, at source.Span) string {
	return display(one(name, args, at))
}

// Module shims: Go code behind Glide interfaces (the recorded stdlib
// strategy for the interpreter tier).
func (in *Interp) moduleCall(mod, name string, args []Value, at source.Span) Value {
	if in.constEval {
		panic(rtErr{at, fmt.Sprintf("a const initializer cannot call %s.%s (pure expressions only)", mod, name)})
	}
	switch mod {
	case "json":
		return in.jsonCall(name, args, at)
	case "http":
		return in.httpCall(name, args, at)
	case "sql":
		return in.sqlCall(name, args, at)
	case "fs":
		return in.fsCall(name, args, at)
	case "os":
		return in.osCall(name, args, at)
	case "process":
		return in.processCall(name, args, at)
	}
	switch mod + "." + name {
	case "time.now":
		if len(args) != 0 {
			panic(rtErr{at, "time.now takes no arguments"})
		}
		return InstantV{T: time.Now()}
	case "time.sleep":
		d, ok := one("time.sleep", args, at).(DurationV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("time.sleep takes a Duration (e.g. 100.ms), got %s", typeName(args[0]))})
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
		d, ok := one("time.after", args, at).(DurationV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("time.after takes a Duration, got %s", typeName(args[0]))})
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
	}
	panic(rtErr{at, fmt.Sprintf("module %s has no function %q", mod, name)})
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
	"List.push":     true,
	"List.sort_by":  true,
	"List.pop":      true,
	"List.insert":   true,
	"List.remove":   true,
	"List.extend":   true,
	"Map.remove":    true,
	"Router.get":    true,
	"Router.post":   true,
	"Router.put":    true,
	"Router.delete": true,
}

// indexOf is `contains` and `index_of` sharing one scan, so the two can
// never disagree about what membership means. Equality is the language's
// structural `==`, which is why an uncomparable element (a function)
// reports at the call rather than quietly answering false.
func indexOf(l *ListV, v Value, at source.Span) int {
	for i, e := range l.Elems {
		if eq(e, v, at) {
			return i
		}
	}
	return -1
}

// listIndex validates a positional argument against a bound. No
// negative-index-from-the-end sugar: `xs[-1]` meaning "last" is a
// Python convenience that silently turns an off-by-one into a read of
// the wrong end, and `last()` says it plainly.
func listIndex(fn string, v Value, bound int, at source.Span) int {
	n, ok := v.(IntV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s takes an Int index, got %s", fn, typeName(v))})
	}
	if n < 0 || int(n) >= bound {
		panic(rtErr{at, fmt.Sprintf("%s: index %d out of range (valid: 0..%d)", fn, n, bound-1)})
	}
	return int(n)
}

// Methods, dispatched on receiver type.
func (in *Interp) methodCall(recv Value, name string, args []Value, at source.Span) Value {
	switch r := recv.(type) {
	case StrV:
		switch name {
		case "split_whitespace":
			if len(args) != 0 {
				panic(rtErr{at, "split_whitespace takes no arguments"})
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
			s, ok := one("cmp", args, at).(StrV)
			if !ok {
				panic(rtErr{at, "String.cmp compares against another String"})
			}
			return IntV(strings.Compare(string(r), string(s)))
		case "contains":
			return BoolV(strings.Contains(string(r), string(strArg(name, args, at))))
		case "starts_with":
			return BoolV(strings.HasPrefix(string(r), string(strArg(name, args, at))))
		case "ends_with":
			return BoolV(strings.HasSuffix(string(r), string(strArg(name, args, at))))
		case "split":
			sep := strArg(name, args, at)
			if sep == "" {
				panic(rtErr{at, "split separator must be non-empty (use runes() for per-character iteration)"})
			}
			l := &ListV{}
			for _, part := range strings.Split(string(r), string(sep)) {
				l.Elems = append(l.Elems, StrV(part))
			}
			return l
		case "lines":
			if len(args) != 0 {
				panic(rtErr{at, "lines takes no arguments"})
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
				panic(rtErr{at, "replace takes (old, new)"})
			}
			old, ok1 := args[0].(StrV)
			new_, ok2 := args[1].(StrV)
			if !ok1 || !ok2 {
				panic(rtErr{at, "replace takes two Strings"})
			}
			return StrV(strings.ReplaceAll(string(r), string(old), string(new_)))
		case "to_upper":
			if len(args) != 0 {
				panic(rtErr{at, "to_upper takes no arguments"})
			}
			return StrV(strings.ToUpper(string(r)))
		case "to_lower":
			if len(args) != 0 {
				panic(rtErr{at, "to_lower takes no arguments"})
			}
			return StrV(strings.ToLower(string(r)))
		case "runes":
			if len(args) != 0 {
				panic(rtErr{at, "runes takes no arguments"})
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
				panic(rtErr{at, "bytes takes no arguments"})
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
		case "parse_int":
			if len(args) != 0 {
				panic(rtErr{at, "parse_int takes no arguments"})
			}
			n, err := strconv.ParseInt(strings.TrimSpace(string(r)), 10, 64)
			if err != nil {
				return NoneV{}
			}
			return some(IntV(n))
		case "trim_prefix":
			return StrV(strings.TrimPrefix(string(r), string(strArg(name, args, at))))
		case "trim_suffix":
			return StrV(strings.TrimSuffix(string(r), string(strArg(name, args, at))))
		case "repeat":
			k, ok := one("repeat", args, at).(IntV)
			if !ok {
				panic(rtErr{at, "repeat takes an Int count"})
			}
			if k < 0 {
				panic(rtErr{at, fmt.Sprintf("repeat count must be >= 0, got %d", k)})
			}
			if len(r) > 0 && int(k) > math.MaxInt/len(r) {
				panic(rtErr{at, fmt.Sprintf("repeat(%d) of a %d-byte string is too large", k, len(r))})
			}
			return StrV(strings.Repeat(string(r), int(k)))
		}
	case IntV, UintV, SizedV, FloatV, RuneV:
		if out, handled := intMethod(r, name, args, at); handled {
			return out
		}
		if out, handled := in.mathMethod(r, name, args, at); handled {
			return out
		}
	case *ListV:
		switch name {
		case "len":
			return IntV(len(r.Elems))
		case "contains":
			return BoolV(indexOf(r, one("contains", args, at), at) >= 0)
		case "index_of":
			if i := indexOf(r, one("index_of", args, at), at); i >= 0 {
				return some(IntV(i))
			}
			return NoneV{}
		case "first":
			nilArgs("first", args, at)
			if len(r.Elems) == 0 {
				return NoneV{}
			}
			return some(r.Elems[0])
		case "last":
			nilArgs("last", args, at)
			if len(r.Elems) == 0 {
				return NoneV{}
			}
			return some(r.Elems[len(r.Elems)-1])
		case "pop":
			// Option, not a panic: popping an empty list is the loop
			// condition of every worklist algorithm, so it is an
			// answer rather than a bug. Indexing keeps the opposite
			// rule — `xs[i]` out of bounds *is* a bug — because there
			// the caller named a specific slot.
			nilArgs("pop", args, at)
			if len(r.Elems) == 0 {
				return NoneV{}
			}
			last := r.Elems[len(r.Elems)-1]
			r.Elems = r.Elems[:len(r.Elems)-1]
			return some(last)
		case "insert":
			if len(args) != 2 {
				panic(rtErr{at, "insert takes (i, v)"})
			}
			i := listIndex("insert", args[0], len(r.Elems)+1, at) // == len appends
			r.Elems = append(r.Elems, nil)
			copy(r.Elems[i+1:], r.Elems[i:])
			r.Elems[i] = args[1]
			return UnitV{}
		case "remove":
			i := listIndex("remove", one("remove", args, at), len(r.Elems), at)
			v := r.Elems[i]
			r.Elems = append(r.Elems[:i], r.Elems[i+1:]...)
			return v
		case "extend":
			other, ok := one("extend", args, at).(*ListV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("extend takes a List, got %s", typeName(args[0]))})
			}
			// Copy the source first: xs.extend(xs) must double the
			// list, not loop forever re-reading a slice it is growing.
			r.Elems = append(r.Elems, append([]Value{}, other.Elems...)...)
			return UnitV{}
		case "reversed":
			// A copy, named like `sorted()`. The pairing is deliberate:
			// in this stdlib a past participle returns a new list and a
			// verb mutates (`sort_by`, `push`), so the name says which
			// one you got without looking it up.
			nilArgs("reversed", args, at)
			out := &ListV{Elems: make([]Value, len(r.Elems))}
			for i, e := range r.Elems {
				out.Elems[len(r.Elems)-1-i] = e
			}
			return out
		case "slice":
			if len(args) != 2 {
				panic(rtErr{at, "slice takes (lo, hi)"})
			}
			lo := listIndex("slice", args[0], len(r.Elems)+1, at)
			hi := listIndex("slice", args[1], len(r.Elems)+1, at)
			if lo > hi {
				panic(rtErr{at, fmt.Sprintf("slice(%d, %d): lo is past hi", lo, hi)})
			}
			// Half-open [lo, hi), and a *copy*: Glide has no slice
			// aliasing anywhere else, and a view that shares storage
			// with its parent would make `mut` a lie about a second
			// binding the reader cannot see.
			return &ListV{Elems: append([]Value{}, r.Elems[lo:hi]...)}
		case "sorted":
			if len(args) != 0 {
				panic(rtErr{at, "sorted takes no arguments"})
			}
			out := &ListV{Elems: append([]Value{}, r.Elems...)}
			var sortErr any
			func() {
				defer func() { sortErr = recover() }()
				sort.SliceStable(out.Elems, func(i, j int) bool {
					return in.less(out.Elems[i], out.Elems[j], at)
				})
			}()
			if sortErr != nil {
				panic(sortErr)
			}
			return out
		case "push":
			r.Elems = append(r.Elems, one("push", args, at))
			return UnitV{}
		case "join":
			sep := strArg(name, args, at)
			var b strings.Builder
			for i, e := range r.Elems {
				s, ok := e.(StrV)
				if !ok {
					panic(rtErr{at, fmt.Sprintf("join needs a List<String>; element %d is %s", i, typeName(e))})
				}
				if i > 0 {
					b.WriteString(string(sep))
				}
				b.WriteString(string(s))
			}
			return StrV(b.String())
		case "repeat":
			arg := one("repeat", args, at)
			k, ok := arg.(IntV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("repeat takes an Int count, got %s", typeName(arg))})
			}
			if k < 0 {
				panic(rtErr{at, fmt.Sprintf("repeat count must be >= 0, got %d", k)})
			}
			// Shallow: the same element values appear k times. With a
			// reference element ([[]].repeat(2)) that is two slots
			// sharing one list — documented in stdlib.md.
			if len(r.Elems) > 0 && int(k) > math.MaxInt/len(r.Elems) {
				panic(rtErr{at, fmt.Sprintf("repeat(%d) of a %d-element list is too large", k, len(r.Elems))})
			}
			out := &ListV{Elems: make([]Value, 0, len(r.Elems)*int(k))}
			for range int(k) {
				out.Elems = append(out.Elems, r.Elems...)
			}
			return out
		case "iter":
			if len(args) != 0 {
				panic(rtErr{at, "iter takes no arguments"})
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
			f := one("sort_by", args, at)
			// Stable, so ties keep first-seen order — deterministic
			// programs without a tiebreak dance.
			var sortErr any
			func() {
				defer func() { sortErr = recover() }()
				sort.SliceStable(r.Elems, func(i, j int) bool {
					c := in.callValue(f, []Value{r.Elems[i], r.Elems[j]}, at)
					n, ok := c.(IntV)
					if !ok {
						panic(rtErr{at, fmt.Sprintf("sort_by comparator must return an Int (cmp order), got %s", typeName(c))})
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
				panic(rtErr{at, "iter takes no arguments"})
			}
			return &IterV{Next: in.iterate(r, at)}
		}
	case *MapV:
		switch name {
		case "iter":
			if len(args) != 0 {
				panic(rtErr{at, "iter takes no arguments"})
			}
			return &IterV{Next: in.iterate(r, at)}
		case "len":
			return IntV(len(r.keys))
		case "entries":
			if len(args) != 0 {
				panic(rtErr{at, "entries takes no arguments"})
			}
			l := &ListV{}
			for _, k := range r.keys {
				l.Elems = append(l.Elems, TupleV{k, r.m[k]})
			}
			return l
		case "keys":
			nilArgs("keys", args, at)
			return &ListV{Elems: append([]Value{}, r.keys...)}
		case "values":
			nilArgs("values", args, at)
			l := &ListV{Elems: make([]Value, 0, len(r.keys))}
			for _, k := range r.keys {
				l.Elems = append(l.Elems, r.m[k])
			}
			return l
		case "contains_key":
			_, found := r.get(hashable(one("contains_key", args, at), at))
			return BoolV(found)
		case "remove":
			// Returns what was there, so the common "take it out and
			// use it" is one lookup. Deleting drops the key from the
			// insertion order; re-inserting later appends at the end,
			// which is the ordering rule M4c specified.
			k := hashable(one("remove", args, at), at)
			v, found := r.get(k)
			if !found {
				return NoneV{}
			}
			delete(r.m, k)
			for i, existing := range r.keys {
				if existing == k {
					r.keys = append(r.keys[:i], r.keys[i+1:]...)
					break
				}
			}
			return some(v)
		}
	case *SenderV:
		switch name {
		case "send":
			in.chanSend(r, one("send", args, at), at)
			return UnitV{}
		case "close":
			if len(args) != 0 {
				panic(rtErr{at, "close takes no arguments"})
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
				panic(rtErr{at, "recv takes no arguments"})
			}
			return in.chanRecv(r)
		case "close":
			panic(rtErr{at, "only the sender half closes a channel"})
		}
	case *RouterV:
		switch name {
		case "get", "post", "put", "delete":
			if len(args) != 2 {
				panic(rtErr{at, fmt.Sprintf("%s takes (pattern, handler)", name)})
			}
			pat, ok := args[0].(StrV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("%s: the pattern is a String", name)})
			}
			switch args[1].(type) {
			case *ClosureV, *FuncV:
			default:
				panic(rtErr{at, fmt.Sprintf("%s: the handler is a function, got %s", name, typeName(args[1]))})
			}
			r.routes = append(r.routes, route{method: strings.ToUpper(name), pattern: string(pat), handler: args[1]})
			return UnitV{}
		}
	case *RequestV:
		switch name {
		case "path_param":
			p := strArg(name, args, at)
			v := r.r.PathValue(string(p))
			if v == "" {
				return NoneV{}
			}
			return some(StrV(v))
		case "body":
			if len(args) != 0 {
				panic(rtErr{at, "body takes no arguments"})
			}
			return StrV(r.body)
		case "method":
			if len(args) != 0 {
				panic(rtErr{at, "method takes no arguments"})
			}
			return StrV(r.r.Method)
		case "path":
			if len(args) != 0 {
				panic(rtErr{at, "path takes no arguments"})
			}
			return StrV(r.r.URL.Path)
		}
	case *ResponseV:
		switch name {
		case "status":
			if len(args) != 0 {
				panic(rtErr{at, "status takes no arguments"})
			}
			return IntV(r.status)
		case "body":
			if len(args) != 0 {
				panic(rtErr{at, "body takes no arguments"})
			}
			return StrV(r.body)
		}
	case *OutputV:
		if v, handled := r.method(name, args, at); handled {
			return v
		}
	case *DbV:
		return in.sqlMethod(r, name, args, at)
	case *ScopeV:
		switch name {
		case "spawn":
			return in.spawnTask(r, one("spawn", args, at), at)
		case "deadline":
			if len(args) != 0 {
				panic(rtErr{at, "deadline takes no arguments"})
			}
			if r.st.deadline.IsZero() {
				return NoneV{}
			}
			return some(InstantV{T: r.st.deadline})
		}
	case *TaskV:
		switch name {
		case "join":
			if len(args) != 0 {
				panic(rtErr{at, "join takes no arguments"})
			}
			return in.joinTask(r, at)
		}
	case *ResultV:
		switch name {
		case "context":
			msg, ok := one("context", args, at).(StrV)
			if !ok {
				panic(rtErr{at, "context takes a String"})
			}
			if r.Ok {
				return r
			}
			return &ResultV{Ok: false, V: &ErrV{Msg: string(msg), Cause: r.V}}
		}
	case *IterV:
		switch name {
		case "take":
			n, ok := one("take", args, at).(IntV)
			if !ok {
				panic(rtErr{at, "take takes an Int"})
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
				panic(rtErr{at, "collect takes no arguments"})
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
			f := one("map", args, at)
			return &IterV{Next: func() (Value, bool) {
				v, ok := r.Next()
				if !ok {
					return nil, false
				}
				return in.callValue(f, []Value{v}, at), true
			}}
		case "filter":
			f := one("filter", args, at)
			return &IterV{Next: func() (Value, bool) {
				for {
					v, ok := r.Next()
					if !ok {
						return nil, false
					}
					keep, isBool := in.callValue(f, []Value{v}, at).(BoolV)
					if !isBool {
						panic(rtErr{at, "filter predicate must return Bool"})
					}
					if bool(keep) {
						return v, true
					}
				}
			}}
		case "enumerate":
			if len(args) != 0 {
				panic(rtErr{at, "enumerate takes no arguments"})
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
			other := in.iterate(one("zip", args, at), at)
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
				panic(rtErr{at, "count takes no arguments"})
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
				panic(rtErr{at, "sum takes no arguments"})
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
				acc = in.binop("+", acc, v, at)
			}
		}
	}
	panic(rtErr{at, fmt.Sprintf("%s has no method %q", typeName(recv), name)})
}

// userCmp orders two values of the same user type through the `cmp`
// method Ord requires. Reports ok=false for anything that is not a
// matching pair of user types, so builtins fall through to the
// built-in comparisons untouched.
func (in *Interp) userCmp(l, r Value, at source.Span) (int, bool) {
	tn := typeName(l)
	if typeName(r) != tn {
		return 0, false
	}
	switch l.(type) {
	case *StructV, *VariantV, *DistinctV:
	default:
		return 0, false
	}
	m := in.findMethod(tn, "cmp", at)
	if m == nil {
		return 0, false
	}
	res := in.callFuncSelf(m, l, []Value{r})
	n, ok := res.(IntV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s.cmp must return an Int, got %s", tn, typeName(res))})
	}
	return int(n), true
}

func isOrderOp(op string) bool {
	switch op {
	case "<", "<=", ">", ">=":
		return true
	}
	return false
}

// naturalLess is the ordering `sorted()` uses. It is defined as
// totalCmp so the two can never disagree — `a.cmp(b) < 0` and
// `sorted()` must agree about every pair, and having written them
// separately once is how NaN would end up ordered one way by the
// method and another by the sort.
func naturalLess(a, b Value, at source.Span) bool {
	return builtinCmp(a, b, at) < 0
}

// less is naturalLess plus user types, so `sorted()` and `<` order a
// value the same way. Having those two disagree would be the worst
// kind of bug: silent, and visible only in the output order.
func (in *Interp) less(a, b Value, at source.Span) bool {
	if n, ok := in.userCmp(a, b, at); ok {
		return n < 0
	}
	return builtinCmp(a, b, at) < 0
}

// builtinCmp is the three-way comparison of two builtin values of the
// same type: negative, zero, positive.
//
// Float is a **total** order, which IEEE 754 is not: NaN sorts after
// every number and equals itself, and -0.0 compares equal to 0.0. So
// `NaN.cmp(NaN)` is 0 while `NaN == NaN` is false. That inconsistency
// is deliberate and is what Java's Double.compare and Rust's
// total_cmp both ship — a sort needs a total order, and equality has
// to obey IEEE. Making cmp partial instead would mean sorting a list
// containing NaN could silently lose elements.
func builtinCmp(a, b Value, at source.Span) int {
	switch x := a.(type) {
	case IntV:
		if y, ok := b.(IntV); ok {
			return cmpOrdered(x, y)
		}
	case UintV:
		if y, ok := b.(UintV); ok {
			return cmpOrdered(x, y)
		}
	case SizedV:
		if y, ok := b.(SizedV); ok && x.Bits == y.Bits && x.Signed == y.Signed {
			return cmpOrdered(x.V, y.V)
		}
	case StrV:
		if y, ok := b.(StrV); ok {
			return cmpOrdered(x, y)
		}
	case RuneV:
		if y, ok := b.(RuneV); ok {
			return cmpOrdered(x, y)
		}
	case FloatV:
		if y, ok := b.(FloatV); ok {
			return floatCmp(float64(x), float64(y))
		}
	case DurationV:
		if y, ok := b.(DurationV); ok {
			return cmpOrdered(x, y)
		}
	}
	panic(rtErr{at, fmt.Sprintf("cannot order %s against %s", typeName(a), typeName(b))})
}

func cmpOrdered[T int64 | uint64 | string | rune | IntV | UintV | StrV | RuneV | DurationV](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// floatCmp is the total order described on builtinCmp: NaN last, NaN
// equal to itself, -0.0 equal to 0.0.
func floatCmp(a, b float64) int {
	an, bn := math.IsNaN(a), math.IsNaN(b)
	switch {
	case an && bn:
		return 0
	case an:
		return 1
	case bn:
		return -1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// iterate adapts any iterable to a next() function for `for … in`.
func (in *Interp) iterate(v Value, at source.Span) func() (Value, bool) {
	switch it := v.(type) {
	case *IterV:
		return it.Next
	case *StructV:
		// Anything with an iter() method is iterable.
		if m := in.findMethod(it.Type, "iter", at); m != nil {
			res := in.callFuncSelf(m, it, nil)
			iv, ok := res.(*IterV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("%s.iter() must return an Iterator, got %s", it.Type, typeName(res))})
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
			// recv returns an Option: None is closed-and-drained. Boxed,
			// so a *sent* None is an ordinary element rather than an
			// end-of-stream signal — the M2 wart, gone.
			inner, ok := readOption(in.chanRecv(it), at)
			return inner, ok
		}
	}
	panic(rtErr{at, fmt.Sprintf("%s is not iterable", typeName(v))})
}
