package services

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	mongoclient "streamflix-backend/internal/mongodb"
	"streamflix-backend/models"
	"streamflix-backend/store"
)

// CleanupInterval is how often local disk is swept.
const CleanupInterval = 5 * time.Minute

// uploadDir is where incoming videos are stored. It mirrors
// controllers.UploadDir, which cannot be referenced here: controllers imports
// this package, so importing it back would be a cycle.
const uploadDir = "./uploads"

// cleanupTimeout bounds one sweep's MongoDB lookups.
const cleanupTimeout = 30 * time.Second

// orphanAge is how long a video with no media document may sit on disk before
// it is treated as abandoned. It covers two cases that no status check can:
// an upload whose owner never submitted the metadata form, and a video whose
// document was deleted from MongoDB. Neither will ever be reported ready,
// so without this they would occupy disk forever.
//
// It has to comfortably exceed the gap between /upload returning a video id and
// media.create arriving with the metadata. That gap is deliberately open-ended
// — the upload registry outlives restarts precisely so a user can close the tab
// and finish the form later — so this is generous.
const orphanAge = 60 * time.Minute

// Cleanup reclaims local disk. The uploaded source and the transcoding output
// are working files: once a video is in the bucket, playback is served from
// there and the local copies are dead weight.
//
// Deleting is left to this sweep rather than done when processing finishes,
// because two goroutines share those files and whichever ended first would be
// deleting from under the other. A crash mid-transcode would skip an inline
// cleanup anyway.
//
// Two things get deleted. A video MongoDB reports as ready — the work is
// done and the bucket has it. And a video with no document at all, once it is
// older than orphanAge: nothing will ever mark it ready, so it would otherwise
// sit forever. Anything still processing or uploading is left alone, so a sweep
// can never race a live pipeline.

// StartCleanup sweeps every CleanupInterval until ctx is cancelled. It returns
// immediately; the loop runs in its own goroutine.
func StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				RunCleanup(ctx)
			}
		}
	}()
}

// RunCleanup performs one sweep: collect the video ids present on local disk,
// then delete the files of every one MongoDB reports as ready.
func RunCleanup(ctx context.Context) {
	repo, err := store.MediaRepository()
	if err != nil {
		log.Printf("cleanup: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()

	removed := 0
	for _, v := range localVideos() {
		doc, err := repo.FindByID(ctx, v.id)
		if err != nil {
			// Only a confirmed "no such document" is actionable. Any other
			// error — an unreachable cluster, a timeout — says nothing about
			// the video, and treating it as missing would let a single
			// database outage delete every pending upload on disk.
			if !errors.Is(err, mongoclient.ErrNotFound) {
				continue
			}

			// No document. Either the metadata form was never submitted, or the
			// document was deleted. Nothing will ever mark this ready, so age is
			// the only signal left — wait long enough that a user still filling
			// in the form can't have their upload swept out from under them.
			if time.Since(v.modTime) > orphanAge {
				log.Printf("[%s] cleanup: orphaned, no media document (age %s)",
					v.id, time.Since(v.modTime).Round(time.Minute))
				remove(v.id)
				removed++
			}
			continue
		}

		if doc.Status != models.StatusReady {
			continue
		}

		remove(v.id)
		removed++
	}

	if removed > 0 {
		log.Printf("cleanup: removed local files for %d video(s)", removed)
	}
}

// localVideo is a video id with files on local disk, and the last time any of
// them was written.
type localVideo struct {
	id      string
	modTime time.Time
}

// localVideos returns the set of video ids with files on local disk, taken from
// both working directories: uploads holds "<videoID>.<ext>" files and processed
// holds "<videoID>/" directories. A video part-way through the pipeline appears
// in both, so the two are combined into one set.
//
// The modtime kept is the newest seen across both locations. It is what decides
// whether a video with no media document is old enough to be abandoned, so it
// must never make a video look older than it is.
func localVideos() []localVideo {
	newest := make(map[string]time.Time)

	note := func(id string, entry os.DirEntry) {
		info, err := entry.Info()
		if err != nil {
			// Without a modtime there is no way to age this out safely, so
			// leave it for the next sweep rather than assuming it is old.
			log.Printf("cleanup: stat %q: %v", entry.Name(), err)
			return
		}
		if seen, ok := newest[id]; !ok || info.ModTime().After(seen) {
			newest[id] = info.ModTime()
		}
	}

	// uploads/<videoID>.<ext> — strip the extension to recover the id.
	entries, err := os.ReadDir(uploadDir)

	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("cleanup: read %q: %v", uploadDir, err)
		}
	} else {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue // .DS_Store and friends
			}
			note(strings.TrimSuffix(name, filepath.Ext(name)), e)
		}
	}

	// processed/<videoID>/ — the directory name is the id.
	entries, err = os.ReadDir(ProcessedDir)

	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("cleanup: read %q: %v", ProcessedDir, err)
		}
	} else {
		for _, e := range entries {
			if e.IsDir() {
				note(e.Name(), e)
			}
		}
	}

	out := make([]localVideo, 0, len(newest))
	for id, mod := range newest {
		out = append(out, localVideo{id: id, modTime: mod})
	}
	return out
}

// remove deletes a video's upload file, its processed directory, and its upload
// registry entry. Failures are logged rather than returned: the caller has no
// recourse, and the next sweep retries whatever didn't go.
func remove(videoID string) {
	// The upload's extension isn't known from the id alone, so match on the
	// prefix rather than guessing ".mp4".
	entries, err := os.ReadDir(uploadDir)

	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), videoID) {
				continue
			}
			path := filepath.Join(uploadDir, e.Name())
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("[%s] cleanup: remove %q: %v", videoID, path, err)
			}
		}
	}

	dir := filepath.Join(ProcessedDir, videoID)
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("[%s] cleanup: remove %q: %v", videoID, dir, err)
	}

	// The registry answers "is this video id real?" for media.create. Once the
	// bytes are gone the answer has to be no, or media.create would accept an
	// id whose file no longer exists. A missing entry is not an error here —
	// the files can outlive the record.
	_ = store.PersistedUploads.Delete(videoID)

	log.Printf("[%s] cleanup: local files removed", videoID)
}
