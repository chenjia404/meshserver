package session

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	proto "github.com/golang/protobuf/proto"
	"github.com/libp2p/go-libp2p/core/network"

	"meshserver/internal/auth"
	"meshserver/internal/channel"
	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	"meshserver/internal/message"
	"meshserver/internal/protocol"
	"meshserver/internal/repository"
	"meshserver/internal/service"
	"meshserver/internal/space"
)

// ConnSession tracks the state associated with one libp2p stream.
type ConnSession struct {
	stream          network.Stream
	writeMu         sync.Mutex
	manager         *Manager
	authenticated   bool
	hello           *sessionv1.Hello
	issuedChallenge *auth.Challenge
	authResult      *auth.Result
	subscribed      map[uint32]struct{}
}

// Manager owns libp2p stream sessions and channel subscriptions.
type Manager struct {
	logger        *slog.Logger
	authService   *auth.Service
	users         repository.UserRepository
	spaces        repository.SpaceRepository
	directory     service.DirectoryService
	messaging     service.MessagingService
	media         service.MediaService
	messages      repository.MessageRepository
	channels      repository.ChannelRepository
	nodePeerID    func() string
	nodeID        uint64
	blobURLBase   string
	mu            sync.RWMutex
	subscriptions map[uint32]map[*ConnSession]struct{}
}

// NewManager builds a session manager.
func NewManager(logger *slog.Logger, authService *auth.Service, users repository.UserRepository, spaces repository.SpaceRepository, directory service.DirectoryService, messaging service.MessagingService, media service.MediaService, messages repository.MessageRepository, channels repository.ChannelRepository, nodePeerID func() string, nodeID uint64, blobURLBase string) *Manager {
	return &Manager{
		logger:        logger,
		authService:   authService,
		users:         users,
		spaces:        spaces,
		directory:     directory,
		messaging:     messaging,
		media:         media,
		messages:      messages,
		channels:      channels,
		nodePeerID:    nodePeerID,
		nodeID:        nodeID,
		blobURLBase:   blobURLBase,
		subscriptions: make(map[uint32]map[*ConnSession]struct{}),
	}
}

// HandleStream serves a single inbound libp2p session stream.
func (m *Manager) HandleStream(s network.Stream) {
	sess := &ConnSession{
		stream:     s,
		manager:    m,
		subscribed: make(map[uint32]struct{}),
	}
	defer func() {
		m.unregisterSession(sess)
		_ = s.Close()
	}()

	m.logger.Info("stream connected", "remote_peer", s.Conn().RemotePeer().String())

	for {
		env, err := protocol.ReadEnvelope(s)
		if err != nil {
			if err != io.EOF {
				m.logger.Warn("stream read failed", "remote_peer", s.Conn().RemotePeer().String(), "error", err)
			}
			return
		}

		if err := m.dispatch(context.Background(), sess, env); err != nil {
			m.logger.Warn("session request failed", "msg_type", env.MsgType.String(), "error", err)
			_ = sess.writeError(env.RequestId, 400, err.Error())
		}
	}
}

// Authenticate is the exported authentication helper for testing and future extension.
func (m *Manager) Authenticate(ctx context.Context, sess *ConnSession, prove *sessionv1.AuthProve) error {
	if sess.issuedChallenge == nil {
		return fmt.Errorf("missing challenge")
	}
	result, err := m.authService.VerifyChallenge(ctx, auth.VerifyChallengeInput{
		ClientPeerID: prove.ClientPeerId,
		NodePeerID:   m.nodePeerID(),
		Nonce:        prove.Nonce,
		IssuedAt:     time.UnixMilli(int64(prove.IssuedAtMs)).UTC(),
		ExpiresAt:    time.UnixMilli(int64(prove.ExpiresAtMs)).UTC(),
		Signature:    prove.Signature,
		PublicKey:    prove.PublicKey,
	})
	if err != nil {
		return err
	}
	sess.authenticated = true
	sess.authResult = result
	return nil
}

// SubscribeChannel adds the connection session to a channel fanout list.
func (m *Manager) SubscribeChannel(sess *ConnSession, channelID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subscriptions[channelID]; !ok {
		m.subscriptions[channelID] = make(map[*ConnSession]struct{})
	}
	m.subscriptions[channelID][sess] = struct{}{}
	sess.subscribed[channelID] = struct{}{}
}

