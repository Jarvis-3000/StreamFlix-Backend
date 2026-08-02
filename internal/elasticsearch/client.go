// Package elasticsearch wraps the official Go Elasticsearch client with a
// small, reusable surface: a single shared connection plus the helpers our
// repositories build on. Domain-specific repositories live in subpackages
// (for example internal/elasticsearch/media) so new indices — creator,
// live_stream, analytics — can be added without touching this package.
package elasticsearch

import (
	"context"
	"fmt"
	"sync"

	"streamflix-backend/config"

	es "github.com/elastic/go-elasticsearch/v8"
)

// Client is a thin, reusable wrapper around the official Elasticsearch client.
// It is safe for concurrent use and is meant to be created once at startup and
// shared across every repository.
type Client struct {
	// ES is the underlying typed client. Repositories use it directly for
	// index and search operations.
	ES *es.TypedClient

	// Raw exposes the low-level client for the occasional API the typed
	// client does not cover (for example, index existence checks).
	Raw *es.Client
}

var (
	instance *Client
	once     sync.Once
	initErr  error
)

// NewClient builds a Client from the given config. It does not contact the
// server; call Ping to verify connectivity. Prefer Shared for the
// application-wide singleton — this constructor exists for tests and for
// callers that need an isolated connection.
func NewClient(cfg *config.Elasticsearch) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("elasticsearch: nil config")
	}

	esCfg := es.Config{
		APIKey: cfg.APIKey,
	}
	// Endpoint takes precedence over CloudID: serverless projects are reached
	// by URL, classic deployments by cloud id.
	switch {
	case cfg.Endpoint != "":
		esCfg.Addresses = []string{cfg.Endpoint}
	case cfg.CloudID != "":
		esCfg.CloudID = cfg.CloudID
	default:
		return nil, fmt.Errorf("elasticsearch: neither endpoint nor cloud id configured")
	}

	typed, err := es.NewTypedClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch: create typed client: %w", err)
	}
	raw, err := es.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch: create client: %w", err)
	}

	return &Client{ES: typed, Raw: raw}, nil
}

// Shared returns the process-wide singleton Client, creating it on first call
// from the given config. Subsequent calls ignore the argument and return the
// same instance (or the same initialization error). This is the only global
// state in the package.
func Shared(cfg *config.Elasticsearch) (*Client, error) {
	once.Do(func() {
		instance, initErr = NewClient(cfg)
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Ping verifies the connection is alive and the credentials are accepted. Call
// it once at startup to fail fast on misconfiguration.
func (c *Client) Ping(ctx context.Context) error {
	ok, err := c.Raw.Ping(c.Raw.Ping.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("elasticsearch: ping: %w", err)
	}
	defer ok.Body.Close()

	if ok.IsError() {
		return fmt.Errorf("elasticsearch: ping returned status %s", ok.Status())
	}
	return nil
}
