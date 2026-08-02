package controllers

import "net/http"

// ListMedia returns all media items.
func ListMedia(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusNotImplemented, map[string]string{
		"message": "list media not implemented yet",
	})
}

// GetMedia returns a single media item by id.
func GetMedia(w http.ResponseWriter, r *http.Request, id string) {
	JSON(w, http.StatusNotImplemented, map[string]string{
		"message": "get media not implemented yet",
		"id":      id,
	})
}

// UpdateMedia updates media metadata by id.
func UpdateMedia(w http.ResponseWriter, r *http.Request, id string) {
	JSON(w, http.StatusNotImplemented, map[string]string{
		"message": "update media not implemented yet",
		"id":      id,
	})
}

// DeleteMedia removes a media item by id.
func DeleteMedia(w http.ResponseWriter, r *http.Request, id string) {
	JSON(w, http.StatusNotImplemented, map[string]string{
		"message": "delete media not implemented yet",
		"id":      id,
	})
}
