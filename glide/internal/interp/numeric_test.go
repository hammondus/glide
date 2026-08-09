package interp

import (
	"strings"
	"testing"
)

// The six narrow widths carry their own size at runtime, so they trap
// at that size rather than at i64's. Every case here answered
// silently and wrongly before M4c: `let x: u8 = 250` then `x + 10`
// printed 260.
func TestSizedOverflowTraps(t *testing.T) {
	for _, tc := range []struct{ name, body, wantErr string }{
		{"u8 add", "let x: u8 = 250\n    _ = x + 10", "u8 overflow: 250 + 10"},
		{"u8 sub below zero", "let x: u8 = 5\n    _ = x - 10", "u8 overflow: 5 - 10"},
		{"u8 mul", "let x: u8 = 16\n    _ = x * 16", "u8 overflow: 16 * 16"},
		{"i8 add", "let x: i8 = 127\n    _ = x + 1", "i8 overflow: 127 + 1"},
		{"i8 sub", "let x: i8 = -128\n    _ = x - 1", "i8 overflow: -128 - 1"},
		{"i8 negate min", "let x: i8 = -128\n    _ = -x", "i8 overflow: negating -128"},
		{"i8 min over minus one", "let x: i8 = -128\n    _ = x / -1", "i8 overflow: -128 / -1"},
		{"i16 mul", "let x: i16 = 300\n    _ = x * 300", "i16 overflow: 300 * 300"},
		{"u16 add", "let x: u16 = 65535\n    _ = x + 1", "u16 overflow: 65535 + 1"},
		{"i32 add", "let x: i32 = 2147483647\n    _ = x + 1", "i32 overflow: 2147483647 + 1"},
		{"u32 mul", "let x: u32 = 65536\n    _ = x * 65536", "u32 overflow: 65536 * 65536"},
		{"u8 divide by zero", "let x: u8 = 5\n    let z: u8 = 0\n    _ = x / z", "division by zero"},
		// Unsigned negation is a range error, not a missing operator:
		// `-x` on a u8 is a sensible thing to write and a wrong thing
		// to mean.
		{"u8 negate", "let x: u8 = 1\n    _ = -x", "u8 overflow: negating 1"},
	} {
		_, err := runProg(t, "fn main() {\n    "+tc.body+"\n}")
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want %q, got %v", tc.name, tc.wantErr, err)
		}
	}
}

// The message names the escape hatch, at every width. "My checksum
// keeps trapping" is answered by a different operator, never by a
// different build — there is no release mode that wraps.
func TestOverflowMessageNamesTheEscapeHatch(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"let x: u8 = 250\n    _ = x + 10", "use wrapping_add"},
		{"let x: u8 = 5\n    _ = x - 10", "use wrapping_sub"},
		{"let x: i16 = 300\n    _ = x * 300", "use wrapping_mul"},
		{"_ = 9223372036854775807 + 1", "use wrapping_add"},
		{"let x: u64 = 18446744073709551615\n    _ = x + 1", "use wrapping_add"},
	} {
		_, err := runProg(t, "fn main() {\n    "+tc.body+"\n}")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("want %q in the error, got %v", tc.want, err)
		}
	}
}

