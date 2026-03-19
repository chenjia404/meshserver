package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"meshserver/internal/channel"
	"meshserver/internal/config"
	"meshserver/internal/media"
	"meshserver/internal/message"
	"meshserver/internal/repository"
	"meshserver/internal/space"
)

var (
	// ErrForbidden is returned for permission failures.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidMessage is returned for validation failures.
	ErrInvalidMessage = errors.New("invalid message")
)

// AttachmentInput is the service input for each attachment.
type AttachmentInput struct {
	MediaID      string
	OriginalName string
	MIMEType     string
	Content      []byte
}

// SendMessageInput holds a new outbound message request.
type SendMessageInput struct {
	ChannelID   uint32
	ClientMsgID string
	MessageType message.Type
	Text        string
	Images      []AttachmentInput
	Files       []AttachmentInput
}

// MessagingService manages channel messaging and sync cursors.
type MessagingService interface {
	SendMessage(ctx context.Context, userID uint64, in SendMessageInput) (*message.Message, error)
	SyncChannel(ctx context.Context, userID uint64, channelID uint32, afterSeq uint64, limit uint32) ([]*message.Message, uint64, bool, error)
	AckDelivered(ctx context.Context, userID uint64, channelID uint32, seq uint64) error
	UpdateRead(ctx context.Context, userID uint64, channelID uint32, seq uint64) error
}

type messagingService struct {
	cfg      *config.Config
	channels repository.ChannelRepository
	messages repository.MessageRepository
	reads    repository.ReadCursorRepository
	media    MediaService
}

// NewMessagingService creates a messaging service.
func NewMessagingService(cfg *config.Config, channels repository.ChannelRepository, messages repository.MessageRepository, reads repository.ReadCursorRepository, media MediaService) MessagingService {
	return &messagingService{
		cfg:      cfg,
		channels: channels,
		messages: messages,
		reads:    reads,
		media:    media,
	}
}

