package media_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"streamflix-backend/config"
	esclient "streamflix-backend/internal/elasticsearch"
	"streamflix-backend/internal/elasticsearch/media"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// This file exercises the media Repository against a real Elasticsearch
// cluster. It is skipped unless ES_INTEGRATION is set, so the default
// `go test ./...` stays offline, fast, and credential-free:
//
//	ES_INTEGRATION=1 go test -run Integration -v ./internal/elasticsearch/media/
//
// Every run uses a freshly generated document id and deletes it again, so the
// test never collides with real data or with a concurrent run of itself.

// opTimeout bounds each cluster round-trip so a hung connection fails the test
// instead of stalling the suite.
const opTimeout = 30 * time.Second

// TestMediaRepository_Integration walks one document through the full
// lifecycle: create, reject duplicate, read, update, reject update of a missing
// id, find by title, delete, and confirm the delete.
//
// The subtests share the document and must run in order, so none of them call
// t.Parallel.
func TestMediaRepository_Integration(t *testing.T) {
	repo := newTestRepository(t)

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// A nonce keeps this run's document distinct from every other run's. It is
	// embedded in the title as a single lowercase-hex token so the standard
	// analyzer keeps it intact and SearchByTitle can target this document
	// exactly.
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
	id := "itest_" + nonce

	// Delete unconditionally at the end: the test may fail partway through and
	// still have indexed the document. A missing id is the expected outcome of
	// the happy path, so it is not an error here.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), opTimeout)
		defer cleanupCancel()

		if err := repo.Delete(cleanupCtx, id); err != nil && !errors.Is(err, esclient.ErrNotFound) {
			t.Errorf("cleanup: delete %q: %v", id, err)
		}
	})

	// Truncated to milliseconds because that is the precision Elasticsearch
	// indexes dates at; keeping nanoseconds would only make the comparisons
	// depend on _source fidelity.
	now := time.Now().UTC().Truncate(time.Millisecond)

	doc := &media.Document{
		ID:          id,
		CreatorID:   "user_integration",
		Title:       fmt.Sprintf("StreamFlix integration probe %s", nonce),
		Description: "Written and removed by TestMediaRepository_Integration.",
		Visibility:  media.VisibilityPublic,
		Status:      media.StatusUploaded,
		Thumbnail:   "https://cdn.example.test/thumbs/" + id + ".jpg",
		URL:         "https://cdn.example.test/hls/" + id + "/master.m3u8",
		Duration:    842,
		Qualities:   []string{"360p", "720p", "1080p"},
		Tags:        []string{"golang", "integration"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	t.Run("Create", func(t *testing.T) {
		if err := repo.Create(ctx, doc); err != nil {
			t.Fatalf("create %q: %v", id, err)
		}
	})

	t.Run("CreateDuplicateIsRejected", func(t *testing.T) {
		err := repo.Create(ctx, doc)
		if !errors.Is(err, esclient.ErrAlreadyExists) {
			t.Fatalf("create duplicate %q: got %v, want ErrAlreadyExists", id, err)
		}
	})

	t.Run("FindByID", func(t *testing.T) {
		got, err := repo.FindByID(ctx, id)
		if err != nil {
			t.Fatalf("find %q: %v", id, err)
		}
		assertDocumentEqual(t, got, doc)
	})

	t.Run("Update", func(t *testing.T) {
		updated := *doc
		updated.Title = fmt.Sprintf("StreamFlix integration probe %s updated", nonce)
		updated.Status = media.StatusReady
		updated.Duration = 900
		updated.Tags = []string{"golang", "integration", "updated"}
		updated.UpdatedAt = now.Add(time.Minute)

		if err := repo.Update(ctx, &updated); err != nil {
			t.Fatalf("update %q: %v", id, err)
		}

		got, err := repo.FindByID(ctx, id)
		if err != nil {
			t.Fatalf("find after update %q: %v", id, err)
		}
		assertDocumentEqual(t, got, &updated)

		// Later subtests search for and delete the current state of the
		// document, so keep the local copy in step with the cluster.
		*doc = updated
	})

	t.Run("UpdateMissingIDIsRejected", func(t *testing.T) {
		missing := *doc
		missing.ID = id + "_absent"

		err := repo.Update(ctx, &missing)
		if !errors.Is(err, esclient.ErrNotFound) {
			t.Fatalf("update missing id: got %v, want ErrNotFound", err)
		}
	})

	t.Run("SearchByTitle", func(t *testing.T) {
		// Create and Update both refresh the index, so the document is
		// searchable by the time we get here.
		hits, err := repo.SearchByTitle(ctx, nonce)
		if err != nil {
			t.Fatalf("search %q: %v", nonce, err)
		}

		idx := slices.IndexFunc(hits, func(d media.Document) bool { return d.ID == id })
		if idx < 0 {
			t.Fatalf("search %q: returned %d hits, none with id %q", nonce, len(hits), id)
		}
		assertDocumentEqual(t, &hits[idx], doc)
	})

	t.Run("Delete", func(t *testing.T) {
		if err := repo.Delete(ctx, id); err != nil {
			t.Fatalf("delete %q: %v", id, err)
		}

		if _, err := repo.FindByID(ctx, id); !errors.Is(err, esclient.ErrNotFound) {
			t.Fatalf("find after delete %q: got %v, want ErrNotFound", id, err)
		}
	})

	t.Run("DeleteMissingIDIsRejected", func(t *testing.T) {
		err := repo.Delete(ctx, id)
		if !errors.Is(err, esclient.ErrNotFound) {
			t.Fatalf("delete already-deleted %q: got %v, want ErrNotFound", id, err)
		}
	})
}

// newTestRepository skips the test unless ES_INTEGRATION is set, then connects
// to the configured cluster and returns a repository with its index ensured.
// Any setup failure is fatal: a half-connected repository would only produce
// confusing downstream failures.
func newTestRepository(t *testing.T) media.Repository {
	t.Helper()

	if os.Getenv("ES_INTEGRATION") == "" {
		t.Skip("set ES_INTEGRATION=1 to run tests against a live Elasticsearch cluster")
	}

	loadRepoEnv(t)

	cfg, err := config.LoadElasticsearch()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	client, err := esclient.Shared(cfg)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping cluster: %v", err)
	}

	repo := media.NewRepository(client, cfg.MediaIndex)
	if err := repo.EnsureIndex(ctx); err != nil {
		t.Fatalf("ensure index %q: %v", cfg.MediaIndex, err)
	}

	t.Logf("connected to Elasticsearch, using index %q", cfg.MediaIndex)
	return repo
}

