package interp

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go: no CGO, cross-compilation stays trivial

	"glide/internal/source"
)

// sql host shim (M2), shaped by DESIGN.md's SQL doctrine:
// raw SQL + mapping, no ORM, no query builder. Named parameters
// (:name) are the one canonical placeholder syntax — the shim
// translates and verifies them at call time (missing/extra names
// are errors naming the parameter); the comptime era moves that
// check to compile time unchanged. NULL is None both directions —
// sql.NullString never exists. Rows come back as Maps (column →
// value, column order) until `derive Row` generates typed mapping.

type DbV struct{ db *sql.DB }

func (in *Interp) sqlCall(name string, args []Value, at source.Span) Value {
	switch name {
	case "open":
		dsn, ok := one("sql.open", args, at).(StrV)
		if !ok {
			panic(rtErr{at, "sql.open takes a DSN String (e.g. \"sqlite:notes.db\")"})
		}
		path, found := strings.CutPrefix(string(dsn), "sqlite:")
		if !found {
			return &ResultV{Ok: false, V: &ErrV{Msg: fmt.Sprintf("unsupported DSN %q (this interpreter ships sqlite: only)", dsn)}}
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return &ResultV{Ok: false, V: &ErrV{Msg: err.Error()}}
		}
		// The GIL serialises interpreter-side access, and sqlite
		// dislikes concurrent writers anyway.
		db.SetMaxOpenConns(1)
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return &ResultV{Ok: false, V: &ErrV{Msg: err.Error()}}
		}
		return &ResultV{Ok: true, V: &DbV{db: db}}
	}
	panic(rtErr{at, fmt.Sprintf("module sql has no function %q", name)})
}

// sqlMethod: exec / query / query_one / close on a Db value.
func (in *Interp) sqlMethod(d *DbV, name string, args []Value, at source.Span) Value {
	switch name {
	case "close":
		if len(args) != 0 {
			panic(rtErr{at, "close takes no arguments"})
		}
		if err := d.db.Close(); err != nil {
			return &ResultV{Ok: false, V: &ErrV{Msg: err.Error()}}
		}
		return &ResultV{Ok: true, V: UnitV{}}
	case "exec", "query", "query_one":
	default:
		panic(rtErr{at, fmt.Sprintf("Db has no method %q", name)})
	}
	if len(args) < 1 || len(args) > 2 {
		panic(rtErr{at, fmt.Sprintf("%s takes (query) or (query, params)", name)})
	}
	q, ok := args[0].(StrV)
	if !ok {
		panic(rtErr{at, fmt.Sprintf("%s: the query is a String, got %s", name, typeName(args[0]))})
	}
	var params *MapV
	if len(args) == 2 {
		params, ok = args[1].(*MapV)
		if !ok {
			panic(rtErr{at, fmt.Sprintf("%s: params are a Map (\"name\": value), got %s", name, typeName(args[1]))})
		}
	}
	query, bound, err := bindNamed(string(q), params, at)
	if err != "" {
		return &ResultV{Ok: false, V: &ErrV{Msg: err}}
	}

	ctx, release := in.hostCtx()
	var out Value
	var failure string
	cancelled := func() bool { return ctx.Err() != nil }
	in.unblock(func() {
		defer release()
		switch name {
		case "exec":
			res, err := d.db.ExecContext(ctx, query, bound...)
			if err != nil {
				failure = err.Error()
				return
			}
			n, _ := res.RowsAffected()
			out = IntV(n)
		case "query", "query_one":
			rows, err := d.db.QueryContext(ctx, query, bound...)
			if err != nil {
				failure = err.Error()
				return
			}
			defer rows.Close()
			list, scanErr := scanRows(rows)
			if scanErr != nil {
				failure = scanErr.Error()
				return
			}
			out = list
		}
	})
	if failure != "" && cancelled() {
		panic(cancelUnwind{})
	}
	if failure != "" {
		return &ResultV{Ok: false, V: &ErrV{Msg: failure}}
	}
	if name == "query_one" {
		l := out.(*ListV)
		switch len(l.Elems) {
		case 0:
			return &ResultV{Ok: true, V: NoneV{}}
		case 1:
			return &ResultV{Ok: true, V: some(l.Elems[0])}
		default:
			return &ResultV{Ok: false, V: &ErrV{Msg: fmt.Sprintf("query_one matched %d rows", len(l.Elems))}}
		}
	}
	return &ResultV{Ok: true, V: out}
}

// bindNamed rewrites :name placeholders to ? and binds values from
// the params map — the dynamic stand-in for the comptime check.
// Every placeholder must be supplied; every param must be used.
func bindNamed(query string, params *MapV, at source.Span) (string, []any, string) {
	var b strings.Builder
	var bound []any
	used := map[string]bool{}
	inStr := byte(0)
	for i := 0; i < len(query); i++ {
		c := query[i]
		if inStr != 0 {
			b.WriteByte(c)
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			inStr = c
			b.WriteByte(c)
		case c == ':' && i+1 < len(query) && isNameByte(query[i+1]):
			j := i + 1
			for j < len(query) && isNameByte(query[j]) {
				j++
			}
			pname := query[i+1 : j]
			i = j - 1
			var v Value
			okKey := false
			if params != nil {
				v, okKey = params.get(StrV(pname))
			}
			if !okKey {
				return "", nil, fmt.Sprintf("query names :%s but params do not supply it", pname)
			}
			used[pname] = true
			b.WriteByte('?')
			bound = append(bound, toDriver(v, at))
		default:
			b.WriteByte(c)
		}
	}
	if params != nil {
		for _, k := range params.keys {
			if !used[string(k.(StrV))] {
				return "", nil, fmt.Sprintf("params supply %q but the query never names it", string(k.(StrV)))
			}
		}
	}
	return b.String(), bound, ""
}

func isNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// toDriver: NULL is None; distinct unwraps (the codec is the
// explicit conversion boundary); Instants store as RFC 3339.
func toDriver(v Value, at source.Span) any {
	switch x := v.(type) {
	case NoneV:
		return nil
	case *SomeV:
		return toDriver(x.V, at) // the box does not reach the driver
	case IntV:
		return int64(x)
	case UintV:
		return uint64(x)
	case SizedV:
		return x.V
	case FloatV:
		return float64(x)
	case StrV:
		return string(x)
	case BoolV:
		return bool(x)
	case InstantV:
		return x.T.Format(time.RFC3339Nano)
	case *DistinctV:
		return toDriver(x.V, at)
	}
	panic(rtErr{at, fmt.Sprintf("cannot bind %s as a SQL parameter", typeName(v))})
}

func scanRows(rows *sql.Rows) (*ListV, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	list := &ListV{}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := newMap()
		for i, col := range cols {
			m.set(StrV(col), fromDriver(cells[i]))
		}
		list.Elems = append(list.Elems, m)
	}
	return list, rows.Err()
}

func fromDriver(v any) Value {
	switch x := v.(type) {
	case nil:
		return NoneV{}
	case int64:
		return IntV(x)
	case float64:
		return FloatV(x)
	case string:
		return StrV(x)
	case []byte:
		return StrV(string(x))
	case bool:
		return BoolV(x)
	case time.Time:
		return InstantV{T: x}
	}
	return StrV(fmt.Sprintf("%v", v))
}
