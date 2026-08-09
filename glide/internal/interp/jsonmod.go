package interp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"glide/internal/source"
)

// json host shim (M2). `derive Json` is the real design — generated
// per-type codecs at comptime. Until comptime exists, the shim does
// structurally what derive will do statically: encode walks the
// value, decode produces dynamic values (objects → Map, arrays →
// List). Typed decode arrives with derive; nothing here survives
// into the compiled tier.

func (in *Interp) jsonCall(name string, args []Value, at source.Span) Value {
	switch name {
	case "encode":
		v := one("json.encode", args, at)
		var b strings.Builder
		encodeJSON(&b, v, at)
		return StrV(b.String())
	case "decode":
		s, ok := one("json.decode", args, at).(StrV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("json.decode takes a String, got %s", typeName(args[0]))})
		}
		dec := json.NewDecoder(strings.NewReader(string(s)))
		dec.UseNumber()
		var raw any
		if err := dec.Decode(&raw); err != nil {
			return &ResultV{Ok: false, V: &ErrV{Msg: "invalid JSON: " + err.Error()}}
		}
		// Trailing garbage after the value is an error, not ignored.
		if dec.More() {
			return &ResultV{Ok: false, V: &ErrV{Msg: "invalid JSON: data after top-level value"}}
		}
		return &ResultV{Ok: true, V: fromJSON(raw, at)}
	}
	panic(rtErr{at, fmt.Sprintf("module json has no function %q", name)})
}

// encodeJSON: structs and string-keyed maps become objects, lists
// and tuples arrays; distinct unwraps (the codec is the explicit
// conversion boundary); Instant is RFC 3339; None is null.
func encodeJSON(b *strings.Builder, v Value, at source.Span) {
	switch x := v.(type) {
	case NoneV:
		b.WriteString("null")
	case BoolV:
		fmt.Fprintf(b, "%t", bool(x))
	case IntV:
		fmt.Fprintf(b, "%d", int64(x))
	case UintV:
		fmt.Fprintf(b, "%d", uint64(x))
	case SizedV:
		fmt.Fprintf(b, "%d", x.V)
	case FloatV:
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			panic(rtErr{at, "JSON cannot represent NaN or infinity"})
		}
		fmt.Fprintf(b, "%g", float64(x))
	case StrV:
		writeJSONString(b, string(x))
	case RuneV:
		writeJSONString(b, string(rune(x)))
	case *DistinctV:
		encodeJSON(b, x.V, at)
	case InstantV:
		writeJSONString(b, x.T.Format(time.RFC3339Nano))
	case *ListV:
		b.WriteByte('[')
		for i, e := range x.Elems {
			if i > 0 {
				b.WriteByte(',')
			}
			encodeJSON(b, e, at)
		}
		b.WriteByte(']')
	case TupleV:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			encodeJSON(b, e, at)
		}
		b.WriteByte(']')
	case *MapV:
		b.WriteByte('{')
		for i, k := range x.keys {
			ks, ok := k.(StrV)
			if !ok {
				panic(rtErr{at, fmt.Sprintf("JSON object keys must be Strings, got %s", typeName(k))})
			}
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, string(ks))
			b.WriteByte(':')
			encodeJSON(b, x.m[k], at)
		}
		b.WriteByte('}')
	case *StructV:
		b.WriteByte('{')
		for i, f := range x.Order {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, f)
			b.WriteByte(':')
			encodeJSON(b, x.Fields[f], at)
		}
		b.WriteByte('}')
	default:
		panic(rtErr{at, fmt.Sprintf("json.encode cannot represent %s (variants and functions wait for derive Json)", typeName(v))})
	}
}

func writeJSONString(b *strings.Builder, s string) {
	enc, _ := json.Marshal(s) // a string never fails to marshal
	b.Write(enc)
}

// fromJSON: null → None (absent and null are one thing — the
// doctrine's third application); whole numbers → Int, else Float.
func fromJSON(raw any, at source.Span) Value {
	switch x := raw.(type) {
	case nil:
		return NoneV{}
	case bool:
		return BoolV(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return IntV(i)
		}
		f, _ := x.Float64()
		return FloatV(f)
	case string:
		return StrV(x)
	case []any:
		l := &ListV{}
		for _, e := range x {
			l.Elems = append(l.Elems, fromJSON(e, at))
		}
		return l
	case map[string]any:
		// Go's decoder loses object key order; sort for determinism.
		m := newMap()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			m.set(StrV(k), fromJSON(x[k], at))
		}
		return m
	}
	panic(rtErr{at, fmt.Sprintf("json.decode: unexpected %T", raw)})
}
