// Package media owns the Elasticsearch index that stores media metadata: its
// mapping, and the searches that are specific to it. The model itself is
// models.Media and the storage mechanics are the generic
// elasticsearch.Repository — this package is only what is true of the media
// index in particular.
//
// A second index (users, say) is the same three pieces: a model with an
// EntityID method, a mapping, and a small package like this one.
package media

import (
	"context"

	esclient "streamflix-backend/internal/elasticsearch"
	"streamflix-backend/models"
)

// Repository stores media documents. It is the generic repository specialised
// to models.Media, plus the queries that only make sense for this index.
type Repository struct {
	*esclient.Repository[models.Media]
}

// NewRepository builds a media repository backed by the given client and index.
// It performs no I/O; call EnsureIndex to create the index.
func NewRepository(client *esclient.Client, index string) *Repository {
	return &Repository{Repository: esclient.NewRepository[models.Media](client, index)}
}

// EnsureIndex creates the media index with its mapping if it does not already
// exist. Safe to call on every startup.
func (r *Repository) EnsureIndex(ctx context.Context) error {
	return r.Repository.EnsureIndex(ctx, indexMapping)
}

// SearchByTitle returns media whose title matches the query via full-text
// search, ordered by relevance. An empty slice means no matches.
func (r *Repository) SearchByTitle(ctx context.Context, title string) ([]models.Media, error) {
	return r.Search(ctx, esclient.MatchQuery("title", title))
}

// FindByCreator returns the media belonging to one creator. creator_id is a
// keyword field, so this is an exact term match rather than a text search.
func (r *Repository) FindByCreator(ctx context.Context, creatorID string) ([]models.Media, error) {
	return r.Search(ctx, esclient.TermQuery("creator_id", creatorID))
}

// EnsureIndex creates the media index with explicit mappings if it does not
// already exist, without needing a repository. main calls this at startup.
func EnsureIndex(ctx context.Context, client *esclient.Client, index string) error {
	return esclient.EnsureIndex(ctx, client, index, indexMapping)
}
