package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"meshserver/internal/media"
	"meshserver/internal/message"
	"meshserver/internal/repository"
)

type messageRow struct {
	ID              uint64         `db:"id"`
	MessageID       string         `db:"message_id"`
	ChannelDBID     uint32         `db:"channel_id"`
	Seq             uint64         `db:"seq"`
	SenderUserID    uint64         `db:"sender_user_id"`
	SenderUserExtID string         `db:"sender_external_id"`
	ClientMsgID     string         `db:"client_msg_id"`
	MessageType     message.Type   `db:"message_type"`
	TextContent     sql.NullString `db:"text_content"`
	Status          message.Status `db:"status"`
	CreatedAt       time.Time      `db:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at"`
	DeletedAt       sql.NullTime   `db:"deleted_at"`
}

type attachmentRow struct {
	MessageDBID  uint64     `db:"message_db_id"`
	ID           uint64     `db:"id"`
	MediaID      string     `db:"media_id"`
	BlobDBID     uint64     `db:"blob_id"`
	BlobID       string     `db:"blob_external_id"`
	Kind         media.Kind `db:"kind"`
	OriginalName string     `db:"original_name"`
	MIMEType     string     `db:"mime_type"`
	Size         uint64     `db:"size"`
	Width        uint32     `db:"width"`
	Height       uint32     `db:"height"`
	CreatedBy    uint64     `db:"created_by"`
	CreatedAt    time.Time  `db:"created_at"`
	SHA256       string     `db:"sha256"`
	StoragePath  string     `db:"storage_path"`
}

func (s *Store) Create(ctx context.Context, in repository.CreateMessageInput) (*message.Message, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create message tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	channelRow, err := s.getChannelForUpdate(ctx, tx, in.ChannelID)
	if err != nil {
		return nil, err
	}

	existing, err := s.getMessageByClientMsgIDTx(ctx, tx, channelRow.ID, in.SenderUserID, in.ClientMsgID)
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}
	if err == nil {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit create message tx (existing): %w", err)
		}
		return s.loadMessageByID(ctx, existing.ID)
	}

	now := s.now()
	nextSeq := channelRow.MessageSeq + 1
	messageID := newExternalID("msg")

	res, execErr := tx.ExecContext(ctx, `
		INSERT INTO messages (
			message_id, channel_id, seq, sender_user_id, client_msg_id, message_type,
			text_content, status, created_at, updated_at, deleted_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'normal', ?, ?, NULL)
	`, messageID, channelRow.ID, nextSeq, in.SenderUserID, in.ClientMsgID, in.MessageType, nullableString(in.Text), now, now)
	if execErr != nil {
		err = fmt.Errorf("insert message: %w", execErr)
		return nil, err
	}

	messageDBID, execErr := res.LastInsertId()
	if execErr != nil {
		err = fmt.Errorf("get inserted message id: %w", execErr)
		return nil, err
	}

	for index, mediaID := range in.AttachmentMedia {
		if _, execErr = tx.ExecContext(ctx, `
			INSERT INTO message_attachments (message_id, media_id, sort_order, created_at)
			VALUES (?, ?, ?, ?)
		`, messageDBID, mediaID, index, now); execErr != nil {
			err = fmt.Errorf("insert message attachment: %w", execErr)
			return nil, err
		}

		if _, execErr = tx.ExecContext(ctx, `
			UPDATE blobs b
			INNER JOIN media_objects mo ON mo.blob_id = b.id
			SET b.ref_count = b.ref_count + 1
			WHERE mo.id = ?
		`, mediaID); execErr != nil {
			err = fmt.Errorf("increment blob ref count: %w", execErr)
			return nil, err
		}
	}

	if _, execErr = tx.ExecContext(ctx, `
		UPDATE channels
		SET message_seq = ?, message_count = message_count + 1, updated_at = ?
		WHERE id = ?
	`, nextSeq, now, channelRow.ID); execErr != nil {
		err = fmt.Errorf("update channel seq: %w", execErr)
		return nil, err
	}

	if execErr = tx.Commit(); execErr != nil {
		err = fmt.Errorf("commit create message tx: %w", execErr)
		return nil, err
	}

	return s.loadMessageByID(ctx, uint64(messageDBID))
}

