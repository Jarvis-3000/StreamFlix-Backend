package services

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ThumbnailCount is how many stills are captured per video. They are spread
// across the running time so the user has a few distinct frames to choose a
// poster from, rather than three near-identical shots of the opening seconds.
const ThumbnailCount = 3

// thumbnailWidth is the width, in pixels, that stills are scaled to. Height
// follows the source aspect ratio.
const thumbnailWidth = 640

// Duration returns the length of the video at srcPath in whole seconds,
// rounded to nearest.
//
// It shells out to ffprobe, which reads the container metadata rather than
// decoding the stream, so this is fast even on large files. ffprobe must be
// available on PATH — it ships alongside ffmpeg.
func Duration(srcPath string) (int, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return 0, fmt.Errorf("source video not found: %w", err)
	}

	args := []string{
		"-v", "error", // suppress everything but real errors
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", // print the bare number
		srcPath,
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ffprobe", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	raw := strings.TrimSpace(stdout.String())
	// Some containers report no duration at all, in which case ffprobe prints
	// "N/A" rather than failing.
	if raw == "" || raw == "N/A" {
		return 0, fmt.Errorf("ffprobe reported no duration for %q", srcPath)
	}

	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("could not parse duration %q: %w", raw, err)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("ffprobe reported a non-positive duration %q", raw)
	}

	return int(math.Round(seconds)), nil
}

// GenerateThumbnails captures ThumbnailCount stills from the video at srcPath
// and writes them into ProcessedDir/<videoID>/thumbnails/ as thumb_1.jpg,
// thumb_2.jpg, ... It returns their paths in capture order, earliest first.
//
// The folder sits beside the hls/ tree under the same video id, so the two
// subtrees upload to matching prefixes in the bucket without overlapping.
//
// Frames are taken at evenly spaced points across the running time, skipping
// the very start and end: the first and last moments of a video are usually a
// black frame or a fade, which makes for a poor poster. For a 60s video with
// the default count that is 15s, 30s and 45s.
//
// duration is the video length in seconds, as returned by Duration. Passing it
// in rather than probing again avoids a second ffprobe call, since the caller
// already needs the duration for its own reasons.
//
// ffmpeg must be available on PATH.
func GenerateThumbnails(videoID, srcPath string, duration int) ([]string, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return nil, fmt.Errorf("source video not found: %w", err)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %d", duration)
	}

	outDir := filepath.Join(ProcessedDir, videoID, ThumbnailsDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create thumbnail dir %q: %w", outDir, err)
	}

	paths := make([]string, 0, ThumbnailCount)
	for i, offset := range thumbnailOffsets(duration, ThumbnailCount) {
		dst := filepath.Join(outDir, fmt.Sprintf("thumb_%d.jpg", i+1))

		if err := captureFrame(srcPath, dst, offset); err != nil {
			return nil, fmt.Errorf("thumbnail %d at %.2fs: %w", i+1, offset, err)
		}
		paths = append(paths, dst)
	}

	return paths, nil
}

// thumbnailOffsets returns count timestamps, in seconds, evenly spaced across a
// video of the given duration. The points sit at 1/(count+1), 2/(count+1), ...
// of the way through, which keeps every frame clear of the first and last
// moments where fades and black frames live.
func thumbnailOffsets(duration, count int) []float64 {
	offsets := make([]float64, 0, count)
	for i := 1; i <= count; i++ {
		offsets = append(offsets, float64(duration)*float64(i)/float64(count+1))
	}
	return offsets
}

// captureFrame extracts a single frame at the given offset (in seconds) and
// writes it to dst as a JPEG.
func captureFrame(srcPath, dst string, offset float64) error {
	args := []string{
		"-y", // overwrite existing output without prompting

		// -ss before -i seeks by keyframe without decoding everything up to
		// that point, which is dramatically faster on long videos. The frame
		// landed on may be slightly off the exact timestamp; for a poster
		// image that does not matter.
		"-ss", strconv.FormatFloat(offset, 'f', 3, 64),
		"-i", srcPath,

		"-frames:v", "1", // one frame only
		"-q:v", "2", // JPEG quality, 2 is near-best (scale is 1-31, lower is better)

		// Scale to a fixed width, preserving aspect ratio. -2 keeps the height
		// even, which some encoders require.
		"-vf", fmt.Sprintf("scale=%d:-2", thumbnailWidth),

		dst,
	}

	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// ffmpeg can exit 0 having written nothing — seeking past the last frame
	// is the usual cause. An empty file would upload cleanly and then render
	// as a broken image, so catch it here.
	info, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("no thumbnail written: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("ffmpeg wrote an empty thumbnail")
	}

	return nil
}
