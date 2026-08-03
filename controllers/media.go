package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	esclient "streamflix-backend/internal/elasticsearch"
	"streamflix-backend/models"
	"streamflix-backend/services"
	"streamflix-backend/store"
)

const mediaOpTimeout = 10 * time.Second

func CreateMedia(params json.RawMessage) (*models.Media, error) {
	var input models.MediaInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	doc, err := validateMediaInput(&input)
	if err != nil {
		return nil, err
	}

	repo, err := store.MediaRepository()
	if err != nil {
		return nil, err
	}

	// The video id must name a real upload — otherwise a client could mint
	// documents for videos that were never uploaded, and there would be no file
	// for the pipeline to transcode.
	upload, exist := store.PersistedUploads.Get(doc.ID)
	if !exist {
		return nil, fmt.Errorf("video not found: %s", doc.ID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	// Create a media doc
	err = repo.Create(ctx, *doc)

	if err != nil {
		if errors.Is(err, esclient.ErrAlreadyExists) {
			return nil, fmt.Errorf("media already exists for video %s", doc.ID)
		}
		log.Printf("media: create %q: %v", doc.ID, err)
		return nil, fmt.Errorf("could not create media")
	}

	// Transcoding is slow, so start it straight away and let it run alongside
	// the metadata work. Both goroutines outlive this request.
	//
	// Neither deletes the source file or the working directory when it finishes.
	// The two stages share both and would race each other, and a crash mid-run
	// would skip the cleanup anyway — so reclaiming disk is left entirely to the
	// sweeper in services/cleanup.go.
	go processVideo(doc.ID, upload.Filename, upload.Path)
	go processMetadata(doc.ID, upload.Filename, upload.Path)

	return doc, nil
}

func GetMediaByID(params json.RawMessage) (*models.Media, error) {
	var req models.MediaId
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	repo, err := store.MediaRepository()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	doc, err := repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, esclient.ErrNotFound) {
			return nil, fmt.Errorf("media not found: %s", id)
		}
		log.Printf("media: find %q: %v", id, err)
		return nil, fmt.Errorf("could not fetch media")
	}
	return doc, nil
}

func ListMedia(params json.RawMessage) ([]models.Media, error) {
	repo, err := store.MediaRepository()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	list, err := repo.Search(ctx, nil)
	if err != nil {
		log.Printf("media: list: %v", err)
		return nil, fmt.Errorf("could not fetch media")
	}

	return list, nil
}

func UpdateMedia(params json.RawMessage) (*models.Media, error) {
	var input models.MediaInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	repo, err := store.MediaRepository()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	doc, err := repo.FindByID(ctx, input.VideoID)
	if err != nil {
		return nil, err
	}

	changed := false

	if input.Title != "" {
		doc.Title = input.Title
		changed = true
	}
	if input.Description != "" {
		doc.Description = input.Description
		changed = true
	}
	if input.Category != "" {
		doc.Category = input.Category
		changed = true
	}
	if input.Visibility != "" {
		doc.Visibility = input.Visibility
		changed = true
	}
	if input.Tags != nil {
		doc.Tags = input.Tags
		changed = true
	}

	if !changed {
		return nil, nil
	}

	doc.UpdatedAt = time.Now().UTC()

	err = repo.Update(ctx, *doc)

	if err != nil {
		log.Printf("[%s] update: %v", input.VideoID, err)
		return nil, err
	}

	return doc, nil
}

func DeleteMedia(params json.RawMessage) (any, error) {
	var input models.MediaInput
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, fmt.Errorf("malformed params")
	}

	repo, err := store.MediaRepository()

	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	err = repo.Delete(ctx, input.VideoID)

	if err != nil {
		return nil, err
	}

	return nil, nil
}

