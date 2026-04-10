package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"meshserver/internal/config"
	"meshserver/internal/message"
	"meshserver/internal/repository"
	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
)

// DirectMessagingService 中心化私聊（1:1），持久化於 MySQL。
type DirectMessagingService interface {
	OpenConversation(ctx context.Context, userID uint64, peerUserID string) (*sessionv1.OpenDirectConversationResp, error)
	ListConversations(ctx context.Context, userID uint64) (*sessionv1.ListDirectConversationsResp, error)
	// SendDirectMessage 回傳 ACK、若需向接收方推播則 non-nil event，以及接收方內部 user id（重複發送時 event 為 nil）。
	SendDirectMessage(ctx context.Context, senderUserID uint64, req *sessionv1.SendDirectMessageReq) (*sessionv1.SendDirectMessageAck, *sessionv1.DirectMessageEvent, uint64, error)
	// AckDirectMessage 最後一個返回值為發送方內部 user id，僅在需推送 DIRECT_PEER_ACK_EVENT 時非 0。
	AckDirectMessage(ctx context.Context, recipientUserID uint64, req *sessionv1.AckDirectMessageReq) (*sessionv1.AckDirectMessageResp, *sessionv1.DirectPeerAckEvent, uint64, error)
	SyncDirectMessages(ctx context.Context, userID uint64, req *sessionv1.SyncDirectMessagesReq) (*sessionv1.SyncDirectMessagesResp, error)
}

type directMessagingService struct {
	cfg    *config.Config
	users  repository.UserRepository
	direct repository.DirectChatRepository
}

// NewDirectMessagingService 建立私聊服務。
func NewDirectMessagingService(cfg *config.Config, users repository.UserRepository, direct repository.DirectChatRepository) DirectMessagingService {
	return &directMessagingService{cfg: cfg, users: users, direct: direct}
}

func messageTypeToProto(t message.Type) sessionv1.MessageType {
	switch t {
	case message.TypeImage:
		return sessionv1.MessageType_IMAGE
	case message.TypeFile:
		return sessionv1.MessageType_FILE
	case message.TypeSystem:
		return sessionv1.MessageType_SYSTEM
	default:
		return sessionv1.MessageType_TEXT
	}
}

func protoToMessageType(t sessionv1.MessageType) message.Type {
	switch t {
	case sessionv1.MessageType_IMAGE:
		return message.TypeImage
	case sessionv1.MessageType_FILE:
		return message.TypeFile
	case sessionv1.MessageType_SYSTEM:
		return message.TypeSystem
	default:
		return message.TypeText
	}
}

func otherParticipant(conv *repository.DirectConversation, self uint64) uint64 {
	if conv.UserLowID == self {
		return conv.UserHighID
	}
	return conv.UserLowID
}

func (s *directMessagingService) OpenConversation(ctx context.Context, userID uint64, peerUserID string) (*sessionv1.OpenDirectConversationResp, error) {
	peer := strings.TrimSpace(peerUserID)
	if peer == "" {
		return nil, ErrInvalidMessage
	}
	u, err := s.users.GetByUserID(ctx, peer)
	if err != nil {
		return nil, err
	}
	if u.ID == userID {
		return nil, ErrInvalidMessage
	}
	conv, err := s.direct.GetOrCreateConversation(ctx, userID, u.ID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.OpenDirectConversationResp{
		Ok:             true,
		ConversationId: conv.ConversationID,
		PeerUserId:     u.UserID,
		LastSeq:        conv.LastSeq,
		Message:        "ok",
	}, nil
}

func (s *directMessagingService) ListConversations(ctx context.Context, userID uint64) (*sessionv1.ListDirectConversationsResp, error) {
	items, err := s.direct.ListConversationsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]*sessionv1.DirectConversationSummary, 0, len(items))
	for _, it := range items {
		out = append(out, &sessionv1.DirectConversationSummary{
			ConversationId:    it.ConversationID,
			PeerUserId:        it.PeerUserID,
			PeerDisplayName:   it.PeerDisplayName,
			LastSeq:           it.LastSeq,
			LastMessageAtMs:   it.LastMessageAtMs,
		})
	}
	return &sessionv1.ListDirectConversationsResp{Conversations: out}, nil
}

