package interp

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// json host shim (M2). `derive Json` is the real design — generated
// per-type codecs at comptime. Until comptime exists, the shim does
// structurally what derive will do statically: encode walks the
// value, decode produces dynamic values (objects → Map, arrays →
// List). Typed decode arrives with derive; nothing here survives
// into the compiled tier.

func (in *Interp) jsonCall(name string, args []Value, line int) Value {
	switch name {
	case "encode":
		v := one("json.encode", args, line)
		var b strings.Builder
		encodeJSON(&b, v, line)
		return StrV(b.String())
	case "decode":
		s, ok := one("json.decode", args, line).(StrV)
		if !ok {
			panic(rtErr{line, fmt.Sprintf("json.decode takes a String, got %s", typeName(args[0]))})
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
		return &ResultV{Ok: true, V: fromJSON(raw, line)}
	}
	panic(rtErr{line, fmt.Sprintf("module json has no function %q", name)})
}

// encodeJSON: structs and string-keyed maps become objects, lists
// and tuples arrays; distinct unwraps (the codec is the explicit
// conversion boundary); Instant is RFC 3339; None is null.
func encodeJSON(b *strings.Builder, v Value, line int) {
	switch x := v.(type) {
	case NoneV:
		b.WriteString("null")
	case BoolV:
		fmt.Fprintf(b, "%t", bool(x))
	case IntV:
		fmt.Fprintf(b, "%d", int64(x))
	case FloatV:
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			panic(rtErr{line, "JSON cannot represent NaN or infinity"})
		}
		fmt.Fprintf(b, "%g", float64(x))
	case StrV:
		writeJSONString(b, string(x))
	case RuneV:
		writeJSONString(b, string(rune(x)))
	case *DistinctV:
		encodeJSON(b, x.V, line)
	case InstantV:
		writeJSONString(b, x.T.Format(time.RFC3339Nano))
	case *ListV:
		b.WriteByte('[')
		for i, e := range x.Elems {
			if i > 0 {
				b.WriteByte(',')
			}
			encodeJSON(b, e, line)
		}
		b.WriteByte(']')
	case TupleV:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			encodeJSON(b, e, line)
		}
		b.WriteByte(']')
	case *MapV:
		b.WriteByte('{')
		for i, k := range x.keys {
			ks, ok := k.(StrV)
			if !ok {
				panic(rtErr{line, fmt.Sprintf("JSON object keys must be Strings, got %s", typeName(k))})
			}
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, string(ks))
			b.WriteByte(':')
			encodeJSON(b, x.m[k], line)
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
			encodeJSON(b, x.Fields[f], line)
		}
		b.WriteByte('}')
	default:
		panic(rtErr{line, fmt.Sprintf("json.encode cannot represent %s (variants and functions wait for derive Json)", typeName(v))})
	}
}

func writeJSONString(b *strings.Builder, s string) {
	enc, _ := json.Marshal(s) // a string never fails to marshal
	b.Write(enc)
}

// fromJSON: null → None (absent and null are one thing — the
// doctrine's third application); whole numbers → Int, else Float.
func fromJSON(raw any, line int) Value {
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
			l.Elems = append(l.Elems, fromJSON(e, line))
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
			m.set(StrV(k), fromJSON(x[k], line))
		}
		return m
	}
	panic(rtErr{line, fmt.Sprintf("json.decode: unexpected %T", raw)})
}
