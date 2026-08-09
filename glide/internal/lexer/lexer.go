// Package lexer turns Glide source into tokens.
//
// Statement termination: Glide has no semicolons. A newline ends a
// statement when the token before it can end an expression (Go's
// rule); all other newlines are insignificant. The parser therefore
// sees explicit Semi tokens and never raw newlines.
package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"glide/internal/source"
)

type Kind int

const (
	EOF Kind = iota
	Semi
	Ident
	Int
	Float
	String
	Rune // 'a' — code point in Token.Int

	KwFn
	KwLet
	KwMut
	KwIf
	KwElse
	KwFor
	KwIn
	KwImport
	KwReturn
	KwTrue
	KwFalse
	KwType
	KwStruct
	KwImpl
	KwMatch
	KwYield
	KwPub
	KwBreak
	KwContinue
	KwDefer
	KwErrdefer
	KwTrait
	KwConst
	KwScope
	KwSelect

	LParen
	RParen
	LBrack
	RBrack
	LBrace
	RBrace
	Comma
	Colon
	Dot
	DotDot
	DotDotEq
	Arrow
	Assign
	PlusEq
	MinusEq
	StarEq
	SlashEq
	PercentEq
	Plus
	Minus
	Star
	Slash
	Percent
	Eq
	Ne
	Lt
	Le
	Gt
	Ge
	Not
	AndAnd
	OrOr
	Pipe
	Question
	QQ
	FatArrow
)

var kindNames = map[Kind]string{
	EOF: "end of file", Semi: "end of line", Ident: "identifier",
	Int: "integer", Float: "float", String: "string", Rune: "rune",
	KwFn: "'fn'", KwLet: "'let'", KwMut: "'mut'", KwIf: "'if'",
	KwElse: "'else'", KwFor: "'for'", KwIn: "'in'", KwImport: "'import'",
	KwReturn: "'return'", KwTrue: "'true'", KwFalse: "'false'",
	LParen: "'('", RParen: "')'", LBrack: "'['", RBrack: "']'",
	LBrace: "'{'", RBrace: "'}'", Comma: "','", Colon: "':'",
	Dot: "'.'", DotDot: "'..'", DotDotEq: "'..='", Arrow: "'->'", Assign: "'='",
	PlusEq: "'+='", MinusEq: "'-='", StarEq: "'*='", SlashEq: "'/='",
	PercentEq: "'%='", Plus: "'+'", Minus: "'-'",
	Star: "'*'", Slash: "'/'", Percent: "'%'", Eq: "'=='", Ne: "'!='",
	Lt: "'<'", Le: "'<='", Gt: "'>'", Ge: "'>='", Not: "'!'",
	AndAnd: "'&&'", OrOr: "'||'", Pipe: "'|'", Question: "'?'", QQ: "'??'",
	FatArrow: "'=>'", KwType: "'type'", KwStruct: "'struct'", KwImpl: "'impl'",
	KwMatch: "'match'", KwYield: "'yield'", KwPub: "'pub'",
	KwBreak: "'break'", KwContinue: "'continue'",
	KwDefer: "'defer'", KwErrdefer: "'errdefer'", KwTrait: "'trait'", KwConst: "'const'",
	KwScope: "'scope'", KwSelect: "'select'",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return fmt.Sprintf("token(%d)", int(k))
}

var keywords = map[string]Kind{
	"fn": KwFn, "let": KwLet, "mut": KwMut, "if": KwIf, "else": KwElse,
	"for": KwFor, "in": KwIn, "import": KwImport, "return": KwReturn,
	"true": KwTrue, "false": KwFalse, "type": KwType, "struct": KwStruct,
	"impl": KwImpl, "match": KwMatch, "yield": KwYield, "pub": KwPub,
	"break": KwBreak, "continue": KwContinue,
	"defer": KwDefer, "errdefer": KwErrdefer, "trait": KwTrait, "const": KwConst,
	"scope": KwScope, "select": KwSelect,
}

