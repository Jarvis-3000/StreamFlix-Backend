// Package mongodb wraps the official Go MongoDB driver with a small, reusable
// surface: a single shared connection plus the helpers our repositories build
// on. Domain-specific repositories live in subpackages (for example
// internal/mongodb/media) so new collections — creator, live_stream, analytics
// — can be added without touching this package.
package mongodb

import (
	"context"
	"fmt"
	"sync"

	"streamflix-backend/config"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Client is a thin, reusable wrapper around the official MongoDB client. It is
// safe for concurrent use and is meant to be created once at startup and shared
// across every repository.
//
// The driver pools connections internally, so one Client is all a process needs.
type Client struct {
	// Mongo is the underlying driver client. Repositories reach through it for
	// collection handles.
	Mongo *mongo.Client

	// DB is the database every repository in this process works against.
	DB *mongo.Database
}

var (
	instance *Client
	once     sync.Once
	initErr  error
)

// NewClient builds a Client from the given config. Connect is lazy — the driver
// dials in the background — so call Ping to verify connectivity. Prefer Shared
// for the application-wide singleton; this constructor exists for tests and for
// callers that need an isolated connection.
func NewClient(ctx context.Context, cfg *config.MongoDB) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mongodb: nil config")
	}
	if cfg.URI == "" {
		return nil, fmt.Errorf("mongodb: no connection uri configured")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("mongodb: connect: %w", err)
	}

	return &Client{Mongo: client, DB: client.Database(cfg.Database)}, nil
}

// Shared returns the process-wide singleton Client, creating it on first call
// from the given config. Subsequent calls ignore the arguments and return the
// same instance (or the same initialization error). This is the only global
// state in the package.
func Shared(ctx context.Context, cfg *config.MongoDB) (*Client, error) {
	once.Do(func() {
		instance, initErr = NewClient(ctx, cfg)
	})
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Ping verifies the connection is alive and the credentials are accepted. Call
// it once at startup to fail fast on misconfiguration.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.Mongo.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongodb: ping: %w", err)
	}
	return nil
}

// Collection returns a handle to the named collection in the configured
// database.
func (c *Client) Collection(name string) *mongo.Collection {
	return c.DB.Collection(name)
}

// Disconnect closes the connection pool. The server blocks for the process
// lifetime, so this is only for tests and for a graceful shutdown path.
func (c *Client) Disconnect(ctx context.Context) error {
	if err := c.Mongo.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongodb: disconnect: %w", err)
	}
	return nil
}