func (s *messagingService) SendMessage(ctx context.Context, userID uint64, in SendMessageInput) (*message.Message, error) {
	ch, err := s.channels.GetByChannelID(ctx, in.ChannelID)
	if err != nil {
		return nil, err
	}
	perm, err := s.channels.GetPermission(ctx, in.ChannelID, userID)
	if err != nil {
		return nil, err
	}
	if !perm.CanView {
		return nil, ErrForbidden
	}

	if err := s.validateMessage(ctx, ch, perm, userID, in); err != nil {
		return nil, err
	}

	attachments, err := s.resolveAttachments(ctx, userID, in)
	if err != nil {
		return nil, err
	}

	item, err := s.messages.Create(ctx, repository.CreateMessageInput{
		ChannelID:       in.ChannelID,
		SenderUserID:    userID,
		ClientMsgID:     in.ClientMsgID,
		MessageType:     inferMessageType(in),
		Text:            strings.TrimSpace(in.Text),
		AttachmentMedia: attachments,
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *messagingService) SyncChannel(ctx context.Context, userID uint64, channelID uint32, afterSeq uint64, limit uint32) ([]*message.Message, uint64, bool, error) {
	member, err := s.channels.IsUserMember(ctx, channelID, userID)
	if err != nil {
		return nil, 0, false, err
	}
	if !member {
		return nil, 0, false, ErrForbidden
	}

	if limit == 0 {
		limit = s.cfg.DefaultSyncLimit
	}
	if limit > s.cfg.MaxSyncLimit {
		limit = s.cfg.MaxSyncLimit
	}

	items, err := s.messages.ListAfterSeq(ctx, channelID, afterSeq, limit+1)
	if err != nil {
		return nil, 0, false, err
	}

	hasMore := uint32(len(items)) > limit
	if hasMore {
		items = items[:limit]
	}

	nextAfterSeq := afterSeq
	if len(items) > 0 {
		nextAfterSeq = items[len(items)-1].Seq
	}
	return items, nextAfterSeq, hasMore, nil
}

func (s *messagingService) AckDelivered(ctx context.Context, userID uint64, channelID uint32, seq uint64) error {
	member, err := s.channels.IsUserMember(ctx, channelID, userID)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return s.reads.UpsertDeliveredSeq(ctx, userID, channelID, seq)
}

func (s *messagingService) UpdateRead(ctx context.Context, userID uint64, channelID uint32, seq uint64) error {
	member, err := s.channels.IsUserMember(ctx, channelID, userID)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return s.reads.UpsertReadSeq(ctx, userID, channelID, seq)
}

func (s *messagingService) validateMessage(ctx context.Context, ch *channel.Channel, perm *channel.Permission, userID uint64, in SendMessageInput) error {
	clientMsgID := strings.TrimSpace(in.ClientMsgID)
	text := strings.TrimSpace(in.Text)

	if len(clientMsgID) < 8 || len(clientMsgID) > 64 {
		return fmt.Errorf("%w: client_msg_id must be 8-64 chars", ErrInvalidMessage)
	}
	if len(text) > s.cfg.MaxTextLen {
		return fmt.Errorf("%w: text too long", ErrInvalidMessage)
	}
	if len(in.Images) > s.cfg.MaxImagesPerMessage {
		return fmt.Errorf("%w: too many images", ErrInvalidMessage)
	}
	if len(in.Files) > s.cfg.MaxFilesPerMessage {
		return fmt.Errorf("%w: too many files", ErrInvalidMessage)
	}
	if len(in.Images) > 0 && len(in.Files) > 0 {
		return fmt.Errorf("%w: images and files cannot coexist", ErrInvalidMessage)
	}
	if text == "" && len(in.Images) == 0 && len(in.Files) == 0 {
		return fmt.Errorf("%w: empty message", ErrInvalidMessage)
	}

	switch {
	case len(in.Images) == 0 && len(in.Files) == 0:
		if inferMessageType(in) != message.TypeText {
			return fmt.Errorf("%w: invalid text message type", ErrInvalidMessage)
		}
	case len(in.Images) > 0:
		if !perm.CanSendImage {
			return ErrForbidden
		}
	case len(in.Files) > 0:
		if !perm.CanSendFile {
			return ErrForbidden
		}
	}

	if !perm.CanSendMessage {
		return ErrForbidden
	}

	if ch.Type == channel.TypeBroadcast && perm.Role != space.RoleOwner && perm.Role != space.RoleAdmin {
		return ErrForbidden
	}

	if ch.SlowModeSeconds > 0 {
		lastTime, err := s.messages.GetLastMessageTime(ctx, ch.ID, userID)
		if err != nil {
			return err
		}
		if lastTime != nil && time.Since(*lastTime) < time.Duration(ch.SlowModeSeconds)*time.Second {
			return fmt.Errorf("%w: slow mode active", ErrInvalidMessage)
		}
	}

	return nil
}

func (s *messagingService) resolveAttachments(ctx context.Context, userID uint64, in SendMessageInput) ([]uint64, error) {
	var source []AttachmentInput
	var kind media.Kind

	switch {
	case len(in.Images) > 0:
		source = in.Images
		kind = media.KindImage
	case len(in.Files) > 0:
		source = in.Files
		kind = media.KindFile
	default:
		return nil, nil
	}

	out := make([]uint64, 0, len(source))
	for _, item := range source {
		if item.MediaID != "" && len(item.Content) == 0 {
			obj, err := s.media.GetMediaByID(ctx, item.MediaID)
			if err != nil {
				return nil, err
			}
			out = append(out, obj.ID)
			continue
		}
		obj, err := s.media.SaveUploadedBlob(ctx, media.SaveUploadedBlobInput{
			Kind:         kind,
			OriginalName: item.OriginalName,
			MIMEType:     item.MIMEType,
			Content:      item.Content,
			CreatedBy:    userID,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, obj.ID)
	}
	return out, nil
}

func inferMessageType(in SendMessageInput) message.Type {
	switch {
	case len(in.Images) > 0:
		return message.TypeImage
	case len(in.Files) > 0:
		return message.TypeFile
	default:
		return message.TypeText
	}
}