func (s *directMessagingService) SendDirectMessage(ctx context.Context, senderUserID uint64, req *sessionv1.SendDirectMessageReq) (*sessionv1.SendDirectMessageAck, *sessionv1.DirectMessageEvent, uint64, error) {
	clientMsgID := strings.TrimSpace(req.ClientMsgId)
	if clientMsgID == "" {
		return nil, nil, 0, ErrInvalidMessage
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, nil, 0, ErrInvalidMessage
	}
	if s.cfg != nil && s.cfg.MaxTextLen > 0 && len([]rune(text)) > s.cfg.MaxTextLen {
		return nil, nil, 0, ErrInvalidMessage
	}
	mt := protoToMessageType(req.MessageType)
	if mt != message.TypeText {
		return nil, nil, 0, ErrInvalidMessage
	}

	var conv *repository.DirectConversation
	var recipientID uint64
	cid := strings.TrimSpace(req.ConversationId)
	if cid != "" {
		c, err := s.direct.GetConversationByExternalID(ctx, cid)
		if err != nil {
			return nil, nil, 0, err
		}
		if senderUserID != c.UserLowID && senderUserID != c.UserHighID {
			return nil, nil, 0, ErrForbidden
		}
		conv = c
		recipientID = otherParticipant(c, senderUserID)
	} else {
		to := strings.TrimSpace(req.ToUserId)
		if to == "" {
			return nil, nil, 0, ErrInvalidMessage
		}
		u, err := s.users.GetByUserID(ctx, to)
		if err != nil {
			return nil, nil, 0, err
		}
		conv, err = s.direct.GetOrCreateConversation(ctx, senderUserID, u.ID)
		if err != nil {
			return nil, nil, 0, err
		}
		recipientID = u.ID
	}

	existing, err := s.direct.GetDirectMessageByClientMsgID(ctx, conv.ConversationID, senderUserID, clientMsgID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, nil, 0, err
	}
	if existing != nil {
		nowMs := uint64(time.Now().UTC().UnixMilli())
		return &sessionv1.SendDirectMessageAck{
			Ok:             true,
			ConversationId: existing.ConversationID,
			ClientMsgId:    clientMsgID,
			MessageId:      existing.MessageID,
			Seq:            existing.Seq,
			ServerTimeMs:   nowMs,
			Message:        "stored",
		}, nil, 0, nil
	}

	msg, err := s.direct.CreateDirectMessage(ctx, repository.CreateDirectMessageInput{
		ConversationID:  conv.ConversationID,
		SenderUserID:    senderUserID,
		RecipientUserID: recipientID,
		ClientMsgID:     clientMsgID,
		MessageType:     mt,
		Text:            text,
	})
	if err != nil {
		if errors.Is(err, repository.ErrDirectChatForbidden) {
			return nil, nil, 0, ErrForbidden
		}
		return nil, nil, 0, err
	}

	nowMs := uint64(time.Now().UTC().UnixMilli())
	ack := &sessionv1.SendDirectMessageAck{
		Ok:             true,
		ConversationId: conv.ConversationID,
		ClientMsgId:    clientMsgID,
		MessageId:      msg.MessageID,
		Seq:            msg.Seq,
		ServerTimeMs:   nowMs,
		Message:        "stored",
	}

	ev := &sessionv1.DirectMessageEvent{
		ConversationId: msg.ConversationID,
		MessageId:      msg.MessageID,
		Seq:            msg.Seq,
		FromUserId:     msg.SenderExternalID,
		ToUserId:       msg.RecipientExternalID,
		MessageType:    messageTypeToProto(msg.MessageType),
		Text:           msg.Text,
		CreatedAtMs:    msg.CreatedAtMs,
	}
	return ack, ev, recipientID, nil
}

func (s *directMessagingService) AckDirectMessage(ctx context.Context, recipientUserID uint64, req *sessionv1.AckDirectMessageReq) (*sessionv1.AckDirectMessageResp, *sessionv1.DirectPeerAckEvent, uint64, error) {
	mid := strings.TrimSpace(req.MessageId)
	if mid == "" {
		return nil, nil, 0, ErrInvalidMessage
	}
	nowMs := uint64(time.Now().UTC().UnixMilli())
	already, senderUID, convID, err := s.direct.AckDirectMessage(ctx, mid, recipientUserID, nowMs)
	if err != nil {
		if errors.Is(err, repository.ErrDirectChatForbidden) {
			return nil, nil, 0, ErrForbidden
		}
		return nil, nil, 0, err
	}
	recv, err := s.users.GetByID(ctx, recipientUserID)
	if err != nil {
		return nil, nil, 0, err
	}
	resp := &sessionv1.AckDirectMessageResp{Ok: true, MessageId: mid, Message: "ok"}
	if already {
		return resp, nil, 0, nil
	}
	return resp, &sessionv1.DirectPeerAckEvent{
		ConversationId: convID,
		MessageId:      mid,
		AckedAtMs:      nowMs,
		PeerUserId:     recv.UserID,
	}, senderUID, nil
}

func (s *directMessagingService) SyncDirectMessages(ctx context.Context, userID uint64, req *sessionv1.SyncDirectMessagesReq) (*sessionv1.SyncDirectMessagesResp, error) {
	cid := strings.TrimSpace(req.ConversationId)
	if cid == "" {
		return nil, ErrInvalidMessage
	}
	conv, err := s.direct.GetConversationByExternalID(ctx, cid)
	if err != nil {
		return nil, err
	}
	if userID != conv.UserLowID && userID != conv.UserHighID {
		return nil, ErrForbidden
	}
	limit := req.Limit
	if limit == 0 {
		limit = 50
	}
	items, nextSeq, hasMore, err := s.direct.ListPendingDirectMessages(ctx, cid, userID, req.AfterSeq, limit)
	if err != nil {
		return nil, err
	}
	events := make([]*sessionv1.DirectMessageEvent, 0, len(items))
	for _, m := range items {
		events = append(events, &sessionv1.DirectMessageEvent{
			ConversationId: m.ConversationID,
			MessageId:      m.MessageID,
			Seq:            m.Seq,
			FromUserId:     m.SenderExternalID,
			ToUserId:       m.RecipientExternalID,
			MessageType:    messageTypeToProto(m.MessageType),
			Text:           m.Text,
			CreatedAtMs:    m.CreatedAtMs,
		})
	}
	return &sessionv1.SyncDirectMessagesResp{
		ConversationId: cid,
		Messages:       events,
		NextAfterSeq:   nextSeq,
		HasMore:        hasMore,
	}, nil
}
