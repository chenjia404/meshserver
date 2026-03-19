package mysql

import (
	"context"
	"fmt"
	"time"

	"meshserver/internal/repository"
)

func (s *Store) UpsertDeliveredSeq(ctx context.Context, userID uint64, channelID uint32, seq uint64) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_channel_reads (user_id, channel_id, last_delivered_seq, last_read_seq, updated_at)
		SELECT ?, c.id, ?, 0, ?
		FROM channels c
		WHERE c.id = ?
		ON DUPLICATE KEY UPDATE
			last_delivered_seq = GREATEST(last_delivered_seq, VALUES(last_delivered_seq)),
			updated_at = VALUES(updated_at)
	`, userID, seq, now, channelID)
	if err != nil {
		return fmt.Errorf("upsert delivered seq: %w", err)
	}
	return nil
}

func (s *Store) UpsertReadSeq(ctx context.Context, userID uint64, channelID uint32, seq uint64) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_channel_reads (user_id, channel_id, last_delivered_seq, last_read_seq, updated_at)
		SELECT ?, c.id, 0, ?, ?
		FROM channels c
		WHERE c.id = ?
		ON DUPLICATE KEY UPDATE
			last_read_seq = GREATEST(last_read_seq, VALUES(last_read_seq)),
			updated_at = VALUES(updated_at)
	`, userID, seq, now, channelID)
	if err != nil {
		return fmt.Errorf("upsert read seq: %w", err)
	}
	return nil
}

func (s *Store) StoreNonce(ctx context.Context, nonceHash string, clientPeerID string, nodePeerID string, issuedAt time.Time, expiresAt time.Time) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_nonces (
			nonce_hash, client_peer_id, node_peer_id, issued_at, expires_at, used_at, created_at
		)
		VALUES (?, ?, ?, ?, ?, NULL, ?)
	`, nonceHash, clientPeerID, nodePeerID, issuedAt, expiresAt, now)
	if err != nil {
		return fmt.Errorf("store nonce: %w", err)
	}
	return nil
}

func (s *Store) UseNonce(ctx context.Context, nonceHash string, clientPeerID string, nodePeerID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE auth_nonces
		SET used_at = ?
		WHERE nonce_hash = ? AND client_peer_id = ? AND node_peer_id = ? AND used_at IS NULL AND expires_at >= ?
	`, now, nonceHash, clientPeerID, nodePeerID, now)
	if err != nil {
		return fmt.Errorf("use nonce: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("use nonce rows affected: %w", err)
	}
	if affected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

var _ repository.ReadCursorRepository = (*Store)(nil)
var _ repository.AuthNonceRepository = (*Store)(nil)