func (s *Store) ListAfterSeq(ctx context.Context, channelID uint32, afterSeq uint64, limit uint32) ([]*message.Message, error) {
	const query = `
		SELECT
			m.id,
			m.message_id,
			m.channel_id,
			m.seq,
			m.sender_user_id,
			u.user_id AS sender_external_id,
			m.client_msg_id,
			m.message_type,
			m.text_content,
			m.status,
			m.created_at,
			m.updated_at,
			m.deleted_at
		FROM messages m
		INNER JOIN channels c ON c.id = m.channel_id
		INNER JOIN users u ON u.id = m.sender_user_id
		WHERE c.id = ? AND m.seq > ? AND m.status = 'normal'
		ORDER BY m.seq ASC
		LIMIT ?
	`

	var rows []messageRow
	if err := s.db.SelectContext(ctx, &rows, query, channelID, afterSeq, limit); err != nil {
		return nil, fmt.Errorf("list messages after seq: %w", err)
	}
	return s.hydrateMessages(ctx, rows)
}

func (s *Store) GetByClientMsgID(ctx context.Context, channelID uint32, senderUserID uint64, clientMsgID string) (*message.Message, error) {
	channelRow, err := s.GetByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	row, err := s.getMessageByClientMsgIDTx(ctx, s.db, channelRow.ID, senderUserID, clientMsgID)
	if err != nil {
		return nil, err
	}
	return s.loadMessageByID(ctx, row.ID)
}

func (s *Store) GetLastMessageTime(ctx context.Context, channelID uint32, senderUserID uint64) (*time.Time, error) {
	const query = `
		SELECT m.created_at
		FROM messages m
		INNER JOIN channels c ON c.id = m.channel_id
		WHERE c.id = ? AND m.sender_user_id = ?
		ORDER BY m.seq DESC
		LIMIT 1
	`
	var when time.Time
	if err := s.db.GetContext(ctx, &when, query, channelID, senderUserID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get last message time: %w", err)
	}
	return &when, nil
}

func (s *Store) ListChannelIDsByMediaID(ctx context.Context, mediaID string) ([]uint32, error) {
	const query = `
		SELECT DISTINCT m.channel_id
		FROM message_attachments ma
		INNER JOIN media_objects mo ON mo.id = ma.media_id
		INNER JOIN messages m ON m.id = ma.message_id
		WHERE mo.media_id = ?
		ORDER BY m.channel_id ASC
	`
	var ids []uint32
	if err := s.db.SelectContext(ctx, &ids, query, mediaID); err != nil {
		return nil, fmt.Errorf("list channel ids by media id: %w", err)
	}
	return ids, nil
}

