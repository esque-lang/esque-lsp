package main

import (
	"fmt"
	"unicode/utf8"
)

// TokKind is the kind of a lexed esque token.
type TokKind int

const (
	TkEOF TokKind = iota
	TkErr
	TkIdent
	TkInt
	TkFloat
	TkString
	TkChar

	// keywords
	TkFn
	TkLet
	TkIf
	TkElse
	TkMatch
	TkTrue
	TkFalse
	TkAs
	TkIn
	TkMut
	TkReturn

	// punctuation / operators
	TkLParen
	TkRParen
	TkLBrace
	TkRBrace
	TkLBracket
	TkRBracket
	TkArrow    // ->
	TkFatArrow // =>
	TkEq
	TkEqEq
	TkBangEq
	TkLt
	TkLtEq
	TkGt
	TkGtEq
	TkSemi
	TkColon
	TkComma
	TkPlus
	TkMinus
	TkStar
	TkSlash
	TkPercent
	TkAmpAmp
	TkPipePipe
	TkBang

	TkPipeArrow // |>
	TkBar       // |  (lambda)
	TkAt        // @ (matmul)
	TkAttr      // @ident attached
	TkDotPlus
	TkDotMinus
	TkDotStar
	TkDotSlash
	TkDotPercent
	TkPlusSlash
	TkStarSlash
	TkMinusSlash
	TkSlashSlash
	TkDotDot
	TkDotDotEq
)

// LexErr represents a lexing error with span information.
type LexErr struct {
	Range Range
	Msg   string
}

// Tok is a lexed token.
type Tok struct {
	Kind  TokKind
	Lit   string
	Range Range
}

// Lexer is a streaming lexer over a UTF-8 source buffer. Positions
// are reported as LSP Position values (0-based line, 0-based UTF-16
// character offset).
type Lexer struct {
	src    []byte
	off    int
	line   int // 0-based
	col    int // 0-based, in UTF-16 code units
	errors []LexErr
}

func NewLexer(src []byte) *Lexer {
	return &Lexer{src: src}
}

func (l *Lexer) pos() Position {
	return Position{Line: l.line, Character: l.col}
}

// All returns all tokens (including no error sentinels) and the
// accumulated lexer errors.
func (l *Lexer) All() ([]Tok, []LexErr) {
	var toks []Tok
	for {
		t := l.Next()
		toks = append(toks, t)
		if t.Kind == TkEOF {
			break
		}
	}
	return toks, l.errors
}

func (l *Lexer) addErr(start Position, msg string) {
	l.errors = append(l.errors, LexErr{Range: Range{Start: start, End: l.pos()}, Msg: msg})
}

func (l *Lexer) peek() byte {
	if l.off >= len(l.src) {
		return 0
	}
	return l.src[l.off]
}

func (l *Lexer) peek2() byte {
	if l.off+1 >= len(l.src) {
		return 0
	}
	return l.src[l.off+1]
}

func (l *Lexer) advance() byte {
	if l.off >= len(l.src) {
		return 0
	}
	c := l.src[l.off]
	l.off++
	if c == '\n' {
		l.line++
		l.col = 0
	} else if c < 0x80 {
		l.col++
	} else {
		// We're in the middle of a multi-byte rune. Only advance
		// the column on the leading byte.
		if c&0xC0 != 0x80 {
			r, _ := utf8.DecodeRune(l.src[l.off-1:])
			if r > 0xFFFF {
				l.col += 2 // surrogate pair in UTF-16
			} else {
				l.col++
			}
		}
	}
	return c
}

func (l *Lexer) skipWS() {
	for l.off < len(l.src) {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.advance()
		case c == '#':
			// `#` opens a line comment that runs to the end of the line.
			// Line comments switched from `//` to `#` in v0.14 so that
			// `//` is free as the divide-reduction operator (TkSlashSlash).
			for l.off < len(l.src) && l.peek() != '\n' {
				l.advance()
			}
		case c == '/' && l.peek2() == '*':
			start := l.pos()
			l.advance()
			l.advance()
			depth := 1
			for depth > 0 && l.off < len(l.src) {
				if l.peek() == '/' && l.peek2() == '*' {
					l.advance()
					l.advance()
					depth++
				} else if l.peek() == '*' && l.peek2() == '/' {
					l.advance()
					l.advance()
					depth--
				} else {
					l.advance()
				}
			}
			if depth != 0 {
				l.addErr(start, "unterminated block comment")
			}
		default:
			return
		}
	}
}

var keywords = map[string]TokKind{
	"fn":     TkFn,
	"let":    TkLet,
	"if":     TkIf,
	"else":   TkElse,
	"match":  TkMatch,
	"true":   TkTrue,
	"false":  TkFalse,
	"as":     TkAs,
	"in":     TkIn,
	"mut":    TkMut,
	"return": TkReturn,
}

