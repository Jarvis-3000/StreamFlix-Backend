package controllers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"streamflix-backend/services"
)

// UploadDir is where incoming videos are stored. It is created once at startup
// (see main), so handlers can assume it exists.
const UploadDir = "./uploads"

// Upload accepts an mp4 upload, saves it to disk, and immediately responds with
// a "queued" status and a generated video ID. The heavy work — transcoding to
// HLS and uploading the result to Supabase — happens asynchronously in a
// background goroutine so the client isn't kept waiting.
func Upload(w http.ResponseWriter, r *http.Request) {
	// Keep up to 32 MB in memory; the rest spills to temp files on disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		Error(w, http.StatusBadRequest, "could not parse upload: "+err.Error())
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		Error(w, http.StatusBadRequest, `missing "video" file field`)
		return
	}
	defer file.Close()

	// A stable ID for this video, used to correlate the async result (and,
	// later, the database row) with what the client was told.
	videoID := uuid.NewString()

	// Create Path to store the file at.
	dstPath := UploadDir + "/" + header.Filename
	dst, err := os.Create(dstPath)
	fmt.Println("destination: ", dstPath)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	written, err := io.Copy(dst, file)
	if err != nil {
		dst.Close()
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Flush and close before handing the file to ffmpeg so it reads the
	// complete upload rather than a partially-written file.
	if err := dst.Close(); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Kick off transcoding + upload in the background. We deliberately do not
	// use the request context here: it's cancelled once we respond, and this
	// work must outlive the request.
	go processVideo(videoID, header.Filename, dstPath)

	JSON(w, http.StatusAccepted, map[string]any{
		"videoId":  videoID,
		"filename": header.Filename,
		"size":     written,
		"status":   "queued",
		"message":  "upload received; processing in the background",
	})
}

// processVideo runs the full background pipeline for a single uploaded video:
// transcode to HLS, upload the resulting folder to Supabase, then record the
// result. It's meant to be run in its own goroutine and never returns anything
// to the caller — progress and outcomes are logged.
func processVideo(videoID, filename, srcPath string) {
	log.Printf("[%s] processing started for %q", videoID, filename)

	// 1. Transcode MP4 -> HLS (index.m3u8 + .ts chunks) on local disk.
	playlist, err := services.MP4ToHLS(srcPath)
	if err != nil {
		log.Printf("[%s] transcoding failed: %v", videoID, err)
		return
	}
	// TODO: Update the status to Transcoded/Processed
	log.Printf("[%s] transcoding complete: %s", videoID, playlist)

	// 2. Upload the whole HLS folder to Supabase S3.
	folder := filepath.Dir(playlist)
	// TODO: Update the status to uploading with progress
	publicURL, err := services.UploadToServer(context.Background(), folder)
	if err != nil {
		log.Printf("[%s] upload to Supabase failed: %v", videoID, err)
		return
	}

	// 3. Persist the result. The database isn't wired up yet, so for now we
	//    just log the success and the playback URL. This is where we'll later
	//    update the video row (status = ready, hls_url = publicURL).
	log.Printf("[%s] SUCCESS: %q HLS uploaded, playback URL: %s", videoID, filename, publicURL)
	log.Printf("[%s] TODO: update database (video %s -> ready, url=%s)", videoID, videoID, publicURL)
}
