package check

import (
	"glide/internal/ast"
	"glide/internal/parser"
)

// The universe traits, written in Glide rather than built as Go
// structs. They *are* Glide declarations, so spelling them as Glide
// is the auditable form: this is the whole definition, there is no
// second half hidden in a table, and the parser is exercised by the
// same source every program is.
//
// Only two, and the omissions are deliberate:
//
//   - `Hash` would need a `hash()` the runtime does not have, and
//     adding one means committing to a hash function — stable across
//     runs? across versions? — with nothing yet forcing the answer.
//   - `Display` would need `to_string()`, and more importantly would
//     constrain nothing: interpolation is universal here, so
//     `fn log<T>(v: T) { println("{v}") }` already accepts every T. A
//     bound everything satisfies is decoration. It earns its keep in
//     Rust because printing there is not universal; here it is.
//
// Both stay designed-only. A trait whose required method the
// evaluator cannot run is precisely the drift between tiers that the
// host-surface test exists to catch, so the rule is: a universe trait
// names machinery that already runs. `Ord` does — `Int` and `String`
// both have `cmp`. `Iterable` does — the evaluator's `iterate`
// already says "anything with an iter() method is iterable".
const preludeSrc = `
trait Ord {
    fn cmp(self, other: Self) -> Int
}

trait Iterable<T> {
    fn iter(self) -> Iterator<T>
}
`

// Prelude parses the universe traits. The result is merged into the
// declaration table before a program's own declarations, so both
// tiers see one set.
func Prelude() ([]*ast.TraitDecl, error) {
	f, err := parser.ParseFile("<universe>", preludeSrc)
	if err != nil {
		return nil, err
	}
	return f.Traits, nil
}

// preludeTraits is the parsed prelude, computed once. It cannot fail
// in a shipped binary — the source is a constant in this file — so a
// parse error here is a build-time bug and panicking is the honest
// response.
var preludeTraits = func() []*ast.TraitDecl {
	ts, err := Prelude()
	if err != nil {
		panic("check: the universe prelude does not parse: " + err.Error())
	}
	return ts
}()
