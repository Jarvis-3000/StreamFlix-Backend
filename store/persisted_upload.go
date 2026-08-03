package store

import (
	"log"

	"streamflix-backend/models"
	"streamflix-backend/persistence"
)

// uploadDataFile is where the uploads map is persisted as JSON.
const uploadDataFile = "data/uploads.json"

// PersistedUploadStore wraps the in-memory UploadStore and persists it to disk,
// exactly as PersistedUserStore does for users. Uploads must outlive a restart:
// a user can upload a video, close the tab, and come back to fill in the
// metadata later — an in-memory-only registry would have forgotten the video id
// by then and rejected a perfectly valid media.create.
type PersistedUploadStore struct {
	*UploadStore
	file *persistence.JSONFile
}

// NewPersistedUploadStore builds the wrapper and loads previously persisted
// uploads from disk.
func NewPersistedUploadStore() *PersistedUploadStore {
	file, err := persistence.NewJSONFile(uploadDataFile)
	if err != nil {
		log.Fatalf("upload store: %v", err)
	}

	p := &PersistedUploadStore{
		UploadStore: NewUploadStore(),
		file:        file,
	}

	if err := file.Load(&p.uploads); err != nil {
		log.Fatalf("upload store: load: %v", err)
	}

	return p
}

// PersistedUploads is the durable upload registry the rest of the app uses.
var PersistedUploads = NewPersistedUploadStore()

// save writes the current uploads map to disk, logging (not returning) any
// error so a failed write never breaks the API response.
func (p *PersistedUploadStore) save(op string) {
	if err := p.file.Save(p.snapshot()); err != nil {
		log.Printf("upload store: save after %s: %v", op, err)
	}
}

// snapshot copies the uploads map under the read lock so saving doesn't race
// with concurrent reads/writes on the underlying store.
func (p *PersistedUploadStore) snapshot() map[string]models.Upload {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make(map[string]models.Upload, len(p.uploads))
	for id, u := range p.uploads {
		out[id] = u
	}
	return out
}

func (p *PersistedUploadStore) Register(videoID, filename, path string, size int64) models.Upload {
	upload := p.UploadStore.Register(videoID, filename, path, size)
	p.save("register")
	return upload
}

func (p *PersistedUploadStore) Delete(videoID string) error {
	if err := p.UploadStore.Delete(videoID); err != nil {
		return err
	}
	p.save("delete")
	return nil
}
