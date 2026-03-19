package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"meshserver/internal/config"
	"meshserver/internal/repository"
)

// Service handles challenge-response authentication.
type Service struct {
	cfg    *config.Config
	users  repository.UserRepository
	nonces repository.AuthNonceRepository
	logger *slog.Logger
}

// NewService creates an authentication service.
func NewService(cfg *config.Config, users repository.UserRepository, nonces repository.AuthNonceRepository, logger *slog.Logger) *Service {
	return &Service{
		cfg:    cfg,
		users:  users,
		nonces: nonces,
		logger: logger,
	}
}

// IssueChallenge creates and persists a short-lived auth challenge.
func (s *Service) IssueChallenge(ctx context.Context, clientPeerID string, nodePeerID string) (*Challenge, error) {
	rawNonce := make([]byte, 32)
	if _, err := rand.Read(rawNonce); err != nil {
		return nil, fmt.Errorf("generate challenge nonce: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	expiresAt := now.Add(s.cfg.ChallengeTTL)
	hash := nonceHash(rawNonce)
	if err := s.nonces.StoreNonce(ctx, hash, clientPeerID, nodePeerID, now, expiresAt); err != nil {
		return nil, err
	}

	challenge := &Challenge{
		ClientPeerID: clientPeerID,
		NodePeerID:   nodePeerID,
		Nonce:        rawNonce,
		IssuedAt:     now,
		ExpiresAt:    expiresAt,
	}
	challenge.Payload = BuildChallengePayload(s.cfg.Libp2pProtocolID, challenge)
	return challenge, nil
}

// VerifyChallenge validates a signed auth proof and returns the authenticated user.
func (s *Service) VerifyChallenge(ctx context.Context, in VerifyChallengeInput) (*Result, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	if now.After(in.ExpiresAt) || now.Before(in.IssuedAt.Add(-1*time.Minute)) {
		return nil, fmt.Errorf("challenge expired")
	}

	pubKey, err := libp2pcrypto.UnmarshalPublicKey(in.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("unmarshal public key: %w", err)
	}

	derivedPeerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("derive peer id from pubkey: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(derivedPeerID.String()), []byte(in.ClientPeerID)) != 1 {
		return nil, fmt.Errorf("peer id mismatch")
	}

	payload := BuildChallengePayload(s.cfg.Libp2pProtocolID, &Challenge{
		ClientPeerID: in.ClientPeerID,
		NodePeerID:   in.NodePeerID,
		Nonce:        in.Nonce,
		IssuedAt:     in.IssuedAt,
		ExpiresAt:    in.ExpiresAt,
	})
	ok, err := pubKey.Verify(payload, in.Signature)
	if err != nil {
		return nil, fmt.Errorf("verify challenge signature: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("invalid challenge signature")
	}

	if err := s.nonces.UseNonce(ctx, nonceHash(in.Nonce), in.ClientPeerID, in.NodePeerID, now); err != nil {
		return nil, fmt.Errorf("consume challenge nonce: %w", err)
	}

	user, err := s.users.CreateIfNotExistsByPeerID(ctx, in.ClientPeerID, in.PublicKey)
	if err != nil {
		return nil, err
	}
	if err := s.users.UpdateLogin(ctx, user.ID, in.PublicKey, now); err != nil {
		return nil, err
	}

	sessionID := newSessionID()
	s.logger.Info("authentication succeeded", "peer_id", in.ClientPeerID, "user_id", user.UserID)
	return &Result{
		SessionID: sessionID,
		User:      user,
		Message:   "authenticated",
	}, nil
}

// BuildChallengePayload returns the exact payload clients must sign.
func BuildChallengePayload(protocolID string, challenge *Challenge) []byte {
	return []byte(fmt.Sprintf(
		"protocol_id=%s\nclient_peer_id=%s\nnode_peer_id=%s\nnonce=%s\nissued_at_ms=%d\nexpires_at_ms=%d\n",
		protocolID,
		challenge.ClientPeerID,
		challenge.NodePeerID,
		base64.StdEncoding.EncodeToString(challenge.Nonce),
		challenge.IssuedAt.UnixMilli(),
		challenge.ExpiresAt.UnixMilli(),
	))
}

func nonceHash(nonce []byte) string {
	sum := sha256.Sum256(nonce)
	return hex.EncodeToString(sum[:])
}

func newSessionID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("sess_%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("sess_%s", hex.EncodeToString(buf))
}