// DeliverMessage broadcasts an event to all currently subscribed sessions.
func (m *Manager) DeliverMessage(channelID uint32, msg *message.Message) {
	event := toMessageEvent(msg, m.blobURLBase)
	m.mu.RLock()
	targets := make([]*ConnSession, 0, len(m.subscriptions[channelID]))
	for sess := range m.subscriptions[channelID] {
		targets = append(targets, sess)
	}
	m.mu.RUnlock()

	for _, sess := range targets {
		if err := sess.write(sessionv1.MsgType_MESSAGE_EVENT, "", event); err != nil {
			m.logger.Warn("deliver message event failed", "channel_id", channelID, "error", err)
		}
	}
}

func (m *Manager) dispatch(ctx context.Context, sess *ConnSession, env *sessionv1.Envelope) error {
	switch env.MsgType {
	case sessionv1.MsgType_HELLO:
		var hello sessionv1.Hello
		if err := protocol.UnmarshalBody(env.Body, &hello); err != nil {
			return err
		}
		sess.hello = &hello
		challenge, err := m.authService.IssueChallenge(ctx, hello.ClientPeerId, m.nodePeerID())
		if err != nil {
			return err
		}
		sess.issuedChallenge = challenge
		return sess.write(sessionv1.MsgType_AUTH_CHALLENGE, env.RequestId, &sessionv1.AuthChallenge{
			NodePeerId:  challenge.NodePeerID,
			Nonce:       challenge.Nonce,
			IssuedAtMs:  uint64(challenge.IssuedAt.UnixMilli()),
			ExpiresAtMs: uint64(challenge.ExpiresAt.UnixMilli()),
			SessionHint: "meshserver-session",
		})
	case sessionv1.MsgType_AUTH_PROVE:
		var prove sessionv1.AuthProve
		if err := protocol.UnmarshalBody(env.Body, &prove); err != nil {
			return err
		}
		if err := m.Authenticate(ctx, sess, &prove); err != nil {
			return err
		}
		servers, err := m.directory.ListSpaces(ctx, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_AUTH_RESULT, env.RequestId, &sessionv1.AuthResult{
			Ok:          true,
			SessionId:   sess.authResult.SessionID,
			UserId:      sess.authResult.User.UserID,
			DisplayName: sess.authResult.User.DisplayName,
			Message:     sess.authResult.Message,
			Spaces:      toSpaceSummaries(servers),
		})
	case sessionv1.MsgType_PING:
		var ping sessionv1.Ping
		if err := protocol.UnmarshalBody(env.Body, &ping); err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_PONG, env.RequestId, &sessionv1.Pong{Nonce: ping.Nonce})
	}

	if !sess.authenticated || sess.authResult == nil || sess.authResult.User == nil {
		return fmt.Errorf("authentication required")
	}

	switch env.MsgType {
	case sessionv1.MsgType_LIST_SPACES_REQ:
		servers, err := m.directory.ListSpaces(ctx, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_LIST_SPACES_RESP, env.RequestId, &sessionv1.ListSpacesResp{
			Spaces: toSpaceSummaries(servers),
		})
	case sessionv1.MsgType_LIST_CHANNELS_REQ:
		var req sessionv1.ListChannelsReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		channels, err := m.directory.ListChannels(ctx, sess.authResult.User.ID, req.SpaceId)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_LIST_CHANNELS_RESP, env.RequestId, &sessionv1.ListChannelsResp{
			SpaceId:  req.SpaceId,
			Channels: toChannelSummaries(channels),
		})
	case sessionv1.MsgType_LIST_SPACE_MEMBERS_REQ:
		var req sessionv1.ListSpaceMembersReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		limit := req.Limit
		if limit == 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		items, err := m.spaces.ListSpaceMembers(ctx, req.SpaceId, req.AfterMemberId, limit+1)
		if err != nil {
			return err
		}
		hasMore := uint32(len(items)) > limit
		if hasMore {
			items = items[:limit]
		}
		resp := &sessionv1.ListSpaceMembersResp{
			SpaceId: req.SpaceId,
			Members: toSpaceMemberSummaries(items),
			HasMore: hasMore,
		}
		if len(items) > 0 {
			resp.NextAfterMemberId = items[len(items)-1].MemberID
		}
		return sess.write(sessionv1.MsgType_LIST_SPACE_MEMBERS_RESP, env.RequestId, resp)
	case sessionv1.MsgType_CREATE_SPACE_REQ:
		var req sessionv1.CreateSpaceReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		canCreateSpace, err := m.spaces.CanCreateSpace(ctx, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if !canCreateSpace {
			return fmt.Errorf("create space permission required")
		}
		item, err := m.spaces.CreateSpace(ctx, repository.CreateSpaceInput{
			HostNodeID:           m.nodeID,
			CreatorUserID:        sess.authResult.User.ID,
			Name:                 req.Name,
			Description:          req.Description,
			Visibility:           toDomainVisibility(req.Visibility),
			AllowChannelCreation: req.AllowChannelCreation,
		})
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_CREATE_SPACE_RESP, env.RequestId, &sessionv1.CreateSpaceResp{
			Ok:      true,
			SpaceId: item.ID,
			Space:   toSpaceSummary(item),
			Message: "created",
		})
	case sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_REQ:
		var req sessionv1.GetCreateSpacePermissionsReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		canCreateSpace, err := m.spaces.CanCreateSpace(ctx, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_RESP, env.RequestId, &sessionv1.GetCreateSpacePermissionsResp{
			Ok:             true,
			CanCreateSpace: canCreateSpace,
			Message:        "ok",
		})
	case sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_REQ:
		var req sessionv1.GetCreateGroupPermissionsReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		spaceItem, err := m.spaces.GetBySpaceID(ctx, req.SpaceId)
		if err != nil {
			return err
		}
		role, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		canCreateGroup, err := m.spaces.CanCreateGroup(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_RESP, env.RequestId, &sessionv1.GetCreateGroupPermissionsResp{
			Ok:             true,
			SpaceId:        req.SpaceId,
			Space:          toSpaceSummary(spaceItem),
			Role:           toProtoMemberRole(role),
			CanCreateGroup: canCreateGroup,
			Message:        "ok",
		})
	case sessionv1.MsgType_SUBSCRIBE_CHANNEL_REQ:
		var req sessionv1.SubscribeChannelReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		member, err := m.channels.IsUserMember(ctx, req.ChannelId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if !member {
			return fmt.Errorf("not a channel member")
		}
		ch, err := m.channels.GetByChannelID(ctx, req.ChannelId)
		if err != nil {
			return err
		}
		m.SubscribeChannel(sess, req.ChannelId)
		return sess.write(sessionv1.MsgType_SUBSCRIBE_CHANNEL_RESP, env.RequestId, &sessionv1.SubscribeChannelResp{
			Ok:             true,
			ChannelId:      req.ChannelId,
			CurrentLastSeq: ch.MessageSeq,
			Message:        "subscribed",
		})
	case sessionv1.MsgType_UNSUBSCRIBE_CHANNEL_REQ:
		var req sessionv1.UnsubscribeChannelReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		m.unsubscribe(sess, req.ChannelId)
		return sess.write(sessionv1.MsgType_UNSUBSCRIBE_CHANNEL_RESP, env.RequestId, &sessionv1.UnsubscribeChannelResp{
			Ok:        true,
			ChannelId: req.ChannelId,
		})
	case sessionv1.MsgType_ADMIN_SET_SPACE_MEMBER_ROLE_REQ:
		var req sessionv1.AdminSetSpaceMemberRoleReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		role, err := toDomainMemberRole(req.Role)
		if err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if role == space.RoleOwner {
			if actorRole != space.RoleOwner {
				return fmt.Errorf("owner role required")
			}
		} else if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}

		target, err := m.users.GetByUserID(ctx, req.TargetUserId)
		if err != nil {
			return err
		}
		if err := m.spaces.SetSpaceMemberRole(ctx, req.SpaceId, target.ID, role); err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_ADMIN_SET_SPACE_MEMBER_ROLE_RESP, env.RequestId, &sessionv1.AdminSetSpaceMemberRoleResp{
			Ok:           true,
			SpaceId:      req.SpaceId,
			TargetUserId: req.TargetUserId,
			Role:         req.Role,
			Message:      "updated",
		})
	case sessionv1.MsgType_ADMIN_SET_SPACE_CHANNEL_CREATION_REQ:
		var req sessionv1.AdminSetSpaceChannelCreationReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		if err := m.spaces.SetSpaceAllowChannelCreation(ctx, req.SpaceId, req.AllowChannelCreation); err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_ADMIN_SET_SPACE_CHANNEL_CREATION_RESP, env.RequestId, &sessionv1.AdminSetSpaceChannelCreationResp{
			Ok:                   true,
			SpaceId:              req.SpaceId,
			AllowChannelCreation: req.AllowChannelCreation,
			Message:              "updated",
		})
	case sessionv1.MsgType_JOIN_SPACE_REQ:
		var req sessionv1.JoinSpaceReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		item, err := m.spaces.JoinSpace(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_JOIN_SPACE_RESP, env.RequestId, &sessionv1.JoinSpaceResp{
			Ok:      true,
			SpaceId: req.SpaceId,
			Space:   toSpaceSummary(item),
			Message: "joined",
		})
	case sessionv1.MsgType_INVITE_SPACE_MEMBER_REQ:
		var req sessionv1.InviteSpaceMemberReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		target, err := m.users.GetByUserID(ctx, req.TargetUserId)
		if err != nil {
			return err
		}
		item, err := m.spaces.InviteSpaceMember(ctx, req.SpaceId, target.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_INVITE_SPACE_MEMBER_RESP, env.RequestId, &sessionv1.InviteSpaceMemberResp{
			Ok:           true,
			SpaceId:      req.SpaceId,
			TargetUserId: req.TargetUserId,
			Space:        toSpaceSummary(item),
			Message:      "invited",
		})
	case sessionv1.MsgType_KICK_SPACE_MEMBER_REQ:
		var req sessionv1.KickSpaceMemberReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		target, err := m.users.GetByUserID(ctx, req.TargetUserId)
		if err != nil {
			return err
		}
		item, err := m.spaces.KickSpaceMember(ctx, req.SpaceId, target.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_KICK_SPACE_MEMBER_RESP, env.RequestId, &sessionv1.KickSpaceMemberResp{
			Ok:           true,
			SpaceId:      req.SpaceId,
			TargetUserId: req.TargetUserId,
			Space:        toSpaceSummary(item),
			Message:      "kicked",
		})
	case sessionv1.MsgType_BAN_SPACE_MEMBER_REQ:
		var req sessionv1.BanSpaceMemberReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		target, err := m.users.GetByUserID(ctx, req.TargetUserId)
		if err != nil {
			return err
		}
		item, err := m.spaces.BanSpaceMember(ctx, req.SpaceId, target.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_BAN_SPACE_MEMBER_RESP, env.RequestId, &sessionv1.BanSpaceMemberResp{
			Ok:           true,
			SpaceId:      req.SpaceId,
			TargetUserId: req.TargetUserId,
			Space:        toSpaceSummary(item),
			Message:      "banned",
		})
	case sessionv1.MsgType_UNBAN_SPACE_MEMBER_REQ:
		var req sessionv1.UnbanSpaceMemberReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		actorRole, err := m.spaces.GetMemberRole(ctx, req.SpaceId, sess.authResult.User.ID)
		if err != nil {
			return err
		}
		if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
			return fmt.Errorf("admin role required")
		}
		target, err := m.users.GetByUserID(ctx, req.TargetUserId)
		if err != nil {
			return err
		}
		item, err := m.spaces.UnbanSpaceMember(ctx, req.SpaceId, target.ID)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_UNBAN_SPACE_MEMBER_RESP, env.RequestId, &sessionv1.UnbanSpaceMemberResp{
			Ok:           true,
			SpaceId:      req.SpaceId,
			TargetUserId: req.TargetUserId,
			Space:        toSpaceSummary(item),
			Message:      "unbanned",
		})
	case sessionv1.MsgType_CREATE_GROUP_REQ:
		var req sessionv1.CreateGroupReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		item, err := m.channels.CreateChannel(ctx, repository.CreateChannelInput{
			SpaceID:         req.SpaceId,
			CreatorUserID:   sess.authResult.User.ID,
			Type:            channel.TypeSpace,
			Name:            req.Name,
			Description:     req.Description,
			Visibility:      toDomainVisibility(req.Visibility),
			SlowModeSeconds: req.SlowModeSeconds,
		})
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_CREATE_GROUP_RESP, env.RequestId, &sessionv1.CreateGroupResp{
			Ok:        true,
			SpaceId:   req.SpaceId,
			ChannelId: item.ID,
			Channel:   toChannelSummary(item),
			Message:   "created",
		})
	case sessionv1.MsgType_CREATE_CHANNEL_REQ:
		var req sessionv1.CreateChannelReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		item, err := m.channels.CreateChannel(ctx, repository.CreateChannelInput{
			SpaceID:         req.SpaceId,
			CreatorUserID:   sess.authResult.User.ID,
			Type:            channel.TypeBroadcast,
			Name:            req.Name,
			Description:     req.Description,
			Visibility:      toDomainVisibility(req.Visibility),
			SlowModeSeconds: req.SlowModeSeconds,
		})
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_CREATE_CHANNEL_RESP, env.RequestId, &sessionv1.CreateChannelResp{
			Ok:        true,
			SpaceId:   req.SpaceId,
			ChannelId: item.ID,
			Channel:   toChannelSummary(item),
			Message:   "created",
		})
	case sessionv1.MsgType_SEND_MESSAGE_REQ:
		var req sessionv1.SendMessageReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		msg, err := m.messaging.SendMessage(ctx, sess.authResult.User.ID, toSendMessageInput(&req))
		if err != nil {
			return err
		}
		if err := sess.write(sessionv1.MsgType_SEND_MESSAGE_ACK, env.RequestId, &sessionv1.SendMessageAck{
			Ok:           true,
			ChannelId:    msg.ChannelDBID,
			ClientMsgId:  msg.ClientMsgID,
			MessageId:    msg.MessageID,
			Seq:          msg.Seq,
			ServerTimeMs: uint64(time.Now().UTC().UnixMilli()),
			Message:      "stored",
		}); err != nil {
			return err
		}
		m.DeliverMessage(msg.ChannelDBID, msg)
		return nil
	case sessionv1.MsgType_CHANNEL_DELIVER_ACK:
		var req sessionv1.ChannelDeliverAck
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		return m.messaging.AckDelivered(ctx, sess.authResult.User.ID, req.ChannelId, req.AckedSeq)
	case sessionv1.MsgType_CHANNEL_READ_UPDATE:
		var req sessionv1.ChannelReadUpdate
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		return m.messaging.UpdateRead(ctx, sess.authResult.User.ID, req.ChannelId, req.LastReadSeq)
	case sessionv1.MsgType_SYNC_CHANNEL_REQ:
		var req sessionv1.SyncChannelReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		items, nextAfterSeq, hasMore, err := m.messaging.SyncChannel(ctx, sess.authResult.User.ID, req.ChannelId, req.AfterSeq, req.Limit)
		if err != nil {
			return err
		}
		events := make([]*sessionv1.MessageEvent, 0, len(items))
		for _, item := range items {
			events = append(events, toMessageEvent(item, m.blobURLBase))
		}
		return sess.write(sessionv1.MsgType_SYNC_CHANNEL_RESP, env.RequestId, &sessionv1.SyncChannelResp{
			ChannelId:    req.ChannelId,
			Messages:     events,
			NextAfterSeq: nextAfterSeq,
			HasMore:      hasMore,
		})
	case sessionv1.MsgType_GET_MEDIA_REQ:
		var req sessionv1.GetMediaReq
		if err := protocol.UnmarshalBody(env.Body, &req); err != nil {
			return err
		}
		channelIDs, err := m.messages.ListChannelIDsByMediaID(ctx, req.MediaId)
		if err != nil {
			return err
		}
		allowed := false
		for _, channelID := range channelIDs {
			perm, err := m.channels.GetPermission(ctx, channelID, sess.authResult.User.ID)
			if err != nil {
				continue
			}
			if perm.CanView {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("forbidden")
		}
		item, content, err := m.media.DownloadMediaByID(ctx, req.MediaId)
		if err != nil {
			return err
		}
		return sess.write(sessionv1.MsgType_GET_MEDIA_RESP, env.RequestId, &sessionv1.GetMediaResp{
			Ok:      true,
			MediaId: req.MediaId,
			File: &sessionv1.MediaFile{
				MediaId:    item.MediaID,
				BlobId:     item.BlobID,
				Sha256:     item.SHA256,
				FileName:   item.OriginalName,
				MimeType:   item.MIMEType,
				Size:       item.Size,
				InlineData: content,
				Url:        "",
			},
			Message: "ok",
		})
	default:
		return fmt.Errorf("unsupported message type %s", env.MsgType.String())
	}
}

