// Package models holds the domain types shared across the app.
package models

import "time"

// User is a stored user record. The server controls ID and the timestamps;
// clients never set them directly.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UserInput is the request payload for creating or updating a user. It carries
// only the client-settable fields.
type UserInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserId struct {
	ID string `json:"id"`
}

type UserIds struct {
	IDs []string `json:"ids"`
}
