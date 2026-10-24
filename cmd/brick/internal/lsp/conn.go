package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"sync"
)

// The JSON-RPC 2.0 transport LSP uses: each message is a JSON body after a
// `Content-Length` header block. It is written by hand on encoding/json so
// the CLI carries no LSP library.

// jsonrpcVersion is the protocol version every message names.
const jsonrpcVersion = "2.0"

// maxMessage bounds one message's body, so a broken client cannot make the
// server allocate without limit. A whole open document arrives in one
// message; 64 MiB is far above any template.
const maxMessage = 64 << 20

// JSON-RPC and LSP error codes the server answers with.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
	// codeRequestFailed is LSP's RequestFailed: the request was valid but
	// the server refuses it (a rename brick cannot carry out safely).
	codeRequestFailed = -32803
)

// message is any JSON-RPC message: a request (ID and Method), a
// notification (Method only) or a response (ID with Result or Error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a response's error.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

// Conn reads and writes framed messages. Reads happen on one goroutine;
// writes are serialized, because diagnostics are published from the
// background check while responses go out from the read loop.
type Conn struct {
	r  *bufio.Reader
	mu sync.Mutex
	w  io.Writer
}

// NewConn frames messages over r and w (stdin and stdout for `brick lsp`).
func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{r: bufio.NewReader(r), w: w}
}

// errNoLength is a header block without Content-Length.
var errNoLength = errors.New("lsp: message has no Content-Length")

// Read returns the next message. io.EOF means the client closed the stream.
func (c *Conn) Read() (*message, error) {
	h, err := textproto.NewReader(c.r).ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(h) == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	v := h.Get("Content-Length")
	if v == "" {
		return nil, errNoLength
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > maxMessage {
		return nil, fmt.Errorf("lsp: bad Content-Length %q", v)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return &message{}, &rpcError{Code: codeParseError, Message: err.Error()}
	}
	return &m, nil
}

// write sends one message.
func (c *Conn) write(m *message) error {
	m.JSONRPC = jsonrpcVersion
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

// Reply answers request id with result, or with err when it is not nil.
func (c *Conn) Reply(id json.RawMessage, result any, err *rpcError) error {
	m := &message{ID: id}
	if err != nil {
		m.Error = err
		return c.write(m)
	}
	b, merr := json.Marshal(result)
	if merr != nil {
		return c.write(&message{ID: id, Error: &rpcError{Code: codeInternalError, Message: merr.Error()}})
	}
	m.Result = b
	return c.write(m)
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(&message{Method: method, Params: b})
}