func (m *Manager) unregisterSession(sess *ConnSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for channelID := range sess.subscribed {
		delete(m.subscriptions[channelID], sess)
		if len(m.subscriptions[channelID]) == 0 {
			delete(m.subscriptions, channelID)
		}
	}
}

func (m *Manager) unsubscribe(sess *ConnSession, channelID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(sess.subscribed, channelID)
	delete(m.subscriptions[channelID], sess)
	if len(m.subscriptions[channelID]) == 0 {
		delete(m.subscriptions, channelID)
	}
}

func (s *ConnSession) write(msgType sessionv1.MsgType, requestID string, body proto.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	env, err := protocol.NewEnvelope(msgType, requestID, body)
	if err != nil {
		return err
	}
	return protocol.WriteEnvelope(s.stream, env)
}

func (s *ConnSession) writeError(requestID string, code uint32, message string) error {
	return s.write(sessionv1.MsgType_ERROR, requestID, &sessionv1.ErrorMsg{
		Code:    code,
		Message: message,
	})
}

func toSpaceSummaries(items []*space.Space) []*sessionv1.SpaceSummary {
	out := make([]*sessionv1.SpaceSummary, 0, len(items))
	for _, item := range items {
		out = append(out, toSpaceSummary(item))
	}
	return out
}

