// Package middleware sits between the server's routing layer (the
// RequestHandler dispatcher) and the controllers. It runs cross-cutting
// concerns — CORS, recovery, logging — that apply across routes.
package middleware

import "net/http"

// preflightMaxAge is how long a browser may cache the result of a preflight,
// in seconds. Without it every upload pays for an extra OPTIONS round trip.
const preflightMaxAge = "86400" // 24 hours

// SetCors writes permissive CORS headers onto the response. Call this at the
// top of the request dispatcher, before any route logic runs.
//
// Every origin is allowed. That is safe here only because the API carries no
// cookies or session state: with "*" a browser refuses to send credentials, so
// a hostile page can call these endpoints but cannot borrow a user's identity
// to do it. Once auth lands, this has to become an allow-list of known origins
// echoed back per request — "*" and Allow-Credentials cannot be combined.
func SetCors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

	// Echo whatever headers the preflight asked for, falling back to the ones
	// our own endpoints need. Echoing matters because the raw-binary upload
	// sends X-Filename, and a fixed list silently breaks any client that adds
	// a header we didn't predict.
	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		w.Header().Set("Access-Control-Allow-Headers", requested)
	} else {
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Filename")
	}

	// Let the browser read the upload response headers it may care about.
	w.Header().Set("Access-Control-Expose-Headers", "Content-Type, Content-Length")
	w.Header().Set("Access-Control-Max-Age", preflightMaxAge)

	// Responses vary by the request's origin and requested headers, so any
	// cache in front of this must key on them rather than serving one origin's
	// response to another.
	w.Header().Add("Vary", "Origin")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
}
