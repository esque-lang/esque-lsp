package main

import (
	"strings"
	"testing"
)

// The v0.14 reserved-keyword set, in source-canonical order. Add a
// new keyword here when one is reserved and the test will require
// docs for it.
var v014Keywords = []string{
	"fn", "let", "mut", "return",
	"if", "else", "match",
	"true", "false",
	"as", "in",
}

// Every primitive numeric / structural type esque admits today.
var v014Types = []string{
	"i8", "i16", "i32", "i64",
	"u8", "u16", "u32", "u64",
	"f32", "f64",
	"bool", "unit", "nat",
}

// The runtime / loop-primitive functions the lexer highlights as
// builtins and the hover panel should describe.
var v014Builtins = []string{
	"tabulate", "scan", "iterate", "iterate_until", "each",
	"print_i32", "print_f32", "print_str",
}

// Every operator that has a hoverFor case in lang.go. Update both
// `operatorDocs` and this list when adding a new one.
var v014Operators = []string{
	"|>",
	"+/", "-/", "*/", "//",
	".+", ".-", ".*", "./", ".%",
	"@",
	"..", "..=",
}

func TestLangKeywordDocsComplete(t *testing.T) {
	for _, kw := range v014Keywords {
		if doc, ok := keywordDocs[kw]; !ok || strings.TrimSpace(doc) == "" {
			t.Errorf("keywordDocs[%q] missing or empty", kw)
		}
	}
}

func TestLangTypeDocsComplete(t *testing.T) {
	for _, name := range v014Types {
		if doc, ok := typeDocs[name]; !ok || strings.TrimSpace(doc) == "" {
			t.Errorf("typeDocs[%q] missing or empty", name)
		}
	}
}

func TestLangBuiltinDocsComplete(t *testing.T) {
	for _, name := range v014Builtins {
		if doc, ok := builtinDocs[name]; !ok || strings.TrimSpace(doc) == "" {
			t.Errorf("builtinDocs[%q] missing or empty", name)
		}
	}
}

func TestLangOperatorDocsComplete(t *testing.T) {
	for _, op := range v014Operators {
		if doc, ok := operatorDocs[op]; !ok || strings.TrimSpace(doc) == "" {
			t.Errorf("operatorDocs[%q] missing or empty", op)
		}
	}
}

// hoverDoc is a small wrapper that lexes a single-token snippet,
// finds the first non-EOF token, and runs hoverFor against it.
func hoverDoc(t *testing.T, snippet string) string {
	t.Helper()
	toks, _ := NewLexer([]byte(snippet)).All()
	var first *Tok
	for i := range toks {
		if toks[i].Kind != TkEOF {
			first = &toks[i]
			break
		}
	}
	if first == nil {
		return ""
	}
	// hoverFor takes a Document only to resolve user-defined fn names;
	// empty doc is fine for keyword / type / builtin / operator cases.
	return hoverFor(first, &Document{})
}

// TestHoverEveryKeyword: hovering on each reserved keyword returns
// a non-empty markdown block. Catches a regression where a new
// keyword is added to the lexer but not to keywordDocs.
func TestHoverEveryKeyword(t *testing.T) {
	for _, kw := range v014Keywords {
		got := hoverDoc(t, kw)
		if got == "" {
			t.Errorf("hover on keyword %q returned empty", kw)
			continue
		}
		if !strings.Contains(got, kw) {
			t.Errorf("hover on keyword %q does not mention the keyword itself: %q", kw, got)
		}
	}
}

// TestHoverEveryType: every primitive type produces a non-empty
// hover block when seen as an identifier.
func TestHoverEveryType(t *testing.T) {
	for _, name := range v014Types {
		got := hoverDoc(t, name)
		if got == "" {
			t.Errorf("hover on type %q returned empty", name)
		}
	}
}

// TestHoverEveryBuiltin: every runtime/loop-primitive ident produces
// a non-empty hover block. nat is a type — covered above — and
// shouldn't double up here.
func TestHoverEveryBuiltin(t *testing.T) {
	for _, name := range v014Builtins {
		got := hoverDoc(t, name)
		if got == "" {
			t.Errorf("hover on builtin %q returned empty", name)
		}
	}
}

// TestHoverEveryOperator: each operator token in v014Operators has
// a hoverFor case and a non-empty doc string.
func TestHoverEveryOperator(t *testing.T) {
	for _, op := range v014Operators {
		got := hoverDoc(t, op)
		if got == "" {
			t.Errorf("hover on operator %q returned empty", op)
		}
	}
}

// TestHoverUnknownIdent: an identifier that is neither a type nor a
// builtin nor a known local fn returns "" rather than a misleading
// "Type or builtin." stub.
func TestHoverUnknownIdent(t *testing.T) {
	if got := hoverDoc(t, "unknown_name_zzz"); got != "" {
		t.Errorf("hover on unknown identifier should be empty; got %q", got)
	}
}

// TestHoverAttrKnownAndUnknown verifies that:
//   - `@io` returns the documented attribute hover.
//   - `@something_user_defined` returns a generic fallback (not
//     empty, but explicitly labelled as such).
func TestHoverAttrKnownAndUnknown(t *testing.T) {
	known := hoverDoc(t, "@io")
	if known == "" {
		t.Fatal("hover on @io returned empty")
	}
	if !strings.Contains(known, "@io") {
		t.Errorf("hover on @io does not mention itself: %q", known)
	}
	unknown := hoverDoc(t, "@something_user_defined")
	if unknown == "" {
		t.Fatal("hover on unknown @attr returned empty (expected a fallback)")
	}
	if !strings.Contains(unknown, "@something_user_defined") {
		t.Errorf("fallback hover should mention the attribute name: %q", unknown)
	}
}

// TestHoverUserFunction: a token whose literal matches a function in
// the in-memory Document index produces a hover with the function
// signature.
func TestHoverUserFunction(t *testing.T) {
	src := `@io fn show(x: i32) -> i32 = print_i32(x)`
	toks, _ := NewLexer([]byte(src)).All()
	doc := &Document{Tokens: toks, Index: Index(toks)}
	// Synthesise a TkIdent token referencing the function name.
	ref := &Tok{Kind: TkIdent, Lit: "show"}
	got := hoverFor(ref, doc)
	if got == "" {
		t.Fatal("hover on user fn returned empty")
	}
	if !strings.Contains(got, "fn show") {
		t.Errorf("hover should include `fn show`; got %q", got)
	}
	if !strings.Contains(got, "@io") {
		t.Errorf("hover should include the @io attribute prefix; got %q", got)
	}
}

// TestCompletionTablesPopulated: each of the three completion-list
// helpers produces at least one entry, and each entry has a Label
// plus a Kind (so the editor renders an icon).
func TestCompletionTablesPopulated(t *testing.T) {
	check := func(name string, items []CompletionItem) {
		t.Helper()
		if len(items) == 0 {
			t.Errorf("%s() produced no completions", name)
		}
		for _, it := range items {
			if it.Label == "" {
				t.Errorf("%s(): empty Label in %+v", name, it)
			}
			if it.Kind == 0 {
				t.Errorf("%s(): missing Kind in %+v", name, it)
			}
		}
	}
	check("keywordCompletions", keywordCompletions())
	check("builtinCompletions", builtinCompletions())
	check("typeCompletions", typeCompletions())
}
