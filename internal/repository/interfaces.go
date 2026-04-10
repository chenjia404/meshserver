package repository

import (
	"context"
	"errors"
	"time"

	"meshserver/internal/channel"
	"meshserver/internal/message"
	"meshserver/internal/space"
)

var (
	// ErrNotFound is returned when the requested row does not exist.
	ErrNotFound = errors.New("repository: not found")
	// ErrDirectChatForbidden is returned when the caller is not a participant in a DM.
	ErrDirectChatForbidden = errors.New("repository: direct chat forbidden")
)

// User is the repository DTO for application users.
type User struct {
	ID          uint64    `db:"id"`
	UserID      string    `db:"user_id"`
	PeerID      string    `db:"peer_id"`
	PubKey      []byte    `db:"pubkey"`
	DisplayName string    `db:"display_name"`
	AvatarURL   string    `db:"avatar_url"`
	Bio         string    `db:"bio"`
	Status      uint8     `db:"status"`
	LastLoginAt time.Time `db:"last_login_at"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// SpaceMember is the repository DTO for a user inside a space.
type SpaceMember struct {
	MemberID    uint64     `db:"member_id"`
	UserID      string     `db:"user_id"`
	DisplayName string     `db:"display_name"`
	AvatarURL   string     `db:"avatar_url"`
	Role        space.Role `db:"role"`
	Nickname    string     `db:"nickname"`
	IsMuted     bool       `db:"is_muted"`
	IsBanned    bool       `db:"is_banned"`
	JoinedAt    time.Time  `db:"joined_at"`
	LastSeenAt  time.Time  `db:"last_seen_at"`
}

// Blob is the repository DTO for deduplicated binaries.
type Blob struct {
	ID          uint64    `db:"id"`
	BlobID      string    `db:"blob_id"`
	SHA256      string    `db:"sha256"`
	Size        uint64    `db:"size"`
	MIMEType    string    `db:"mime_type"`
	StoragePath string    `db:"storage_path"`
	RefCount    uint64    `db:"ref_count"`
	CreatedAt   time.Time `db:"created_at"`
}

// MediaObject is the repository DTO for message attachments.
type MediaObject struct {
	ID           uint64    `db:"id"`
	MediaID      string    `db:"media_id"`
	BlobDBID     uint64    `db:"blob_id"`
	BlobID       string    `db:"blob_external_id"`
	Kind         string    `db:"kind"`
	OriginalName string    `db:"original_name"`
	MIMEType     string    `db:"mime_type"`
	Size         uint64    `db:"size"`
	Width        uint32    `db:"width"`
	Height       uint32    `db:"height"`
	CreatedBy    uint64    `db:"created_by"`
	CreatedAt    time.Time `db:"created_at"`
	SHA256       string    `db:"sha256"`
	StoragePath  string    `db:"storage_path"`
	// FileCID 僅 kind=file 時有值：UnixFS CID（與設定 ipfs.* 一致）。
	FileCID string `db:"file_cid"`
}

// UserRepository owns user rows.
type UserRepository interface {
	GetByPeerID(ctx context.Context, peerID string) (*User, error)
	GetByID(ctx context.Context, id uint64) (*User, error)
	GetByUserID(ctx context.Context, userID string) (*User, error)
	CreateIfNotExistsByPeerID(ctx context.Context, peerID string, pubKey []byte) (*User, error)
	UpdateLogin(ctx context.Context, userID uint64, pubKey []byte, when time.Time) error
}

// SpaceRepository owns top-level space directory data.
type SpaceRepository interface {
	ListByUserID(ctx context.Context, userID uint64) ([]*space.Space, error)
	GetBySpaceID(ctx context.Context, spaceID uint32) (*space.Space, error)
	GetMemberRole(ctx context.Context, spaceID uint32, userID uint64) (space.Role, error)
	CanCreateGroup(ctx context.Context, spaceID uint32, userID uint64) (bool, error)
	CreateSpace(ctx context.Context, in CreateSpaceInput) (*space.Space, error)
	JoinSpace(ctx context.Context, spaceID uint32, userID uint64) (*space.Space, error)
	InviteSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error)
	KickSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error)
	BanSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error)
	UnbanSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error)
	ListSpaceMembers(ctx context.Context, spaceID uint32, afterMemberID uint64, limit uint32) ([]*SpaceMember, error)
	SetSpaceMemberRole(ctx context.Context, spaceID uint32, targetUserID uint64, role space.Role) error
	SetSpaceAllowChannelCreation(ctx context.Context, spaceID uint32, enabled bool) error
}

// ChannelRepository owns channels and memberships.
type ChannelRepository interface {
	ListBySpaceIDForUser(ctx context.Context, spaceID uint32, userID uint64) ([]*channel.Channel, error)
	GetByChannelID(ctx context.Context, channelID uint32) (*channel.Channel, error)
	IsUserMember(ctx context.Context, channelID uint32, userID uint64) (bool, error)
	GetPermission(ctx context.Context, channelID uint32, userID uint64) (*channel.Permission, error)
	CreateChannel(ctx context.Context, in CreateChannelInput) (*channel.Channel, error)
	SetGroupAutoDeleteAfterSeconds(ctx context.Context, channelID uint32, seconds uint32) error
}

// CreateChannelInput describes a new channel to create.
type CreateChannelInput struct {
	SpaceID         uint32
	CreatorUserID   uint64
	Type            channel.Type
	Name            string
	Description     string
	Visibility      space.Visibility
	SlowModeSeconds uint32
	// BypassSpaceChannelCreationPolicy is set by the session layer for the site-wide admin (MESHSERVER_DEFAULT_ADMIN_PEER_ID)
	// so they can create channels when they are owner/admin even if allow_channel_creation is off on the space.
	BypassSpaceChannelCreationPolicy bool
}

// CreateSpaceInput describes a new space to create.
type CreateSpaceInput struct {
	HostNodeID           uint64
	CreatorUserID        uint64
	Name                 string
	Description          string
	Visibility           space.Visibility
	AllowChannelCreation bool
}

// CreateMessageInput is the repository input for transactional message creation.
type CreateMessageInput struct {
	ChannelID        uint32
	SenderUserID     uint64
	ClientMsgID      string
	MessageType      message.Type
	Text             string
	AttachmentMedia  []uint64
	IncrementBlobRef []uint64
}

// MessageRepository owns transactional message storage.
type MessageRepository interface {
	Create(ctx context.Context, in CreateMessageInput) (*message.Message, error)
	ListAfterSeq(ctx context.Context, channelID uint32, afterSeq uint64, limit uint32) ([]*message.Message, error)
	GetByClientMsgID(ctx context.Context, channelID uint32, senderUserID uint64, clientMsgID string) (*message.Message, error)
	GetLastMessageTime(ctx context.Context, channelID uint32, senderUserID uint64) (*time.Time, error)
	ListChannelIDsByMediaID(ctx context.Context, mediaID string) ([]uint32, error)
	CleanupExpiredMessages(ctx context.Context, now time.Time, limit uint32) (uint32, error)
}

// ReadCursorRepository owns delivery and read cursor tracking.
type ReadCursorRepository interface {
	UpsertDeliveredSeq(ctx context.Context, userID uint64, channelID uint32, seq uint64) error
	UpsertReadSeq(ctx context.Context, userID uint64, channelID uint32, seq uint64) error
}

// CreateBlobInput describes a new blob row.
type CreateBlobInput struct {
	BlobID      string
	SHA256      string
	Size        uint64
	MIMEType    string
	StoragePath string
	RefCount    uint64
}

// BlobRepository owns deduplicated blob metadata.
type BlobRepository interface {
	GetBySHA256(ctx context.Context, sha256 string) (*Blob, error)
	CreateBlob(ctx context.Context, in CreateBlobInput) (*Blob, error)
	IncRef(ctx context.Context, blobID uint64) error
}

// CreateMediaInput describes a new media object row.
type CreateMediaInput struct {
	MediaID      string
	BlobID       uint64
	Kind         string
	OriginalName string
	MIMEType     string
	Size         uint64
	Width        *uint32
	Height       *uint32
	CreatedBy    uint64
	FileCID      string // kind=file 時為 UnixFS CID；圖片為空
}

// MediaRepository owns logical media objects.
type MediaRepository interface {
	CreateMedia(ctx context.Context, in CreateMediaInput) (*MediaObject, error)
	GetByMediaID(ctx context.Context, mediaID string) (*MediaObject, error)
}

// AuthNonceRepository stores challenge nonces to prevent replay.
type AuthNonceRepository interface {
	StoreNonce(ctx context.Context, nonceHash string, clientPeerID string, nodePeerID string, issuedAt time.Time, expiresAt time.Time) error
	UseNonce(ctx context.Context, nonceHash string, clientPeerID string, nodePeerID string, now time.Time) error
}

// NodeRecord captures local node metadata.
type NodeRecord struct {
	ID          uint64 `db:"id"`
	NodeID      string `db:"node_id"`
	PeerID      string `db:"peer_id"`
	Name        string `db:"name"`
	PublicAddrs []string
	Status      uint8     `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// NodeRepository stores running node metadata.
type NodeRepository interface {
	Upsert(ctx context.Context, record NodeRecord) (*NodeRecord, error)
}

// BootstrapRepository seeds demo data and default memberships.
type BootstrapRepository interface {
}

// DirectConversation is a 1:1 chat bucket (canonical user_low_id < user_high_id).
type DirectConversation struct {
	ID              uint64
	ConversationID  string
	UserLowID       uint64
	UserHighID      uint64
	LastSeq         uint64
	LastMessageAtMs uint64
}

// DirectConversationListItem is one row for LIST_DIRECT_CONVERSATIONS.
type DirectConversationListItem struct {
	ConversationID  string
	PeerUserID      string
	PeerDisplayName string
	LastSeq         uint64
	LastMessageAtMs uint64
}

// DirectMessage is a persisted DM row.
type DirectMessage struct {
	MessageID           string
	ConversationID      string
	Seq                 uint64
	SenderUserID        uint64
	RecipientUserID     uint64
	SenderExternalID    string
	RecipientExternalID string
	ClientMsgID         string
	MessageType         message.Type
	Text                string
	CreatedAtMs         uint64
	RecipientAckedAtMs  *uint64
}

// CreateDirectMessageInput is used to insert a new DM after idempotency checks.
type CreateDirectMessageInput struct {
	ConversationID  string
	SenderUserID    uint64
	RecipientUserID uint64
	ClientMsgID     string
	MessageType     message.Type
	Text            string
}

// DirectChatRepository stores server-mediated direct messages (MySQL).
type DirectChatRepository interface {
	GetOrCreateConversation(ctx context.Context, userAID, userBID uint64) (*DirectConversation, error)
	GetConversationByExternalID(ctx context.Context, conversationID string) (*DirectConversation, error)
	ListConversationsForUser(ctx context.Context, userID uint64) ([]DirectConversationListItem, error)
	CreateDirectMessage(ctx context.Context, in CreateDirectMessageInput) (*DirectMessage, error)
	GetDirectMessageByClientMsgID(ctx context.Context, conversationID string, senderUserID uint64, clientMsgID string) (*DirectMessage, error)
	GetDirectMessageByMessageID(ctx context.Context, messageID string) (*DirectMessage, error)
	AckDirectMessage(ctx context.Context, messageID string, recipientUserID uint64, ackedAtMs uint64) (alreadyAcked bool, senderUserID uint64, conversationID string, err error)
	ListPendingDirectMessages(ctx context.Context, conversationID string, recipientUserID uint64, afterSeq uint64, limit uint32) ([]*DirectMessage, uint64, bool, error)
}
