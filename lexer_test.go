package main

import (
	"strings"
	"testing"
)

func TestLexerSimpleFunction(t *testing.T) {
	src := `fn add(a: i32, b: i32) -> i32 = a + b`
	toks, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokKind{
		TkFn, TkIdent, TkLParen, TkIdent, TkColon, TkIdent, TkComma,
		TkIdent, TkColon, TkIdent, TkRParen, TkArrow, TkIdent, TkEq,
		TkIdent, TkPlus, TkIdent, TkEOF,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("tok[%d] = %d, want %d", i, toks[i].Kind, k)
		}
	}
}

func TestLexerAttribute(t *testing.T) {
	src := `@io fn main() -> i32 = print_i32(42)`
	toks, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	if toks[0].Kind != TkAttr || toks[0].Lit != "io" {
		t.Errorf("first token = %+v, want @io attr", toks[0])
	}
}

func TestLexerNestedBlockComment(t *testing.T) {
	src := `/* outer /* inner */ still in outer */ fn main() -> i32 = 0`
	_, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("nested comment should lex cleanly, got errs: %v", errs)
	}
}

func TestLexerUnterminatedString(t *testing.T) {
	src := `fn main() -> i32 = "hello`
	_, errs := NewLexer([]byte(src)).All()
	if len(errs) == 0 {
		t.Fatal("expected an error for unterminated string")
	}
	if !strings.Contains(errs[0].Msg, "unterminated") {
		t.Errorf("got error %q, want one mentioning 'unterminated'", errs[0].Msg)
	}
}

func TestIndexFunctions(t *testing.T) {
	src := `
@io fn show(p: f32) -> f32 = print_f32(p)

fn add(a: i32, b: i32) -> i32 = a + b

fn main() -> i32 = {
    let x = add(1, 2);
    x
}
`
	toks, _ := NewLexer([]byte(src)).All()
	idx := Index(toks)
	if len(idx.Functions) != 3 {
		t.Fatalf("got %d functions, want 3", len(idx.Functions))
	}
	names := []string{idx.Functions[0].Name, idx.Functions[1].Name, idx.Functions[2].Name}
	want := []string{"show", "add", "main"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("fn[%d] = %q, want %q", i, names[i], want[i])
		}
	}
	if !contains(idx.Functions[0].Attrs, "io") {
		t.Errorf("show should have @io attr; got %v", idx.Functions[0].Attrs)
	}
}

// TestLexerLineCommentHash locks in the v0.14 line-comment syntax:
// `#` opens a line comment that ends at newline, and the surrounding
// program lexes as if the comment were whitespace.
func TestLexerLineCommentHash(t *testing.T) {
	src := "# preface\nfn main() -> i32 = 0  # tail\n"
	toks, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokKind{
		TkFn, TkIdent, TkLParen, TkRParen, TkArrow, TkIdent, TkEq,
		TkInt, TkEOF,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("tok[%d] = %d, want %d", i, toks[i].Kind, k)
		}
	}
}

// TestLexerReduceOps pins down all four reduction-prefix operators
// emitted as distinct token kinds. Before v0.14, `//` was a comment
// lead-in and `TkSlashSlash` was unreachable; the # comment swap made
// it live.
func TestLexerReduceOps(t *testing.T) {
	src := `+/xs -/ys */zs //ws`
	toks, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokKind{
		TkPlusSlash, TkIdent,
		TkMinusSlash, TkIdent,
		TkStarSlash, TkIdent,
		TkSlashSlash, TkIdent,
		TkEOF,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("tok[%d] = %d, want %d", i, toks[i].Kind, k)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
