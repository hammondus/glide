package interp

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
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
}

func one(name string, args []Value, line int) Value {
	if len(args) != 1 {
		panic(rtErr{line, fmt.Sprintf("%s takes exactly one argument, got %d", name, len(args))})
	}
	return args[0]
}

func displayArg(name string, args []Value, line int) string {
	return display(one(name, args, line))
}

// Module shims: Go code behind Glide interfaces (the recorded stdlib
// strategy for the interpreter tier).
func (in *Interp) moduleCall(mod, name string, args []Value, line int) Value {
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
		panic(exitPanic{code: int(code)})
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
	panic(rtErr{line, fmt.Sprintf("cannot order %s against %s", typeName(a), typeName(b))})
}

// iterate adapts any iterable to a next() function for `for … in`.
func (in *Interp) iterate(v Value, line int) func() (Value, bool) {
	switch it := v.(type) {
	case *IterV:
		return it.Next
	case *StructV:
		// Anything with an iter() method is iterable.
		if m := in.methods[it.Type]["iter"]; m != nil {
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
	}
	panic(rtErr{line, fmt.Sprintf("%s is not iterable", typeName(v))})
}
