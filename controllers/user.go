package controllers

import (
	"encoding/json"
	"fmt"

	"streamflix-backend/models"
	"streamflix-backend/store"
)

func CreateUser(params json.RawMessage) (any, error) {
	var input models.UserInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	if input.Name == "" || input.Email == "" {
		return nil, fmt.Errorf("name and email are required")
	}

	return store.PersistedUsers.Create(&input), nil
}

func GetUser(params json.RawMessage) (any, error) {
	var req models.UserId

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("malformed params")
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	user, exist := store.PersistedUsers.Get(req.ID)
	if !exist {
		return nil, fmt.Errorf("user not found: %s", req.ID)
	}
	return user, nil
}

func UpdateUser(params json.RawMessage) (any, error) {
	var req models.User

	err := json.Unmarshal(params, &req)

	if err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	if req.Name == "" && req.Email == "" {
		return nil, fmt.Errorf("no field passed to update")
	}

	return store.PersistedUsers.Update(req.ID, req.Name, req.Email)
}

func ListUsers(params json.RawMessage) (any, error) {
	var req models.UserIds

	// Params are optional for list; only fail on malformed (non-null) JSON.
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("malformed params")
		}
	}

	return store.PersistedUsers.List(req.IDs), nil
}

func DeleteUser(params json.RawMessage) (any, error) {
	var req models.UserId

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("malformed params")
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	if err := store.PersistedUsers.Delete(req.ID); err != nil {
		return nil, err
	}
	return map[string]string{"id": req.ID}, nil
}
