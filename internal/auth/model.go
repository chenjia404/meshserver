package auth

import (
	"time"

	"meshserver/internal/repository"
)

// User is the authenticated application user model.
type User = repository.User

// Challenge is the issued challenge sent to the client.
type Challenge struct {
	ClientPeerID string
	NodePeerID   string
	Nonce        []byte
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Payload      []byte
}

// VerifyChallengeInput is the verification request data.
type VerifyChallengeInput struct {
	ClientPeerID string
	NodePeerID   string
	Nonce        []byte
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Signature    []byte
	PublicKey    []byte
}

// Result is the outcome of a successful authentication.
type Result struct {
	SessionID string
	User      *repository.User
	Message   string
}