// StrPart is one segment of an interpolated string literal: either
// literal text or the source of an embedded expression with an
// optional format spec ("{n:6}" -> expr "n", spec "6").
type StrPart struct {
	IsExpr bool
	S      string
	Spec   string
	Line   int
	Pos    int // byte offset of S in the file, so sub-lexing keeps file coordinates
}

type Token struct {
	source.Span // byte range in the file this token was lexed from
	Kind        Kind
	Text        string
	Int         int64
	Float       float64
	Parts       []StrPart // String tokens only
	Line        int
}

// Tokens whose presence at end-of-line means the statement is complete.
var endsExpr = map[Kind]bool{
	Ident: true, Int: true, Float: true, String: true, Rune: true,
	KwTrue: true, KwFalse: true, KwReturn: true,
	KwBreak: true, KwContinue: true,
	RParen: true, RBrack: true, RBrace: true, Question: true,
}

type lexer struct {
	src   string
	i     int
	start int // byte offset the token being scanned began at
	base  int // offset of src within the enclosing file (interpolation)
	line  int
	toks  []Token
}

func Lex(src string) ([]Token, error) { return LexAt(src, 0, 1) }

// LexAt lexes a fragment that sits at byte offset base of a larger
// file, starting at the given line. Interpolation segments are lexed
// this way so their tokens carry offsets into the *file*, not into the
// segment — otherwise every diagnostic inside "{x + 1}" would point at
// the wrong place.
func LexAt(src string, base, line int) ([]Token, error) {
	lx := &lexer{src: src, base: base, line: line}
	if err := lx.run(); err != nil {
		return nil, err
	}
	return lx.toks, nil
}

// span covers [from, lx.i) in file coordinates.
func (lx *lexer) span(from int) source.Span {
	return source.Span{Pos: lx.base + from, End: lx.base + lx.i}
}

func (lx *lexer) errf(format string, args ...any) error {
	return source.Diagnostic{
		Span: source.At(lx.base + lx.i),
		Msg:  fmt.Sprintf(format, args...),
	}
}

// col reports the 1-based column of byte offset pos on its line.
func (lx *lexer) col(pos int) int {
	return pos - strings.LastIndexByte(lx.src[:pos], '\n')
}

func (lx *lexer) emit(k Kind, text string) {
	lx.toks = append(lx.toks, Token{
		Span: lx.span(lx.start),
		Kind: k, Text: text, Line: lx.line,
	})
}

func (lx *lexer) prev() *Token {
	if len(lx.toks) == 0 {
		return nil
	}
	return &lx.toks[len(lx.toks)-1]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentCont(c byte) bool { return isIdentStart(c) || isDigit(c) }

func (lx *lexer) run() error {
	for lx.i < len(lx.src) {
		// Every emit() in this iteration spans from here. Whitespace
		// and comment branches emit nothing, so a stale start is
		// never observable.
		lx.start = lx.i
		c := lx.src[lx.i]
		switch {
		case c == '\n':
			if p := lx.prev(); p != nil && endsExpr[p.Kind] {
				lx.emit(Semi, "newline")
			}
			lx.line++
			lx.i++
		case c == ' ' || c == '\t' || c == '\r':
			lx.i++
		case c == '/' && lx.i+1 < len(lx.src) && lx.src[lx.i+1] == '/':
			for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
				lx.i++
			}
		case c == '"':
			if err := lx.lexString(); err != nil {
				return err
			}
		case c == '\'':
			if err := lx.lexRune(); err != nil {
				return err
			}
		case c == '`':
			if err := lx.lexRaw(); err != nil {
				return err
			}
		case isDigit(c):
			lx.lexNumber()
		case isIdentStart(c):
			lx.lexIdent()
		default:
			if err := lx.lexOp(); err != nil {
				return err
			}
		}
	}
	// The trailing implicit semicolon and EOF are zero-width at the
	// end of input.
	lx.start = lx.i
	if p := lx.prev(); p != nil && endsExpr[p.Kind] {
		lx.emit(Semi, "newline")
	}
	lx.emit(EOF, "")
	return nil
}

func (lx *lexer) lexIdent() {
	start := lx.i
	for lx.i < len(lx.src) && isIdentCont(lx.src[lx.i]) {
		lx.i++
	}
	text := lx.src[start:lx.i]
	if k, ok := keywords[text]; ok {
		lx.emit(k, text)
		return
	}
	lx.emit(Ident, text)
}

