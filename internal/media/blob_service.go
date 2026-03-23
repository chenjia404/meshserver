package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"meshserver/internal/config"
	"meshserver/internal/repository"
	"meshserver/internal/storage"
)

// BlobService saves and deduplicates attachment binaries.
type BlobService struct {
	blobs          repository.BlobRepository
	mediaRepo      repository.MediaRepository
	storage        *storage.LocalBlobStore
	maxUploadBytes int64
	ipfsCfg        config.IPFSConfig
}

// NewBlobService creates a blob service bound to local storage.
// ipfsCfg 用於計算檔案附件之 UnixFS CID（與設定 ipfs.chunker、hash_function 等一致）。
func NewBlobService(blobs repository.BlobRepository, mediaRepo repository.MediaRepository, storage *storage.LocalBlobStore, maxUploadBytes int64, ipfsCfg config.IPFSConfig) *BlobService {
	return &BlobService{
		blobs:          blobs,
		mediaRepo:      mediaRepo,
		storage:        storage,
		maxUploadBytes: maxUploadBytes,
		ipfsCfg:        ipfsCfg,
	}
}

// Put stores attachment content, deduplicates blobs, and creates a media object.
func (s *BlobService) Put(ctx context.Context, r io.Reader, meta PutBlobInput) (*PutBlobResult, error) {
	payload, err := io.ReadAll(io.LimitReader(r, s.maxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read blob payload: %w", err)
	}
	if int64(len(payload)) == 0 {
		return nil, fmt.Errorf("empty blob payload")
	}
	if int64(len(payload)) > s.maxUploadBytes {
		return nil, fmt.Errorf("blob exceeds max upload size")
	}

	sum := sha256.Sum256(payload)
	shaHex := hex.EncodeToString(sum[:])
	mimeType := strings.TrimSpace(meta.MIMEType)
	if mimeType == "" {
		mimeType = http.DetectContentType(payload)
	}

	blob, err := s.blobs.GetBySHA256(ctx, shaHex)
	dedupHit := err == nil
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}

	if blob == nil {
		relativePath := filepath.Join(shaHex[0:2], shaHex[2:4], shaHex)
		if err := s.storage.Write(relativePath, payload); err != nil {
			return nil, err
		}
		blob, err = s.blobs.CreateBlob(ctx, repository.CreateBlobInput{
			BlobID:      newBlobID(),
			SHA256:      shaHex,
			Size:        uint64(len(payload)),
			MIMEType:    mimeType,
			StoragePath: relativePath,
			RefCount:    0,
		})
		if err != nil {
			return nil, err
		}
	} else if !s.storage.Exists(blob.StoragePath) {
		if err := s.storage.Write(blob.StoragePath, payload); err != nil {
			return nil, err
		}
	}

	width, height := detectImageSize(payload, meta.Kind)
	var fileCID string
	if meta.Kind == KindFile {
		cidStr, err := ComputeUnixFSFileCID(&s.ipfsCfg, payload)
		if err != nil {
			return nil, fmt.Errorf("compute file cid: %w", err)
		}
		fileCID = cidStr
	}
	mediaObject, err := s.mediaRepo.CreateMedia(ctx, repository.CreateMediaInput{
		MediaID:      newMediaID(),
		BlobID:       blob.ID,
		Kind:         string(meta.Kind),
		OriginalName: meta.OriginalName,
		MIMEType:     mimeType,
		Size:         uint64(len(payload)),
		Width:        width,
		Height:       height,
		CreatedBy:    meta.CreatedBy,
		FileCID:      fileCID,
	})
	if err != nil {
		return nil, err
	}

	return &PutBlobResult{
		Blob:     blob,
		Media:    mediaObject,
		DedupHit: dedupHit,
	}, nil
}

// StatBySHA256 returns blob metadata for a digest.
func (s *BlobService) StatBySHA256(ctx context.Context, sha256 string) (*Blob, error) {
	return s.blobs.GetBySHA256(ctx, sha256)
}

// Open opens a stored blob by relative path.
func (s *BlobService) Open(path string) (io.ReadCloser, error) {
	return s.storage.Open(path)
}

func detectImageSize(payload []byte, kind Kind) (*uint32, *uint32) {
	if kind != KindImage {
		return nil, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return nil, nil
	}
	width := uint32(cfg.Width)
	height := uint32(cfg.Height)
	return &width, &height
}

func newBlobID() string {
	return newGeneratedID("blob")
}

func newMediaID() string {
	return newGeneratedID("media")
}

func newGeneratedID(prefix string) string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, timeNowNano())
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buf))
}

func timeNowNano() int64 {
	return time.Now().UTC().UnixNano()
}
