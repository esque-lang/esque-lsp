package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// framed builds a Content-Length framed payload for testing.
func framed(body string) string {
	return "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body
}

func itoa(n int) string {
	// Avoid pulling fmt into a hot helper; this file only frames bytes.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestConnRoundTrip verifies that a message written through conn.writeMessage
// reads back identically through conn.readMessage on a fresh reader.
func TestConnRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	out := newConn(strings.NewReader(""), &buf)
	want := rawMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"trace":"off"}`),
	}
	if err := out.writeMessage(want); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}
	in := newConn(&buf, io.Discard)
	got, err := in.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if got.JSONRPC != want.JSONRPC || got.Method != want.Method ||
		string(got.ID) != string(want.ID) || string(got.Params) != string(want.Params) {
		t.Errorf("round-trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

// TestConnReadTwoMessagesInOneBuffer verifies that when two framed
// messages arrive together (a common case for slow clients flushing
// stdio), readMessage demarcates them correctly across two calls.
func TestConnReadTwoMessagesInOneBuffer(t *testing.T) {
	body1 := `{"jsonrpc":"2.0","id":1,"method":"a"}`
	body2 := `{"jsonrpc":"2.0","id":2,"method":"b"}`
	in := newConn(strings.NewReader(framed(body1)+framed(body2)), io.Discard)

	m1, err := in.readMessage()
	if err != nil {
		t.Fatalf("first readMessage: %v", err)
	}
	if m1.Method != "a" {
		t.Errorf("first method = %q, want %q", m1.Method, "a")
	}
	m2, err := in.readMessage()
	if err != nil {
		t.Fatalf("second readMessage: %v", err)
	}
	if m2.Method != "b" {
		t.Errorf("second method = %q, want %q", m2.Method, "b")
	}
}

// TestConnLowercaseContentLength verifies the spec-permissive
// case-insensitive header parse: editors are not required to
// capitalise `Content-Length`.
func TestConnLowercaseContentLength(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"ping"}`
	wire := "content-length: " + itoa(len(body)) + "\r\n\r\n" + body
	in := newConn(strings.NewReader(wire), io.Discard)
	m, err := in.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if m.Method != "ping" {
		t.Errorf("method = %q, want %q", m.Method, "ping")
	}
}

// TestConnMissingContentLength asserts that headers without a
// Content-Length entry fail loudly rather than producing an
// empty-message ghost or hanging.
func TestConnMissingContentLength(t *testing.T) {
	wire := "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n"
	in := newConn(strings.NewReader(wire), io.Discard)
	_, err := in.readMessage()
	if err == nil {
		t.Fatal("expected error for missing Content-Length, got success")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Errorf("error %q should mention Content-Length", err)
	}
}

// TestConnBadContentLength asserts that a non-numeric Content-Length
// value is rejected rather than silently treated as zero.
func TestConnBadContentLength(t *testing.T) {
	wire := "Content-Length: not-a-number\r\n\r\n{}"
	in := newConn(strings.NewReader(wire), io.Discard)
	_, err := in.readMessage()
	if err == nil {
		t.Fatal("expected error for bad Content-Length, got success")
	}
}

// TestConnBadJSONBody asserts that a well-framed but malformed JSON
// body produces a descriptive error rather than a panic.
func TestConnBadJSONBody(t *testing.T) {
	body := `{not json at all}`
	in := newConn(strings.NewReader(framed(body)), io.Discard)
	_, err := in.readMessage()
	if err == nil {
		t.Fatal("expected error for malformed JSON body, got success")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("error %q should mention JSON", err)
	}
}

// TestConnReplyAndNotify exercises the two send-side helpers and
// confirms the wire form is parseable as a single JSON-RPC message.
func TestConnReplyAndNotify(t *testing.T) {
	var buf bytes.Buffer
	c := newConn(strings.NewReader(""), &buf)

	if err := c.reply(json.RawMessage(`42`), map[string]int{"x": 1}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if err := c.notify("textDocument/publishDiagnostics", map[string]string{"uri": "file:///a"}); err != nil {
		t.Fatalf("notify: %v", err)
	}

	// Read both back through the conn reader to round-trip the wire bytes.
	in := newConn(&buf, io.Discard)
	r1, err := in.readMessage()
	if err != nil {
		t.Fatalf("first readMessage: %v", err)
	}
	if string(r1.ID) != "42" || string(r1.Result) != `{"x":1}` {
		t.Errorf("reply round-trip mismatch: %+v", r1)
	}
	r2, err := in.readMessage()
	if err != nil {
		t.Fatalf("second readMessage: %v", err)
	}
	if r2.Method != "textDocument/publishDiagnostics" || r2.ID != nil {
		t.Errorf("notify round-trip mismatch: %+v", r2)
	}
}

// TestConnReplyError builds an error response and confirms its wire
// representation deserialises with the right code and message.
func TestConnReplyError(t *testing.T) {
	var buf bytes.Buffer
	c := newConn(strings.NewReader(""), &buf)
	if err := c.replyError(json.RawMessage(`7`), errInvalidParams, "missing textDocument"); err != nil {
		t.Fatalf("replyError: %v", err)
	}
	in := newConn(&buf, io.Discard)
	m, err := in.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if m.Error == nil {
		t.Fatalf("expected rpcError, got nil")
	}
	if m.Error.Code != errInvalidParams || m.Error.Message != "missing textDocument" {
		t.Errorf("error round-trip mismatch: code=%d msg=%q", m.Error.Code, m.Error.Message)
	}
	if got, want := m.Error.Error(), "rpc error -32602: missing textDocument"; got != want {
		t.Errorf("rpcError.Error() = %q, want %q", got, want)
	}
}
