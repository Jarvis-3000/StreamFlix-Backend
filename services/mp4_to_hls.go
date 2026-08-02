// Package services holds the video-processing pipeline that turns uploaded
// source files into streamable formats.
package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProcessedDir is the root under which HLS output is written. Each input video
// gets its own subfolder here, named after the source file (without extension).
const ProcessedDir = "./processed"

// hlsSegmentDuration is the target length, in seconds, of each .ts chunk.
const hlsSegmentDuration = "6"

// MP4ToHLS transcodes the mp4 at srcPath into a 720p HLS stream and writes the
// playlist and .ts chunks into ProcessedDir/<name>/, where <name> is the source
// filename without its extension. It returns the path to the generated
// playlist (index.m3u8).
//
// ffmpeg must be available on PATH.
func MP4ToHLS(srcPath string) (string, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return "", fmt.Errorf("source video not found: %w", err)
	}

	// Derive the output folder from the source filename, e.g.
	// ./uploads/movie.mp4 -> ./processed/movie/
	base := filepath.Base(srcPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	outDir := filepath.Join(ProcessedDir, name)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("could not create output dir %q: %w", outDir, err)
	}

	playlist := filepath.Join(outDir, "index.m3u8")
	// Chunks are named segment_000.ts, segment_001.ts, ...
	segmentPattern := filepath.Join(outDir, "segment_%03d.ts")

	args := []string{
		"-y",          // overwrite existing output without prompting
		"-i", srcPath, // input

		// Scale to 720p height while preserving aspect ratio. -2 keeps the
		// width divisible by 2, which H.264 requires.
		"-vf", "scale=-2:720",

		// Video: H.264, reasonable quality/size tradeoff for 720p.
		"-c:v", "libx264",
		"-profile:v", "main",
		"-crf", "20",
		"-preset", "veryfast",
		"-sc_threshold", "0", // disable scene-cut keyframes so GOPs stay regular
		"-g", "48", // keyframe every 48 frames (~2s at 24fps) for clean segments

		// Audio: AAC stereo.
		"-c:a", "aac",
		"-ar", "48000",
		"-b:a", "128k",

		// HLS output.
		"-f", "hls",
		"-hls_time", hlsSegmentDuration,
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segmentPattern,
		playlist,
	}

	cmd := exec.Command("ffmpeg", args...)
	// Surface ffmpeg's progress/errors on the server's stderr for debugging.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w", err)
	}

	return playlist, nil
}
