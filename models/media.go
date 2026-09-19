package models

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
	StatusUploading  Status = "uploading"
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

// Media is the metadata document for one video.
//
// Every field carries a bson tag alongside its json one: without it the driver
// derives the BSON name by lowercasing the Go field ("creatorid"), which would
// not match the indexes or the queries that filter on "creator_id". The bson
// names are kept identical to the json ones so a document reads the same in the
// database as it does on the wire.
type Media struct {
	ID          string     `json:"id" bson:"id"`
	CreatorID   string     `json:"creator_id" bson:"creator_id"`
	Title       string     `json:"title" bson:"title"`
	Description string     `json:"description" bson:"description"`
	Visibility  Visibility `json:"visibility" bson:"visibility"`
	Status      Status     `json:"status" bson:"status"`
	Category    string     `json:"category" bson:"category"`
	Thumbnail   string     `json:"thumbnail" bson:"thumbnail"`
	URL         string     `json:"url" bson:"url"`
	Duration    int        `json:"duration" bson:"duration"`
	Qualities   []string   `json:"qualities" bson:"qualities"` // rendition labels, e.g. ["720p","1080p"].
	Tags        []string   `json:"tags" bson:"tags"`           // free-form keyword labels for filtering and discovery.
	CreatedAt   time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" bson:"updated_at"`
}

// EntityID implements mongodb.Entity, letting the generic repository address
// this document by id without knowing which field holds it. The repository
// copies this value into the document's _id.
func (m Media) EntityID() string { return m.ID }

// Upload is the record of a video file received by POST /upload and parked on
// local disk. It exists so the later media.create call can prove the video id
// it was given is real and learn where the bytes are.
//
// It does not track whether a media document was built from it: the document id
// is the video id and lands in MongoDB's unique _id, so a duplicate insert is
// rejected outright and publishing the same upload twice is already impossible
// without a second flag here to keep in sync.
//
// It is deliberately not the media document: an upload is raw bytes plus the
// little we know from the multipart header, with no user-supplied metadata.
type Upload struct {
	// VideoID is the id handed back to the client by /upload. It is also the
	// id of the media document eventually created from this upload.
	VideoID string `json:"videoId"`

	// Filename is the original name from the multipart header, kept for
	// display only — it never determines where the file is stored.
	Filename string `json:"filename"`

	// Path is where the file actually lives on local disk.
	Path string `json:"path"`

	// Size is the number of bytes written to disk.
	Size int64 `json:"size"`

	CreatedAt time.Time `json:"createdAt"`
}

type MediaInput struct {
	VideoID     string     `json:"video_id"`
	CreatorID   string     `json:"creator_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Visibility  Visibility `json:"visibility"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
}

// MediaId addresses a single media document by id.
type MediaId struct {
	ID string `json:"id"`
}

// MediaListRequest is the request payload for media.list: the metadata a user fills
// in on the upload form, plus the video id that ties it to an earlier upload.
type MediaListRequest struct {
	CreatorID string `json:"creator_id"`
}

// Media metadata limits. These bound what a client may send so a single
// request can't write an unreasonably large document.
const (
	MediaTitleMaxLen       = 100
	MediaDescriptionMaxLen = 5000
	MediaMaxTags           = 30
	MediaTagMaxLen         = 50
)

// MediaCategories is the closed set of categories a media item may declare.
// Category is a keyword field backing navigation and filtering, so it is
// validated against this list rather than accepted free-form.
var MediaCategories = []string{
	"film",
	"music",
	"gaming",
	"sports",
	"news",
	"education",
	"entertainment",
	"technology",
	"travel",
	"other",
}

// IsValidMediaCategory reports whether category is one of MediaCategories.
func IsValidMediaCategory(category string) bool {
	for _, c := range MediaCategories {
		if c == category {
			return true
		}
	}
	return false
}
