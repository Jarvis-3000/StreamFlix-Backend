package controllers

import "net/http"

// Health reports service availability.
func Health(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"health":  "100%",
		"service": "streamflix-backend",
	})
}
