package mongodb

import (
	"context"
	"fmt"
	"regexp"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Entity is what a type must provide to be stored by Repository: its own id.
// The repository writes that id into MongoDB's _id field, so it has to be able
// to ask for it rather than guessing at a field name.
type Entity interface {
	EntityID() string
}

type Repository[T Entity] struct {
	client     *Client
	collection string
}

const DefaultSearchSize = 50

func NewRepository[T Entity](client *Client, collection string) *Repository[T] {
	return &Repository[T]{client: client, collection: collection}
}

// Collection returns the name of the collection this repository reads and
// writes.
func (r *Repository[T]) Collection() string { return r.collection }

// coll returns the driver handle for this repository's collection.
func (r *Repository[T]) coll() *mongo.Collection {
	return r.client.Collection(r.collection)
}

// EnsureIndexes creates the collection's indexes if they are not already
// present. The index list is owned by the package that defines the model. Safe
// to call on every startup.
func (r *Repository[T]) EnsureIndexes(ctx context.Context, indexes []mongo.IndexModel) error {
	return EnsureIndexes(ctx, r.client, r.collection, indexes)
}

// Create inserts a new document. It fails with ErrAlreadyExists if the id is
// already taken — the unique _id index gives us create-only semantics without a
// preceding read.
func (r *Repository[T]) Create(ctx context.Context, doc T) error {
	id := doc.EntityID()
	if id == "" {
		return fmt.Errorf("%s: create: id is required", r.collection)
	}

	body, err := marshalWithID(doc, id)
	if err != nil {
		return fmt.Errorf("%s: create %q: %w", r.collection, id, err)
	}

	if _, err := r.coll().InsertOne(ctx, body); err != nil {
		if isDuplicateKey(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("%s: create %q: %w", r.collection, id, err)
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
		return fmt.Errorf("%s: update: id is required", r.collection)
	}

	body, err := marshalWithID(doc, id)
	if err != nil {
		return fmt.Errorf("%s: update %q: %w", r.collection, id, err)
	}

	// ReplaceOne without upsert touches nothing when the id is absent, so the
	// matched count is the existence check — no separate round trip.
	res, err := r.coll().ReplaceOne(ctx, bson.M{"_id": id}, body)
	if err != nil {
		return fmt.Errorf("%s: update %q: %w", r.collection, id, err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Save writes a document whether or not it already exists, replacing any
// current version. Use it when the caller does not care which happened; prefer
// Create or Update when "already exists" or "missing" is an error.
func (r *Repository[T]) Save(ctx context.Context, doc T) error {
	id := doc.EntityID()
	if id == "" {
		return fmt.Errorf("%s: save: id is required", r.collection)
	}

	body, err := marshalWithID(doc, id)
	if err != nil {
		return fmt.Errorf("%s: save %q: %w", r.collection, id, err)
	}

	opts := options.Replace().SetUpsert(true)
	if _, err := r.coll().ReplaceOne(ctx, bson.M{"_id": id}, body, opts); err != nil {
		return fmt.Errorf("%s: save %q: %w", r.collection, id, err)
	}
	return nil
}

func (r *Repository[T]) FindByID(ctx context.Context, id string) (*T, error) {
	if id == "" {
		return nil, fmt.Errorf("%s: find: id is required", r.collection)
	}

	var doc T
	err := r.coll().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if isNoDocuments(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%s: find %q: %w", r.collection, id, err)
	}
	return &doc, nil
}

func (r *Repository[T]) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%s: delete: id is required", r.collection)
	}

	res, err := r.coll().DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("%s: delete %q: %w", r.collection, id, err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository[T]) Exists(ctx context.Context, id string) (bool, error) {
	err := r.coll().FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	switch {
	case err == nil:
		return true, nil
	case isNoDocuments(err):
		return false, nil
	default:
		return false, fmt.Errorf("%s: exists %q: %w", r.collection, id, err)
	}
}

// Search runs a query against the collection and decodes the matching
// documents. The filter is a raw MongoDB query document, so each collection
// expresses its own searches — a text match on title here, an equality filter
// on email there — without the repository needing to know a thing about the
// fields involved.
//
// A nil filter matches everything. The result is capped at DefaultSearchSize.
func (r *Repository[T]) Search(ctx context.Context, filter any) ([]T, error) {
	if filter == nil {
		filter = bson.M{}
	}

	opts := options.Find().SetLimit(int64(DefaultSearchSize))
	cursor, err := r.coll().Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: search: %w", r.collection, err)
	}
	defer cursor.Close(ctx)

	docs := make([]T, 0, DefaultSearchSize)
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("%s: search: decode: %w", r.collection, err)
	}
	return docs, nil
}

// MatchQuery builds a case-insensitive substring match on a single field — the
// common case behind Search, so callers do not have to assemble the filter by
// hand for it.
//
// A regex is used rather than a $text index: $text allows only one index per
// collection and matches whole words only, while a regex covers partial words
// (the "sta" that should find "Starship") and stays per-field, which is the
// better trade for title search at this scale. It does not use an index, so it
// scans the collection — move to Atlas Search if this grows hot. An empty query
// matches everything.
func MatchQuery(field, query string) any {
	if query == "" {
		return bson.M{}
	}
	return bson.M{field: bson.M{"$regex": regexp.QuoteMeta(query), "$options": "i"}}
}

// TermQuery builds an exact-match query on a single field. Use it for ids,
// enums and tags — anything compared whole rather than searched.
func TermQuery(field, value string) any {
	return bson.M{field: value}
}

// marshalWithID converts doc to a BSON document and sets _id to the entity's
// id, so MongoDB's primary key is the same value the rest of the app addresses
// the document by. The model's own "id" field is left in place as well: callers
// decode straight into the model and expect to find it there.
func marshalWithID(doc any, id string) (bson.M, error) {
	data, err := bson.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal document: %w", err)
	}

	var body bson.M
	if err := bson.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}

	body["_id"] = id
	return body, nil
}
