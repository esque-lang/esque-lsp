package main

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// session wires a Server up to a pair of io.Pipes so a test can speak
// LSP to it in-process. clientWrite sends bytes to the server; the
// server's replies arrive on the connection wrapped around the
// server-side reader.
type session struct {
	t      *testing.T
	server *Server
	// Client-side conn — used to send to / receive from the server.
	conn *conn
	done chan error
}

func newSession(t *testing.T) *session {
	t.Helper()

	clientToServer, fromClient := io.Pipe()
	toClient, serverToClient := io.Pipe()

	srv := NewServer()
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(clientToServer, serverToClient)
	}()

	// Bridge: the client's conn writes go *to* the server (via
	// fromClient.Write -> clientToServer.Read inside Server.Run) and
	// reads come *from* the server (via toClient.Read).
	cc := newConn(toClient, fromClient)

	t.Cleanup(func() {
		_ = fromClient.Close()
		_ = toClient.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("server did not exit within 2s of pipe close")
		}
	})

	return &session{t: t, server: srv, conn: cc, done: done}
}

// request sends a JSON-RPC request and reads the matching response.
// Notifications that arrive ahead of the response (e.g.
// publishDiagnostics on didOpen) are returned alongside.
func (s *session) request(method string, id int, params any) (*rawMessage, []*rawMessage) {
	s.t.Helper()
	body, _ := json.Marshal(params)
	if err := s.conn.writeMessage(rawMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(itoa(id)),
		Method:  method,
		Params:  body,
	}); err != nil {
		s.t.Fatalf("send %s: %v", method, err)
	}
	var notifs []*rawMessage
	for {
		m, err := s.conn.readMessage()
		if err != nil {
			s.t.Fatalf("read response to %s: %v", method, err)
		}
		// Notifications carry no ID; responses do.
		if len(m.ID) == 0 {
			notifs = append(notifs, m)
			continue
		}
		return m, notifs
	}
}

// notify sends a one-way notification.
func (s *session) notify(method string, params any) {
	s.t.Helper()
	body, _ := json.Marshal(params)
	if err := s.conn.writeMessage(rawMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  body,
	}); err != nil {
		s.t.Fatalf("notify %s: %v", method, err)
	}
}

// nextNotif reads the next message and asserts it's a notification.
// Used after didOpen / didChange to drain a publishDiagnostics that
// the server sends unprompted.
func (s *session) nextNotif() *rawMessage {
	s.t.Helper()
	m, err := s.conn.readMessage()
	if err != nil {
		s.t.Fatalf("read notification: %v", err)
	}
	if len(m.ID) != 0 {
		s.t.Fatalf("expected notification, got message with id %s", m.ID)
	}
	return m
}

// TestServerInitializeAdvertisesCapabilities sends `initialize` and
// confirms the server's reply lists the capabilities the README and
// `protocol.go` claim it supports.
func TestServerInitializeAdvertisesCapabilities(t *testing.T) {
	s := newSession(t)
	resp, _ := s.request("initialize", 1, InitializeParams{Trace: "off"})
	if resp.Error != nil {
		t.Fatalf("initialize returned rpc error: %+v", resp.Error)
	}
	var res InitializeResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("unmarshal InitializeResult: %v", err)
	}
	if !res.Capabilities.HoverProvider {
		t.Error("expected HoverProvider=true")
	}
	if !res.Capabilities.DefinitionProvider {
		t.Error("expected DefinitionProvider=true")
	}
	if !res.Capabilities.DocumentSymbolProvider {
		t.Error("expected DocumentSymbolProvider=true")
	}
	if res.Capabilities.CompletionProvider == nil {
		t.Error("expected CompletionProvider to be advertised")
	}
	if res.Capabilities.TextDocumentSync != 1 {
		t.Errorf("TextDocumentSync = %d, want 1 (full)", res.Capabilities.TextDocumentSync)
	}
	if res.ServerInfo == nil || res.ServerInfo.Name != "esque-lsp" {
		t.Errorf("ServerInfo = %+v, want Name=esque-lsp", res.ServerInfo)
	}
}

