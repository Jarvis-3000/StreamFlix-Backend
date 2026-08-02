// Package media is the Elasticsearch repository for media metadata. One
// document represents one uploaded media item. The video files themselves live
// in object storage; only searchable metadata is stored here.
package media

import "time"

// Visibility controls who can see a media item.
type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private"
)

// Status tracks a media item through the processing pipeline.
type Status string

const (
	StatusUploaded   Status = "uploaded"
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

// Document is a single media item's metadata as stored in Elasticsearch. It is
// intentionally flat and additive: new optional fields (views, likes,
// processing progress, live-stream metadata) can be appended over time without
// breaking existing documents, provided the index mapping is extended to match.
//
// The video bytes are never stored here — only URLs pointing at object storage
// and the metadata needed to search and list media.
type Document struct {
	ID          string     `json:"id"`
	CreatorID   string     `json:"creator_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Visibility  Visibility `json:"visibility"`
	Status      Status     `json:"status"`

	Thumbnail string `json:"thumbnail"`
	URL       string `json:"url"`

	// Duration is the media length in seconds.
	Duration int `json:"duration"`

	// Qualities lists the available rendition labels, e.g. ["720p","1080p"].
	Qualities []string `json:"qualities"`

	// Tags are free-form keyword labels for filtering and discovery.
	Tags []string `json:"tags"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
