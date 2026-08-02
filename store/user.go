package store

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"streamflix-backend/models"

	"github.com/google/uuid"
)

type UserStore struct {
	mu    sync.RWMutex
	users map[string]models.User
}

func NewUserStore() *UserStore {
	return &UserStore{users: make(map[string]models.User)}
}

var Users = NewUserStore()

func (s *UserStore) Create(userInput *models.UserInput) models.User {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	user := models.User{
		ID:        uuid.New().String(),
		Name:      userInput.Name,
		Email:     userInput.Email,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.users[user.ID] = user

	return user
}

func (s *UserStore) Get(id string) (models.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exist := s.users[id]

	return user, exist
}

// List returns users as a slice sorted by CreatedAt. When ids is non-empty the
// result is filtered to those ids (unknown ids are skipped); an empty ids slice
// returns all users.
func (s *UserStore) List(ids []string) []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var users []models.User
	if len(ids) > 0 {
		for _, id := range ids {
			if u, ok := s.users[id]; ok {
				users = append(users, u)
			}
		}
	} else {
		for _, u := range s.users {
			users = append(users, u)
		}
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt.Before(users[j].CreatedAt)
	})

	return users
}

// Update applies the non-empty fields of input to the user with the given id
// and bumps UpdatedAt. The read-modify-write happens under a single write lock
// so concurrent updates can't clobber each other. Returns the updated user.
func (s *UserStore) Update(id string, name, email string) (models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exist := s.users[id]
	if !exist {
		return models.User{}, fmt.Errorf("user not found")
	}

	if name != "" {
		user.Name = name
	}
	if email != "" {
		user.Email = email
	}
	user.UpdatedAt = time.Now()

	s.users[id] = user
	return user, nil
}

func (s *UserStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exist := s.users[id]; !exist {
		return fmt.Errorf("user not found")
	}

	delete(s.users, id)
	return nil
}