// TestServerDidOpenPublishesDiagnostics opens a doc with a lex
// error (an unterminated string) and confirms the unsolicited
// `textDocument/publishDiagnostics` notification arrives and carries
// the expected diagnostic.
func TestServerDidOpenPublishesDiagnostics(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	s.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        "file:///a.esq",
			LanguageID: "esque",
			Version:    1,
			Text:       `fn main() -> i32 = "no closing`,
		},
	})
	notif := s.nextNotif()
	if notif.Method != "textDocument/publishDiagnostics" {
		t.Fatalf("expected publishDiagnostics, got %q", notif.Method)
	}
	var p PublishDiagnosticsParams
	if err := json.Unmarshal(notif.Params, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.URI != "file:///a.esq" {
		t.Errorf("URI = %q", p.URI)
	}
	if len(p.Diagnostics) == 0 {
		t.Fatal("expected at least one diagnostic for unterminated string, got none")
	}
	if !strings.Contains(strings.ToLower(p.Diagnostics[0].Message), "unterminated") {
		t.Errorf("first diagnostic should mention unterminated string; got %q", p.Diagnostics[0].Message)
	}
}

// TestServerHoverOnKeyword opens a tiny file and asks for hover info
// on the `fn` keyword. End-to-end exercises: didOpen path, lexer,
// indexer, hoverFor, conn round-trip.
func TestServerHoverOnKeyword(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	s.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI: "file:///b.esq", Version: 1, LanguageID: "esque",
			Text: "fn main() -> i32 = 0\n",
		},
	})
	_ = s.nextNotif() // drain the publishDiagnostics

	resp, _ := s.request("textDocument/hover", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///b.esq"},
		Position:     Position{Line: 0, Character: 0}, // on `fn`
	})
	if resp.Error != nil {
		t.Fatalf("hover rpc error: %+v", resp.Error)
	}
	var h Hover
	if err := json.Unmarshal(resp.Result, &h); err != nil {
		t.Fatalf("unmarshal Hover: %v", err)
	}
	if h.Contents.Kind != "markdown" {
		t.Errorf("Contents.Kind = %q, want markdown", h.Contents.Kind)
	}
	if !strings.Contains(h.Contents.Value, "Function declaration") {
		t.Errorf("hover value should mention `Function declaration`; got %q", h.Contents.Value)
	}
}

// TestServerDocumentSymbolReturnsFunctions opens a doc with two
// top-level fns and confirms `documentSymbol` returns them both with
// the right kind + selection range pointing at the name.
func TestServerDocumentSymbolReturnsFunctions(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	s.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI: "file:///c.esq", Version: 1, LanguageID: "esque",
			Text: "fn add(a: i32, b: i32) -> i32 = a + b\nfn main() -> i32 = 0\n",
		},
	})
	_ = s.nextNotif()

	resp, _ := s.request("textDocument/documentSymbol", 3, struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}{TextDocument: TextDocumentIdentifier{URI: "file:///c.esq"}})

	var syms []DocumentSymbol
	if err := json.Unmarshal(resp.Result, &syms); err != nil {
		t.Fatalf("unmarshal []DocumentSymbol: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("symbols = %d, want 2", len(syms))
	}
	if syms[0].Name != "add" || syms[1].Name != "main" {
		t.Errorf("names = [%q, %q], want [add, main]", syms[0].Name, syms[1].Name)
	}
	if syms[0].Kind != SymKindFunction {
		t.Errorf("first kind = %d, want SymKindFunction (%d)", syms[0].Kind, SymKindFunction)
	}
	// Detail should look like `fn(a, b)` — after the params-indexer
	// fix from the preceding commit, type names must not appear.
	if strings.Contains(syms[0].Detail, "i32") {
		t.Errorf("Detail %q should not contain type names", syms[0].Detail)
	}
}

