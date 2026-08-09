package check

import (
	"glide/internal/ast"
	"glide/internal/program"
	"glide/internal/source"
	"glide/internal/types"
)

// resolve turns a type as *written* into a type as *meant*.
//
// Resolution order is deliberate: type parameters in scope beat
// everything (so `T` inside `fn f<T>(x: T)` is the parameter, not a
// type named T declared elsewhere), then the built-in constructors,
// then the primitives, then the program's own declarations. That makes
// `List` and `Int` effectively reserved — program.Load does not stop a
// program declaring `type List = …`, and this is where such a
// declaration becomes unreachable rather than ambiguous.
func (c *checker) resolve(te *ast.TypeExpr) types.Type {
	if te == nil {
		return types.Unknown
	}
	var t types.Type
	switch te.Kind {
	case ast.TypeUnit:
		t = types.Unit
	case ast.TypeTuple:
		elems := make([]types.Type, len(te.Elems))
		for i, e := range te.Elems {
			elems[i] = c.resolve(e)
		}
		t = &types.Tuple{Elems: elems}
	case ast.TypeFunc:
		f := &types.Func{Ret: types.Unit}
		for _, e := range te.Elems {
			f.Params = append(f.Params, types.Param{Type: c.resolve(e)})
		}
		if te.Ret != nil {
			f.Ret = c.resolve(te.Ret)
		}
		t = f
	default:
		t = c.resolveName(te)
	}
	if te.Optional {
		t = types.Opt(t)
	}
	return t
}

func (c *checker) resolveName(te *ast.TypeExpr) types.Type {
	// `Self` is the receiver's type: the concrete type inside an
	// impl, the trait's own type variable inside a trait. It has to
	// beat the type parameters, because a generic trait's `Self` is
	// not one of its parameters.
	if te.Name == "Self" && c.selfT != nil {
		if len(te.Args) > 0 {
			c.errf(te.Span, "Self takes no type arguments")
		}
		return c.selfT
	}
	if v, ok := c.tparams[te.Name]; ok {
		if len(te.Args) > 0 {
			c.errf(te.Span, "type parameter %s takes no type arguments", te.Name)
		}
		return v
	}
	if ctor, ok := types.Builtins[te.Name]; ok {
		return c.resolveCtor(ctor, te)
	}
	if b, ok := types.Primitives[te.Name]; ok {
		if len(te.Args) > 0 {
			c.errf(te.Span, "%s takes no type arguments", te.Name)
		}
		return b
	}
	proto, ok := c.named[te.Name]
	if !ok {
		c.errf(te.Span, "unknown type %q", te.Name)
		return types.Unknown
	}
	args := c.resolveArgs(te)
	switch {
	case len(proto.Params) == 0 && len(args) > 0:
		c.errf(te.Span, "%s takes no type arguments", te.Name)
		return proto
	case len(proto.Params) > 0 && len(args) == 0:
		// `Tree` where `Tree<T>` was declared. Not an error yet — an
		// unapplied generic is Unknown-parameterised, so the checker
		// keeps going and reports the real mismatch at the use site
		// instead of a second complaint here.
		args = make([]types.Type, len(proto.Params))
		for i := range args {
			args[i] = types.Unknown
		}
	case len(args) != len(proto.Params):
		c.errf(te.Span, "%s takes %d type argument(s), got %d",
			te.Name, len(proto.Params), len(args))
		return types.Unknown
	}
	return proto.Instantiate(args)
}

func (c *checker) resolveCtor(ctor types.Ctor, te *ast.TypeExpr) types.Type {
	args := c.resolveArgs(te)
	if want := ctor.Arity(); len(args) != want {
		if len(args) == 0 {
			// An unapplied constructor: `List` with no `<T>`. Same
			// reasoning as an unapplied user generic — fill with
			// Unknown and let the use site do the complaining.
			args = make([]types.Type, want)
			for i := range args {
				args[i] = types.Unknown
			}
		} else {
			c.errf(te.Span, "%s takes %d type argument(s), got %d", ctor, want, len(args))
			return types.Unknown
		}
	}
	return types.Apply(ctor, args...)
}