func toSpaceSummary(item *space.Space) *sessionv1.SpaceSummary {
	return &sessionv1.SpaceSummary{
		SpaceId:              item.ID,
		Name:                 item.Name,
		AvatarUrl:            item.AvatarURL,
		Description:          item.Description,
		Visibility:           toVisibility(item.Visibility),
		MemberCount:          item.MemberCount,
		AllowChannelCreation: item.AllowChannelCreation,
	}
}

func toChannelSummary(item *channel.Channel) *sessionv1.ChannelSummary {
	return &sessionv1.ChannelSummary{
		ChannelId:       item.ID,
		SpaceId:         item.SpaceDBID,
		Type:            toChannelType(item.Type),
		Name:            item.Name,
		Description:     item.Description,
		Visibility:      toVisibility(item.Visibility),
		SlowModeSeconds: item.SlowModeSeconds,
		LastSeq:         item.MessageSeq,
		MemberCount:     item.MemberCount,
		CanView:         item.Permission.CanView,
		CanSendMessage:  item.Permission.CanSendMessage,
		CanSendImage:    item.Permission.CanSendImage,
		CanSendFile:     item.Permission.CanSendFile,
	}
}

func toChannelSummaries(items []*channel.Channel) []*sessionv1.ChannelSummary {
	out := make([]*sessionv1.ChannelSummary, 0, len(items))
	for _, item := range items {
		out = append(out, toChannelSummary(item))
	}
	return out
}

