package store

import (
	"fmt"
	"sync"

	"streamflix-backend/internal/elasticsearch/media"
)

// mediaRepo is the process-wide media repository. Unlike the user and upload
// stores it cannot be built at package-init time: it needs a live
// Elasticsearch client, which only exists after config is loaded. main wires it
// up via SetMediaRepository during startup.
var (
	mediaRepoMu sync.RWMutex
	mediaRepo   *media.Repository
)

// SetMediaRepository installs the repository controllers will use. Call it once
// at startup, before the server starts serving.
func SetMediaRepository(repo *media.Repository) {
	mediaRepoMu.Lock()
	defer mediaRepoMu.Unlock()
	mediaRepo = repo
}

// MediaRepository returns the installed media repository, or an error if
// startup never installed one. Returning an error rather than a nil interface
// keeps a misconfigured process from panicking on the first request.
func MediaRepository() (*media.Repository, error) {
	mediaRepoMu.RLock()
	defer mediaRepoMu.RUnlock()

	if mediaRepo == nil {
		return nil, fmt.Errorf("media repository is not configured")
	}
	return mediaRepo, nil
}
