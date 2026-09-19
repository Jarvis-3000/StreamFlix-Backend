package main

import (
	"context"
	"log"
	"os"
	"time"

	"streamflix-backend/config"
	"streamflix-backend/controllers"
	"streamflix-backend/internal/mongodb"
	"streamflix-backend/internal/mongodb/media"
	"streamflix-backend/server"
	"streamflix-backend/services"
	"streamflix-backend/store"

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

	// Start reclaiming local disk. This runs after initializeDB because the
	// sweeper needs the media repository to tell a finished video from one that
	// is still transcoding.
	initializeCleanup()

	// Railway and Render inject the port to listen on as PORT and route
	// external traffic to it. Honour that first, then an explicit SERVER_ADDR
	// for local overrides, then fall back to the development default.
	addr := os.Getenv("SERVER_ADDR")
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
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

// Start the background sweep that deletes local files once a video is ready in
// the bucket. Tied to the process lifetime: the server blocks until exit, so
// there is nothing to cancel it from, and each pass is short and idempotent.
func initializeCleanup() {
	services.StartCleanup(context.Background())
	log.Printf("cleanup: sweeping every %s", services.CleanupInterval)
}

// dbInitTimeout bounds the startup handshake with MongoDB, so an unreachable
// cluster fails fast instead of hanging the process.
const dbInitTimeout = 15 * time.Second

// Connect to MongoDB Atlas and make sure every collection we depend on has its
// indexes. Misconfiguration or an unreachable cluster is fatal: the API is
// useless without its datastore, so we fail at startup rather than on the first
// request.
func initializeDB() {
	cfg, err := config.LoadMongoDB()
	if err != nil {
		log.Fatalf("mongodb: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbInitTimeout)
	defer cancel()

	client, err := mongodb.Shared(ctx, cfg)
	if err != nil {
		log.Fatalf("mongodb: %v", err)
	}

	if err := client.Ping(ctx); err != nil {
		log.Fatalf("mongodb: unreachable: %v", err)
	}

	if err := media.EnsureIndexes(ctx, client, cfg.MediaCollection); err != nil {
		log.Fatalf("mongodb: %v", err)
	}

	// Hand the repository to the controllers before we start serving, so the
	// first media request finds it already wired up.
	store.SetMediaRepository(media.NewRepository(client, cfg.MediaCollection))

	log.Printf("mongodb: connected to %q, collection %q ready", cfg.Database, cfg.MediaCollection)
}
