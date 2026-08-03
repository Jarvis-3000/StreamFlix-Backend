// Package services holds the video-processing pipeline that turns uploaded
// source files into streamable formats.
package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ProcessedDir is the root under which processing output is written. Each video
// gets its own subfolder here, named after its video id.
const ProcessedDir = "./processed"

// Layout of a video's output folder, both locally under ProcessedDir/<videoID>/
// and in the S3 bucket under <videoID>/. Keeping the two identical means an
// upload is a straight copy of a directory, with no key rewriting.
//
//	<videoID>/
//	├── thumbnails/
//	│   ├── thumb_1.jpg
//	│   ├── thumb_2.jpg
//	│   └── thumb_3.jpg
//	└── hls/
//	    ├── master.m3u8
//	    └── 480p/
//	        ├── playlist.m3u8
//	        ├── segment000.ts
//	        └── ...
//
// The two subtrees are disjoint, so transcoding and thumbnail capture can run
// and upload concurrently without coordinating.
const (
	HLSDir        = "hls"
	ThumbnailsDir = "thumbnails"
)

// hlsSegmentDuration is the target length, in seconds, of each .ts chunk.
const hlsSegmentDuration = "6"

// renditions are the quality levels produced for each video. Only 480p today;
// the master playlist exists so additional renditions can be added later
// without changing the URL players are already pointed at.
var renditions = []rendition{
	{name: "480p", height: 480, videoBitrate: "1400k", audioBitrate: "128k", bandwidth: 1528000, resolution: "854x480"},
}

// rendition describes one quality level of the output stream.
type rendition struct {
	name         string // folder name and quality label, e.g. "720p"
	height       int    // output height in pixels; width follows the aspect ratio
	videoBitrate string
	audioBitrate string
	bandwidth    int    // peak bits/sec, advertised in the master playlist
	resolution   string // advertised in the master playlist, e.g. "1280x720"
}

// MP4ToHLS transcodes the mp4 at srcPath into HLS and writes the output into
// ProcessedDir/<videoID>/hls/: one subfolder per rendition, plus a master
// playlist listing them. It returns the path to the master playlist.
//
// ffmpeg must be available on PATH.
func MP4ToHLS(videoID, srcPath string) (string, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return "", fmt.Errorf("source video not found: %w", err)
	}

	outDir := filepath.Join(ProcessedDir, videoID, HLSDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("could not create output dir %q: %w", outDir, err)
	}

	for _, r := range renditions {
		if err := transcodeRendition(srcPath, outDir, r); err != nil {
			return "", fmt.Errorf("rendition %s: %w", r.name, err)
		}
	}

	master := filepath.Join(outDir, "master.m3u8")
	if err := writeMasterPlaylist(master, renditions); err != nil {
		return "", err
	}

	return master, nil
}

// transcodeRendition produces a single quality level into hlsDir/<name>/.
func transcodeRendition(srcPath, hlsDir string, r rendition) error {
	outDir := filepath.Join(hlsDir, r.name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("could not create rendition dir %q: %w", outDir, err)
	}

	playlist := filepath.Join(outDir, "playlist.m3u8")
	// Chunks are named segment000.ts, segment001.ts, ...
	segmentPattern := filepath.Join(outDir, "segment%03d.ts")

	args := []string{
		"-y",          // overwrite existing output without prompting
		"-i", srcPath, // input

		// Scale to the target height while preserving aspect ratio. -2 keeps the
		// width divisible by 2, which H.264 requires.
		"-vf", fmt.Sprintf("scale=-2:%d", r.height),

		// Video: H.264, reasonable quality/size tradeoff.
		"-c:v", "libx264",
		"-profile:v", "main",
		"-crf", "20",
		"-maxrate", r.videoBitrate,
		"-bufsize", r.videoBitrate,
		"-preset", "veryfast",
		"-sc_threshold", "0", // disable scene-cut keyframes so GOPs stay regular
		"-g", "48", // keyframe every 48 frames (~2s at 24fps) for clean segments

		// Audio: AAC stereo.
		"-c:a", "aac",
		"-ar", "48000",
		"-b:a", r.audioBitrate,

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
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return nil
}

// writeMasterPlaylist writes the top-level playlist that points at each
// rendition's own playlist. Paths are relative, so the file keeps working
// wherever the hls folder is served from.
func writeMasterPlaylist(path string, rs []rendition) error {
	body := "#EXTM3U\n#EXT-X-VERSION:3\n"
	for _, r := range rs {
		body += fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n%s/playlist.m3u8\n",
			r.bandwidth, r.resolution, r.name,
		)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("could not write master playlist %q: %w", path, err)
	}
	return nil
}
