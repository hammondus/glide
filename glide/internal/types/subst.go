package types

// Subst replaces type parameters by name. It is used for two jobs that
// are the same job: instantiating a user generic (`Tree<Int>` binds
// that declaration's `T`), and instantiating a builtin method
// signature (`List<Int>.push` binds the table's `T`).
//
// Substitution is by *name*, not by identity, because a type parameter
// is scoped to one declaration and the checker never has two live
// scopes at once. If that ever stops being true — nested generic
// closures capturing an outer `T` — this becomes the place it breaks,
// loudly, rather than silently binding the wrong one.
func Subst(t Type, m map[string]Type) Type {
	if len(m) == 0 || t == nil {
		return t
	}
	switch x := t.(type) {
	case *Var:
		if r, ok := m[x.Name]; ok {
			return r
		}
		return x
	case *App:
		args, changed := substAll(x.Args, m)
		if !changed {
			return x
		}
		return &App{C: x.C, Args: args}
	case *Named:
		// A Named's Fields/Variants/Base are resolved lazily against
		// Args, so substituting the arguments is enough; nothing else
		// needs rewriting here.
		args, changed := substAll(x.Args, m)
		if !changed {
			return x
		}
		out := *x
		out.Args = args
		return &out
	case *Tuple:
		elems, changed := substAll(x.Elems, m)
		if !changed {
			return x
		}
		return &Tuple{Elems: elems}
	case *Func:
		out := *x
		out.Params = make([]Param, len(x.Params))
		copy(out.Params, x.Params)
		changed := false
		for i := range out.Params {
			if s := Subst(out.Params[i].Type, m); s != out.Params[i].Type {
				out.Params[i].Type = s
				changed = true
			}
		}
		if r := Subst(x.Ret, m); r != x.Ret {
			out.Ret = r
			changed = true
		}
		if !changed {
			return x
		}
		return &out
	}
	return t
}

func substAll(ts []Type, m map[string]Type) ([]Type, bool) {
	var out []Type
	changed := false
	for i, t := range ts {
		s := Subst(t, m)
		if s != t && !changed {
			changed = true
			out = make([]Type, len(ts))
			copy(out, ts[:i])
		}
		if changed {
			out[i] = s
		}
	}
	return out, changed
}
