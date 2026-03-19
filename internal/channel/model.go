package channel

import (
	"time"

	"meshserver/internal/space"
)

// Type identifies the channel semantics.
type Type string

const (
	TypeSpace     Type = "group"
	TypeBroadcast Type = "channel"
)

// Permission describes a user's effective channel permissions.
type Permission struct {
	Role             space.Role `db:"role"`
	CanView          bool       `db:"can_view"`
	CanSendMessage   bool       `db:"can_send_message"`
	CanSendImage     bool       `db:"can_send_image"`
	CanSendFile      bool       `db:"can_send_file"`
	CanDeleteMessage bool       `db:"can_delete_message"`
	CanManageChannel bool       `db:"can_manage_channel"`
}

// Channel represents a communication unit inside a space.
type Channel struct {
	ID                     uint32           `db:"id"`
	SpaceDBID              uint32           `db:"space_id"`
	Type                   Type             `db:"type"`
	Name                   string           `db:"name"`
	Description            string           `db:"description"`
	Visibility             space.Visibility `db:"visibility"`
	SlowModeSeconds        uint32           `db:"slow_mode_seconds"`
	AutoDeleteAfterSeconds uint32           `db:"auto_delete_after_seconds"`
	MessageSeq             uint64           `db:"message_seq"`
	MessageCount           uint64           `db:"message_count"`
	MemberCount            uint32           `db:"member_count"`
	CreatedBy              uint64           `db:"created_by"`
	Status                 uint8            `db:"status"`
	SortOrder              int              `db:"sort_order"`
	Permission             Permission
	CreatedAt              time.Time `db:"created_at"`
	UpdatedAt              time.Time `db:"updated_at"`
}
