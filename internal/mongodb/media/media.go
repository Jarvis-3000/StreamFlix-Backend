// Package media owns the MongoDB collection that stores media metadata: its
// indexes, and the queries that are specific to it. The model itself is
// models.Media and the storage mechanics are the generic mongodb.Repository —
// this package is only what is true of the media collection in particular.
//
// A second collection (users, say) is the same three pieces: a model with an
// EntityID method, an index set, and a small package like this one.
package media

import (
	"context"

	mongoclient "streamflix-backend/internal/mongodb"
	"streamflix-backend/models"
)

// Repository stores media documents. It is the generic repository specialised
// to models.Media, plus the queries that only make sense for this collection.
type Repository struct {
	*mongoclient.Repository[models.Media]
}

// NewRepository builds a media repository backed by the given client and
// collection. It performs no I/O; call EnsureIndexes to create the indexes.
func NewRepository(client *mongoclient.Client, collection string) *Repository {
	return &Repository{Repository: mongoclient.NewRepository[models.Media](client, collection)}
}

// EnsureIndexes creates the media collection's indexes if they do not already
// exist. Safe to call on every startup.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	return r.Repository.EnsureIndexes(ctx, indexes)
}

// SearchByTitle returns media whose title matches the query, as a
// case-insensitive substring. An empty slice means no matches.
func (r *Repository) SearchByTitle(ctx context.Context, title string) ([]models.Media, error) {
	return r.Search(ctx, mongoclient.MatchQuery("title", title))
}

// FindByCreator returns the media belonging to one creator. creator_id is an
// opaque id, so this is an exact match rather than a text search.
func (r *Repository) FindByCreator(ctx context.Context, creatorID string) ([]models.Media, error) {
	return r.Search(ctx, mongoclient.TermQuery("creator_id", creatorID))
}

// EnsureIndexes creates the media collection's indexes without needing a
// repository. main calls this at startup.
func EnsureIndexes(ctx context.Context, client *mongoclient.Client, collection string) error {
	return mongoclient.EnsureIndexes(ctx, client, collection, indexes)
}
