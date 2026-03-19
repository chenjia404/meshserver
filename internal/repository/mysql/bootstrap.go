package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"meshserver/internal/repository"
)

func (s *Store) Upsert(ctx context.Context, record repository.NodeRecord) (*repository.NodeRecord, error) {
	now := s.now()
	if record.NodeID == "" {
		record.NodeID = newExternalID("node")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO nodes (node_id, peer_id, name, public_addrs, status, created_at, updated_at)
		VALUES (?, ?, ?, CAST(? AS JSON), ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			public_addrs = VALUES(public_addrs),
			status = VALUES(status),
			updated_at = VALUES(updated_at)
	`, record.NodeID, record.PeerID, record.Name, marshalJSON(record.PublicAddrs), record.Status, now, now)
	if err != nil {
		return nil, fmt.Errorf("upsert node: %w", err)
	}

	var out repository.NodeRecord
	if err := fetchOne(ctx, s.db, `
		SELECT id, node_id, peer_id, name, status, created_at, updated_at
		FROM nodes
		WHERE peer_id = ?
		LIMIT 1
	`, []any{record.PeerID}, &out); err != nil {
		return nil, err
	}
	out.PublicAddrs = record.PublicAddrs
	return &out, nil
}

func refreshAggregateCounts(ctx context.Context, tx *sqlx.Tx, serverDBID uint32) error {
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, `
		UPDATE servers
		SET member_count = (SELECT COUNT(1) FROM server_members WHERE space_id = ? AND is_banned = 0),
			channel_count = (SELECT COUNT(1) FROM channels WHERE space_id = ? AND status = 1),
			updated_at = ?
		WHERE id = ?
	`, serverDBID, serverDBID, now, serverDBID); err != nil {
		return fmt.Errorf("refresh server counts: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE channels
		SET subscriber_count = (SELECT COUNT(1) FROM channel_members WHERE channel_id = channels.id),
			member_count = (SELECT COUNT(1) FROM channel_members WHERE channel_id = channels.id),
			updated_at = ?
		WHERE space_id = ?
	`, now, serverDBID); err != nil {
		return fmt.Errorf("refresh channel counts: %w", err)
	}
	return nil
}

var _ repository.NodeRepository = (*Store)(nil)
var _ repository.BootstrapRepository = (*Store)(nil)
