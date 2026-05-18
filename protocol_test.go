package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Round-trip helper: marshal v, unmarshal into a fresh zero-value of
// the same type, and confirm the result is deep-equal to v. Catches
// `omitempty` / pointer-vs-value / tag drift in protocol.go.
func roundTrip[T any](t *testing.T, label string, v T) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: marshal: %v", label, err)
	}
	var got T
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s: unmarshal %s: %v", label, data, err)
	}
	if !reflect.DeepEqual(got, v) {
		t.Errorf("%s: round-trip mismatch\nwire: %s\n got: %+v\nwant: %+v", label, data, got, v)
	}
}

func TestProtocolPositionRange(t *testing.T) {
	roundTrip(t, "Position", Position{Line: 3, Character: 7})
	roundTrip(t, "Range", Range{
		Start: Position{Line: 1, Character: 0},
		End:   Position{Line: 1, Character: 10},
	})
	roundTrip(t, "Location", Location{
		URI: "file:///tmp/a.esq",
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 5},
		},
	})
}

func TestProtocolInitializeResult(t *testing.T) {
	res := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:       1,
			HoverProvider:          true,
			DefinitionProvider:     true,
			DocumentSymbolProvider: true,
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{"@", ".", "|"},
			},
		},
		ServerInfo: &ServerInfo{Name: "esque-lsp", Version: "0.14.0"},
	}
	roundTrip(t, "InitializeResult", res)

	// The completionProvider field carries `omitempty` — confirm that
	// an unset value drops out of the wire form rather than serialising
	// as `null` (which Helix has historically rejected).
	bare := InitializeResult{
		Capabilities: ServerCapabilities{TextDocumentSync: 1},
	}
	data, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	if strings.Contains(string(data), "completionProvider") {
		t.Errorf("bare ServerCapabilities should omit completionProvider; got %s", data)
	}
}

func TestProtocolHover(t *testing.T) {
	rng := Range{
		Start: Position{Line: 2, Character: 4},
		End:   Position{Line: 2, Character: 6},
	}
	roundTrip(t, "Hover (with range)", Hover{
		Contents: MarkupContent{Kind: "markdown", Value: "**`fn`**\n\nFunction declaration."},
		Range:    &rng,
	})
	roundTrip(t, "Hover (no range)", Hover{
		Contents: MarkupContent{Kind: "plaintext", Value: "no markup"},
	})
}

func TestProtocolCompletionItem(t *testing.T) {
	roundTrip(t, "CompletionItem (full)", CompletionItem{
		Label:         "tabulate",
		Kind:          CompletionKindFunction,
		Detail:        "fn(N, f)",
		Documentation: "Index-driven tensor construction.",
		InsertText:    "tabulate($1, |i| $2)",
	})
	roundTrip(t, "CompletionItem (minimal)", CompletionItem{Label: "let"})
}

func TestProtocolDocumentSymbol(t *testing.T) {
	rng := Range{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 4, Character: 1},
	}
	nameRng := Range{
		Start: Position{Line: 0, Character: 3},
		End:   Position{Line: 0, Character: 7},
	}
	roundTrip(t, "DocumentSymbol (flat)", DocumentSymbol{
		Name:           "main",
		Detail:         "fn()",
		Kind:           SymKindFunction,
		Range:          rng,
		SelectionRange: nameRng,
	})
	roundTrip(t, "DocumentSymbol (nested)", DocumentSymbol{
		Name:           "outer",
		Kind:           SymKindFunction,
		Range:          rng,
		SelectionRange: nameRng,
		Children: []DocumentSymbol{
			{
				Name:           "inner",
				Kind:           SymKindFunction,
				Range:          rng,
				SelectionRange: nameRng,
			},
		},
	})
}

func TestProtocolDiagnostic(t *testing.T) {
	rng := Range{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 0, Character: 3},
	}
	roundTrip(t, "Diagnostic", Diagnostic{
		Range:    rng,
		Severity: DiagSeverityError,
		Source:   "esque-lsp",
		Message:  "unterminated string literal",
	})
	// PublishDiagnosticsParams carries an `omitempty` *int Version
	// field; round-trip both states explicitly.
	v := 7
	roundTrip(t, "PublishDiagnosticsParams (versioned)", PublishDiagnosticsParams{
		URI:         "file:///a.esq",
		Version:     &v,
		Diagnostics: []Diagnostic{{Range: rng, Severity: DiagSeverityWarn, Message: "x"}},
	})
	roundTrip(t, "PublishDiagnosticsParams (unversioned)", PublishDiagnosticsParams{
		URI:         "file:///a.esq",
		Diagnostics: []Diagnostic{},
	})
}

// TestProtocolDidChangeTextDocumentParams covers the trickier nested
// case: a slice of TextDocumentContentChangeEvent each carrying an
// optional Range pointer.
func TestProtocolDidChangeTextDocumentParams(t *testing.T) {
	rng := Range{
		Start: Position{Line: 5, Character: 2},
		End:   Position{Line: 5, Character: 8},
	}
	roundTrip(t, "DidChange (incremental)", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: "file:///b.esq", Version: 3},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Range: &rng, Text: "fn"},
		},
	})
	roundTrip(t, "DidChange (full)", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: "file:///b.esq", Version: 3},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: "fn main() -> i32 = 0"},
		},
	})
}