// TestServerDefinitionJumpsToFn confirms `textDocument/definition`
// resolves a call-site identifier to the function's name range.
func TestServerDefinitionJumpsToFn(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	src := "fn add(a: i32, b: i32) -> i32 = a + b\nfn main() -> i32 = add(1, 2)\n"
	s.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI: "file:///d.esq", Version: 1, LanguageID: "esque",
			Text: src,
		},
	})
	_ = s.nextNotif()

	// Line 1 (0-indexed), the `add` call-site starts at column 20:
	//   `fn main() -> i32 = add(1, 2)`
	//    0123456789012345678901
	resp, _ := s.request("textDocument/definition", 4, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///d.esq"},
		Position:     Position{Line: 1, Character: 20},
	})

	if string(resp.Result) == "null" {
		t.Fatalf("definition returned null; expected location to fn add")
	}
	var locs []Location
	if err := json.Unmarshal(resp.Result, &locs); err != nil {
		t.Fatalf("unmarshal []Location: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("locations = %d, want 1", len(locs))
	}
	if locs[0].URI != "file:///d.esq" {
		t.Errorf("URI = %q", locs[0].URI)
	}
	// `add` name lives on line 0 starting at column 3.
	if locs[0].Range.Start.Line != 0 || locs[0].Range.Start.Character != 3 {
		t.Errorf("Range.Start = %+v, want {Line:0, Character:3}", locs[0].Range.Start)
	}
}

// TestServerCompletionIncludesKeywordsAndUserFns: completion at any
// position returns the keyword/type/builtin stock plus any user fns
// the indexer found.
func TestServerCompletionIncludesKeywordsAndUserFns(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	s.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI: "file:///e.esq", Version: 1, LanguageID: "esque",
			Text: "fn custom() -> i32 = 0\n",
		},
	})
	_ = s.nextNotif()

	resp, _ := s.request("textDocument/completion", 5, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///e.esq"},
		Position:     Position{Line: 0, Character: 0},
	})
	var items []CompletionItem
	if err := json.Unmarshal(resp.Result, &items); err != nil {
		t.Fatalf("unmarshal []CompletionItem: %v", err)
	}
	labels := make(map[string]bool, len(items))
	for _, it := range items {
		labels[it.Label] = true
	}
	for _, want := range []string{"fn", "let", "match", "i32", "tabulate", "custom"} {
		if !labels[want] {
			t.Errorf("completion missing %q (labels: %v)", want, labels)
		}
	}
}

// TestServerUnknownMethodReturnsError: an unknown request method gets
// a structured -32601 (method-not-found) response, not silence.
func TestServerUnknownMethodReturnsError(t *testing.T) {
	s := newSession(t)
	_, _ = s.request("initialize", 1, InitializeParams{})

	resp, _ := s.request("textDocument/wibble", 99, struct{}{})
	if resp.Error == nil {
		t.Fatal("expected rpc error for unknown method, got none")
	}
	if resp.Error.Code != errMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, errMethodNotFound)
	}
}

// TestServerShutdownThenExit: shutdown returns null and sets the
// shutdown flag; exit then causes Run to return io.EOF / nil.
func TestServerShutdownThenExit(t *testing.T) {
	clientToServer, fromClient := io.Pipe()
	toClient, serverToClient := io.Pipe()
	srv := NewServer()

	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = srv.Run(clientToServer, serverToClient)
	}()
	cc := newConn(toClient, fromClient)

	// shutdown
	if err := cc.writeMessage(rawMessage{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "shutdown",
	}); err != nil {
		t.Fatalf("send shutdown: %v", err)
	}
	resp, err := cc.readMessage()
	if err != nil {
		t.Fatalf("read shutdown response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("shutdown rpc error: %+v", resp.Error)
	}

	// exit
	if err := cc.writeMessage(rawMessage{
		JSONRPC: "2.0", Method: "exit",
	}); err != nil {
		t.Fatalf("send exit: %v", err)
	}

	// Wait for Run to return.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		if runErr != nil {
			t.Errorf("Run returned %v after exit, want nil", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit within 2s of `exit` notification")
	}

	_ = fromClient.Close()
	_ = toClient.Close()
}
