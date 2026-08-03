package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/optype"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/refresh"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/result"
)

// Entity is what a type must provide to be stored by Repository: its own id.
// Elasticsearch addresses every document by an id that lives outside the
// document body, so the repository has to be able to ask for it rather than
// guessing at a field name.
type Entity interface {
	EntityID() string
}

type Repository[T Entity] struct {
	client *Client
	index  string
}

const DefaultSearchSize = 50

func NewRepository[T Entity](client *Client, index string) *Repository[T] {
	return &Repository[T]{client: client, index: index}
}

// Index returns the name of the index this repository reads and writes.
func (r *Repository[T]) Index() string { return r.index }

// EnsureIndex creates the index with the given mapping if it does not already
// exist. The mapping is the raw JSON body of a create-index request, owned by
// the package that defines the model. Safe to call on every startup.
func (r *Repository[T]) EnsureIndex(ctx context.Context, mapping string) error {
	return EnsureIndex(ctx, r.client, r.index, mapping)
}

func (r *Repository[T]) Create(ctx context.Context, doc T) error {
	id := doc.EntityID()
	if id == "" {
		return fmt.Errorf("%s: create: id is required", r.index)
	}

	// op_type=create makes Elasticsearch reject the write if the id already
	// exists, giving us create-only semantics.
	res, err := r.client.ES.Index(r.index).
		Id(id).
		Request(doc).
		OpType(optype.Create).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		if isVersionConflict(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("%s: create %q: %w", r.index, id, err)
	}
	if res.Result == result.Notfound {
		return fmt.Errorf("%s: create %q: unexpected result %q", r.index, id, res.Result)
	}
	return nil
}

// Update replaces an existing document wholesale. It fails with ErrNotFound if
// the id does not exist.
//
// This is a read-modify-write from the caller's side: fetch with FindByID,
// change the fields you care about, then pass the whole document back.
func (r *Repository[T]) Update(ctx context.Context, doc T) error {
	id := doc.EntityID()
	if id == "" {
		return fmt.Errorf("%s: update: id is required", r.index)
	}

	exists, err := r.Exists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	// A full re-index of the id replaces the stored document.
	_, err = r.client.ES.Index(r.index).
		Id(id).
		Request(doc).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("%s: update %q: %w", r.index, id, err)
	}
	return nil
}

// Save indexes a document whether or not it already exists, replacing any
// current version. Use it when the caller does not care which happened;
// prefer Create or Update when "already exists" or "missing" is an error.
func (r *Repository[T]) Save(ctx context.Context, doc T) error {
	id := doc.EntityID()
	if id == "" {
		return fmt.Errorf("%s: save: id is required", r.index)
	}

	_, err := r.client.ES.Index(r.index).
		Id(id).
		Request(doc).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("%s: save %q: %w", r.index, id, err)
	}
	return nil
}

func (r *Repository[T]) FindByID(ctx context.Context, id string) (*T, error) {
	if id == "" {
		return nil, fmt.Errorf("%s: find: id is required", r.index)
	}

	res, err := r.client.ES.Get(r.index, id).Do(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%s: find %q: %w", r.index, id, err)
	}
	if !res.Found {
		return nil, ErrNotFound
	}

	var doc T
	if err := json.Unmarshal(res.Source_, &doc); err != nil {
		return nil, fmt.Errorf("%s: find %q: decode source: %w", r.index, id, err)
	}
	return &doc, nil
}

func (r *Repository[T]) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%s: delete: id is required", r.index)
	}

	res, err := r.client.ES.Delete(r.index, id).
		Refresh(refresh.True).
		Do(ctx)
	if err != nil {
		if isNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("%s: delete %q: %w", r.index, id, err)
	}
	if res.Result == result.Notfound {
		return ErrNotFound
	}
	return nil
}

func (r *Repository[T]) Exists(ctx context.Context, id string) (bool, error) {
	ok, err := r.client.ES.Exists(r.index, id).Do(ctx)
	if err != nil {
		return false, fmt.Errorf("%s: exists %q: %w", r.index, id, err)
	}
	return ok, nil
}

// Search runs an Elasticsearch query against the index and decodes the hits.
// The query is the raw typed-client DSL, so each index expresses its own
// searches — a match on title here, a term filter on email there — without the
// repository needing to know a thing about the fields involved.
//
// A nil query matches everything. Size defaults to DefaultSearchSize unless the
// query sets its own.
func (r *Repository[T]) Search(ctx context.Context, query *types.Query) ([]T, error) {
	req := &search.Request{Query: query}
	if req.Size == nil {
		size := DefaultSearchSize
		req.Size = &size
	}

	res, err := r.client.ES.Search().
		Index(r.index).
		Request(req).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: search: %w", r.index, err)
	}

	docs := make([]T, 0, len(res.Hits.Hits))
	for _, hit := range res.Hits.Hits {
		var doc T
		if err := json.Unmarshal(hit.Source_, &doc); err != nil {
			return nil, fmt.Errorf("%s: search: decode hit: %w", r.index, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// MatchQuery builds a full-text match query on a single field — the common case
// behind Search, so callers do not have to assemble the DSL by hand for it.
func MatchQuery(field, query string) *types.Query {
	return &types.Query{
		Match: map[string]types.MatchQuery{
			field: {Query: query},
		},
	}
}

// TermQuery builds an exact-match query on a single keyword field. Use it for
// ids, enums and tags — anything mapped as "keyword" rather than "text".
func TermQuery(field, value string) *types.Query {
	return &types.Query{
		Term: map[string]types.TermQuery{
			field: {Value: value},
		},
	}
}
