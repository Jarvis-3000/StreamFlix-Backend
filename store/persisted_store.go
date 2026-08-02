package store

import (
	"log"

	"streamflix-backend/models"
	"streamflix-backend/persistence"
)

// userDataFile is where the users map is persisted as JSON.
const userDataFile = "data/users.json"

// PersistedUserStore wraps the in-memory UserStore and persists its data to a
// JSON file on disk. It forwards reads straight through and saves the whole
// store to disk after every mutation, so data survives restarts. The embedded
// *UserStore already serializes access with its own lock.
type PersistedUserStore struct {
	*UserStore
	file *persistence.JSONFile
}

// NewPersistedUserStore builds the wrapper, loads any previously persisted users
// from disk, and returns it ready to use.
func NewPersistedUserStore() *PersistedUserStore {
	file, err := persistence.NewJSONFile(userDataFile)
	if err != nil {
		log.Fatalf("user store: %v", err)
	}

	p := &PersistedUserStore{
		UserStore: NewUserStore(),
		file:      file,
	}

	if err := file.Load(&p.users); err != nil {
		log.Fatalf("user store: load: %v", err)
	}

	return p
}

// PersistedUsers is the durable user store the rest of the app should use.
var PersistedUsers = NewPersistedUserStore()

// save writes the current users map to disk, logging (not returning) any error
// so a failed write never breaks the API response.
func (p *PersistedUserStore) save(op string) {
	if err := p.file.Save(p.snapshot()); err != nil {
		log.Printf("user store: save after %s: %v", op, err)
	}
}

// snapshot copies the users map under the read lock so saving doesn't race with
// concurrent reads/writes on the underlying store.
func (p *PersistedUserStore) snapshot() map[string]models.User {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make(map[string]models.User, len(p.users))
	for id, u := range p.users {
		out[id] = u
	}
	return out
}

func (p *PersistedUserStore) Create(userInput *models.UserInput) models.User {
	user := p.UserStore.Create(userInput)
	p.save("create")
	return user
}

func (p *PersistedUserStore) Update(id, name, email string) (models.User, error) {
	user, err := p.UserStore.Update(id, name, email)
	if err != nil {
		return user, err
	}
	p.save("update")
	return user, nil
}

func (p *PersistedUserStore) Delete(id string) error {
	if err := p.UserStore.Delete(id); err != nil {
		return err
	}
	p.save("delete")
	return nil
}
