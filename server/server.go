// Package server is the HTTP entry point. A single catch-all handler
// (RequestHandler) receives every request, applies cross-cutting concerns
// (recovery, CORS), handles the fixed endpoints, and hands everything else to
// the middleware router. This layer holds no business logic.
package server

import (
	"log"
	"net/http"
	"runtime/debug"

	"streamflix-backend/controllers"
	"streamflix-backend/middleware"
	"streamflix-backend/utils"
)

// Fixed (exact-match) endpoints handled directly by the server.
const (
	HEALTH = "/health"
	UPLOAD = "/upload"
	RPC    = "/rpc"
)

// server is the running HTTP server instance.
var server *http.Server

// Init builds the mux, registers the catch-all handler, and starts listening
// on addr (e.g. ":8080"). It blocks until the server stops.
func Init(addr string) error {
	log.Printf("Starting web server: %s", addr)

	r := http.NewServeMux()
	r.HandleFunc("/", RequestHandler)

	server = &http.Server{
		Addr:    addr,
		Handler: r,
	}
	return server.ListenAndServe()
}

// RequestHandler is the single entry point for every request. It recovers from
// panics, applies CORS, short-circuits preflight requests, handles the fixed
// endpoints, then defers all resource routing to the middleware router.
func RequestHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic: %v\n%s", rec, debug.Stack())
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		}
	}()

	middleware.SetCors(w, r)

	// CORS preflight — respond and stop.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent) // 204
		return
	}

	switch {
	case utils.PathIs(HEALTH, r):
		controllers.Health(w, r)
	case utils.PathIs(UPLOAD, r):
		controllers.Upload(w, r)
	case utils.PathIs(RPC, r):
		middleware.Route(w, r)
	default:
		controllers.Error(w, http.StatusNotFound, "page not found")
	}
}
