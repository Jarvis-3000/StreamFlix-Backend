package store

import (
	"fmt"
	"sync"
	"time"

	"streamflix-backend/models"
)

// UploadStore is the in-memory registry of uploads received by /upload, keyed
// by video id. It is the source of truth for "does this video id exist?" — the
// question media.create has to answer before it will build a document.
type UploadStore struct {
	mu      sync.RWMutex
	uploads map[string]models.Upload
}

func NewUploadStore() *UploadStore {
	return &UploadStore{uploads: make(map[string]models.Upload)}
}

// Register records a freshly received upload under videoID.
func (s *UploadStore) Register(videoID, filename, path string, size int64) models.Upload {
	s.mu.Lock()
	defer s.mu.Unlock()

	upload := models.Upload{
		VideoID:   videoID,
		Filename:  filename,
		Path:      path,
		Size:      size,
		CreatedAt: time.Now(),
	}

	s.uploads[videoID] = upload
	return upload
}

// Get returns the upload for videoID.
func (s *UploadStore) Get(videoID string) (models.Upload, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	upload, exist := s.uploads[videoID]
	return upload, exist
}

// Delete removes the upload record for videoID.
func (s *UploadStore) Delete(videoID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exist := s.uploads[videoID]; !exist {
		return fmt.Errorf("upload not found")
	}

	delete(s.uploads, videoID)
	return nil
}
