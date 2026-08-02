package media_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"streamflix-backend/config"
	esclient "streamflix-backend/internal/elasticsearch"
	"streamflix-backend/internal/elasticsearch/media"
)

// Example_initialization shows how to wire the media repository at startup.
// This is the code that belongs in (or is called from) your main.go — it does
// not replace your existing server setup.
func Example_initialization() {
	ctx := context.Background()

	// 1. Load and validate config from the environment.
	cfg, err := config.LoadElasticsearch()
	if err != nil {
		log.Fatalf("elasticsearch config: %v", err)
	}

	// 2. Build the shared, process-wide client and verify connectivity.
	client, err := esclient.Shared(cfg)
	if err != nil {
		log.Fatalf("elasticsearch client: %v", err)
	}
	if err := client.Ping(ctx); err != nil {
		log.Fatalf("elasticsearch ping: %v", err)
	}

	// 3. Build the media repository and ensure its index exists.
	mediaRepo := media.NewRepository(client, cfg.MediaIndex)
	if err := mediaRepo.EnsureIndex(ctx); err != nil {
		log.Fatalf("ensure media index: %v", err)
	}

	// mediaRepo is now ready to inject into controllers/services. The helpers
	// below show typical read/write calls against it.
	exampleCreate(mediaRepo)
	exampleFindByID(mediaRepo)
}

// exampleCreate shows how to index a new media document.
func exampleCreate(mediaRepo media.Repository) {
	ctx := context.Background()

	doc := &media.Document{
		ID:          "vid_01H8XGJ",
		CreatorID:   "user_42",
		Title:       "How I Built a Video Platform in Go",
		Description: "A walkthrough of the StreamFlix backend architecture.",
		Visibility:  media.VisibilityPublic,
		Status:      media.StatusReady,
		Thumbnail:   "https://cdn.streamflix.dev/thumbs/vid_01H8XGJ.jpg",
		URL:         "https://cdn.streamflix.dev/hls/vid_01H8XGJ/master.m3u8",
		Duration:    842,
		Qualities:   []string{"360p", "720p", "1080p"},
		Tags:        []string{"golang", "backend", "video"},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := mediaRepo.Create(ctx, doc); err != nil {
		if errors.Is(err, esclient.ErrAlreadyExists) {
			log.Printf("media %q already exists", doc.ID)
			return
		}
		log.Fatalf("create media: %v", err)
	}
	fmt.Printf("indexed media %q\n", doc.ID)
}

// exampleFindByID shows how to fetch a media document and handle the
// not-found case.
func exampleFindByID(mediaRepo media.Repository) {
	ctx := context.Background()

	doc, err := mediaRepo.FindByID(ctx, "vid_01H8XGJ")
	if err != nil {
		if errors.Is(err, esclient.ErrNotFound) {
			log.Printf("media not found")
			return
		}
		log.Fatalf("find media: %v", err)
	}
	fmt.Printf("found %q by creator %q (%d qualities)\n",
		doc.Title, doc.CreatorID, len(doc.Qualities))
}
