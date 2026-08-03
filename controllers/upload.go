package controllers

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"streamflix-backend/store"
)

// UploadDir is where incoming videos are stored.
const UploadDir = "./uploads"

// defaultMaxUploadBytes caps a single uploaded video. Without a ceiling a
// client could stream indefinitely and fill the disk, since we write straight
// through to a file rather than buffering in memory.
//
// 500 MB is sized against the disk a video actually costs, which is the source
// plus the transcode output: both are on disk at once, because cleanup only
// deletes the pair once Elasticsearch reports the video ready. The single 480p
// rendition runs at 1400k video + 128k audio, so output grows at roughly 11 MB
// per minute of footage no matter how big the source was. A 500 MB upload is
// about twelve minutes of phone video, so peak disk lands near 640 MB — inside
// a 1 GB volume with room for another upload alongside it.
const defaultMaxUploadBytes = 500 << 20 // 500 MB

// MaxUploadBytes is the effective cap, overridable with MAX_UPLOAD_BYTES so the
// limit can be matched to the volume a deployment actually has without a
// rebuild. A missing, unparseable, or non-positive value keeps the default.
var MaxUploadBytes = loadMaxUploadBytes()

func loadMaxUploadBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("MAX_UPLOAD_BYTES"))
	if raw == "" {
		return defaultMaxUploadBytes
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		log.Printf("upload: ignoring invalid MAX_UPLOAD_BYTES %q, using %d", raw, int64(defaultMaxUploadBytes))
		return defaultMaxUploadBytes
	}
	return n
}

// Uplaod accepts a video, stores it in local and returns videoId
func Upload(w http.ResponseWriter, r *http.Request) {
	file, filename, err := openUploadBody(r)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	defer file.Close()

	// A stable ID for this video, used to correlate the async result (and,
	// later, the database row) with what the client was told.
	videoID := uuid.NewString()

	// Store under the video id, not the client's filename: two users uploading
	// "video.mp4" would otherwise overwrite each other, and a crafted filename
	// could escape UploadDir entirely. The original name is kept only as
	// metadata. The extension is preserved so ffmpeg can infer the format.
	dstPath := filepath.Join(UploadDir, videoID+uploadExt(filename, r))
	dst, err := os.Create(dstPath)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Stop at the cap rather than trusting Content-Length, which a client
	// controls and a chunked upload omits entirely.
	size, err := io.Copy(dst, io.LimitReader(file, MaxUploadBytes+1))
	if err != nil {
		dst.Close()
		os.Remove(dstPath)
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if size > MaxUploadBytes {
		dst.Close()
		os.Remove(dstPath)
		Error(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("video exceeds the %d byte limit", MaxUploadBytes))
		return
	}
	if size == 0 {
		dst.Close()
		os.Remove(dstPath)
		Error(w, http.StatusBadRequest, "empty request body: send the video bytes as the request body")
		return
	}

	// Flush and close before handing the file to ffmpeg so it reads the
	// complete upload rather than a partially-written file.
	err = dst.Close()
	if err != nil {
		os.Remove(dstPath)
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Record the upload before responding. Until this exists there is nothing
	// for media.create to validate the returned videoId against, so the client
	// must not be told about an id we haven't registered.
	store.PersistedUploads.Register(videoID, filename, dstPath, size)

	// Transcoding deliberately does not start here. The bytes are only half the
	// story: until media.create supplies the title and visibility there is no
	// document to record progress against, and a video the user abandons at the
	// metadata form should not cost us an ffmpeg run. CreateMedia kicks off the
	// pipeline once the document exists.
	JSON(w, http.StatusAccepted, map[string]any{
		"videoId":  videoID,
		"filename": filename,
		"size":     size,
		"status":   "awaiting_metadata",
		"message":  "upload received; submit media.create with this videoId to publish it",
	})
}

// openUploadBody returns a reader over the uploaded video bytes plus the
// original filename, if the client supplied one. A multipart request is read
// from its "video" field; anything else is treated as the raw video stream.
//
// The returned reader is always safe to Close: for the raw case the body is
// closed by net/http anyway, so closing it twice is harmless.
func openUploadBody(r *http.Request) (io.ReadCloser, string, error) {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)

	if err != nil {
		mediaType = ""
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		file, header, err := r.FormFile("video")
		if err != nil {
			return nil, "", fmt.Errorf(`multipart upload is missing the "video" file field`)
		}
		return file, filepath.Base(header.Filename), nil
	}

	if r.Body == nil {
		return nil, "", fmt.Errorf("empty request body: send the video bytes as the request body")
	}
	return r.Body, rawUploadFilename(r), nil
}

// rawUploadFilename recovers the original name of a raw binary upload, which
// carries no multipart header to read it from. Clients may pass it via the
// X-Filename header or a ?filename= query param; both are optional, and the
// name is only ever metadata — it never determines the path we write to.
func rawUploadFilename(r *http.Request) string {
	name := r.Header.Get("X-Filename")
	if name == "" {
		name = r.URL.Query().Get("filename")
	}
	if name == "" {
		return ""
	}
	// Base strips any directory components a client may have included.
	return filepath.Base(strings.TrimSpace(name))
}

// videoExtByType maps the video content types we expect onto the extension we
// store them under. It is an explicit table rather than mime.ExtensionsByType
// because that returns every registered extension in alphabetical order —
// "video/mp4" yields ".m4v" first and "video/x-matroska" yields ".mk3d", so
// taking the first entry would misname the most common uploads.
var videoExtByType = map[string]string{
	"video/mp4":        ".mp4",
	"video/quicktime":  ".mov",
	"video/webm":       ".webm",
	"video/x-matroska": ".mkv",
	"video/x-msvideo":  ".avi",
	"video/mpeg":       ".mpeg",
	"video/3gpp":       ".3gp",
	"video/x-flv":      ".flv",
	"video/x-ms-wmv":   ".wmv",
}

// uploadExt picks the extension to store the file under. The client's filename
// wins when it has one; otherwise it is inferred from Content-Type so ffmpeg
// can still recognise the container. An unknown or generic type yields ".mp4",
// our default input format.
func uploadExt(filename string, r *http.Request) string {
	if ext := filepath.Ext(filename); ext != "" {
		return ext
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return ".mp4"
	}
	if ext, ok := videoExtByType[strings.ToLower(mediaType)]; ok {
		return ext
	}
	return ".mp4"
}
