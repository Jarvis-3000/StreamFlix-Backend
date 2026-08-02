package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	esclient "streamflix-backend/internal/elasticsearch"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/optype"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/refresh"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/result"
)

// Repository is the persistence API for media metadata. Callers depend on this
// interface, not the concrete Elasticsearch implementation, so the backing
// store can be swapped or mocked in tests.
type Repository interface {
	// Create indexes a new media document. It fails with
	// elasticsearch.ErrAlreadyExists if a document with the same id exists.
	Create(ctx context.Context, media *Document) error

	// Update replaces an existing media document. It fails with
	// elasticsearch.ErrNotFound if the id does not exist.
	Update(ctx context.Context, media *Document) error

	// PartialUpdate applies only the fields that are set on media — every
	// field but the id, which addresses the document — to an existing media
	// document, leaving the rest untouched. Prefer it over Update whenever the
	// caller holds a change rather than a whole record: it needs no prior read
	// and cannot clobber fields it never looked at.
	//
	// A zero value reads as "not set", so this cannot clear a field; pass a
	// full document to Update to blank one out. It fails with
	// elasticsearch.ErrNotFound if the id does not exist.
	PartialUpdate(ctx context.Context, media *Document) error

	// FindByID returns the media document with the given id, or
	// elasticsearch.ErrNotFound if none exists.
	FindByID(ctx context.Context, id string) (*Document, error)

	// Delete removes the media document with the given id. It fails with
	// elasticsearch.ErrNotFound if the id does not exist.
	Delete(ctx context.Context, id string) error

	// SearchByTitle returns media whose title matches the query via full-text
	// search, ordered by relevance. An empty slice means no matches.
	SearchByTitle(ctx context.Context, title string) ([]Document, error)

	// EnsureIndex creates the backing index with explicit mappings if it does
	// not already exist. Safe to call at startup.
	EnsureIndex(ctx context.Context) error
}

// searchSize caps how many documents SearchByTitle returns per call.
const searchSize = 50

// repository is the Elasticsearch-backed Repository implementation.
type repository struct {
	client *esclient.Client
	index  string
}

// NewRepository builds a media Repository backed by the given client and
// index. It performs no I/O; call EnsureIndex to create the index.
func NewRepository(client *esclient.Client, index string) Repository {
	return &repository{client: client, index: index}
}

// EnsureIndex implements Repository.
func (r *repository) EnsureIndex(ctx context.Context) error {
	return EnsureIndex(ctx, r.client, r.index)
}

// Create implements Repository.
func (r *repository) Create(ctx context.Context, media *Document) error {
	if media == nil || media.ID == "" {
		return fmt.Errorf("media: create: document and id are required")
	}

	// op_type=create makes Elasticsearch reject the write if the id already
	// exists, giving us create-only semantics.
	res, err := r.client.ES.Index(r.index).
		Id(media.ID).
		Request(media).
		OpType(optype.Create).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		if isVersionConflict(err) {
			return esclient.ErrAlreadyExists
		}
		return fmt.Errorf("media: create %q: %w", media.ID, err)
	}
	if res.Result == result.Notfound {
		return fmt.Errorf("media: create %q: unexpected result %q", media.ID, res.Result)
	}
	return nil
}

// Update implements Repository.
func (r *repository) Update(ctx context.Context, media *Document) error {
	if media == nil || media.ID == "" {
		return fmt.Errorf("media: update: document and id are required")
	}

	exists, err := r.exists(ctx, media.ID)
	if err != nil {
		return err
	}
	if !exists {
		return esclient.ErrNotFound
	}

	// A full re-index of the id replaces the stored document.
	_, err = r.client.ES.Index(r.index).
		Id(media.ID).
		Request(media).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("media: update %q: %w", media.ID, err)
	}
	return nil
}

// PartialUpdate implements Repository.
func (r *repository) PartialUpdate(ctx context.Context, media *Document) error {
	if media == nil || media.ID == "" {
		return fmt.Errorf("media: partial update: document and id are required")
	}

	doc, err := json.Marshal(setFields(media))
	if err != nil {
		return fmt.Errorf("media: partial update %q: encode fields: %w", media.ID, err)
	}

	// The _update API merges doc into the stored source server-side, so unlike
	// Update this is a single round trip with no read-modify-write window for
	// a concurrent writer to slip into.
	_, err = r.client.ES.Update(r.index, media.ID).
		Request(&update.Request{Doc: doc}).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		// Elasticsearch answers 404 for a missing id here, as Get does.
		if isNotFound(err) {
			return esclient.ErrNotFound
		}
		return fmt.Errorf("media: partial update %q: %w", media.ID, err)
	}
	return nil
}