// Next returns the next token. On error, it emits a TkErr and
// records the error.
func (l *Lexer) Next() Tok {
	l.skipWS()
	start := l.pos()
	if l.off >= len(l.src) {
		return Tok{Kind: TkEOF, Range: Range{Start: start, End: start}}
	}
	c := l.peek()

	// string literal
	if c == '"' {
		return l.lexString(start)
	}
	// char literal
	if c == '\'' {
		return l.lexChar(start)
	}

	// identifier / keyword
	if isIdentStart(c) {
		begin := l.off
		for l.off < len(l.src) && isIdentCont(l.peek()) {
			l.advance()
		}
		lit := string(l.src[begin:l.off])
		if k, ok := keywords[lit]; ok {
			return Tok{Kind: k, Lit: lit, Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkIdent, Lit: lit, Range: Range{Start: start, End: l.pos()}}
	}

	// number
	if c >= '0' && c <= '9' {
		return l.lexNumber(start)
	}

	// punctuation
	switch c {
	case '(':
		l.advance()
		return Tok{Kind: TkLParen, Lit: "(", Range: Range{Start: start, End: l.pos()}}
	case ')':
		l.advance()
		return Tok{Kind: TkRParen, Lit: ")", Range: Range{Start: start, End: l.pos()}}
	case '{':
		l.advance()
		return Tok{Kind: TkLBrace, Lit: "{", Range: Range{Start: start, End: l.pos()}}
	case '}':
		l.advance()
		return Tok{Kind: TkRBrace, Lit: "}", Range: Range{Start: start, End: l.pos()}}
	case '[':
		l.advance()
		return Tok{Kind: TkLBracket, Lit: "[", Range: Range{Start: start, End: l.pos()}}
	case ']':
		l.advance()
		return Tok{Kind: TkRBracket, Lit: "]", Range: Range{Start: start, End: l.pos()}}
	case ';':
		l.advance()
		return Tok{Kind: TkSemi, Lit: ";", Range: Range{Start: start, End: l.pos()}}
	case ':':
		l.advance()
		return Tok{Kind: TkColon, Lit: ":", Range: Range{Start: start, End: l.pos()}}
	case ',':
		l.advance()
		return Tok{Kind: TkComma, Lit: ",", Range: Range{Start: start, End: l.pos()}}
	case '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Tok{Kind: TkEqEq, Lit: "==", Range: Range{Start: start, End: l.pos()}}
		}
		if l.peek() == '>' {
			l.advance()
			return Tok{Kind: TkFatArrow, Lit: "=>", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkEq, Lit: "=", Range: Range{Start: start, End: l.pos()}}
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Tok{Kind: TkBangEq, Lit: "!=", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkBang, Lit: "!", Range: Range{Start: start, End: l.pos()}}
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Tok{Kind: TkLtEq, Lit: "<=", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkLt, Lit: "<", Range: Range{Start: start, End: l.pos()}}
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Tok{Kind: TkGtEq, Lit: ">=", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkGt, Lit: ">", Range: Range{Start: start, End: l.pos()}}
	case '+':
		l.advance()
		if l.peek() == '/' {
			l.advance()
			return Tok{Kind: TkPlusSlash, Lit: "+/", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkPlus, Lit: "+", Range: Range{Start: start, End: l.pos()}}
	case '-':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			return Tok{Kind: TkArrow, Lit: "->", Range: Range{Start: start, End: l.pos()}}
		}
		if l.peek() == '/' {
			l.advance()
			return Tok{Kind: TkMinusSlash, Lit: "-/", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkMinus, Lit: "-", Range: Range{Start: start, End: l.pos()}}
	case '*':
		l.advance()
		if l.peek() == '/' {
			l.advance()
			return Tok{Kind: TkStarSlash, Lit: "*/", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkStar, Lit: "*", Range: Range{Start: start, End: l.pos()}}
	case '/':
		l.advance()
		if l.peek() == '/' {
			// `//` is the divide-reduction prefix operator (v0.14). It
			// was a line-comment lead-in before v0.14; the `#`-comment
			// branch in skipWS replaced that.
			l.advance()
			return Tok{Kind: TkSlashSlash, Lit: "//", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkSlash, Lit: "/", Range: Range{Start: start, End: l.pos()}}
	case '%':
		l.advance()
		return Tok{Kind: TkPercent, Lit: "%", Range: Range{Start: start, End: l.pos()}}
	case '&':
		l.advance()
		if l.peek() == '&' {
			l.advance()
			return Tok{Kind: TkAmpAmp, Lit: "&&", Range: Range{Start: start, End: l.pos()}}
		}
		l.addErr(start, "unexpected '&' (expected '&&')")
		return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
	case '|':
		l.advance()
		if l.peek() == '|' {
			l.advance()
			return Tok{Kind: TkPipePipe, Lit: "||", Range: Range{Start: start, End: l.pos()}}
		}
		if l.peek() == '>' {
			l.advance()
			return Tok{Kind: TkPipeArrow, Lit: "|>", Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkBar, Lit: "|", Range: Range{Start: start, End: l.pos()}}
	case '@':
		l.advance()
		if l.off < len(l.src) && isIdentStart(l.peek()) {
			begin := l.off
			for l.off < len(l.src) && isIdentCont(l.peek()) {
				l.advance()
			}
			name := string(l.src[begin:l.off])
			return Tok{Kind: TkAttr, Lit: name, Range: Range{Start: start, End: l.pos()}}
		}
		return Tok{Kind: TkAt, Lit: "@", Range: Range{Start: start, End: l.pos()}}
	case '.':
		l.advance()
		switch l.peek() {
		case '+':
			l.advance()
			return Tok{Kind: TkDotPlus, Lit: ".+", Range: Range{Start: start, End: l.pos()}}
		case '-':
			l.advance()
			return Tok{Kind: TkDotMinus, Lit: ".-", Range: Range{Start: start, End: l.pos()}}
		case '*':
			l.advance()
			return Tok{Kind: TkDotStar, Lit: ".*", Range: Range{Start: start, End: l.pos()}}
		case '/':
			l.advance()
			return Tok{Kind: TkDotSlash, Lit: "./", Range: Range{Start: start, End: l.pos()}}
		case '%':
			l.advance()
			return Tok{Kind: TkDotPercent, Lit: ".%", Range: Range{Start: start, End: l.pos()}}
		case '.':
			l.advance()
			if l.peek() == '=' {
				l.advance()
				return Tok{Kind: TkDotDotEq, Lit: "..=", Range: Range{Start: start, End: l.pos()}}
			}
			return Tok{Kind: TkDotDot, Lit: "..", Range: Range{Start: start, End: l.pos()}}
		}
		l.addErr(start, "unexpected '.'")
		return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
	}

	// Unknown rune: advance and report.
	r, sz := utf8.DecodeRune(l.src[l.off:])
	if sz < 1 {
		sz = 1
	}
	for i := 0; i < sz; i++ {
		l.advance()
	}
	l.addErr(start, fmt.Sprintf("unexpected character %q", r))
	return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
}

func (l *Lexer) lexNumber(start Position) Tok {
	begin := l.off
	isFloat := false
	for l.off < len(l.src) {
		c := l.peek()
		switch {
		case c >= '0' && c <= '9':
			l.advance()
		case c == '_' && l.peek2() >= '0' && l.peek2() <= '9':
			l.advance()
		case c == '.' && l.peek2() >= '0' && l.peek2() <= '9':
			isFloat = true
			l.advance()
		case c == 'e' || c == 'E':
			isFloat = true
			l.advance()
			if l.peek() == '+' || l.peek() == '-' {
				l.advance()
			}
		default:
			goto suffix
		}
	}
suffix:
	if l.peek() == '_' && (l.peek2() == 'f' || l.peek2() == 'i' || l.peek2() == 'u') {
		l.advance()
		for l.off < len(l.src) && isIdentCont(l.peek()) {
			l.advance()
		}
		lit := string(l.src[begin:l.off])
		if len(lit) >= 3 {
			tail := lit[len(lit)-3:]
			if tail == "f32" || tail == "f64" {
				isFloat = true
			}
		}
	}
	lit := string(l.src[begin:l.off])
	kind := TkInt
	if isFloat {
		kind = TkFloat
	}
	return Tok{Kind: kind, Lit: lit, Range: Range{Start: start, End: l.pos()}}
}

func (l *Lexer) lexString(start Position) Tok {
	l.advance() // opening "
	for l.off < len(l.src) {
		c := l.peek()
		if c == '"' {
			l.advance()
			return Tok{Kind: TkString, Range: Range{Start: start, End: l.pos()}}
		}
		if c == '\n' {
			l.addErr(start, "unterminated string literal (newline)")
			return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
		}
		if c == '\\' {
			l.advance()
			if l.off < len(l.src) {
				l.advance()
			}
			continue
		}
		l.advance()
	}
	l.addErr(start, "unterminated string literal")
	return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
}

func (l *Lexer) lexChar(start Position) Tok {
	l.advance() // opening '
	if l.off >= len(l.src) {
		l.addErr(start, "unterminated char literal")
		return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
	}
	if l.peek() == '\\' {
		l.advance()
		if l.off < len(l.src) {
			l.advance()
		}
	} else {
		_, sz := utf8.DecodeRune(l.src[l.off:])
		if sz < 1 {
			sz = 1
		}
		for i := 0; i < sz; i++ {
			l.advance()
		}
	}
	if l.peek() != '\'' {
		l.addErr(start, "expected closing ' in char literal")
		return Tok{Kind: TkErr, Range: Range{Start: start, End: l.pos()}}
	}
	l.advance()
	return Tok{Kind: TkChar, Range: Range{Start: start, End: l.pos()}}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