// Boundary values that must NOT trap: both ends of every width are
// reachable, which is the point of literals being magnitudes.
func TestSizedBoundariesAreReachable(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let a: i8 = -128
    let b: i8 = 127
    let c: u8 = 255
    let d: i16 = -32768
    let e: u16 = 65535
    let f: i32 = -2147483648
    let g: u32 = 4294967295
    println("{a} {b} {c} {d} {e} {f} {g}")
    println(b - 1)
    println(c / 5)
    println(a % -1)
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "-128 127 255 -32768 65535 -2147483648 4294967295\n126\n51\n0\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

func TestWrappingMethods(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let a: u8 = 250
    println(a.wrapping_add(10))
    println(a.wrapping_sub(255))
    println(a.wrapping_mul(3))
    let b: i8 = 127
    println(b.wrapping_add(1))
    println(b.wrapping_neg())
    let c: i8 = -128
    println(c.wrapping_neg())
    let d: u32 = 4294967295
    println(d.wrapping_mul(d))
    println(9223372036854775807.wrapping_add(1))
    println((0 - 9223372036854775807 - 1).wrapping_neg())
    let e: u64 = 18446744073709551615
    println(e.wrapping_add(1))
    println(e.wrapping_neg())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"4",                    // 260 mod 256
		"251",                  // 250-255 mod 256
		"238",                  // 750 mod 256
		"-128",                 // i8 127+1
		"-127",                 // i8 -(127)
		"-128",                 // i8 -(-128) is itself
		"1",                    // (2^32-1)^2 mod 2^32
		"-9223372036854775808", // Int max + 1
		"-9223372036854775808", // -(Int min) is itself
		"0",                    // u64 max + 1
		"1",                    // -(u64 max) mod 2^64
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// The width has to live on the value, not on the checker's annotation
// for the operator node: generics are type-erased, so by the time `+`
// runs inside double<T> there is no static type left to consult.
func TestWidthSurvivesTypeErasure(t *testing.T) {
	src := `
fn double<T>(v: T) -> T {
    v + v
}

fn main() {
    let small: u8 = 100
    println(double(small))
    let big: u8 = 200
    println(double(big))
}`
	out, err := runProg(t, src)
	if err == nil || !strings.Contains(err.Error(), "u8 overflow: 200 + 200") {
		t.Fatalf("want a u8 trap inside the generic body, got %v", err)
	}
	if out != "200\n" {
		t.Fatalf("the in-range call should still have printed: %q", out)
	}
}

// Sized values are ordinary values everywhere else: map keys, sorting,
// equality, interpolation and the json codec all have to know them, or
// a u8 is a type you can compute with but not store.
func TestSizedValuesFlowThroughTheRuntime(t *testing.T) {
	out, err := runProg(t, `
import json

fn main() {
    let mut m: Map<u8, String> = [:]
    m[200] = "two hundred"
    println(m[200])

    let l: List<i32> = [3, 1, 2]
    println(l.sorted())

    let a: u8 = 7
    println(json.encode([a]))
    println("{a}")
    println(a == 7)
    println(a.cmp(9))
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "two hundred\n[1, 2, 3]\n[7]\n7\ntrue\n-1\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// Explicit conversion, Go's spelling. Before it, a sized value could
// only come from a literal, which ruled out every use the sized types
// exist for.
func TestNumericConversion(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let n = 200
    println(u8(n))
    println(i32(u8(n)) * 1000)
    println(Int(u8(n)) + 1000)
    println(u64(n))
    println(Float(7) / 2.0)
    println(f32(1.5))
    println(Int(3.7))
    println(Int(-3.7))
    println(Int(-0.5))
    println(Int('a'))
    println(Rune(97))
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "200\n200000\n1200\n200\n3.5\n1.5\n3\n-3\n0\n97\na\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// Out of range traps rather than truncating — the one place this
// parts company with Go, where `uint8(300)` is 44 in silence. A
// language whose `+` traps at the declared width cannot hand back 44
// for the same overflow spelled as a conversion.
func TestConversionOutOfRangeTraps(t *testing.T) {
	for _, tc := range []struct{ body, wantErr string }{
		{"let n = 300\n    _ = u8(n)", "u8 overflow: 300 does not fit"},
		{"let n = 300\n    _ = u8(n)", "use wrapping_u8 to truncate"},
		{"let n = -1\n    _ = u8(n)", "u8 overflow: -1 does not fit"},
		{"let n = 128\n    _ = i8(n)", "i8 overflow: 128 does not fit"},
		{"let n = 70000\n    _ = u16(n)", "u16 overflow: 70000 does not fit"},
		{"let f = 1000000000000000000000.0\n    _ = Int(f)", "Int overflow"},
		{"let n = -1\n    _ = Rune(n)", "-1 is not a Unicode code point"},
		{"let n = 55296\n    _ = Rune(n)", "not a Unicode code point"}, // surrogate half
		{`_ = u8("hi")`, "cannot convert String to u8"},
		{"_ = String(65)", "String is not a conversion"},
	} {
		_, err := runProg(t, "fn main() {\n    "+tc.body+"\n}")
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want %q, got %v", tc.body, tc.wantErr, err)
		}
	}
}

// A constant that cannot fit is a compile error, not a trap: `u8(300)`
// is exactly as knowable as `let x: u8 = 300`.
func TestConstantConversionIsRejectedStatically(t *testing.T) {
	for _, tc := range []struct{ body, wantErr string }{
		{"_ = u8(300)", "300 does not fit in u8"},
		{"_ = i8(-129)", "does not fit in i8"},
		{"_ = Rune(1114112)", "not a Unicode code point"},
		{"_ = u8(1, 2)", "converts exactly one value"},
	} {
		_, err := runProg(t, "fn main() {\n    "+tc.body+"\n}")
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want %q, got %v", tc.body, tc.wantErr, err)
		}
	}
}

func TestWrappingConversion(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    println(123456.wrapping_u8())
    println(255.wrapping_u8())
    println(123456.wrapping_i16())
    println((0 - 1).wrapping_u8())
    println((0 - 1).wrapping_u64())
    println(200.wrapping_i8())
    let b: u8 = 255
    println(b.wrapping_i8())
    println(b.wrapping_i64())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"64",                   // 123456 mod 256
		"255",                  // already fits
		"-7616",                // 123456 mod 65536, sign-extended
		"255",                  // -1 mod 256
		"18446744073709551615", // -1 as u64
		"-56",                  // 200 mod 256, sign-extended
		"-1",                   // u8 255 reinterpreted as i8
		"255",                  // u8 255 widened
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// Primitive type names are reserved, because `u8` now means something
// in expression position: a program-level `fn u8()` would silently
// win and the conversion would vanish. Locals still shadow, as they do
// in Go, and the failure is loud.
func TestPrimitiveNamesAreReserved(t *testing.T) {
	for _, tc := range []struct{ src, wantErr string }{
		{"fn u8() -> Int { 5 }\nfn main() {}", `"u8" is a builtin and cannot be redeclared`},
		{"type Int = struct { n: Bool }\nfn main() {}", `"Int" is a builtin and cannot be redeclared`},
		{"const u8 = 5\nfn main() {}", `"u8" is a builtin and cannot be redeclared`},
		// A local shadows, and then the conversion is simply gone.
		{"fn main() {\n    let u8 = 5\n    println(u8(3))\n}", "Int is not callable"},
	} {
		_, err := runProg(t, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("want %q, got %v", tc.wantErr, err)
		}
	}
}

// Every integer width gets the same method set. Before M4c only Int
// had any, so a u64 could not even answer cmp.
func TestEveryWidthHasTheIntMethods(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let a: u64 = 5
    let b: i16 = -3
    println(a.cmp(9))
    println(b.cmp(-3))
    println(b.wrapping_add(1))
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "-1\n0\n-2\n" {
		t.Fatalf("output:\n%q", out)
	}
}

// Float's `cmp` is a TOTAL order, which IEEE 754 is not: NaN sorts
// after every number and equals itself. So `NaN.cmp(NaN)` is 0 while
// `NaN == NaN` is false. That inconsistency is deliberate — a sort
// needs a total order, equality has to obey IEEE — and it is what
// Java's Double.compare and Rust's total_cmp both ship. A partial
// `cmp` would let sorting a list containing NaN silently lose
// elements.
func TestFloatTotalOrder(t *testing.T) {
	out, err := runProg(t, `
fn main() {
    let nan = 0.0 / 0.0
    let inf = 1.0 / 0.0
    println(nan.cmp(nan))
    println(nan.cmp(1.0))
    println((1.0).cmp(nan))
    println(nan.cmp(inf))
    println((0.0).cmp(0.0 - 0.0))
    println(nan == nan)
    println([1.0, nan, 0.0 - 1.0].sorted())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"0",            // NaN equals itself under cmp
		"1",            // NaN sorts after a number
		"-1",           // and a number before NaN
		"1",            // NaN even after infinity
		"0",            // -0.0 and 0.0 compare equal, matching ==
		"false",        // but == stays IEEE
		"[-1, 1, NaN]", // so a sort puts NaN last instead of losing it
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// `<` on a user type calls its `cmp`, and `sorted()` uses the same
// path — having those two disagree would be silent and visible only
// in the output order.
func TestUserTypeOrderingUsesOneComparison(t *testing.T) {
	out, err := runProg(t, `
type P = struct { n: Int }
impl Ord for P {
    // Deliberately REVERSED, so the test can tell that the method is
    // really being called rather than fields being compared.
    fn cmp(self, other: Self) -> Int { other.n - self.n }
}
fn main() {
    let a = P{ n: 1 }
    let b = P{ n: 2 }
    println("{a < b} {a <= b} {a > b} {a >= b}")
    println([a, b].sorted())
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "false false true true\n[P{ n: 2 }, P{ n: 1 }]\n"
	if out != want {
		t.Fatalf("output:\n%q\nwant:\n%q", out, want)
	}
}

// Equality is untouched by Ord: structural, universal, needing no
// declaration and offering no way to redefine it.
func TestEqualityStaysStructural(t *testing.T) {
	out, err := runProg(t, `
type P = struct { n: Int }
impl Ord for P {
    fn cmp(self, other: Self) -> Int { 0 }   // "everything is equal"
}
fn main() {
    println(P{ n: 1 } == P{ n: 1 })
    println(P{ n: 1 } == P{ n: 2 })
    println(P{ n: 1 } <= P{ n: 2 })
}`)
	if err != nil {
		t.Fatal(err)
	}
	// cmp says "equal" for every pair, and == still disagrees: it never
	// consulted the trait.
	if out != "true\nfalse\ntrue\n" {
		t.Fatalf("output:\n%q", out)
	}
}
