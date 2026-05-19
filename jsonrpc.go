package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// rawMessage is the on-the-wire JSON-RPC envelope. Either a request,
// a response, or a notification — the same struct, with the unused
// fields zeroed.
type rawMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

const (
	errParseError     = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternalError  = -32603
)

// maxMsgSize caps the Content-Length we are willing to allocate for a
// single JSON-RPC message. LSP messages are routinely under 1 MiB; 16
// MiB is a comfortable ceiling that prevents a malformed or hostile
// header from triggering an OOM allocation.
const maxMsgSize = 16 << 20

// conn is the framed JSON-RPC transport between the server and a
// single client (the editor).
type conn struct {
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex // serialises writes
}

func newConn(in io.Reader, out io.Writer) *conn {
	return &conn{in: bufio.NewReader(in), out: out}
}

// readMessage reads one Content-Length framed JSON-RPC message.
func (c *conn) readMessage() (*rawMessage, error) {
	var contentLen int
	for {
		line, err := c.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.ToLower(strings.TrimSpace(parts[0])) == "content-length" {
			n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %v", err)
			}
			contentLen = n
		}
	}
	if contentLen <= 0 || contentLen > maxMsgSize {
		return nil, fmt.Errorf("content-length out of range: %d", contentLen)
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(c.in, buf); err != nil {
		return nil, err
	}
	var m rawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, fmt.Errorf("bad JSON body: %v", err)
	}
	return &m, nil
}

func (c *conn) writeMessage(m any) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.out.Write(body)
	return err
}

func (c *conn) reply(id json.RawMessage, result any) error {
	r, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.writeMessage(rawMessage{JSONRPC: "2.0", ID: id, Result: r})
}

func (c *conn) replyError(id json.RawMessage, code int, msg string) error {
	return c.writeMessage(rawMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	})
}

func (c *conn) notify(method string, params any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.writeMessage(rawMessage{JSONRPC: "2.0", Method: method, Params: p})
}