// setFields collects the fields of media that carry a value into the body of
// an _update request, keyed by their mapped names. The id is left out: it
// addresses the document rather than living in its body, and is the one field
// a partial update may not change.
//
// updated_at is always written, stamped with the current time unless the
// caller pinned one, so a partial write cannot leave the timestamp stale.
func setFields(media *Document) map[string]any {
	fields := make(map[string]any, 12)

	if media.CreatorID != "" {
		fields["creator_id"] = media.CreatorID
	}
	if media.Title != "" {
		fields["title"] = media.Title
	}
	if media.Description != "" {
		fields["description"] = media.Description
	}
	if media.Visibility != "" {
		fields["visibility"] = media.Visibility
	}
	if media.Status != "" {
		fields["status"] = media.Status
	}
	if media.Thumbnail != "" {
		fields["thumbnail"] = media.Thumbnail
	}
	if media.URL != "" {
		fields["url"] = media.URL
	}
	if media.Duration != 0 {
		fields["duration"] = media.Duration
	}
	if len(media.Qualities) > 0 {
		fields["qualities"] = media.Qualities
	}
	if len(media.Tags) > 0 {
		fields["tags"] = media.Tags
	}
	if !media.CreatedAt.IsZero() {
		fields["created_at"] = media.CreatedAt
	}

	if media.UpdatedAt.IsZero() {
		fields["updated_at"] = time.Now().UTC()
	} else {
		fields["updated_at"] = media.UpdatedAt
	}
	return fields
}

// FindByID implements Repository.
func (r *repository) FindByID(ctx context.Context, id string) (*Document, error) {
	if id == "" {
		return nil, fmt.Errorf("media: find: id is required")
	}

	res, err := r.client.ES.Get(r.index, id).Do(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, esclient.ErrNotFound
		}
		return nil, fmt.Errorf("media: find %q: %w", id, err)
	}
	if !res.Found {
		return nil, esclient.ErrNotFound
	}

	var doc Document
	if err := json.Unmarshal(res.Source_, &doc); err != nil {
		return nil, fmt.Errorf("media: find %q: decode source: %w", id, err)
	}
	return &doc, nil
}

// Delete implements Repository.
func (r *repository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("media: delete: id is required")
	}

	res, err := r.client.ES.Delete(r.index, id).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		if isNotFound(err) {
			return esclient.ErrNotFound
		}
		return fmt.Errorf("media: delete %q: %w", id, err)
	}
	if res.Result == result.Notfound {
		return esclient.ErrNotFound
	}
	return nil
}

// SearchByTitle implements Repository.
func (r *repository) SearchByTitle(ctx context.Context, title string) ([]Document, error) {
	res, err := r.client.ES.Search().
		Index(r.index).
		Request(&search.Request{
			Size: intPtr(searchSize),
			Query: &types.Query{
				Match: map[string]types.MatchQuery{
					"title": {Query: title},
				},
			},
		}).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("media: search title %q: %w", title, err)
	}

	docs := make([]Document, 0, len(res.Hits.Hits))
	for _, hit := range res.Hits.Hits {
		var doc Document
		if err := json.Unmarshal(hit.Source_, &doc); err != nil {
			return nil, fmt.Errorf("media: search title %q: decode hit: %w", title, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// exists reports whether a document with the given id is present.
func (r *repository) exists(ctx context.Context, id string) (bool, error) {
	ok, err := r.client.ES.Exists(r.index, id).Do(ctx)
	if err != nil {
		return false, fmt.Errorf("media: exists %q: %w", id, err)
	}
	return ok, nil
}

// intPtr returns a pointer to i, for the pointer-typed request fields.
func intPtr(i int) *int { return &i }

// isNotFound reports whether err is an Elasticsearch 404.
func isNotFound(err error) bool {
	return statusCode(err) == 404
}

// isVersionConflict reports whether err is an Elasticsearch 409, which
// op_type=create returns when the id already exists.
func isVersionConflict(err error) bool {
	return statusCode(err) == 409
}

// statusCode extracts the HTTP status from a typed-client error, or 0 if the
// error is not an Elasticsearch response error.
func statusCode(err error) int {
	var esErr *types.ElasticsearchError
	if errors.As(err, &esErr) && esErr.Status != 0 {
		return esErr.Status
	}
	return 0
}
