package media

import (
	"time"

	"meshserver/internal/repository"
)

// Kind describes attachment kind.
type Kind string

const (
	KindImage Kind = "image"
	KindFile  Kind = "file"
)

// Blob is the deduplicated physical object metadata.
type Blob = repository.Blob

// MediaObject is a logical attachment object used by messages.
type MediaObject = repository.MediaObject

// SaveUploadedBlobInput is the input used by the media service.
type SaveUploadedBlobInput struct {
	Kind         Kind
	OriginalName string
	MIMEType     string
	Content      []byte
	CreatedBy    uint64
}

// PutBlobInput is the lower-level storage input.
type PutBlobInput struct {
	Kind         Kind
	OriginalName string
	MIMEType     string
	CreatedBy    uint64
}

// PutBlobResult captures the dedupe result and created metadata.
type PutBlobResult struct {
	Blob     *Blob
	Media    *MediaObject
	DedupHit bool
}

// AttachmentRef is a convenience DTO for message hydration.
type AttachmentRef struct {
	CreatedAt time.Time
}