func (lx *lexer) lexNumber() {
	start := lx.i
	for lx.i < len(lx.src) && (isDigit(lx.src[lx.i]) || lx.src[lx.i] == '_') {
		lx.i++
	}
	isFloat := false
	// "1.5" is a float; "1..n" is a range and "x.1.cmp" tuple access.
	if lx.i+1 < len(lx.src) && lx.src[lx.i] == '.' && isDigit(lx.src[lx.i+1]) {
		isFloat = true
		lx.i++
		for lx.i < len(lx.src) && (isDigit(lx.src[lx.i]) || lx.src[lx.i] == '_') {
			lx.i++
		}
	}
	text := strings.ReplaceAll(lx.src[start:lx.i], "_", "")
	if isFloat {
		f, _ := strconv.ParseFloat(text, 64)
		lx.toks = append(lx.toks, Token{Span: lx.span(lx.start), Kind: Float, Text: text, Float: f, Line: lx.line})
	} else {
		n, _ := strconv.ParseInt(text, 10, 64)
		lx.toks = append(lx.toks, Token{Span: lx.span(lx.start), Kind: Int, Text: text, Int: n, Line: lx.line})
	}
}

// unicodeEscape decodes `\u{HEX}` starting at src[at] (the
// backslash). Returns the rune and the total byte length consumed.
func (lx *lexer) unicodeEscape(at int) (rune, int, error) {
	j := at + 2 // past \u
	if j >= len(lx.src) || lx.src[j] != '{' {
		return 0, 0, lx.errf(`unicode escape is \u{HEX}, e.g. \u{1F600}`)
	}
	j++
	start := j
	for j < len(lx.src) && lx.src[j] != '}' && lx.src[j] != '\n' {
		j++
	}
	if j >= len(lx.src) || lx.src[j] != '}' {
		return 0, 0, lx.errf(`unterminated \u{…} escape`)
	}
	n, err := strconv.ParseUint(lx.src[start:j], 16, 32)
	if err != nil || start == j {
		return 0, 0, lx.errf(`bad unicode escape \u{%s}`, lx.src[start:j])
	}
	r := rune(n)
	if r > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
		return 0, 0, lx.errf(`\u{%s} is not a valid code point`, lx.src[start:j])
	}
	return r, j + 1 - at, nil
}

// lexRune scans 'a' — exactly one rune, its own type. The escape
// family matches strings, plus \' for the delimiter.
func (lx *lexer) lexRune() error {
	lx.i++ // opening '
	if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
		return lx.errf("unterminated rune literal")
	}
	var r rune
	if lx.src[lx.i] == '\\' {
		if lx.i+1 >= len(lx.src) {
			return lx.errf("unterminated escape")
		}
		e := lx.src[lx.i+1]
		if e == 'u' {
			ru, n, err := lx.unicodeEscape(lx.i)
			if err != nil {
				return err
			}
			r = ru
			lx.i += n
		} else {
			switch e {
			case 'n':
				r = '\n'
			case 't':
				r = '\t'
			case 'r':
				r = '\r'
			case '\\', '\'', '"':
				r = rune(e)
			default:
				return lx.errf(`unknown escape \%c`, e)
			}
			lx.i += 2
		}
	} else {
		ru, size := utf8.DecodeRuneInString(lx.src[lx.i:])
		r = ru
		lx.i += size
	}
	if lx.i >= len(lx.src) || lx.src[lx.i] != '\'' {
		return lx.errf("a rune literal holds exactly one rune ('a'); for text use a string")
	}
	lx.i++
	lx.toks = append(lx.toks, Token{Span: lx.span(lx.start), Kind: Rune, Int: int64(r), Text: string(r), Line: lx.line})
	return nil
}

