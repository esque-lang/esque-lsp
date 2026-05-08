package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
)

// Server is the LSP server state.
type Server struct {
	mu        sync.Mutex
	docs      map[string]*Document
	conn      *conn
	shutdown  bool
}

// Document is a server-side cached open file.
type Document struct {
	URI     string
	Version int
	Text    string
	Tokens  []Tok
	LexErrs []LexErr
	Index   FileIndex
}

func NewServer() *Server {
	return &Server{docs: make(map[string]*Document)}
}

func (s *Server) Run(in io.Reader, out io.Writer) error {
	s.conn = newConn(in, out)
	for {
		msg, err := s.conn.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		s.dispatch(msg)
		if s.shutdown && msg.Method == "exit" {
			return nil
		}
	}
}

func (s *Server) dispatch(m *rawMessage) {
	switch m.Method {
	case "initialize":
		s.onInitialize(m)
	case "initialized":
		// no-op
	case "shutdown":
		s.shutdown = true
		_ = s.conn.reply(m.ID, nil)
	case "exit":
		// handled by Run loop
	case "textDocument/didOpen":
		s.onDidOpen(m)
	case "textDocument/didChange":
		s.onDidChange(m)
	case "textDocument/didClose":
		s.onDidClose(m)
	case "textDocument/didSave":
		// no-op (we revalidate on every change)
	case "textDocument/hover":
		s.onHover(m)
	case "textDocument/completion":
		s.onCompletion(m)
	case "textDocument/documentSymbol":
		s.onDocumentSymbol(m)
	case "textDocument/definition":
		s.onDefinition(m)
	default:
		if len(m.ID) > 0 {
			_ = s.conn.replyError(m.ID, errMethodNotFound, "method not found: "+m.Method)
		}
	}
}

func (s *Server) onInitialize(m *rawMessage) {
	res := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:       1, // full text on every change
			HoverProvider:          true,
			DefinitionProvider:     true,
			DocumentSymbolProvider: true,
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{"@", ".", "|"},
			},
		},
		ServerInfo: &ServerInfo{Name: "esque-lsp", Version: version},
	}
	_ = s.conn.reply(m.ID, res)
}

func (s *Server) updateDoc(uri string, version int, text string) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := &Document{URI: uri, Version: version, Text: text}
	tokens, errs := NewLexer([]byte(text)).All()
	doc.Tokens = tokens
	doc.LexErrs = errs
	doc.Index = Index(tokens)
	s.docs[uri] = doc
	return doc
}

func (s *Server) getDoc(uri string) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.docs[uri]
}

func (s *Server) onDidOpen(m *rawMessage) {
	var p DidOpenTextDocumentParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		log.Printf("didOpen unmarshal: %v", err)
		return
	}
	doc := s.updateDoc(p.TextDocument.URI, p.TextDocument.Version, p.TextDocument.Text)
	s.publishDiagnostics(doc)
}

func (s *Server) onDidChange(m *rawMessage) {
	var p DidChangeTextDocumentParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		log.Printf("didChange unmarshal: %v", err)
		return
	}
	if len(p.ContentChanges) == 0 {
		return
	}
	// We advertised TextDocumentSyncKind=Full; the spec then mandates
	// the last change is the full text.
	last := p.ContentChanges[len(p.ContentChanges)-1]
	doc := s.updateDoc(p.TextDocument.URI, p.TextDocument.Version, last.Text)
	s.publishDiagnostics(doc)
}

func (s *Server) onDidClose(m *rawMessage) {
	var p DidCloseTextDocumentParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return
	}
	s.mu.Lock()
	delete(s.docs, p.TextDocument.URI)
	s.mu.Unlock()
	// Clear diagnostics on close.
	_ = s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         p.TextDocument.URI,
		Diagnostics: []Diagnostic{},
	})
}

func (s *Server) publishDiagnostics(doc *Document) {
	diags := make([]Diagnostic, 0, len(doc.LexErrs))
	for _, e := range doc.LexErrs {
		diags = append(diags, Diagnostic{
			Range:    e.Range,
			Severity: DiagSeverityError,
			Source:   "esque-lsp",
			Message:  e.Msg,
		})
	}
	// Simple structural check: warn on top-level files with no `fn main`.
	hasMain := false
	for _, fn := range doc.Index.Functions {
		if fn.Name == "main" {
			hasMain = true
			break
		}
	}
	if !hasMain && len(doc.Index.Functions) > 0 {
		diags = append(diags, Diagnostic{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
			Severity: DiagSeverityHint,
			Source:   "esque-lsp",
			Message:  "no `fn main` defined in this file",
		})
	}
	v := doc.Version
	_ = s.conn.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         doc.URI,
		Version:     &v,
		Diagnostics: diags,
	})
}