// loadRepoEnv loads the repository-root .env, since `go test` runs with the
// package directory as its working directory and would not otherwise see it.
// Variables already present in the environment win, so CI can override the
// file. A missing .env is fine — the config may come from the environment.
func loadRepoEnv(t *testing.T) {
	t.Helper()

	root, err := repoRoot()
	if err != nil {
		t.Logf("skipping .env load: %v", err)
		return
	}
	if err := godotenv.Load(filepath.Join(root, ".env")); err != nil {
		t.Logf("no .env loaded from %s: %v", root, err)
	}
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod found above working directory")
		}
		dir = parent
	}
}

// assertDocumentEqual reports every field of got that differs from want, so a
// failure shows the whole picture rather than the first mismatch.
func assertDocumentEqual(t *testing.T, got, want *media.Document) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.CreatorID != want.CreatorID {
		t.Errorf("CreatorID: got %q, want %q", got.CreatorID, want.CreatorID)
	}
	if got.Title != want.Title {
		t.Errorf("Title: got %q, want %q", got.Title, want.Title)
	}
	if got.Description != want.Description {
		t.Errorf("Description: got %q, want %q", got.Description, want.Description)
	}
	if got.Visibility != want.Visibility {
		t.Errorf("Visibility: got %q, want %q", got.Visibility, want.Visibility)
	}
	if got.Status != want.Status {
		t.Errorf("Status: got %q, want %q", got.Status, want.Status)
	}
	if got.Thumbnail != want.Thumbnail {
		t.Errorf("Thumbnail: got %q, want %q", got.Thumbnail, want.Thumbnail)
	}
	if got.URL != want.URL {
		t.Errorf("URL: got %q, want %q", got.URL, want.URL)
	}
	if got.Duration != want.Duration {
		t.Errorf("Duration: got %d, want %d", got.Duration, want.Duration)
	}
	if !slices.Equal(got.Qualities, want.Qualities) {
		t.Errorf("Qualities: got %v, want %v", got.Qualities, want.Qualities)
	}
	if !slices.Equal(got.Tags, want.Tags) {
		t.Errorf("Tags: got %v, want %v", got.Tags, want.Tags)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt: got %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt: got %s, want %s", got.UpdatedAt, want.UpdatedAt)
	}
}
