// Package utils holds small shared helpers used across the server.
package utils

import (
	"bytes"
	"net/http"
)

// PathIs reports whether the request path is exactly route. Use this for fixed
// endpoints like /health or /upload.
func PathIs(route string, r *http.Request) bool {
	return bytes.Equal([]byte(r.URL.Path), []byte(route))
}

// PathPrefixIs reports whether path starts with route. Use this for route
// groups that carry an id or sub-path, e.g. /media/{id}.
func PathPrefixIs(route string, path []byte) bool {
	return bytes.HasPrefix(path, []byte(route))
}