func (s *Server) onDocumentSymbol(m *rawMessage) {
	var p struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		_ = s.conn.replyError(m.ID, errInvalidParams, err.Error())
		return
	}
	doc := s.getDoc(p.TextDocument.URI)
	if doc == nil {
		_ = s.conn.reply(m.ID, []DocumentSymbol{})
		return
	}
	syms := make([]DocumentSymbol, 0, len(doc.Index.Functions))
	for _, fn := range doc.Index.Functions {
		detail := fmt.Sprintf("fn(%s)", strings.Join(fn.Params, ", "))
		if len(fn.Attrs) > 0 {
			var prefix []string
			for _, a := range fn.Attrs {
				prefix = append(prefix, "@"+a)
			}
			detail = strings.Join(prefix, " ") + " " + detail
		}
		syms = append(syms, DocumentSymbol{
			Name:           fn.Name,
			Detail:         detail,
			Kind:           SymKindFunction,
			Range:          fn.Range,
			SelectionRange: fn.NameRng,
		})
	}
	_ = s.conn.reply(m.ID, syms)
}

func (s *Server) onDefinition(m *rawMessage) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		_ = s.conn.replyError(m.ID, errInvalidParams, err.Error())
		return
	}
	doc := s.getDoc(p.TextDocument.URI)
	if doc == nil {
		_ = s.conn.reply(m.ID, nil)
		return
	}
	tok := tokenAt(doc, p.Position)
	if tok == nil || tok.Kind != TkIdent {
		_ = s.conn.reply(m.ID, nil)
		return
	}
	for _, fn := range doc.Index.Functions {
		if fn.Name == tok.Lit {
			_ = s.conn.reply(m.ID, []Location{{URI: doc.URI, Range: fn.NameRng}})
			return
		}
	}
	_ = s.conn.reply(m.ID, nil)
}

func (s *Server) onHover(m *rawMessage) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		_ = s.conn.replyError(m.ID, errInvalidParams, err.Error())
		return
	}
	doc := s.getDoc(p.TextDocument.URI)
	if doc == nil {
		_ = s.conn.reply(m.ID, nil)
		return
	}
	tok := tokenAt(doc, p.Position)
	if tok == nil {
		_ = s.conn.reply(m.ID, nil)
		return
	}
	value := hoverFor(tok, doc)
	if value == "" {
		_ = s.conn.reply(m.ID, nil)
		return
	}
	rng := tok.Range
	_ = s.conn.reply(m.ID, Hover{
		Contents: MarkupContent{Kind: "markdown", Value: value},
		Range:    &rng,
	})
}

func (s *Server) onCompletion(m *rawMessage) {
	var p TextDocumentPositionParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		_ = s.conn.replyError(m.ID, errInvalidParams, err.Error())
		return
	}
	items := keywordCompletions()
	items = append(items, builtinCompletions()...)
	items = append(items, typeCompletions()...)
	if doc := s.getDoc(p.TextDocument.URI); doc != nil {
		for _, fn := range doc.Index.Functions {
			items = append(items, CompletionItem{
				Label:  fn.Name,
				Kind:   CompletionKindFunction,
				Detail: fmt.Sprintf("fn(%s)", strings.Join(fn.Params, ", ")),
			})
		}
	}
	_ = s.conn.reply(m.ID, items)
}

// tokenAt finds the token covering the given position. Returns nil
// if the position falls in whitespace.
func tokenAt(doc *Document, pos Position) *Tok {
	for i := range doc.Tokens {
		t := &doc.Tokens[i]
		if t.Kind == TkEOF {
			break
		}
		if posIn(pos, t.Range) {
			return t
		}
	}
	return nil
}

// posIn returns true if `p` falls inside `r` (start inclusive, end
// exclusive). Editors send positions as 0-based UTF-16 offsets.
func posIn(p Position, r Range) bool {
	if before(p, r.Start) {
		return false
	}
	if before(r.End, p) {
		return false
	}
	// Allow position == End (caret at the right edge of an ident).
	if p == r.End {
		return false
	}
	return true
}

func before(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}
