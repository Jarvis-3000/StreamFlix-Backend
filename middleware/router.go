package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"streamflix-backend/controllers"
	"streamflix-backend/models"
)

// Route is the single JSON-RPC entry point (POST /rpc). It reads the body once,
// decodes the JSON-RPC envelope, dispatches on the method name, and writes a
// JSON-RPC 2.0 response.
func Route(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: codeParseError, Message: "invalid JSON"},
			ID:      jsonNull,
		})
		return
	}

	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: codeInvalidRequest, Message: "invalid request"},
			ID:      req.ID,
		})
		return
	}

	result, rpcErr := handleResources(req.Method, req.Params)

	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	writeRPC(w, resp)
}

// handleResources splits the JSON-RPC method into "<resource>.<method>" (e.g.
// "user.create"), routes on the resource, and turns a handler's plain error into
// a JSON-RPC invalid-params error. This is the single place errors are mapped.
func handleResources(fullMethod string, params json.RawMessage) (any, *rpcError) {
	resource, method, ok := strings.Cut(fullMethod, ".")
	if !ok {
		return nil, methodNotFound(fullMethod)
	}

	var result any
	var err error
	switch resource {
	case models.RESOURCE_USER:
		result, err = handleUser(method, params)
	case models.RESOURCE_MEDIA:
		result, err = handleMedia(method, params)
	default:
		return nil, methodNotFound(fullMethod)
	}

	if err != nil {
		// An unknown method keeps the distinct method-not-found code; any other
		// handler error is reported as invalid params.
		if errors.Is(err, errMethodNotFound) {
			return nil, methodNotFound(fullMethod)
		}
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	return result, nil
}

// handleUser dispatches the method part of a "user.*" call.
func handleUser(method string, params json.RawMessage) (any, error) {
	switch method {
	case models.USER_CREATE:
		return controllers.CreateUser(params)
	case models.USER_GET:
		return controllers.GetUser(params)
	case models.USER_LIST:
		return controllers.ListUsers(params)
	case models.USER_DELETE:
		return controllers.DeleteUser(params)
	case models.USER_UPDATE:
		return controllers.UpdateUser(params)
	default:
		return nil, errMethodNotFound
	}
}

// handleMedia dispatches the method part of a "media.*" call.
func handleMedia(method string, params json.RawMessage) (any, error) {
	return nil, errMethodNotFound
}

// errMethodNotFound is returned by a resource handler for an unknown method.
var errMethodNotFound = errors.New("method not found")

// methodNotFound builds the JSON-RPC error for an unknown resource or method.
func methodNotFound(fullMethod string) *rpcError {
	return &rpcError{Code: codeMethodNotFound, Message: "method not found: " + fullMethod}
}

// writeRPC encodes a JSON-RPC response. The transport status is always 200; the
// error, if any, lives in the body per the JSON-RPC spec.
func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// jsonNull is the JSON-RPC id used when the request couldn't be parsed.
var jsonNull = json.RawMessage("null")
