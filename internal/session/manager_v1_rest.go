package session

import (
	"context"

	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	"meshserver/internal/channel"
	"meshserver/internal/repository"
	"meshserver/internal/space"
)

// CreateSpaceForAPI mirrors CREATE_SPACE_REQ / CREATE_SPACE_RESP (site-wide admin only).
func (m *Manager) CreateSpaceForAPI(ctx context.Context, userID uint64, userPeerID string, req *sessionv1.CreateSpaceReq) (*sessionv1.CreateSpaceResp, error) {
	if !m.isGlobalAdmin(userPeerID) {
		return nil, ErrCreateSpacePermission
	}
	item, err := m.spaces.CreateSpace(ctx, repository.CreateSpaceInput{
		HostNodeID:           m.nodeID,
		CreatorUserID:        userID,
		Name:                 req.Name,
		Description:          req.Description,
		Visibility:           toDomainVisibility(req.Visibility),
		AllowChannelCreation: true,
	})
	if err != nil {
		return nil, err
	}
	sum, err := m.spaceSummaryForViewer(ctx, item, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.CreateSpaceResp{
		Ok:      true,
		SpaceId: item.ID,
		Space:   sum,
		Message: "created",
	}, nil
}

// GetCreateSpacePermissionsForAPI mirrors GET_CREATE_SPACE_PERMISSIONS_*.
func (m *Manager) GetCreateSpacePermissionsForAPI(ctx context.Context, userPeerID string) (*sessionv1.GetCreateSpacePermissionsResp, error) {
	_ = ctx
	return &sessionv1.GetCreateSpacePermissionsResp{
		Ok:             true,
		CanCreateSpace: m.isGlobalAdmin(userPeerID),
		Message:        "ok",
	}, nil
}

// GetCreateGroupPermissionsForAPI mirrors GET_CREATE_GROUP_PERMISSIONS_*.
func (m *Manager) GetCreateGroupPermissionsForAPI(ctx context.Context, userID uint64, userPeerID string, spaceID uint32) (*sessionv1.GetCreateGroupPermissionsResp, error) {
	spaceItem, err := m.spaces.GetBySpaceID(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	role, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	canCreateGroup, err := m.spaces.CanCreateGroup(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if m.isGlobalAdmin(userPeerID) && (role == space.RoleOwner || role == space.RoleAdmin) {
		canCreateGroup = true
	}
	spaceSum, err := m.spaceSummaryForViewer(ctx, spaceItem, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.GetCreateGroupPermissionsResp{
		Ok:             true,
		SpaceId:        spaceID,
		Space:          spaceSum,
		Role:           toProtoMemberRole(role),
		CanCreateGroup: canCreateGroup,
		Message:        "ok",
	}, nil
}

// AdminSetSpaceMemberRoleForAPI mirrors ADMIN_SET_SPACE_MEMBER_ROLE_*.
func (m *Manager) AdminSetSpaceMemberRoleForAPI(ctx context.Context, userID uint64, spaceID uint32, targetUserExtID string, protoRole sessionv1.MemberRole) (*sessionv1.AdminSetSpaceMemberRoleResp, error) {
	role, err := toDomainMemberRole(protoRole)
	if err != nil {
		return nil, err
	}
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if role == space.RoleOwner {
		if actorRole != space.RoleOwner {
			return nil, ErrOwnerRoleRequired
		}
	} else if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	target, err := m.users.GetByUserID(ctx, targetUserExtID)
	if err != nil {
		return nil, err
	}
	if err := m.spaces.SetSpaceMemberRole(ctx, spaceID, target.ID, role); err != nil {
		return nil, err
	}
	return &sessionv1.AdminSetSpaceMemberRoleResp{
		Ok:           true,
		SpaceId:      spaceID,
		TargetUserId: targetUserExtID,
		Role:         protoRole,
		Message:      "updated",
	}, nil
}

// AdminSetSpaceChannelCreationForAPI mirrors ADMIN_SET_SPACE_CHANNEL_CREATION_*.
func (m *Manager) AdminSetSpaceChannelCreationForAPI(ctx context.Context, userID uint64, spaceID uint32, allow bool) (*sessionv1.AdminSetSpaceChannelCreationResp, error) {
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	if err := m.spaces.SetSpaceAllowChannelCreation(ctx, spaceID, allow); err != nil {
		return nil, err
	}
	return &sessionv1.AdminSetSpaceChannelCreationResp{
		Ok:                   true,
		SpaceId:              spaceID,
		AllowChannelCreation: allow,
		Message:              "updated",
	}, nil
}

// AdminSetGroupAutoDeleteForAPI mirrors ADMIN_SET_GROUP_AUTO_DELETE_*.
func (m *Manager) AdminSetGroupAutoDeleteForAPI(ctx context.Context, userID uint64, channelID uint32, seconds uint32) (*sessionv1.AdminSetGroupAutoDeleteResp, error) {
	ch, err := m.channels.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch.Type != channel.TypeSpace {
		return nil, ErrAutoDeleteGroupOnly
	}
	actorRole, err := m.spaces.GetMemberRole(ctx, ch.SpaceDBID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	if err := m.channels.SetGroupAutoDeleteAfterSeconds(ctx, channelID, seconds); err != nil {
		return nil, err
	}
	if deleted, err := m.messaging.CleanupExpiredMessages(ctx); err == nil && deleted > 0 {
		m.logger.Info("cleanup expired messages after auto-delete update", "deleted", deleted, "channel_id", channelID)
	} else if err != nil {
		m.logger.Warn("cleanup expired messages after auto-delete update failed", "channel_id", channelID, "error", err)
	}
	ch, err = m.channels.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.AdminSetGroupAutoDeleteResp{
		Ok:                     true,
		ChannelId:              channelID,
		AutoDeleteAfterSeconds: ch.AutoDeleteAfterSeconds,
		Channel:                toChannelSummary(ch),
		Message:                "updated",
	}, nil
}

// InviteSpaceMemberForAPI mirrors INVITE_SPACE_MEMBER_*.
func (m *Manager) InviteSpaceMemberForAPI(ctx context.Context, userID uint64, userPeerID string, spaceID uint32, targetUserExtID string) (*sessionv1.InviteSpaceMemberResp, error) {
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	target, err := m.users.GetByUserID(ctx, targetUserExtID)
	if err != nil {
		return nil, err
	}
	item, err := m.spaces.InviteSpaceMember(ctx, spaceID, target.ID)
	if err != nil {
		return nil, err
	}
	sum, err := m.spaceSummaryForViewer(ctx, item, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.InviteSpaceMemberResp{
		Ok:           true,
		SpaceId:      spaceID,
		TargetUserId: targetUserExtID,
		Space:        sum,
		Message:      "invited",
	}, nil
}

// KickSpaceMemberForAPI mirrors KICK_SPACE_MEMBER_*.
func (m *Manager) KickSpaceMemberForAPI(ctx context.Context, userID uint64, userPeerID string, spaceID uint32, targetUserExtID string) (*sessionv1.KickSpaceMemberResp, error) {
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	target, err := m.users.GetByUserID(ctx, targetUserExtID)
	if err != nil {
		return nil, err
	}
	item, err := m.spaces.KickSpaceMember(ctx, spaceID, target.ID)
	if err != nil {
		return nil, err
	}
	sum, err := m.spaceSummaryForViewer(ctx, item, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.KickSpaceMemberResp{
		Ok:           true,
		SpaceId:      spaceID,
		TargetUserId: targetUserExtID,
		Space:        sum,
		Message:      "kicked",
	}, nil
}

// BanSpaceMemberForAPI mirrors BAN_SPACE_MEMBER_*.
func (m *Manager) BanSpaceMemberForAPI(ctx context.Context, userID uint64, userPeerID string, spaceID uint32, targetUserExtID string) (*sessionv1.BanSpaceMemberResp, error) {
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	target, err := m.users.GetByUserID(ctx, targetUserExtID)
	if err != nil {
		return nil, err
	}
	item, err := m.spaces.BanSpaceMember(ctx, spaceID, target.ID)
	if err != nil {
		return nil, err
	}
	sum, err := m.spaceSummaryForViewer(ctx, item, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.BanSpaceMemberResp{
		Ok:           true,
		SpaceId:      spaceID,
		TargetUserId: targetUserExtID,
		Space:        sum,
		Message:      "banned",
	}, nil
}

// UnbanSpaceMemberForAPI mirrors UNBAN_SPACE_MEMBER_*.
func (m *Manager) UnbanSpaceMemberForAPI(ctx context.Context, userID uint64, userPeerID string, spaceID uint32, targetUserExtID string) (*sessionv1.UnbanSpaceMemberResp, error) {
	actorRole, err := m.spaces.GetMemberRole(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}
	if actorRole != space.RoleOwner && actorRole != space.RoleAdmin {
		return nil, ErrAdminRoleRequired
	}
	target, err := m.users.GetByUserID(ctx, targetUserExtID)
	if err != nil {
		return nil, err
	}
	item, err := m.spaces.UnbanSpaceMember(ctx, spaceID, target.ID)
	if err != nil {
		return nil, err
	}
	sum, err := m.spaceSummaryForViewer(ctx, item, userID, userPeerID)
	if err != nil {
		return nil, err
	}
	return &sessionv1.UnbanSpaceMemberResp{
		Ok:           true,
		SpaceId:      spaceID,
		TargetUserId: targetUserExtID,
		Space:        sum,
		Message:      "unbanned",
	}, nil
}

// CreateGroupForAPI mirrors CREATE_GROUP_*.
func (m *Manager) CreateGroupForAPI(ctx context.Context, userID uint64, userPeerID string, req *sessionv1.CreateGroupReq) (*sessionv1.CreateGroupResp, error) {
	item, err := m.channels.CreateChannel(ctx, repository.CreateChannelInput{
		SpaceID:                          req.SpaceId,
		CreatorUserID:                    userID,
		Type:                             channel.TypeSpace,
		Name:                             req.Name,
		Description:                      req.Description,
		Visibility:                       toDomainVisibility(req.Visibility),
		SlowModeSeconds:                  req.SlowModeSeconds,
		BypassSpaceChannelCreationPolicy: m.isGlobalAdmin(userPeerID),
	})
	if err != nil {
		return nil, err
	}
	return &sessionv1.CreateGroupResp{
		Ok:        true,
		SpaceId:   req.SpaceId,
		ChannelId: item.ID,
		Channel:   toChannelSummary(item),
		Message:   "created",
	}, nil
}

// CreateChannelForAPI mirrors CREATE_CHANNEL_* (broadcast channel).
func (m *Manager) CreateChannelForAPI(ctx context.Context, userID uint64, userPeerID string, req *sessionv1.CreateChannelReq) (*sessionv1.CreateChannelResp, error) {
	item, err := m.channels.CreateChannel(ctx, repository.CreateChannelInput{
		SpaceID:                          req.SpaceId,
		CreatorUserID:                    userID,
		Type:                             channel.TypeBroadcast,
		Name:                             req.Name,
		Description:                      req.Description,
		Visibility:                       toDomainVisibility(req.Visibility),
		SlowModeSeconds:                  req.SlowModeSeconds,
		BypassSpaceChannelCreationPolicy: m.isGlobalAdmin(userPeerID),
	})
	if err != nil {
		return nil, err
	}
	return &sessionv1.CreateChannelResp{
		Ok:        true,
		SpaceId:   req.SpaceId,
		ChannelId: item.ID,
		Channel:   toChannelSummary(item),
		Message:   "created",
	}, nil
}

// AckDeliveredForAPI mirrors CHANNEL_DELIVER_ACK.
func (m *Manager) AckDeliveredForAPI(ctx context.Context, userID uint64, channelID uint32, ackedSeq uint64) error {
	return m.messaging.AckDelivered(ctx, userID, channelID, ackedSeq)
}

// UpdateReadForAPI mirrors CHANNEL_READ_UPDATE.
func (m *Manager) UpdateReadForAPI(ctx context.Context, userID uint64, channelID uint32, lastReadSeq uint64) error {
	return m.messaging.UpdateRead(ctx, userID, channelID, lastReadSeq)
}
