package types_test

import (
	"testing"

	"glide/internal/types"
)

func TestString(t *testing.T) {
	for _, tc := range []struct {
		t    types.Type
		want string
	}{
		{types.Int, "Int"},
		{types.Opt(types.String), "String?"},
		// Option<Option<T>> has no `?` spelling that isn't ambiguous,
		// so it prints long-form rather than `T??`.
		{types.Opt(types.Opt(types.Int)), "Option<Int?>"},
		{types.Apply(types.List, types.Int), "List<Int>"},
		{types.Apply(types.Map, types.String, types.Apply(types.List, types.Int)), "Map<String, List<Int>>"},
		{types.Apply(types.Result, types.Unit, types.Apply(types.Error)), "Result<(), Error>"},
		{types.Apply(types.Router), "Router"},
		{&types.Tuple{Elems: []types.Type{types.Int, types.String}}, "(Int, String)"},
		{types.Unknown, "?"},
	} {
		if got := tc.t.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestIdenticalIsStrictAboutUnknown(t *testing.T) {
	if types.Identical(types.Unknown, types.Int) {
		t.Error("Identical must not treat Unknown as a wildcard")
	}
	if !types.Compatible(types.Unknown, types.Int) {
		t.Error("Compatible must treat Unknown as a wildcard")
	}
	// The wildcard reaches inside a constructor: a bare `None` is
	// Option<?> and has to satisfy an Option<String>.
	if !types.Compatible(types.Opt(types.Unknown), types.Opt(types.String)) {
		t.Error("Unknown should be a wildcard at any depth")
	}
	// Int and i64 are one type, spelled two ways.
	if types.Primitives["i64"] != types.Int {
		t.Error("i64 must resolve to Int itself, not a lookalike")
	}
}

func TestAssignability(t *testing.T) {
	list := types.Apply(types.List, types.Int)
	for _, tc := range []struct {
		v, dst types.Type
		want   bool
		why    string
	}{
		{types.Int, types.Int, true, "identical"},
		{types.Int, types.Opt(types.Int), true, "implicit T -> T?"},
		{types.Opt(types.Int), types.Int, false, "T? -> T needs unwrapping"},
		{types.UntypedInt, types.U8, true, "a literal lands in any numeric type"},
		{types.UntypedInt, types.Float, true, "1 is a fine Float"},
		{types.UntypedFloat, types.Int, false, "1.5 is not an Int"},
		{types.UntypedInt, types.Rune, false, "Rune is out of the integer lattice"},
		{types.I32, types.Int, false, "no implicit numeric conversion"},
		{types.Int, types.Primitives["i64"], true, "i64 is Int"},
		{types.UntypedInt, types.Opt(types.Int), true, "literal wraps into an Option"},
		{list, types.Apply(types.List, types.String), false, "element types differ"},
	} {
		if got := types.AssignableTo(tc.v, tc.dst); got != tc.want {
			t.Errorf("AssignableTo(%s, %s) = %v, want %v (%s)", tc.v, tc.dst, got, tc.want, tc.why)
		}
	}
}

func TestJoin(t *testing.T) {
	for _, tc := range []struct {
		a, b types.Type
		want string // "" = no join
	}{
		{types.Int, types.Int, "Int"},
		{types.Int, types.String, ""},
		{types.UntypedInt, types.Float, "Float"},
		{types.Int, types.Opt(types.Int), "Int?"},
		{types.Opt(types.Unknown), types.Opt(types.Int), "Int?"},
		{types.Unknown, types.String, "String"},
		// The known arm fills the unknown arm's hole: an empty list
		// literal joined with [1] is a List<Int>, not a List<?>.
		{types.Apply(types.List, types.Unknown), types.Apply(types.List, types.Int), "List<Int>"},
	} {
		got := types.Join(tc.a, tc.b)
		if tc.want == "" {
			if got != nil {
				t.Errorf("Join(%s, %s) = %s, want no join", tc.a, tc.b, got)
			}
			continue
		}
		if got == nil || got.String() != tc.want {
			t.Errorf("Join(%s, %s) = %v, want %s", tc.a, tc.b, got, tc.want)
		}
	}
}

// Constants are a magnitude plus a sign, which is the only shape in
// which both ends of the range are expressible: i64's minimum is 2^63
// and u64's maximum is 2^64-1, and no int64 holds either.
func TestFitsIn(t *testing.T) {
	for _, tc := range []struct {
		mag  uint64
		neg  bool
		b    *types.Basic
		want bool
	}{
		{300, false, types.U8, false},
		{255, false, types.U8, true},
		{1, true, types.U8, false},
		{0, true, types.U8, true}, // -0 is 0
		{127, false, types.I8, true},
		{128, false, types.I8, false},
		{128, true, types.I8, true}, // the negative side reaches one further
		{1 << 40, false, types.Int, true},
		{1 << 40, false, types.I32, false},
		{1 << 63, true, types.Int, true},     // i64 minimum, unwritable before M4b
		{1 << 63, false, types.Int, false},   // one past i64 maximum
		{^uint64(0), false, types.U64, true}, // u64 maximum
		{^uint64(0), false, types.Int, false},
	} {
		if got := types.FitsIn(tc.mag, tc.neg, tc.b); got != tc.want {
			t.Errorf("FitsIn(%d, neg=%v, %s) = %v, want %v", tc.mag, tc.neg, tc.b, got, tc.want)
		}
	}
}
