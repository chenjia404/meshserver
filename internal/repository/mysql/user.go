package mysql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"meshserver/internal/repository"
)

func (s *Store) GetByPeerID(ctx context.Context, peerID string) (*repository.User, error) {
	const query = `
		SELECT
			id,
			user_id,
			peer_id,
			pubkey,
			display_name,
			COALESCE(avatar_url, '') AS avatar_url,
			COALESCE(bio, '') AS bio,
			status,
			COALESCE(last_login_at, created_at) AS last_login_at,
			created_at,
			updated_at
		FROM users
		WHERE peer_id = ?
		LIMIT 1
	`

	var user repository.User
	if err := fetchOne(ctx, s.db, query, []any{peerID}, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetByID(ctx context.Context, id uint64) (*repository.User, error) {
	const query = `
		SELECT
			id,
			user_id,
			peer_id,
			pubkey,
			display_name,
			COALESCE(avatar_url, '') AS avatar_url,
			COALESCE(bio, '') AS bio,
			status,
			COALESCE(last_login_at, created_at) AS last_login_at,
			created_at,
			updated_at
		FROM users
		WHERE id = ?
		LIMIT 1
	`

	var user repository.User
	if err := fetchOne(ctx, s.db, query, []any{id}, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetByUserID(ctx context.Context, userID string) (*repository.User, error) {
	const query = `
		SELECT
			id,
			user_id,
			peer_id,
			pubkey,
			display_name,
			COALESCE(avatar_url, '') AS avatar_url,
			COALESCE(bio, '') AS bio,
			status,
			COALESCE(last_login_at, created_at) AS last_login_at,
			created_at,
			updated_at
		FROM users
		WHERE user_id = ?
		LIMIT 1
	`

	var user repository.User
	if err := fetchOne(ctx, s.db, query, []any{userID}, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) CreateIfNotExistsByPeerID(ctx context.Context, peerID string, pubKey []byte) (*repository.User, error) {
	now := s.now()
	displayName := "Peer " + shortPeerID(peerID)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (
			user_id, peer_id, pubkey, display_name, status, last_login_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			pubkey = CASE WHEN VALUES(pubkey) IS NULL OR LENGTH(VALUES(pubkey)) = 0 THEN pubkey ELSE VALUES(pubkey) END,
			display_name = display_name,
			updated_at = VALUES(updated_at)
	`, newExternalID("u"), peerID, emptyBytesToNil(pubKey), displayName, now, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert user by peer id: %w", err)
	}

	user, err := s.GetByPeerID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Store) UpdateLogin(ctx context.Context, userID uint64, pubKey []byte, when time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET pubkey = CASE WHEN ? IS NULL OR LENGTH(?) = 0 THEN pubkey ELSE ? END,
			last_login_at = ?,
			updated_at = ?
		WHERE id = ?
	`, emptyBytesToNil(pubKey), emptyBytesToNil(pubKey), emptyBytesToNil(pubKey), when, when, userID)
	if err != nil {
		return fmt.Errorf("update user login: %w", err)
	}
	return nil
}

func shortPeerID(peerID string) string {
	peerID = strings.TrimSpace(peerID)
	if len(peerID) <= 8 {
		return peerID
	}
	return peerID[len(peerID)-8:]
}

func emptyBytesToNil(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return value
}

var _ repository.UserRepository = (*Store)(nil)
