package service

import (
	"bytes"
	"context"
	"io"

	"meshserver/internal/media"
	"meshserver/internal/repository"
)

// MediaService stores and resolves attachment metadata.
type MediaService interface {
	SaveUploadedBlob(ctx context.Context, in media.SaveUploadedBlobInput) (*media.MediaObject, error)
	GetMediaByID(ctx context.Context, mediaID string) (*media.MediaObject, error)
	DownloadMediaByID(ctx context.Context, mediaID string) (*media.MediaObject, []byte, error)
}

type mediaService struct {
	blobService *media.BlobService
	mediaRepo   repository.MediaRepository
}

// NewMediaService creates a media service.
func NewMediaService(blobService *media.BlobService, mediaRepo repository.MediaRepository) MediaService {
	return &mediaService{
		blobService: blobService,
		mediaRepo:   mediaRepo,
	}
}

func (s *mediaService) SaveUploadedBlob(ctx context.Context, in media.SaveUploadedBlobInput) (*media.MediaObject, error) {
	result, err := s.blobService.Put(ctx, bytes.NewReader(in.Content), media.PutBlobInput{
		Kind:         in.Kind,
		OriginalName: in.OriginalName,
		MIMEType:     in.MIMEType,
		CreatedBy:    in.CreatedBy,
	})
	if err != nil {
		return nil, err
	}
	return result.Media, nil
}

func (s *mediaService) GetMediaByID(ctx context.Context, mediaID string) (*media.MediaObject, error) {
	return s.mediaRepo.GetByMediaID(ctx, mediaID)
}

func (s *mediaService) DownloadMediaByID(ctx context.Context, mediaID string) (*media.MediaObject, []byte, error) {
	item, err := s.mediaRepo.GetByMediaID(ctx, mediaID)
	if err != nil {
		return nil, nil, err
	}
	rc, err := s.blobService.Open(item.StoragePath)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}
	return item, content, nil
}