func toSendMessageInput(req *sessionv1.SendMessageReq) service.SendMessageInput {
	input := service.SendMessageInput{
		ChannelID:   req.ChannelId,
		ClientMsgID: req.ClientMsgId,
		MessageType: toDomainMessageType(req.MessageType),
	}
	if req.Content == nil {
		return input
	}
	input.Text = req.Content.Text
	for _, image := range req.Content.Images {
		input.Images = append(input.Images, service.AttachmentInput{
			MediaID:      image.MediaId,
			OriginalName: image.OriginalName,
			MIMEType:     image.MimeType,
			Content:      image.InlineData,
		})
	}
	for _, file := range req.Content.Files {
		input.Files = append(input.Files, service.AttachmentInput{
			MediaID:      file.MediaId,
			OriginalName: file.FileName,
			MIMEType:     file.MimeType,
			Content:      file.InlineData,
		})
	}
	return input
}

func toMessageEvent(item *message.Message, blobURLBase string) *sessionv1.MessageEvent {
	content := &sessionv1.MessageContent{
		Text: item.Content.Text,
	}
	for _, image := range item.Content.Images {
		content.Images = append(content.Images, &sessionv1.MediaImage{
			MediaId:      image.MediaID,
			BlobId:       image.BlobID,
			Sha256:       image.SHA256,
			Url:          blobURL(blobURLBase, image.StoragePath),
			Width:        image.Width,
			Height:       image.Height,
			MimeType:     image.MIMEType,
			Size:         image.Size,
			OriginalName: image.OriginalName,
		})
	}
	for _, file := range item.Content.Files {
		content.Files = append(content.Files, &sessionv1.MediaFile{
			MediaId:  file.MediaID,
			BlobId:   file.BlobID,
			Sha256:   file.SHA256,
			FileName: file.OriginalName,
			Url:      blobURL(blobURLBase, file.StoragePath),
			MimeType: file.MIMEType,
			Size:     file.Size,
		})
	}
	return &sessionv1.MessageEvent{
		ChannelId:    item.ChannelDBID,
		MessageId:    item.MessageID,
		Seq:          item.Seq,
		SenderUserId: item.SenderUserExtID,
		MessageType:  toProtoMessageType(item.MessageType),
		Content:      content,
		CreatedAtMs:  uint64(item.CreatedAt.UnixMilli()),
	}
}

func blobURL(base string, storagePath string) string {
	base = strings.TrimRight(base, "/")
	return base + "/" + strings.TrimLeft(storagePath, "/")
}
