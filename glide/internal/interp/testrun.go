package interp

import (
	"fmt"
	"io"
	"math/rand"
	"strings"

	"glide/internal/ast"
)

// RunTests executes every `test` block. Parameterless tests run once;
// tests with parameters are property tests: inputs are generated,
// and a failing input is shrunk to the smallest failure we can find.
// The seed is fixed so failures reproduce; a --seed flag can arrive
// when someone needs it.
const testCases = 100

func RunTests(f *ast.File, out io.Writer) (failed int) {
	in := New()
	in.Stdout = out
	in.Stderr = out
	if err := in.load(f); err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return 1
	}
	if len(f.Tests) == 0 && len(f.Benches) == 0 {
		fmt.Fprintln(out, "no tests")
		return 0
	}
	for ti, t := range f.Tests {
		rng := rand.New(rand.NewSource(int64(42 + ti)))
		if err := in.runTest(t, rng); err != nil {
			fmt.Fprintf(out, "FAIL  %s\n      %s\n", t.Name, strings.ReplaceAll(err.Error(), "\n", "\n      "))
			failed++
			continue
		}
		if len(t.Params) == 0 {
			fmt.Fprintf(out, "ok    %s\n", t.Name)
		} else {
			fmt.Fprintf(out, "ok    %s  (%d cases)\n", t.Name, testCases)
		}
	}
	for _, b := range f.Benches {
		fmt.Fprintf(out, "skip  bench %q (benchmarks not implemented yet)\n", b.Name)
	}
	return failed
}

func (in *Interp) runTest(t *ast.TestDecl, rng *rand.Rand) error {
	if len(t.Params) == 0 {
		return in.runCase(t, nil)
	}
	for c := 0; c < testCases; c++ {
		args := make([]Value, len(t.Params))
		for i, p := range t.Params {
			v, err := generate(p.Type, rng, c)
			if err != nil {
				return fmt.Errorf("parameter %s: %v", p.Name, err)
			}
			args[i] = v
		}
		if err := in.runCase(t, args); err != nil {
			args = in.shrink(t, args)
			// Re-run the minimal case for its (possibly different)
			// failure message.
			ferr := in.runCase(t, args)
			if ferr == nil { // shrink raced into a pass; report the original
				ferr = err
			}
			return fmt.Errorf("%s\n%v", describeArgs(t.Params, args), ferr)
		}
	}
	return nil
}

func (in *Interp) runCase(t *ast.TestDecl, args []Value) (err error) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case testFail:
			err = in.errAt(p.at, "%s", p.msg)
		case rtErr:
			err = in.errAt(p.at, "%s", p.msg)
		case exitPanic:
			err = fmt.Errorf("test called os.exit(%d)", p.code)
		default:
			panic(p)
		}
	}()
	in.enterRoot()
	defer in.exitRoot()
	env := newEnv(in.global, true)
	for i, p := range t.Params {
		env.declare(p.Name, args[i], false, t.Span)
	}
	_, _ = in.evalBlock(t.Body, env)
	return nil
}

func describeArgs(params []ast.Param, args []Value) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = fmt.Sprintf("%s = %s", params[i].Name, render(a, true))
	}
	return "input: " + strings.Join(parts, ", ")
}

// generate builds a value for a parameter type. Case 0 is always the
// simplest value the type has — the empty-case bug is too common to
// leave to chance.
//
// Matching on the TypeExpr rather than its spelling is what lets
// List<T> recurse: M1-M3 switched on the string "List<Int>" and so
// supported exactly that one list. shrinkValue is already structural,
// so nothing downstream needed to change.
func generate(typ *ast.TypeExpr, rng *rand.Rand, caseNo int) (Value, error) {
	if typ == nil || typ.Kind != ast.TypeName || typ.Optional {
		return nil, fmt.Errorf("cannot generate values of type %s (supported: Int, Bool, String, List<T>)", typ)
	}
	switch typ.Name {
	case "Int":
		if len(typ.Args) != 0 {
			break
		}
		if caseNo == 0 {
			return IntV(0), nil
		}
		return IntV(rng.Intn(201) - 100), nil
	case "Bool":
		if len(typ.Args) != 0 {
			break
		}
		return BoolV(caseNo%2 == 0), nil
	case "String":
		if len(typ.Args) != 0 {
			break
		}
		if caseNo == 0 {
			return StrV(""), nil
		}
		n := rng.Intn(12)
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteByte(byte('a' + rng.Intn(26)))
		}
		return StrV(sb.String()), nil
	case "List":
		if len(typ.Args) != 1 {
			break
		}
		if caseNo == 0 {
			return &ListV{}, nil // case 0 is the empty list, whatever the element type
		}
		n := rng.Intn(30)
		l := &ListV{}
		for i := 0; i < n; i++ {
			// Element case numbers start at 1: case 0 is reserved for
			// "the simplest value", and a list of thirty zeroes is a
			// worse test than a list of thirty random elements.
			e, err := generate(typ.Args[0], rng, i+1)
			if err != nil {
				return nil, err
			}
			l.Elems = append(l.Elems, e)
		}
		return l, nil
	}
	return nil, fmt.Errorf("cannot generate values of type %s (supported: Int, Bool, String, List<T>)", typ)
}

// shrink greedily minimises failing inputs: for lists, fewer
// elements, then smaller ones; for ints, toward zero.
func (in *Interp) shrink(t *ast.TestDecl, args []Value) []Value {
	cur := args
	for budget := 0; budget < 500; budget++ {
		improved := false
		for i := range cur {
			for _, cand := range shrinkValue(cur[i]) {
				trial := append([]Value{}, cur...)
				trial[i] = cand
				if in.runCase(t, trial) != nil {
					cur = trial
					improved = true
					break
				}
			}
			if improved {
				break
			}
		}
		if !improved {
			return cur
		}
	}
	return cur
}

func shrinkValue(v Value) []Value {
	switch x := v.(type) {
	case IntV:
		if x == 0 {
			return nil
		}
		out := []Value{IntV(0)}
		if x/2 != 0 && x/2 != x {
			out = append(out, x/2)
		}
		if x < 0 {
			out = append(out, -x)
		}
		return out
	case StrV:
		if len(x) == 0 {
			return nil
		}
		return []Value{StrV(""), x[:len(x)/2]}
	case *ListV:
		n := len(x.Elems)
		if n == 0 {
			return nil
		}
		var out []Value
		out = append(out, &ListV{})
		if n > 1 {
			out = append(out, &ListV{Elems: append([]Value{}, x.Elems[:n/2]...)})
			out = append(out, &ListV{Elems: append([]Value{}, x.Elems[n/2:]...)})
		}
		for i := 0; i < n; i++ {
			cand := &ListV{Elems: append([]Value{}, x.Elems[:i]...)}
			cand.Elems = append(cand.Elems, x.Elems[i+1:]...)
			out = append(out, cand)
		}
		for i, e := range x.Elems {
			for _, se := range shrinkValue(e) {
				cand := &ListV{Elems: append([]Value{}, x.Elems...)}
				cand.Elems[i] = se
				out = append(out, cand)
			}
		}
		return out
	}
	return nil
}
