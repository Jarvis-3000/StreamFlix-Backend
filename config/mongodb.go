// Package config loads and validates application configuration from the
// environment. Each subsystem gets its own typed config struct so callers
// depend on parsed values rather than reaching into os.Getenv directly.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// MongoDB holds the connection settings for MongoDB Atlas.
//
// The Atlas connection string already carries the credentials, so URI alone is
// enough to connect. Username and Password are accepted separately as well, for
// deployments that keep the secret out of the URI: when both are present they
// are injected into the URI's userinfo, replacing whatever it carried.
type MongoDB struct {
	// URI is the full mongodb+srv:// connection string. Required.
	URI string

	// Username and Password are optional. When both are set they override the
	// credentials embedded in URI.
	Username string
	Password string

	// Database is the name of the database holding our collections.
	Database string

	// MediaCollection is the collection that stores media metadata.
	MediaCollection string
}

// LoadMongoDB reads MongoDB settings from the environment and validates them.
// It returns an error listing every missing required value so misconfiguration
// surfaces at startup rather than on the first request.
//
// Environment variables:
//
//	MONGODB_ATLAS_URL         full mongodb+srv connection string (required)
//	MONGODB_ATLAS_USERNAME    optional; overrides the user in the URI
//	MONGODB_ATLAS_PASSWORD    optional; overrides the password in the URI
//	MONGODB_DATABASE          database name (defaults to "streamflix")
//	MONGODB_COLLECTION_MEDIA  collection name (defaults to "media")
func LoadMongoDB() (*MongoDB, error) {
	cfg := &MongoDB{
		URI:             strings.TrimSpace(os.Getenv("MONGODB_ATLAS_URL")),
		Username:        strings.TrimSpace(os.Getenv("MONGODB_ATLAS_USERNAME")),
		Password:        strings.TrimSpace(os.Getenv("MONGODB_ATLAS_PASSWORD")),
		Database:        strings.TrimSpace(os.Getenv("MONGODB_DATABASE")),
		MediaCollection: strings.TrimSpace(os.Getenv("MONGODB_COLLECTION_MEDIA")),
	}

	if cfg.Database == "" {
		cfg.Database = "streamflix"
	}
	if cfg.MediaCollection == "" {
		cfg.MediaCollection = "media"
	}

	if cfg.URI == "" {
		return nil, fmt.Errorf("mongodb config: missing required env vars: MONGODB_ATLAS_URL")
	}

	uri, err := applyCredentials(cfg.URI, cfg.Username, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("mongodb config: %w", err)
	}
	cfg.URI = uri

	return cfg, nil
}

// applyCredentials returns rawURI with user and password set as its userinfo.
// It is a no-op unless both are supplied, so a URI that already embeds its
// credentials is left exactly as written.
func applyCredentials(rawURI, user, password string) (string, error) {
	if user == "" || password == "" {
		return rawURI, nil
	}

	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("parse MONGODB_ATLAS_URL: %w", err)
	}
	// url.UserPassword escapes both halves, so a password with reserved
	// characters (@ / : are all legal in Atlas passwords) survives the round
	// trip instead of corrupting the host.
	parsed.User = url.UserPassword(user, password)
	return parsed.String(), nil
}
