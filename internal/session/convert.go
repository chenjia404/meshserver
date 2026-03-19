package session

import (
	"fmt"
	"time"

	"meshserver/internal/channel"
	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	"meshserver/internal/message"
	"meshserver/internal/repository"
	"meshserver/internal/space"
)

func toVisibility(value space.Visibility) sessionv1.Visibility {
	switch value {
	case space.VisibilityPublic:
		return sessionv1.Visibility_PUBLIC
	default:
		return sessionv1.Visibility_PRIVATE
	}
}

func toDomainVisibility(value sessionv1.Visibility) space.Visibility {
	switch value {
	case sessionv1.Visibility_PUBLIC:
		return space.VisibilityPublic
	default:
		return space.VisibilityPrivate
	}
}

func toChannelType(value channel.Type) sessionv1.ChannelType {
	switch value {
	case channel.TypeSpace:
		return sessionv1.ChannelType_GROUP
	default:
		return sessionv1.ChannelType_BROADCAST
	}
}

func toProtoMessageType(value message.Type) sessionv1.MessageType {
	switch value {
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

func toDomainMessageType(value sessionv1.MessageType) message.Type {
	switch value {
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

func toDomainMemberRole(value sessionv1.MemberRole) (space.Role, error) {
	switch value {
	case sessionv1.MemberRole_OWNER:
		return space.RoleOwner, nil
	case sessionv1.MemberRole_ADMIN:
		return space.RoleAdmin, nil
	case sessionv1.MemberRole_MEMBER:
		return space.RoleMember, nil
	case sessionv1.MemberRole_SUBSCRIBER:
		return space.RoleSubscriber, nil
	default:
		return "", fmt.Errorf("unsupported member role")
	}
}

func toProtoMemberRole(value space.Role) sessionv1.MemberRole {
	switch value {
	case space.RoleOwner:
		return sessionv1.MemberRole_OWNER
	case space.RoleAdmin:
		return sessionv1.MemberRole_ADMIN
	case space.RoleMember:
		return sessionv1.MemberRole_MEMBER
	case space.RoleSubscriber:
		return sessionv1.MemberRole_SUBSCRIBER
	default:
		return sessionv1.MemberRole_MEMBER_ROLE_UNSPECIFIED
	}
}

func toSpaceMemberSummary(item *repository.SpaceMember) *sessionv1.SpaceMemberSummary {
	return &sessionv1.SpaceMemberSummary{
		MemberId:     item.MemberID,
		UserId:       item.UserID,
		DisplayName:  item.DisplayName,
		AvatarUrl:    item.AvatarURL,
		Role:         toProtoMemberRole(item.Role),
		Nickname:     item.Nickname,
		IsMuted:      item.IsMuted,
		IsBanned:     item.IsBanned,
		JoinedAtMs:   uint64(item.JoinedAt.UnixMilli()),
		LastSeenAtMs: unixMilliOrZero(item.LastSeenAt),
	}
}

func toSpaceMemberSummaries(items []*repository.SpaceMember) []*sessionv1.SpaceMemberSummary {
	out := make([]*sessionv1.SpaceMemberSummary, 0, len(items))
	for _, item := range items {
		out = append(out, toSpaceMemberSummary(item))
	}
	return out
}

func unixMilliOrZero(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UnixMilli())
}
