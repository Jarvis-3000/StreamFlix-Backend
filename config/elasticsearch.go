// Package config loads and validates application configuration from the
// environment. Each subsystem gets its own typed config struct so callers
// depend on parsed values rather than reaching into os.Getenv directly.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Elasticsearch holds the connection settings for Elasticsearch Cloud.
//
// Connection precedence is Endpoint first, then CloudID: serverless Elastic
// Cloud projects are addressed by their raw endpoint URL, while classic
// deployments use the base64 CloudID. Supplying either one (plus an API key)
// is enough to connect.
type Elasticsearch struct {
	// Endpoint is the full https URL of the Elasticsearch project. Preferred
	// for serverless Elastic Cloud. Optional if CloudID is set.
	Endpoint string

	// CloudID is the base64 cloud identifier for classic Elastic Cloud
	// deployments. Optional if Endpoint is set.
	CloudID string

	// APIKey authenticates every request. Required.
	APIKey string

	// MediaIndex is the name of the index that stores media metadata.
	MediaIndex string
}

// LoadElasticsearch reads Elasticsearch settings from the environment and
// validates them. It returns an error listing every missing required value so
// misconfiguration surfaces at startup rather than on the first request.
//
// Environment variables:
//
//	ELASTICSEARCH_ENDPOINT    full https URL (preferred; serverless)
//	ELASTICSEARCH_CLOUD_ID    base64 cloud id (classic deployments)
//	ELASTICSEARCH_API_KEY     required
//	ELASTICSEARCH_INDEX_MEDIA index name (defaults to "media")
func LoadElasticsearch() (*Elasticsearch, error) {
	cfg := &Elasticsearch{
		Endpoint:   strings.TrimSpace(os.Getenv("ELASTICSEARCH_ENDPOINT")),
		CloudID:    strings.TrimSpace(os.Getenv("ELASTICSEARCH_CLOUD_ID")),
		APIKey:     strings.TrimSpace(os.Getenv("ELASTICSEARCH_API_KEY")),
		MediaIndex: strings.TrimSpace(os.Getenv("ELASTICSEARCH_INDEX_MEDIA")),
	}

	if cfg.MediaIndex == "" {
		cfg.MediaIndex = "media"
	}

	var missing []string
	if cfg.Endpoint == "" && cfg.CloudID == "" {
		missing = append(missing, "ELASTICSEARCH_ENDPOINT or ELASTICSEARCH_CLOUD_ID")
	}
	if cfg.APIKey == "" {
		missing = append(missing, "ELASTICSEARCH_API_KEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("elasticsearch config: missing required env vars: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}
