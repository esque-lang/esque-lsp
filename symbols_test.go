package main

import (
	"reflect"
	"testing"
)

// tokensFor lexes the src and returns the token stream, failing the
// test on any lex error. Tests that *want* to feed broken input
// should drive the lexer directly and ignore errors.
func tokensFor(t *testing.T, src string) []Tok {
	t.Helper()
	toks, errs := NewLexer([]byte(src)).All()
	if len(errs) != 0 {
		t.Fatalf("unexpected lex errors for %q: %v", src, errs)
	}
	return toks
}

// TestIndexBasic locks the happy-path shape: function name, parameter
// list, attribute stack.
func TestIndexBasic(t *testing.T) {
	src := `@io fn show(p: f32) -> f32 = print_f32(p)
fn add(a: i32, b: i32) -> i32 = a + b
`
	idx := Index(tokensFor(t, src))
	if len(idx.Functions) != 2 {
		t.Fatalf("functions = %d, want 2", len(idx.Functions))
	}
	if got, want := idx.Functions[0].Name, "show"; got != want {
		t.Errorf("Functions[0].Name = %q, want %q", got, want)
	}
	if got, want := idx.Functions[0].Attrs, []string{"io"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Functions[0].Attrs = %v, want %v", got, want)
	}
	if got, want := idx.Functions[1].Name, "add"; got != want {
		t.Errorf("Functions[1].Name = %q, want %q", got, want)
	}
	if got, want := idx.Functions[1].Params, []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Functions[1].Params = %v, want %v", got, want)
	}
}

// TestIndexShapeParameters confirms the indexer correctly skips a
// `[ShapeParams]` block before the parameter list rather than
// confusing shape names for value parameters.
func TestIndexShapeParameters(t *testing.T) {
	src := `fn dot[N](x: f32[N], y: f32[N]) -> f32 = +/(x .* y)`
	idx := Index(tokensFor(t, src))
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(idx.Functions))
	}
	fn := idx.Functions[0]
	if fn.Name != "dot" {
		t.Errorf("Name = %q, want dot", fn.Name)
	}
	// Only `x` and `y` are value parameters; `N` is a shape parameter
	// and must not appear in fn.Params.
	if got, want := fn.Params, []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Params = %v, want %v", got, want)
	}
}

// TestIndexBlockBody verifies the indexer recognises the `{ … }`
// body form alongside the `= expr` form, and that nested braces
// don't terminate the body early.
func TestIndexBlockBody(t *testing.T) {
	src := `fn main() -> i32 = {
    let xs = [1, 2, 3];
    let m = match xs[0] {
        0 => 10,
        _ => 99
    };
    +/xs
}`
	idx := Index(tokensFor(t, src))
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(idx.Functions))
	}
	if idx.Functions[0].Name != "main" {
		t.Errorf("Name = %q, want main", idx.Functions[0].Name)
	}
}

// TestIndexKernelAttribute confirms the legacy `@kernel` single-token
// form is captured and propagated into IsKernel.
func TestIndexKernelAttribute(t *testing.T) {
	src := `@kernel fn k(x: f32[8]) -> f32 = +/x`
	idx := Index(tokensFor(t, src))
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(idx.Functions))
	}
	fn := idx.Functions[0]
	if !fn.IsKernel {
		t.Errorf("IsKernel = false, want true")
	}
	if got, want := fn.Attrs, []string{"kernel"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Attrs = %v, want %v", got, want)
	}
}

// TestIndexMultipleAttributes pins the multi-attribute case: every
// attribute lands in fn.Attrs in source order.
func TestIndexMultipleAttributes(t *testing.T) {
	src := `@io @grad fn loss(x: f32) -> f32 = x`
	idx := Index(tokensFor(t, src))
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(idx.Functions))
	}
	if got, want := idx.Functions[0].Attrs, []string{"io", "grad"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Attrs = %v, want %v", got, want)
	}
}

// TestIndexAttrFollowedByNonFn confirms attributes attached to
// something that is *not* a function (a dangling line, a malformed
// declaration) are discarded rather than being silently attached to
// the next function down the file.
func TestIndexAttrFollowedByNonFn(t *testing.T) {
	src := `@io let x = 1
fn main() -> i32 = 0`
	// Lex this — we don't care about errors, just feed the tokens to
	// the indexer.
	toks, _ := NewLexer([]byte(src)).All()
	idx := Index(toks)
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(idx.Functions))
	}
	if len(idx.Functions[0].Attrs) != 0 {
		t.Errorf("orphan @io should not attach to main; got Attrs=%v", idx.Functions[0].Attrs)
	}
}

// --- "tolerates partial / broken files" — README's load-bearing claim ---
//
// Each test below feeds a malformed input and asserts the indexer
// returns rather than panics. That's the bar: a syntax error mid-edit
// must not crash the LSP.

func TestIndexPartialFn(t *testing.T) {
	// Ends mid-parameter-list — what a typing user looks like.
	src := `fn add(a: i32, `
	toks, _ := NewLexer([]byte(src)).All()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Index panicked on partial input: %v", r)
		}
	}()
	idx := Index(toks)
	if len(idx.Functions) != 1 {
		t.Fatalf("functions = %d, want 1 (best-effort)", len(idx.Functions))
	}
	if idx.Functions[0].Name != "add" {
		t.Errorf("partial function should still report its name")
	}
}

func TestIndexFnWithoutName(t *testing.T) {
	src := `fn ( ) -> i32 = 0`
	toks, _ := NewLexer([]byte(src)).All()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Index panicked on nameless fn: %v", r)
		}
	}()
	idx := Index(toks)
	if len(idx.Functions) != 0 {
		t.Errorf("nameless fn should be skipped; got %v", idx.Functions)
	}
}

func TestIndexStrayBrace(t *testing.T) {
	src := `}
fn ok() -> i32 = 0`
	toks, _ := NewLexer([]byte(src)).All()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Index panicked on stray brace: %v", r)
		}
	}()
	idx := Index(toks)
	if len(idx.Functions) != 1 || idx.Functions[0].Name != "ok" {
		t.Errorf("indexer should still find `ok` after a stray `}`; got %v", idx.Functions)
	}
}

func TestIndexUnterminatedString(t *testing.T) {
	// The lexer will report an error; the indexer should still walk
	// the tokens it did get and not panic.
	src := `fn a() -> i32 = "no closing
fn b() -> i32 = 0`
	toks, _ := NewLexer([]byte(src)).All()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Index panicked on lex-error input: %v", r)
		}
	}()
	_ = Index(toks)
}

func TestIndexEmptyInput(t *testing.T) {
	idx := Index(tokensFor(t, ""))
	if len(idx.Functions) != 0 {
		t.Errorf("empty input should produce no functions; got %v", idx.Functions)
	}
}
