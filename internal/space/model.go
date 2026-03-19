package space

import "time"

// Visibility controls who can see a space or channel.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// Role describes a membership role within a space or channel.
type Role string

const (
	RoleOwner      Role = "owner"
	RoleAdmin      Role = "admin"
	RoleMember     Role = "member"
	RoleSubscriber Role = "subscriber"
)

// Space is the business model for a community.
type Space struct {
	ID                   uint32     `db:"id"`
	HostNodeID           uint64     `db:"host_node_id"`
	OwnerUserID          uint64     `db:"owner_user_id"`
	Name                 string     `db:"name"`
	AvatarURL            string     `db:"avatar_url"`
	Description          string     `db:"description"`
	Visibility           Visibility `db:"visibility"`
	MemberCount          uint32     `db:"member_count"`
	ChannelCount         uint32     `db:"channel_count"`
	AllowChannelCreation bool       `db:"allow_channel_creation"`
	Status               uint8      `db:"status"`
	CreatedAt            time.Time  `db:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at"`
}

// Membership records a user inside a space.
type Membership struct {
	ID         uint64    `db:"id"`
	SpaceDBID  uint32    `db:"space_id"`
	UserDBID   uint64    `db:"user_id"`
	Role       Role      `db:"role"`
	Nickname   string    `db:"nickname"`
	IsMuted    bool      `db:"is_muted"`
	IsBanned   bool      `db:"is_banned"`
	JoinedAt   time.Time `db:"joined_at"`
	LastSeenAt time.Time `db:"last_seen_at"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}