func (c *checker) resolveArgs(te *ast.TypeExpr) []types.Type {
	if len(te.Args) == 0 {
		return nil
	}
	out := make([]types.Type, len(te.Args))
	for i, a := range te.Args {
		out[i] = c.resolve(a)
	}
	return out
}

// declareTypes builds the *Named for every declared type, in two
// passes: create them all first, then fill in fields and variants.
// One pass would fail on mutual recursion (`type A = struct { b: B }`
// alongside `type B = struct { a: A? }`), which is ordinary code, not
// an edge case.
func (c *checker) declareTypes(tab *program.Table) {
	// Timeout is the language's own type: `scope(timeout: d)`
	// evaluates to Result<T, Timeout>, and the value it produces is a
	// bare variant so `Err(Timeout)` matches and `fn from(t: Timeout)`
	// converts. It is declared here rather than reserved, so a program
	// that declares its own Timeout simply wins.
	if _, userDeclared := tab.Types["Timeout"]; !userDeclared {
		to := &types.Named{Name: "Timeout"}
		to.Variants = []*types.Variant{{Name: "Timeout", Owner: to}}
		c.named["Timeout"] = to
	}
	for name, td := range tab.Types {
		n := &types.Named{Name: name, Decl: td}
		for _, tp := range td.TypeParams {
			n.Params = append(n.Params, &types.Var{Name: tp.Name, Bounds: boundNames(tp.Bounds)})
		}
		c.named[name] = n
	}
	for name, td := range tab.Types {
		n := c.named[name]
		// The declaration's own parameters are in scope while its
		// fields resolve, and only there.
		c.withParams(n.Params, func() {
			switch {
			case td.Distinct != nil:
				n.Base = c.resolve(td.Distinct)
			case td.Variants != nil:
				for i := range td.Variants {
					n.Variants = append(n.Variants, c.variant(n, &td.Variants[i]))
				}
			default:
				for _, f := range td.Fields {
					n.Fields = append(n.Fields, types.Field{
						Name: f.Name, Type: c.resolve(f.Type), Pub: f.Pub,
					})
				}
			}
		})
	}
}

func (c *checker) variant(owner *types.Named, vd *ast.VariantDecl) *types.Variant {
	v := &types.Variant{Name: vd.Name, Owner: owner}
	for _, f := range vd.Fields {
		v.Fields = append(v.Fields, types.Field{Name: f.Name, Type: c.resolve(f.Type), Pub: f.Pub})
	}
	for _, pt := range vd.Payload {
		v.Args = append(v.Args, c.resolve(pt))
	}
	return v
}

func boundNames(bs []*ast.TypeExpr) []string {
	if len(bs) == 0 {
		return nil
	}
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
	}
	return out
}

// withParams runs f with these type parameters in scope, restoring the
// previous scope afterwards. Type parameters do not nest in Glide (a
// nested fn cannot capture, so it cannot capture a `T` either), so a
// save/restore of the whole map is correct and cheaper than a chain.
func (c *checker) withParams(vs []*types.Var, f func()) {
	if len(vs) == 0 {
		f()
		return
	}
	saved := c.tparams
	c.tparams = make(map[string]*types.Var, len(saved)+len(vs))
	for k, v := range saved {
		c.tparams[k] = v
	}
	for _, v := range vs {
		c.tparams[v.Name] = v
	}
	f()
	c.tparams = saved
}

// signature resolves a function declaration's type. Type parameters
// are in scope for the whole signature, including the return type.
func (c *checker) signature(fd *ast.FuncDecl) *types.Func {
	f := &types.Func{Name: fd.Name, Self: fd.Self, Decl: fd}
	for _, tp := range fd.TypeParams {
		f.TypeParams = append(f.TypeParams, &types.Var{Name: tp.Name, Bounds: boundNames(tp.Bounds)})
	}
	c.withParams(f.TypeParams, func() {
		for _, prm := range fd.Params {
			f.Params = append(f.Params, types.Param{
				Name:       prm.Name,
				Type:       c.resolve(prm.Type),
				HasDefault: prm.Default != nil,
			})
		}
		if fd.RetType == nil {
			f.Ret = types.Unit
		} else {
			f.Ret = c.resolve(fd.RetType)
		}
	})
	return f
}

func (c *checker) errf(at source.Span, format string, args ...any) {
	c.bag.Add(at, format, args...)
}
