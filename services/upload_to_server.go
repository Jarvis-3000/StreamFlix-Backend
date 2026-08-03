package services

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// UploadHLS uploads a local HLS folder — the master playlist plus each
// rendition's playlist and .ts chunks — to the Supabase S3 bucket under
// <videoID>/hls/, and returns the public URL of the master playlist.
//
//	./processed/<videoID>/hls/  ->  <bucket>/<videoID>/hls/master.m3u8
//	                                <bucket>/<videoID>/hls/480p/playlist.m3u8
//	                                <bucket>/<videoID>/hls/480p/segment000.ts
//
// The bucket mirrors the local tree exactly, so the relative references inside
// the playlists keep resolving once served.
//
// Supabase S3 config is read from the environment:
//
//	SUPABASE_S3_ENDPOINT, SUPABASE_S3_REGION,
//	SUPABASE_S3_ACCESS_KEY_ID, SUPABASE_S3_SECRET_ACCESS_KEY,
//	SUPABASE_BUCKET
func UploadHLS(ctx context.Context, videoID, folderPath string) (string, error) {
	prefix := videoID + "/" + HLSDir

	keys, err := uploadFolder(ctx, folderPath, prefix)
	if err != nil {
		return "", err
	}

	cfg, err := loadSupabaseConfig()
	if err != nil {
		return "", err
	}

	// The master playlist is the URL players are pointed at; the rendition
	// playlists are reached through it.
	masterKey, ok := keys["master.m3u8"]
	if !ok {
		return "", fmt.Errorf("no master.m3u8 found in %q", folderPath)
	}
	return publicURL(cfg, masterKey), nil
}

// UploadThumbnails uploads a local thumbnails folder to the Supabase S3 bucket
// under <videoID>/thumbnails/ and returns the public URLs of the stills, in the
// same order as the localPaths given.
//
// Ordering matters to the caller: the first still is the one recorded on the
// media document as the poster, so the result cannot be a bare directory walk
// (which would come back in whatever order the filesystem yields).
func UploadThumbnails(ctx context.Context, videoID, folderPath string, localPaths []string) ([]string, error) {
	prefix := videoID + "/" + ThumbnailsDir

	keys, err := uploadFolder(ctx, folderPath, prefix)
	if err != nil {
		return nil, err
	}

	cfg, err := loadSupabaseConfig()
	if err != nil {
		return nil, err
	}

	urls := make([]string, 0, len(localPaths))
	for _, p := range localPaths {
		rel, err := filepath.Rel(folderPath, p)
		if err != nil {
			return nil, err
		}
		key, ok := keys[filepath.ToSlash(rel)]
		if !ok {
			return nil, fmt.Errorf("thumbnail %q was not uploaded", rel)
		}
		urls = append(urls, publicURL(cfg, key))
	}
	return urls, nil
}

// uploadFolder walks folderPath and uploads every file beneath it to the bucket
// under keyPrefix, preserving relative paths. It returns a map from each file's
// slash-separated path relative to folderPath to the S3 key it was written to.
func uploadFolder(ctx context.Context, folderPath, keyPrefix string) (map[string]string, error) {
	cfg, err := loadSupabaseConfig()
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(folderPath)
	if err != nil {
		return nil, fmt.Errorf("folder not found: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", folderPath)
	}

	client := newSupabaseS3Client(cfg)
	keys := make(map[string]string)

	err = filepath.WalkDir(folderPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(folderPath, path)
		if err != nil {
			return err
		}
		// S3 keys always use forward slashes, regardless of OS.
		rel = filepath.ToSlash(rel)
		key := keyPrefix + "/" + rel

		if err := uploadFile(ctx, client, cfg.bucket, key, path); err != nil {
			return fmt.Errorf("uploading %q: %w", rel, err)
		}
		fmt.Printf("uploaded %s -> %s\n", rel, key)

		keys[rel] = key
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no files to upload in %q", folderPath)
	}
	return keys, nil
}

// uploadFile streams a single local file to the given S3 key.
func uploadFile(ctx context.Context, client *s3.Client, bucket, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentTypeFor(path)),
	})
	return err
}

// contentTypeFor returns an appropriate Content-Type for HLS files so players
// and browsers interpret them correctly. Falls back to a generic binary type.
func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// supabaseConfig holds the S3 connection settings read from the environment.
type supabaseConfig struct {
	endpoint  string
	region    string
	accessKey string
	secretKey string
	bucket    string
}

// loadSupabaseConfig reads and validates the Supabase S3 settings from the env.
func loadSupabaseConfig() (supabaseConfig, error) {
	cfg := supabaseConfig{
		endpoint:  os.Getenv("SUPABASE_S3_ENDPOINT"),
		region:    os.Getenv("SUPABASE_S3_REGION"),
		accessKey: os.Getenv("SUPABASE_S3_ACCESS_KEY_ID"),
		secretKey: os.Getenv("SUPABASE_S3_SECRET_ACCESS_KEY"),
		bucket:    os.Getenv("SUPABASE_BUCKET"),
	}

	var missing []string
	if cfg.endpoint == "" {
		missing = append(missing, "SUPABASE_S3_ENDPOINT")
	}
	if cfg.region == "" {
		missing = append(missing, "SUPABASE_S3_REGION")
	}
	if cfg.accessKey == "" {
		missing = append(missing, "SUPABASE_S3_ACCESS_KEY_ID")
	}
	if cfg.secretKey == "" {
		missing = append(missing, "SUPABASE_S3_SECRET_ACCESS_KEY")
	}
	if cfg.bucket == "" {
		missing = append(missing, "SUPABASE_BUCKET")
	}
	if len(missing) > 0 {
		return supabaseConfig{}, fmt.Errorf("missing Supabase S3 env vars: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// newSupabaseS3Client builds an S3 client pointed at Supabase's S3-compatible
// endpoint. Supabase requires path-style addressing (bucket in the path, not
// the host).
func newSupabaseS3Client(cfg supabaseConfig) *s3.Client {
	return s3.New(s3.Options{
		Region:       cfg.region,
		BaseEndpoint: aws.String(cfg.endpoint),
		UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.accessKey, cfg.secretKey, "",
		),
	})
}

// publicURL builds the public playback URL for an uploaded object key.
//
// Supabase serves public bucket objects from a stable path derived from the S3
// endpoint host, so we swap the "/storage/v1/s3" suffix for the public
// "/storage/v1/object/public/<bucket>/<key>" path.
func publicURL(cfg supabaseConfig, key string) string {
	base := strings.TrimSuffix(cfg.endpoint, "/storage/v1/s3")
	return fmt.Sprintf("%s/storage/v1/object/public/%s/%s", base, cfg.bucket, key)
}
