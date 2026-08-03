package elasticsearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

// EnsureIndex creates index with the given mapping if it does not already
// exist. The mapping is the raw JSON body of a create-index request, owned by
// whichever package defines the model — this function only knows how to send
// it. It is idempotent and safe to call on every startup.
func EnsureIndex(ctx context.Context, client *Client, index, mapping string) error {
	exists, err := IndexExists(ctx, client, index)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	res, err := client.Raw.Indices.Create(
		index,
		client.Raw.Indices.Create.WithContext(ctx),
		client.Raw.Indices.Create.WithBody(bytes.NewReader([]byte(mapping))),
	)
	if err != nil {
		return fmt.Errorf("create index %q: %w", index, err)
	}
	defer res.Body.Close()

	if res.IsError() {
		// A concurrent startup may have created it between our check and now;
		// treat "already exists" as success.
		if res.StatusCode == http.StatusBadRequest && bodyMentions(res.Body, "resource_already_exists_exception") {
			return nil
		}
		return fmt.Errorf("create index %q: %s", index, res.String())
	}
	return nil
}

// IndexExists reports whether the given index is present.
func IndexExists(ctx context.Context, client *Client, index string) (bool, error) {
	res, err := client.Raw.Indices.Exists(
		[]string{index},
		client.Raw.Indices.Exists.WithContext(ctx),
	)
	if err != nil {
		return false, fmt.Errorf("check index %q: %w", index, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("check index %q: unexpected status %s", index, res.Status())
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