func (s *Store) CleanupExpiredMessages(ctx context.Context, now time.Time, limit uint32) (uint32, error) {
	if limit == 0 {
		limit = 100
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin cleanup expired messages tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	type expiredRow struct {
		ID        uint64 `db:"id"`
		ChannelID uint32 `db:"channel_id"`
	}
	var rows []expiredRow
	if err = tx.SelectContext(ctx, &rows, `
		SELECT
			m.id,
			m.channel_id
		FROM messages m
		INNER JOIN channels c ON c.id = m.channel_id
		WHERE m.status = 'normal'
			AND c.auto_delete_after_seconds > 0
			AND m.created_at < DATE_SUB(?, INTERVAL c.auto_delete_after_seconds SECOND)
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT ?
	`, now, limit); err != nil {
		return 0, fmt.Errorf("load expired messages: %w", err)
	}
	if len(rows) == 0 {
		if err = tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit cleanup expired messages tx (empty): %w", err)
		}
		return 0, nil
	}

	deletedByChannel := make(map[uint32]uint64)
	for _, row := range rows {
		res, execErr := tx.ExecContext(ctx, `
			UPDATE messages
			SET status = 'deleted', deleted_at = ?, updated_at = ?
			WHERE id = ? AND status = 'normal'
		`, now, now, row.ID)
		if execErr != nil {
			err = fmt.Errorf("mark expired message deleted: %w", execErr)
			return 0, err
		}
		affected, execErr := res.RowsAffected()
		if execErr != nil {
			err = fmt.Errorf("expired message rows affected: %w", execErr)
			return 0, err
		}
		if affected > 0 {
			deletedByChannel[row.ChannelID]++
		}
	}

	for channelID, count := range deletedByChannel {
		if count == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE channels
			SET message_count = CASE
				WHEN message_count > ? THEN message_count - ?
				ELSE 0
			END,
			updated_at = ?
			WHERE id = ?
		`, count, count, now, channelID); err != nil {
			return 0, fmt.Errorf("update expired channel count: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit cleanup expired messages tx: %w", err)
	}

	var total uint32
	for _, count := range deletedByChannel {
		total += uint32(count)
	}
	return total, nil
}

func (s *Store) loadMessageByID(ctx context.Context, id uint64) (*message.Message, error) {
	const query = `
		SELECT
			m.id,
			m.message_id,
			m.channel_id,
			m.seq,
			m.sender_user_id,
			u.user_id AS sender_external_id,
			m.client_msg_id,
			m.message_type,
			m.text_content,
			m.status,
			m.created_at,
			m.updated_at,
			m.deleted_at
		FROM messages m
		INNER JOIN channels c ON c.id = m.channel_id
		INNER JOIN users u ON u.id = m.sender_user_id
		WHERE m.id = ?
		LIMIT 1
	`
	var row messageRow
	if err := fetchOne(ctx, s.db, query, []any{id}, &row); err != nil {
		return nil, err
	}

	items, err := s.hydrateMessages(ctx, []messageRow{row})
	if err != nil {
		return nil, err
	}
	return items[0], nil
}

func (s *Store) hydrateMessages(ctx context.Context, rows []messageRow) ([]*message.Message, error) {
	if len(rows) == 0 {
		return []*message.Message{}, nil
	}

	items := make([]*message.Message, 0, len(rows))
	ids := make([]uint64, 0, len(rows))
	index := make(map[uint64]*message.Message, len(rows))
	for _, row := range rows {
		item := &message.Message{
			ID:              row.ID,
			MessageID:       row.MessageID,
			ChannelDBID:     row.ChannelDBID,
			Seq:             row.Seq,
			SenderUserID:    row.SenderUserID,
			SenderUserExtID: row.SenderUserExtID,
			ClientMsgID:     row.ClientMsgID,
			MessageType:     row.MessageType,
			TextContent:     row.TextContent.String,
			Status:          row.Status,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			Content: message.Content{
				Text: row.TextContent.String,
			},
		}
		if row.DeletedAt.Valid {
			item.DeletedAt = row.DeletedAt.Time
		}
		items = append(items, item)
		index[row.ID] = item
		ids = append(ids, row.ID)
	}

	query, args, err := sqlx.In(`
		SELECT
			ma.message_id AS message_db_id,
			mo.id,
			mo.media_id,
			mo.blob_id,
			b.blob_id AS blob_external_id,
			mo.kind,
			COALESCE(mo.original_name, '') AS original_name,
			COALESCE(mo.mime_type, '') AS mime_type,
			mo.size,
			COALESCE(mo.width, 0) AS width,
			COALESCE(mo.height, 0) AS height,
			mo.created_by,
			mo.created_at,
			b.sha256,
			b.storage_path
		FROM message_attachments ma
		INNER JOIN media_objects mo ON mo.id = ma.media_id
		INNER JOIN blobs b ON b.id = mo.blob_id
		WHERE ma.message_id IN (?)
		ORDER BY ma.message_id ASC, ma.sort_order ASC
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("build attachment query: %w", err)
	}

	var attachments []attachmentRow
	if err := s.db.SelectContext(ctx, &attachments, query, args...); err != nil {
		return nil, fmt.Errorf("load message attachments: %w", err)
	}

	for _, row := range attachments {
		target := index[row.MessageDBID]
		if target == nil {
			continue
		}
		obj := &message.Attachment{
			ID:           row.ID,
			MediaID:      row.MediaID,
			BlobDBID:     row.BlobDBID,
			BlobID:       row.BlobID,
			Kind:         string(row.Kind),
			OriginalName: row.OriginalName,
			MIMEType:     row.MIMEType,
			Size:         row.Size,
			Width:        row.Width,
			Height:       row.Height,
			CreatedBy:    row.CreatedBy,
			CreatedAt:    row.CreatedAt,
			SHA256:       row.SHA256,
			StoragePath:  row.StoragePath,
		}
		if row.Kind == media.KindImage {
			target.Content.Images = append(target.Content.Images, obj)
		} else {
			target.Content.Files = append(target.Content.Files, obj)
		}
	}

	return items, nil
}

func (s *Store) getChannelForUpdate(ctx context.Context, exec sqlx.ExtContext, channelID uint32) (*struct {
	ID         uint32 `db:"id"`
	MessageSeq uint64 `db:"message_seq"`
}, error) {
	var row struct {
		ID         uint32 `db:"id"`
		MessageSeq uint64 `db:"message_seq"`
	}
	if err := fetchOne(ctx, exec, `
		SELECT id, message_seq
		FROM channels
		WHERE id = ?
		FOR UPDATE
	`, []any{channelID}, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) getMessageByClientMsgIDTx(ctx context.Context, exec sqlx.ExtContext, channelDBID uint32, senderUserID uint64, clientMsgID string) (*message.Message, error) {
	var row message.Message
	err := fetchOne(ctx, exec, `
		SELECT id
		FROM messages
		WHERE channel_id = ? AND sender_user_id = ? AND client_msg_id = ?
		LIMIT 1
	`, []any{channelDBID, senderUserID, clientMsgID}, &row)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ repository.MessageRepository = (*Store)(nil)
