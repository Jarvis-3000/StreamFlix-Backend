package main

import (
	"context"
	"log"
	"os"
	"time"

	"streamflix-backend/config"
	"streamflix-backend/controllers"
	"streamflix-backend/internal/elasticsearch"
	"streamflix-backend/internal/elasticsearch/media"
	"streamflix-backend/server"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env if present; not fatal if it's missing (e.g. in prod the env
	// is set by the platform).
	if err := godotenv.Load(); err != nil {
		log.Printf("no .env file loaded: %v", err)
	}

	initializeUpload()
	initializeDB()

	addr := os.Getenv("SERVER_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if err := server.Init(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// Ensure the upload directory exists before we start serving requests, so
// the upload handler can assume it's there. Fail fast if it can't be made.
func initializeUpload() {
	if err := os.MkdirAll(controllers.UploadDir, 0o755); err != nil {
		log.Fatalf("could not create upload dir %q: %v", controllers.UploadDir, err)
	}
}

// dbInitTimeout bounds the startup handshake with Elasticsearch, so an
// unreachable cluster fails fast instead of hanging the process.
const dbInitTimeout = 15 * time.Second

// Connect to Elasticsearch and make sure every index we depend on exists with
// the right mappings. Misconfiguration or an unreachable cluster is fatal:
// the API is useless without its datastore, so we fail at startup rather than
// on the first request.
func initializeDB() {
	cfg, err := config.LoadElasticsearch()
	if err != nil {
		log.Fatalf("elasticsearch: %v", err)
	}

	client, err := elasticsearch.Shared(cfg)
	if err != nil {
		log.Fatalf("elasticsearch: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbInitTimeout)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		log.Fatalf("elasticsearch: unreachable: %v", err)
	}

	if err := media.EnsureIndex(ctx, client, cfg.MediaIndex); err != nil {
		log.Fatalf("elasticsearch: %v", err)
	}

	log.Printf("elasticsearch: connected, index %q ready", cfg.MediaIndex)
}