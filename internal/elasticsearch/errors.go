package elasticsearch

import "errors"

// Sentinel errors shared across repositories. Callers use errors.Is to branch
// on them (for example, returning 404 when a document is not found) without
// depending on the underlying Elasticsearch response shape.
var (
	// ErrNotFound is returned when a document with the requested id does not
	// exist.
	ErrNotFound = errors.New("elasticsearch: document not found")

	// ErrAlreadyExists is returned by create operations when a document with
	// the same id already exists.
	ErrAlreadyExists = errors.New("elasticsearch: document already exists")
)