// Internal Functions
func validateMediaInput(input *models.MediaInput) (*models.Media, error) {
	videoID := strings.TrimSpace(input.VideoID)
	if videoID == "" {
		return nil, fmt.Errorf("video_id is required")
	}

	creatorID := strings.TrimSpace(input.CreatorID)
	if creatorID == "" {
		return nil, fmt.Errorf("creator_id is required")
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if len(title) > models.MediaTitleMaxLen {
		return nil, fmt.Errorf("title must be at most %d characters", models.MediaTitleMaxLen)
	}

	description := strings.TrimSpace(input.Description)
	if len(description) > models.MediaDescriptionMaxLen {
		return nil, fmt.Errorf("description must be at most %d characters", models.MediaDescriptionMaxLen)
	}

	visibility, err := parseVisibility(input.Visibility)
	if err != nil {
		return nil, err
	}

	category := strings.ToLower(strings.TrimSpace(input.Category))
	if category == "" {
		return nil, fmt.Errorf("category is required")
	}
	if !models.IsValidMediaCategory(category) {
		return nil, fmt.Errorf("category must be one of: %s", strings.Join(models.MediaCategories, ", "))
	}

	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &models.Media{
		ID:          videoID,
		CreatorID:   creatorID,
		Title:       title,
		Description: description,
		Visibility:  visibility,
		Category:    category,
		// URL, Duration and Qualities stay zero. They are only known once
		// transcoding finishes, and processVideo fills them in then.
		Status:    models.StatusProcessing,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func parseVisibility(raw models.Visibility) (models.Visibility, error) {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case string(models.VisibilityPublic):
		return models.VisibilityPublic, nil
	case string(models.VisibilityUnlisted):
		return models.VisibilityUnlisted, nil
	case string(models.VisibilityPrivate):
		return models.VisibilityPrivate, nil
	case "":
		return "", fmt.Errorf("visibility is required")
	default:
		return "", fmt.Errorf("visibility must be one of: %s, %s, %s",
			models.VisibilityPublic, models.VisibilityUnlisted, models.VisibilityPrivate)
	}
}

func normalizeTags(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	tags := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for _, tag := range raw {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if len(tag) > models.MediaTagMaxLen {
			return nil, fmt.Errorf("each tag must be at most %d characters", models.MediaTagMaxLen)
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}

	if len(tags) > models.MediaMaxTags {
		return nil, fmt.Errorf("at most %d tags are allowed", models.MediaMaxTags)
	}
	return tags, nil
}

// processMetadata derives the video's duration and poster images. It runs
// alongside processVideo rather than after it: neither reads the other's
// output, and they write to separate subfolders of the same video prefix
// (thumbnails/ and hls/), so the two can transcode and upload in parallel.
//
// The steps here are ordered, though — GenerateThumbnails needs the duration to
// space its captures across the running time.
func processMetadata(mediaID, filename, srcPath string) {
	duration, err := processDuration(mediaID, filename, srcPath)
	if err != nil {
		// A missing duration is not fatal to playback, but it does mean the
		// stills can't be spaced across the video, so stop here.
		return
	}
	processThumbnail(mediaID, filename, srcPath, duration)
}

// Get duration and update media doc
func processDuration(mediaID, filename, srcPath string) (int, error) {
	duration, err := services.Duration(srcPath)
	if err != nil {
		log.Printf("[%s] duration probe failed: %v", mediaID, err)
		return 0, err
	}
	log.Printf("[%s] duration: %ds", mediaID, duration)

	// Only the duration is written here. processVideo owns status and url, and
	// the two goroutines touch disjoint fields so neither undoes the other.
	updateMedia(mediaID, "duration", func(doc *models.Media) {
		doc.Duration = duration
	})

	return duration, nil
}

// Generate thumbnails + upload on S3 + update media doc
func processThumbnail(mediaID, filename, srcPath string, duration int) {
	// 1. Capture the stills into ./processed/<mediaID>/thumbnails/.
	paths, err := services.GenerateThumbnails(mediaID, srcPath, duration)
	if err != nil {
		log.Printf("[%s] thumbnail generation failed: %v", mediaID, err)
		return
	}
	log.Printf("[%s] generated %d thumbnails", mediaID, len(paths))

	// 2. Upload them to <mediaID>/thumbnails/ in the bucket, beside the hls/
	//    folder processVideo is uploading. The request context is long gone by
	//    now, so this gets its own.
	folder := filepath.Dir(paths[0])
	urls, err := services.UploadThumbnails(context.Background(), mediaID, folder, paths)
	if err != nil {
		log.Printf("[%s] thumbnail upload failed: %v", mediaID, err)
		return
	}

	// 3. Record the first still as the poster. All three are uploaded and
	//    addressable by URL, so a creator can be offered the others later
	//    without regenerating anything.
	updateMedia(mediaID, "thumbnail", func(doc *models.Media) {
		doc.Thumbnail = urls[0]
	})
	log.Printf("[%s] thumbnail set: %s", mediaID, urls[0])
}

// Transcode + Upload on S3 (Supabase)
func processVideo(mediaID, filename, srcPath string) {
	log.Printf("[%s] processing started for %q", mediaID, filename)

	// 1. Transcode MP4 -> HLS (master.m3u8 + per-rendition playlists and .ts
	//    chunks) on local disk.
	master, err := services.MP4ToHLS(mediaID, srcPath)
	if err != nil {
		log.Printf("[%s] transcoding failed: %v", mediaID, err)
		setMediaStatus(mediaID, models.StatusFailed, "")
		return
	}
	log.Printf("[%s] transcoding complete: %s", mediaID, master)

	setMediaStatus(mediaID, models.StatusUploading, "")

	// 2. Upload the whole hls folder to Supabase S3. The request context is
	//    long gone by now, so this gets its own.
	folder := filepath.Dir(master)
	publicURL, err := services.UploadHLS(context.Background(), mediaID, folder)
	if err != nil {
		log.Printf("[%s] upload to Supabase failed: %v", mediaID, err)
		setMediaStatus(mediaID, models.StatusFailed, "")
		return
	}

	// 3. Record the playback URL and flip the document to ready.
	setMediaStatus(mediaID, models.StatusReady, publicURL)
	log.Printf("[%s] SUCCESS: %q HLS uploaded, playback URL: %s", mediaID, filename, publicURL)
}

func setMediaStatus(mediaID string, status models.Status, url string) {
	updateMedia(mediaID, fmt.Sprintf("status %q", status), func(doc *models.Media) {
		doc.Status = status
		if url != "" {
			doc.URL = url
		}
	})
}

// updateMedia fetches a media document, applies mutate to it and writes it
// back. what names the change for log messages.
//
// The read and the write are not atomic, so two concurrent callers can
// interleave and the later write will carry the earlier one's stale copy of any
// field it didn't touch. The pipeline's concurrent writers — processVideo and
// processMetadata — deliberately mutate disjoint fields to keep that window
// harmless. Adding a third writer, or letting either touch the other's fields,
// would need optimistic concurrency (seq_no/primary_term) instead.
func updateMedia(mediaID, what string, mutate func(*models.Media)) {
	repo, err := store.MediaRepository()
	if err != nil {
		log.Printf("[%s] %s: %v", mediaID, what, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), mediaOpTimeout)
	defer cancel()

	doc, err := repo.FindByID(ctx, mediaID)
	if err != nil {
		log.Printf("[%s] %s: fetch: %v", mediaID, what, err)
		return
	}

	mutate(doc)
	doc.UpdatedAt = time.Now().UTC()

	if err := repo.Update(ctx, *doc); err != nil {
		log.Printf("[%s] %s: update: %v", mediaID, what, err)
	}
}