// lexRaw scans `…` — no escapes, no interpolation, multiline. It
// cannot contain a backtick: accepted, by definition of raw.
func (lx *lexer) lexRaw() error {
	startLine := lx.line
	lx.i++ // opening backtick
	start := lx.i
	for lx.i < len(lx.src) && lx.src[lx.i] != '`' {
		if lx.src[lx.i] == '\n' {
			lx.line++
		}
		lx.i++
	}
	if lx.i >= len(lx.src) {
		return fmt.Errorf("line %d: unclosed raw string (opened with `)", startLine)
	}
	content := lx.src[start:lx.i]
	lx.i++
	lx.toks = append(lx.toks, Token{
		Span:  lx.span(lx.start),
		Kind:  String,
		Parts: []StrPart{{S: content}},
		Line:  startLine,
	})
	return nil
}

func (lx *lexer) lexOp() error {
	two := ""
	if lx.i+1 < len(lx.src) {
		two = lx.src[lx.i : lx.i+2]
	}
	twoKinds := map[string]Kind{
		"->": Arrow, "..": DotDot, "??": QQ, "&&": AndAnd, "||": OrOr,
		"==": Eq, "!=": Ne, "<=": Le, ">=": Ge, "+=": PlusEq, "-=": MinusEq,
		"*=": StarEq, "/=": SlashEq, "%=": PercentEq,
		"=>": FatArrow,
	}
	if k, ok := twoKinds[two]; ok {
		if k == DotDot && lx.i+2 < len(lx.src) && lx.src[lx.i+2] == '=' {
			lx.emit(DotDotEq, "..=")
			lx.i += 3
			return nil
		}
		lx.emit(k, two)
		lx.i += 2
		return nil
	}
	oneKinds := map[byte]Kind{
		'(': LParen, ')': RParen, '[': LBrack, ']': RBrack,
		'{': LBrace, '}': RBrace, ',': Comma, ':': Colon, '.': Dot,
		'=': Assign, '+': Plus, '-': Minus, '*': Star, '/': Slash,
		'%': Percent, '<': Lt, '>': Gt, '!': Not, '|': Pipe, '?': Question,
	}
	c := lx.src[lx.i]
	if k, ok := oneKinds[c]; ok {
		// A line beginning with `.` continues the previous statement,
		// so a multi-line adapter chain can put each `.filter(…)` on
		// its own line. Every Semi is newline-synthesized (there is
		// no `;`), so retracting it here cannot eat explicit
		// punctuation. `..` at line start is DotDot and stays a
		// statement break — and so does `.Variant`: case is
		// load-bearing, methods/fields are lowercase, so a
		// capitalised name after the dot is the variant shorthand
		// starting a new statement, not a continuation.
		if k == Dot {
			if p := lx.prev(); p != nil && p.Kind == Semi {
				if !(lx.i+1 < len(lx.src) && lx.src[lx.i+1] >= 'A' && lx.src[lx.i+1] <= 'Z') {
					lx.toks = lx.toks[:len(lx.toks)-1]
				}
			}
		}
		lx.emit(k, string(c))
		lx.i++
		return nil
	}
	// Decode the whole rune: c is one *byte*, and reporting the first
	// byte of a multi-byte character prints mojibake ("Ã" for "ö").
	r, _ := utf8.DecodeRuneInString(lx.src[lx.i:])
	if r >= utf8.RuneSelf {
		return lx.errf("unexpected character %q (identifiers are ASCII)", r)
	}
	return lx.errf("unexpected character %q", string(c))
}

