package service

import (
	"context"

	"meshserver/internal/channel"
	"meshserver/internal/repository"
	"meshserver/internal/space"
)

// DirectoryService lists visible spaces and channels for a user.
type DirectoryService interface {
	ListSpaces(ctx context.Context, userID uint64) ([]*space.Space, error)
	ListChannels(ctx context.Context, userID uint64, spaceID uint32) ([]*channel.Channel, error)
}

type directoryService struct {
	spaces   repository.SpaceRepository
	channels repository.ChannelRepository
}

// NewDirectoryService creates a directory service.
func NewDirectoryService(spaces repository.SpaceRepository, channels repository.ChannelRepository) DirectoryService {
	return &directoryService{spaces: spaces, channels: channels}
}

func (s *directoryService) ListSpaces(ctx context.Context, userID uint64) ([]*space.Space, error) {
	return s.spaces.ListByUserID(ctx, userID)
}

func (s *directoryService) ListChannels(ctx context.Context, userID uint64, spaceID uint32) ([]*channel.Channel, error) {
	return s.channels.ListBySpaceIDForUser(ctx, spaceID, userID)
}
