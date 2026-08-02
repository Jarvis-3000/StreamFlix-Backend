package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	esclient "streamflix-backend/internal/elasticsearch"
)

// indexMapping is the explicit mapping for the media index. Dynamic mapping is
// disabled ("dynamic": "strict") so any field not declared here is rejected at
// index time — this keeps the schema honest and forces mapping changes to be
// deliberate.
//
// Field type choices:
//   - keyword  : exact-match / filter / aggregate (ids, urls, enums, tags)
//   - text     : full-text search (title, description)
//   - date     : timestamps
//   - integer  : duration in seconds
//
// title also carries a keyword sub-field (title.keyword) for exact-match and
// sorting alongside full-text search.
//
// The document is left open for the future fields listed in Document's doc
// comment; add them here (and only here) when they are introduced.
const indexMapping = `{
  "mappings": {
    "dynamic": "strict",
    "properties": {
      "id":                  { "type": "keyword" },
      "creator_id":          { "type": "keyword" },
      "title":               { "type": "text", "fields": { "keyword": { "type": "keyword", "ignore_above": 256 } } },
      "description":         { "type": "text" },
      "visibility":          { "type": "keyword" },
      "status":              { "type": "keyword" },
      "thumbnail":       	 { "type": "keyword", "index": false },
      "url": 				 { "type": "keyword", "index": false },
      "duration":            { "type": "integer" },
      "qualities":           { "type": "keyword" },
      "tags":                { "type": "keyword" },
      "created_at":          { "type": "date" },
      "updated_at":          { "type": "date" }
    }
  }
}`

// EnsureIndex creates the media index with explicit mappings if it does not
// already exist. It is idempotent and safe to call on every startup.
func EnsureIndex(ctx context.Context, client *esclient.Client, index string) error {
	exists, err := indexExists(ctx, client, index)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	res, err := client.Raw.Indices.Create(
		index,
		client.Raw.Indices.Create.WithContext(ctx),
		client.Raw.Indices.Create.WithBody(bytes.NewReader([]byte(indexMapping))),
	)
	if err != nil {
		return fmt.Errorf("media: create index %q: %w", index, err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// A concurrent startup may have created it between our check and now;
		// treat "already exists" as success.
		if res.StatusCode == http.StatusBadRequest && bodyMentions(res.Body, "resource_already_exists_exception") {
			return nil
		}
		return fmt.Errorf("media: create index %q: %s", index, res.String())
	}
	return nil
}

// indexExists reports whether the given index is present.
func indexExists(ctx context.Context, client *esclient.Client, index string) (bool, error) {
	res, err := client.Raw.Indices.Exists(
		[]string{index},
		client.Raw.Indices.Exists.WithContext(ctx),
	)
	if err != nil {
		return false, fmt.Errorf("media: check index %q: %w", index, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("media: check index %q: unexpected status %s", index, res.Status())
	}
}

// bodyMentions reports whether the response body contains the given marker
// string. Used to recognise Elasticsearch error types (e.g.
// "resource_already_exists_exception") without unmarshalling the full error
// envelope.
func bodyMentions(body io.Reader, marker string) bool {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(body); err != nil {
		return false
	}
	return bytes.Contains(buf.Bytes(), []byte(marker))
}
