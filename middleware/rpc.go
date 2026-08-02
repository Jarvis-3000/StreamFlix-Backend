package middleware

import "encoding/json"

// JSON-RPC 2.0 envelope types. See https://www.jsonrpc.org/specification.

// rpcRequest is an incoming JSON-RPC call. Params is left raw so each handler
// can decode it into its own concrete type.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// rpcResponse is a successful or error JSON-RPC reply. Exactly one of Result or
// Error is set. ID echoes the request id (null for parse errors).
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// rpcError is the error object of a JSON-RPC response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)
