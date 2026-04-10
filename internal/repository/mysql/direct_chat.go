package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"meshserver/internal/message"
	"meshserver/internal/repository"
)

func normalizeUserPair(a, b uint64) (low, high uint64) {
	if a < b {
		return a, b
	}
	return b, a
}

var _ repository.DirectChatRepository = (*Store)(nil)

func (s *Store) GetOrCreateConversation(ctx context.Context, userAID, userBID uint64) (*repository.DirectConversation, error) {
	if userAID == userBID {
		return nil, fmt.Errorf("invalid peer")
	}
	low, high := normalizeUserPair(userAID, userBID)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var row struct {
		ID              uint64    `db:"id"`
		ConversationID  string    `db:"conversation_id"`
		UserLowID       uint64    `db:"user_low_id"`
		UserHighID      uint64    `db:"user_high_id"`
		LastSeq         uint64    `db:"last_seq"`
		LastMessageAtMs uint64    `db:"last_message_at_ms"`
		CreatedAt       time.Time `db:"created_at"`
		UpdatedAt       time.Time `db:"updated_at"`
	}
	q := `SELECT id, conversation_id, user_low_id, user_high_id, last_seq, last_message_at_ms, created_at, updated_at
		FROM direct_conversations WHERE user_low_id = ? AND user_high_id = ? FOR UPDATE`
	err = sqlx.GetContext(ctx, tx, &row, q, low, high)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &repository.DirectConversation{
			ID:              row.ID,
			ConversationID: row.ConversationID,
			UserLowID:       row.UserLowID,
			UserHighID:      row.UserHighID,
			LastSeq:         row.LastSeq,
			LastMessageAtMs: row.LastMessageAtMs,
		}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	now := s.now()
	cid := newExternalID("dmcnv")
	_, err = tx.ExecContext(ctx, `
		INSERT INTO direct_conversations (conversation_id, user_low_id, user_high_id, last_seq, last_message_at_ms, created_at, updated_at)
		VALUES (?, ?, ?, 0, 0, ?, ?)
	`, cid, low, high, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert direct_conversations: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &repository.DirectConversation{
		ConversationID: cid,
		UserLowID:      low,
		UserHighID:     high,
		LastSeq:        0,
	}, nil
}

func (s *Store) GetConversationByExternalID(ctx context.Context, conversationID string) (*repository.DirectConversation, error) {
	var row struct {
		ID              uint64 `db:"id"`
		ConversationID  string `db:"conversation_id"`
		UserLowID       uint64 `db:"user_low_id"`
		UserHighID      uint64 `db:"user_high_id"`
		LastSeq         uint64 `db:"last_seq"`
		LastMessageAtMs uint64 `db:"last_message_at_ms"`
	}
	err := sqlx.GetContext(ctx, s.db, &row, `
		SELECT id, conversation_id, user_low_id, user_high_id, last_seq, last_message_at_ms
		FROM direct_conversations WHERE conversation_id = ?
	`, strings.TrimSpace(conversationID))
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &repository.DirectConversation{
		ID:              row.ID,
		ConversationID:  row.ConversationID,
		UserLowID:       row.UserLowID,
		UserHighID:      row.UserHighID,
		LastSeq:         row.LastSeq,
		LastMessageAtMs: row.LastMessageAtMs,
	}, nil
}

func (s *Store) ListConversationsForUser(ctx context.Context, userID uint64) ([]repository.DirectConversationListItem, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT dc.conversation_id, dc.user_low_id, dc.user_high_id, dc.last_seq, dc.last_message_at_ms,
			ul.user_id AS low_uid, ul.display_name AS low_name,
			uh.user_id AS high_uid, uh.display_name AS high_name
		FROM direct_conversations dc
		JOIN users ul ON ul.id = dc.user_low_id
		JOIN users uh ON uh.id = dc.user_high_id
		WHERE dc.user_low_id = ? OR dc.user_high_id = ?
		ORDER BY dc.updated_at DESC
	`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []repository.DirectConversationListItem
	for rows.Next() {
		var (
			convID, lowUID, highUID string
			userLowID, userHighID   uint64
			lastSeq, lastAt         uint64
			lowName, highName       string
		)
		if err := rows.Scan(&convID, &userLowID, &userHighID, &lastSeq, &lastAt, &lowUID, &lowName, &highUID, &highName); err != nil {
			return nil, err
		}
		var peerUID, peerName string
		if userID == userLowID {
			peerUID, peerName = highUID, highName
		} else {
			peerUID, peerName = lowUID, lowName
		}
		out = append(out, repository.DirectConversationListItem{
			ConversationID:  convID,
			PeerUserID:      peerUID,
			PeerDisplayName: peerName,
			LastSeq:         lastSeq,
			LastMessageAtMs: lastAt,
		})
	}
	return out, rows.Err()
}

func (s *Store) loadDirectMessageRow(ctx context.Context, exec sqlx.ExtContext, where string, args ...any) (*repository.DirectMessage, error) {
	q := `
		SELECT dm.message_id, dm.conversation_id, dm.seq, dm.sender_user_id, dm.recipient_user_id,
			dm.client_msg_id, dm.message_type, dm.text_content, dm.created_at_ms, dm.recipient_acked_at_ms,
			su.user_id AS sender_ext, ru.user_id AS recv_ext
		FROM direct_messages dm
		JOIN users su ON su.id = dm.sender_user_id
		JOIN users ru ON ru.id = dm.recipient_user_id
		WHERE ` + where
	var row struct {
		MessageID          string         `db:"message_id"`
		ConversationID     string         `db:"conversation_id"`
		Seq                uint64         `db:"seq"`
		SenderUserID       uint64         `db:"sender_user_id"`
		RecipientUserID    uint64         `db:"recipient_user_id"`
		ClientMsgID        string         `db:"client_msg_id"`
		MessageType        string         `db:"message_type"`
		TextContent        sql.NullString `db:"text_content"`
		CreatedAtMs        uint64         `db:"created_at_ms"`
		RecipientAcked     sql.NullInt64  `db:"recipient_acked_at_ms"`
		SenderExt          string         `db:"sender_ext"`
		RecvExt            string         `db:"recv_ext"`
	}
	if err := sqlx.GetContext(ctx, exec, &row, q, args...); err != nil {
		return nil, normalizeNotFound(err)
	}
	var acked *uint64
	if row.RecipientAcked.Valid {
		v := uint64(row.RecipientAcked.Int64)
		acked = &v
	}
	return &repository.DirectMessage{
		MessageID:           row.MessageID,
		ConversationID:      row.ConversationID,
		Seq:                 row.Seq,
		SenderUserID:        row.SenderUserID,
		RecipientUserID:     row.RecipientUserID,
		SenderExternalID:    row.SenderExt,
		RecipientExternalID: row.RecvExt,
		ClientMsgID:         row.ClientMsgID,
		MessageType:         message.Type(row.MessageType),
		Text:                row.TextContent.String,
		CreatedAtMs:         row.CreatedAtMs,
		RecipientAckedAtMs:  acked,
	}, nil
}

func (s *Store) GetDirectMessageByClientMsgID(ctx context.Context, conversationID string, senderUserID uint64, clientMsgID string) (*repository.DirectMessage, error) {
	return s.loadDirectMessageRow(ctx, s.db, `dm.conversation_id = ? AND dm.sender_user_id = ? AND dm.client_msg_id = ?`, conversationID, senderUserID, clientMsgID)
}

func (s *Store) GetDirectMessageByMessageID(ctx context.Context, messageID string) (*repository.DirectMessage, error) {
	return s.loadDirectMessageRow(ctx, s.db, `dm.message_id = ?`, messageID)
}

func (s *Store) CreateDirectMessage(ctx context.Context, in repository.CreateDirectMessageInput) (*repository.DirectMessage, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var conv struct {
		ID              uint64 `db:"id"`
		ConversationID  string `db:"conversation_id"`
		UserLowID       uint64 `db:"user_low_id"`
		UserHighID      uint64 `db:"user_high_id"`
		LastSeq         uint64 `db:"last_seq"`
		LastMessageAtMs uint64 `db:"last_message_at_ms"`
	}
	if err = sqlx.GetContext(ctx, tx, &conv, `
		SELECT id, conversation_id, user_low_id, user_high_id, last_seq, last_message_at_ms
		FROM direct_conversations WHERE conversation_id = ? FOR UPDATE
	`, in.ConversationID); err != nil {
		return nil, normalizeNotFound(err)
	}
	if in.SenderUserID != conv.UserLowID && in.SenderUserID != conv.UserHighID {
		return nil, repository.ErrDirectChatForbidden
	}
	if in.RecipientUserID != conv.UserLowID && in.RecipientUserID != conv.UserHighID {
		return nil, repository.ErrDirectChatForbidden
	}
	if in.SenderUserID == in.RecipientUserID {
		return nil, fmt.Errorf("invalid recipient")
	}

	existing, err := s.loadDirectMessageRow(ctx, tx, `dm.conversation_id = ? AND dm.sender_user_id = ? AND dm.client_msg_id = ?`,
		in.ConversationID, in.SenderUserID, in.ClientMsgID)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if err != repository.ErrNotFound {
		return nil, err
	}

	now := s.now()
	createdMs := uint64(now.UnixMilli())
	msgID := newExternalID("dmmsg")
	nextSeq := conv.LastSeq + 1
	mt := string(in.MessageType)
	if mt == "" {
		mt = string(message.TypeText)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO direct_messages (
			message_id, conversation_id, seq, sender_user_id, recipient_user_id,
			client_msg_id, message_type, text_content, created_at_ms, recipient_acked_at_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)
	`, msgID, in.ConversationID, nextSeq, in.SenderUserID, in.RecipientUserID, in.ClientMsgID, mt, nullableString(in.Text), createdMs, now)
	if err != nil {
		return nil, fmt.Errorf("insert direct_messages: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE direct_conversations SET last_seq = ?, last_message_at_ms = ?, updated_at = ? WHERE id = ?
	`, nextSeq, createdMs, now, conv.ID)
	if err != nil {
		return nil, fmt.Errorf("update direct_conversations: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetDirectMessageByMessageID(ctx, msgID)
}

func (s *Store) AckDirectMessage(ctx context.Context, messageID string, recipientUserID uint64, ackedAtMs uint64) (bool, uint64, string, error) {
	msg, err := s.GetDirectMessageByMessageID(ctx, messageID)
	if err != nil {
		return false, 0, "", err
	}
	if msg.RecipientUserID != recipientUserID {
		return false, 0, "", repository.ErrDirectChatForbidden
	}
	if msg.RecipientAckedAtMs != nil {
		return true, msg.SenderUserID, msg.ConversationID, nil
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE direct_messages SET recipient_acked_at_ms = ? WHERE message_id = ? AND recipient_user_id = ? AND recipient_acked_at_ms IS NULL
	`, ackedAtMs, messageID, recipientUserID)
	if err != nil {
		return false, 0, "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// 并发下可能已被其它请求 ACK
		m2, err := s.GetDirectMessageByMessageID(ctx, messageID)
		if err != nil {
			return false, 0, "", err
		}
		if m2.RecipientAckedAtMs != nil {
			return true, m2.SenderUserID, m2.ConversationID, nil
		}
		return false, 0, "", fmt.Errorf("ack failed")
	}
	return false, msg.SenderUserID, msg.ConversationID, nil
}

func (s *Store) ListPendingDirectMessages(ctx context.Context, conversationID string, recipientUserID uint64, afterSeq uint64, limit uint32) ([]*repository.DirectMessage, uint64, bool, error) {
	if limit == 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryxContext(ctx, `
		SELECT dm.message_id, dm.conversation_id, dm.seq, dm.sender_user_id, dm.recipient_user_id,
			dm.client_msg_id, dm.message_type, dm.text_content, dm.created_at_ms, dm.recipient_acked_at_ms,
			su.user_id AS sender_ext, ru.user_id AS recv_ext
		FROM direct_messages dm
		JOIN users su ON su.id = dm.sender_user_id
		JOIN users ru ON ru.id = dm.recipient_user_id
		WHERE dm.conversation_id = ? AND dm.recipient_user_id = ? AND dm.recipient_acked_at_ms IS NULL AND dm.seq > ?
		ORDER BY dm.seq ASC
		LIMIT ?
	`, conversationID, recipientUserID, afterSeq, limit)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()

	var out []*repository.DirectMessage
	for rows.Next() {
		var row struct {
			MessageID       string         `db:"message_id"`
			ConversationID  string         `db:"conversation_id"`
			Seq             uint64         `db:"seq"`
			SenderUserID    uint64         `db:"sender_user_id"`
			RecipientUserID uint64         `db:"recipient_user_id"`
			ClientMsgID     string         `db:"client_msg_id"`
			MessageType     string         `db:"message_type"`
			TextContent     sql.NullString `db:"text_content"`
			CreatedAtMs     uint64         `db:"created_at_ms"`
			RecipientAcked  sql.NullInt64  `db:"recipient_acked_at_ms"`
			SenderExt       string         `db:"sender_ext"`
			RecvExt         string         `db:"recv_ext"`
		}
		if err := rows.StructScan(&row); err != nil {
			return nil, 0, false, err
		}
		var acked *uint64
		if row.RecipientAcked.Valid {
			v := uint64(row.RecipientAcked.Int64)
			acked = &v
		}
		out = append(out, &repository.DirectMessage{
			MessageID:           row.MessageID,
			ConversationID:      row.ConversationID,
			Seq:                 row.Seq,
			SenderUserID:        row.SenderUserID,
			RecipientUserID:     row.RecipientUserID,
			SenderExternalID:    row.SenderExt,
			RecipientExternalID: row.RecvExt,
			ClientMsgID:         row.ClientMsgID,
			MessageType:         message.Type(row.MessageType),
			Text:                row.TextContent.String,
			CreatedAtMs:         row.CreatedAtMs,
			RecipientAckedAtMs:  acked,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}
	var nextAfter uint64 = afterSeq
	hasMore := uint32(len(out)) == limit
	if len(out) > 0 {
		nextAfter = out[len(out)-1].Seq
	}
	return out, nextAfter, hasMore, nil
}
