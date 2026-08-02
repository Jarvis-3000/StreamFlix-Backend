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

// UploadToServer uploads an entire local HLS folder (the .m3u8 playlist plus
// its .ts chunks) to the Supabase S3 bucket and returns the public URL of the
// playlist.
//
// The folder is uploaded under a key prefix derived from its own name, e.g.
//
//	./processed/movie/  ->  <bucket>/movie/index.m3u8, <bucket>/movie/segment_000.ts, ...
//
// so the returned playlist URL points at the same directory layout the player
// expects (relative .ts references in the playlist keep working).
//
// Supabase S3 config is read from the environment:
//
//	SUPABASE_S3_ENDPOINT, SUPABASE_S3_REGION,
//	SUPABASE_S3_ACCESS_KEY_ID, SUPABASE_S3_SECRET_ACCESS_KEY,
//	SUPABASE_BUCKET
func UploadToServer(ctx context.Context, folderPath string) (string, error) {
	cfg, err := loadSupabaseConfig()
	if err != nil {
		return "", err
	}

	info, err := os.Stat(folderPath)
	if err != nil {
		return "", fmt.Errorf("folder not found: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", folderPath)
	}

	// Use the folder's own name as the key prefix in the bucket.
	prefix := filepath.Base(folderPath)

	client := newSupabaseS3Client(cfg)

	// Walk the folder and upload every file, preserving relative paths under
	// the prefix. HLS folders are flat in practice, but WalkDir handles any
	// nesting correctly.
	var playlistKey string
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
		key := prefix + "/" + filepath.ToSlash(rel)

		if err := uploadFile(ctx, client, cfg.bucket, key, path); err != nil {
			return fmt.Errorf("uploading %q: %w", rel, err)
		}
		fmt.Printf("uploaded %s -> %s\n", rel, key)

		if strings.HasSuffix(strings.ToLower(rel), ".m3u8") {
			playlistKey = key
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if playlistKey == "" {
		return "", fmt.Errorf("no .m3u8 playlist found in %q", folderPath)
	}

	return publicURL(cfg, playlistKey), nil
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
