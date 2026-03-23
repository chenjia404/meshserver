package message

import "time"

// Type defines the message payload category.
type Type string

const (
	TypeText   Type = "text"
	TypeImage  Type = "image"
	TypeFile   Type = "file"
	TypeSystem Type = "system"
)

// Status tracks message lifecycle.
type Status string

const (
	StatusNormal  Status = "normal"
	StatusDeleted Status = "deleted"
)

// Attachment is the expanded attachment metadata embedded into a message snapshot.
type Attachment struct {
	ID           uint64
	MediaID      string
	BlobDBID     uint64
	BlobID       string
	Kind         string
	OriginalName string
	MIMEType     string
	Size         uint64
	Width        uint32
	Height       uint32
	CreatedBy    uint64
	CreatedAt    time.Time
	SHA256       string
	// FileCID 僅附件為檔案時有值（IPFS UnixFS CID）；圖片為空。
	FileCID     string
	StoragePath string
}

// Content is the fixed content layout supported by the first version.
type Content struct {
	Text   string
	Images []*Attachment
	Files  []*Attachment
}

// Message is the persisted message entity with expanded content.
type Message struct {
	ID              uint64 `db:"id"`
	MessageID       string `db:"message_id"`
	ChannelDBID     uint32 `db:"channel_id"`
	Seq             uint64    `db:"seq"`
	SenderUserID    uint64    `db:"sender_user_id"`
	SenderUserExtID string    `db:"sender_external_id"`
	ClientMsgID     string    `db:"client_msg_id"`
	MessageType     Type      `db:"message_type"`
	TextContent     string    `db:"text_content"`
	Status          Status    `db:"status"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
	DeletedAt       time.Time `db:"deleted_at"`
	Content         Content
}
