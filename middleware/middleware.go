// Package middleware sits between the server's routing layer (the
// RequestHandler dispatcher) and the controllers. It runs cross-cutting
// concerns — CORS, recovery, logging — that apply across routes.
package middleware

import "net/http"

// SetCors writes permissive CORS headers onto the response. Call this at the
// top of the request dispatcher, before any route logic runs.
func SetCors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