func (lx *lexer) lexString() error {
	startLine := lx.line
	openCol := lx.col(lx.i)
	lx.i++ // opening quote
	var parts []StrPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, StrPart{S: lit.String(), Line: startLine})
			lit.Reset()
		}
	}
	for {
		if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
			return lx.errf("unterminated string (opened at column %d)", openCol)
		}
		c := lx.src[lx.i]
		switch c {
		case '"':
			lx.i++
			flush()
			lx.toks = append(lx.toks, Token{Span: lx.span(lx.start), Kind: String, Parts: parts, Line: startLine})
			return nil
		case '\\':
			if lx.i+1 >= len(lx.src) {
				return lx.errf("unterminated escape")
			}
			e := lx.src[lx.i+1]
			if e == 'u' {
				r, n, err := lx.unicodeEscape(lx.i)
				if err != nil {
					return err
				}
				lit.WriteRune(r)
				lx.i += n
				continue
			}
			switch e {
			case 'n':
				lit.WriteByte('\n')
			case 't':
				lit.WriteByte('\t')
			case 'r':
				lit.WriteByte('\r')
			case '\\', '"', '{', '}':
				lit.WriteByte(e)
			default:
				return lx.errf(`unknown escape \%c`, e)
			}
			lx.i += 2
		case '{':
			flush()
			braceCol := lx.col(lx.i)
			lx.i++
			depth := 0
			exprStart := lx.i
			specAt := -1
			for {
				if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
					return lx.errf("unterminated interpolation (opened at column %d)", braceCol)
				}
				ch := lx.src[lx.i]
				// Nothing after a spec's ':' may open a brace or string: a
				// spec is just a width (e.g. {x:6}). Erroring here pins the
				// mistake — a missing '}' — to its column, instead of the
				// stray char derailing the rest of the scan.
				if specAt >= 0 && (ch == '{' || ch == '"') {
					return lx.errf("unexpected %q in format spec (a spec is a width, e.g. {x:6}) — missing '}' before it?", string(ch))
				}
				if ch == '"' {
					if err := lx.skipNestedString(); err != nil {
						return err
					}
					continue
				}
				if ch == '(' || ch == '[' || ch == '{' {
					depth++
				}
				if ch == ')' || ch == ']' {
					depth--
				}
				if ch == '}' {
					if depth == 0 {
						break
					}
					depth--
				}
				if ch == ':' && depth == 0 && specAt < 0 {
					specAt = lx.i
				}
				lx.i++
			}
			exprEnd := lx.i
			spec := ""
			if specAt >= 0 {
				spec = lx.src[specAt+1 : exprEnd]
				exprEnd = specAt
			}
			raw := lx.src[exprStart:exprEnd]
			expr := strings.TrimSpace(raw)
			if expr == "" {
				return lx.errf("empty interpolation")
			}
			// TrimSpace moved the start; the offset has to move with
			// it or every diagnostic inside "{ x + 1 }" lands one
			// character early.
			lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
			parts = append(parts, StrPart{
				IsExpr: true, S: expr, Spec: spec, Line: startLine,
				Pos: lx.base + exprStart + lead,
			})
			lx.i++ // '}'
		default:
			lit.WriteByte(c)
			lx.i++
		}
	}
}

// skipNestedString advances past a string literal (opening quote at lx.i)
// found inside an interpolation. The text is captured raw and re-lexed by
// the parser, so only the structure needed to locate the closing quote is
// tracked here: escapes, and nested interpolations, whose braces may in
// turn hide further strings.
func (lx *lexer) skipNestedString() error {
	// A quote inside an interpolation that runs to end-of-line is more
	// often the outer string's closing quote arriving early (a '}' was
	// dropped) than a genuine nested literal — hence the hint.
	openCol := lx.col(lx.i)
	lx.i++ // opening quote
	for {
		if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
			return lx.errf("unterminated string inside interpolation (opened at column %d) — missing '}' before it?", openCol)
		}
		switch lx.src[lx.i] {
		case '"':
			lx.i++
			return nil
		case '\\':
			if lx.i+1 >= len(lx.src) || lx.src[lx.i+1] == '\n' {
				return lx.errf("unterminated escape")
			}
			lx.i += 2
		case '{':
			braceCol := lx.col(lx.i)
			lx.i++
			depth := 0
			for {
				if lx.i >= len(lx.src) || lx.src[lx.i] == '\n' {
					return lx.errf("unterminated interpolation (opened at column %d)", braceCol)
				}
				ch := lx.src[lx.i]
				if ch == '"' {
					if err := lx.skipNestedString(); err != nil {
						return err
					}
					continue
				}
				if ch == '(' || ch == '[' || ch == '{' {
					depth++
				}
				if ch == ')' || ch == ']' {
					depth--
				}
				if ch == '}' {
					if depth == 0 {
						break
					}
					depth--
				}
				lx.i++
			}
			lx.i++ // '}'
		default:
			lx.i++
		}
	}
}
