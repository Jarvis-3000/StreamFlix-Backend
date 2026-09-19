package mongodb

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// EnsureIndexes creates the given indexes on a collection if they are not
// already present. MongoDB creates a collection on first write and needs no
// schema declared up front, so the only startup work is making sure the indexes
// that back our queries exist.
//
// CreateMany is idempotent for an index whose keys and options are unchanged,
// so this is safe to call on every startup.
func EnsureIndexes(ctx context.Context, client *Client, collection string, indexes []mongo.IndexModel) error {
	if len(indexes) == 0 {
		return nil
	}

	_, err := client.Collection(collection).Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("create indexes on %q: %w", collection, err)
	}
	return nil
}

// isDuplicateKey reports whether err is a MongoDB duplicate-key error (code
// 11000), which an insert returns when the _id is already taken.
func isDuplicateKey(err error) bool {
	return mongo.IsDuplicateKeyError(err)
}

// isNoDocuments reports whether err is the driver's "no documents in result"
// error, returned by FindOne when nothing matched.
func isNoDocuments(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}
