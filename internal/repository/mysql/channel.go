package mysql

import (
	"context"
	"fmt"
	"strings"

	"meshserver/internal/channel"
	"meshserver/internal/repository"
	"meshserver/internal/space"
)

type channelRow struct {
	channel.Channel
	Role             string `db:"role"`
	CanView          bool   `db:"can_view"`
	CanSendMessage   bool   `db:"can_send_message"`
	CanSendImage     bool   `db:"can_send_image"`
	CanSendFile      bool   `db:"can_send_file"`
	CanDeleteMessage bool   `db:"can_delete_message"`
	CanManageChannel bool   `db:"can_manage_channel"`
}

func (s *Store) ListBySpaceIDForUser(ctx context.Context, spaceID uint32, userID uint64) ([]*channel.Channel, error) {
	const query = `
		SELECT
			c.id,
			c.space_id,
			c.type,
			c.name,
			COALESCE(c.description, '') AS description,
			c.visibility,
			c.slow_mode_seconds,
			c.auto_delete_after_seconds,
			c.message_seq,
			c.message_count,
			c.member_count,
			c.created_by,
			c.status,
			c.sort_order,
			c.created_at,
			c.updated_at,
			cm.role,
			cm.can_view,
			cm.can_send_message,
			cm.can_send_image,
			cm.can_send_file,
			cm.can_delete_message,
			cm.can_manage_channel
		FROM channels c
		INNER JOIN servers s ON s.id = c.space_id
		INNER JOIN channel_members cm ON cm.channel_id = c.id
		WHERE s.id = ? AND cm.user_id = ? AND c.status = 1
		ORDER BY c.sort_order ASC, c.name ASC
	`

	var rows []*channelRow
	if err := s.db.SelectContext(ctx, &rows, query, spaceID, userID); err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	items := make([]*channel.Channel, 0, len(rows))
	for _, row := range rows {
		row.Channel.Permission = channel.Permission{
			Role:             space.Role(row.Role),
			CanView:          row.CanView,
			CanSendMessage:   row.CanSendMessage,
			CanSendImage:     row.CanSendImage,
			CanSendFile:      row.CanSendFile,
			CanDeleteMessage: row.CanDeleteMessage,
			CanManageChannel: row.CanManageChannel,
		}
		items = append(items, &row.Channel)
	}

	return items, nil
}

func (s *Store) GetByChannelID(ctx context.Context, channelID uint32) (*channel.Channel, error) {
	const query = `
		SELECT
			c.id,
			c.space_id,
			c.type,
			c.name,
			COALESCE(c.description, '') AS description,
			c.visibility,
			c.slow_mode_seconds,
			c.auto_delete_after_seconds,
			c.message_seq,
			c.message_count,
			c.member_count,
			c.created_by,
			c.status,
			c.sort_order,
			c.created_at,
			c.updated_at
		FROM channels c
		INNER JOIN servers s ON s.id = c.space_id
		WHERE c.id = ?
		LIMIT 1
	`

	var item channel.Channel
	if err := fetchOne(ctx, s.db, query, []any{channelID}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) IsUserMember(ctx context.Context, channelID uint32, userID uint64) (bool, error) {
	const query = `
		SELECT COUNT(1)
		FROM channel_members cm
		INNER JOIN channels c ON c.id = cm.channel_id
		WHERE c.id = ? AND cm.user_id = ?
	`
	var count int
	if err := s.db.GetContext(ctx, &count, query, channelID, userID); err != nil {
		return false, fmt.Errorf("count channel membership: %w", err)
	}
	return count > 0, nil
}

func (s *Store) GetPermission(ctx context.Context, channelID uint32, userID uint64) (*channel.Permission, error) {
	const query = `
		SELECT
			cm.role,
			cm.can_view,
			cm.can_send_message,
			cm.can_send_image,
			cm.can_send_file,
			cm.can_delete_message,
			cm.can_manage_channel
		FROM channel_members cm
		INNER JOIN channels c ON c.id = cm.channel_id
		WHERE c.id = ? AND cm.user_id = ?
		LIMIT 1
	`
	var row struct {
		Role             string `db:"role"`
		CanView          bool   `db:"can_view"`
		CanSendMessage   bool   `db:"can_send_message"`
		CanSendImage     bool   `db:"can_send_image"`
		CanSendFile      bool   `db:"can_send_file"`
		CanDeleteMessage bool   `db:"can_delete_message"`
		CanManageChannel bool   `db:"can_manage_channel"`
	}
	if err := fetchOne(ctx, s.db, query, []any{channelID, userID}, &row); err != nil {
		return nil, err
	}
	return &channel.Permission{
		Role:             space.Role(row.Role),
		CanView:          row.CanView,
		CanSendMessage:   row.CanSendMessage,
		CanSendImage:     row.CanSendImage,
		CanSendFile:      row.CanSendFile,
		CanDeleteMessage: row.CanDeleteMessage,
		CanManageChannel: row.CanManageChannel,
	}, nil
}

func (s *Store) CreateChannel(ctx context.Context, in repository.CreateChannelInput) (*channel.Channel, error) {
	if in.SpaceID == 0 {
		return nil, fmt.Errorf("create channel: space id is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("create channel: name is required")
	}
	if in.Type != channel.TypeSpace && in.Type != channel.TypeBroadcast {
		return nil, fmt.Errorf("create channel: unsupported channel type")
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create channel tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var serverRow struct {
		ID                   uint32 `db:"id"`
		AllowChannelCreation bool   `db:"allow_channel_creation"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, allow_channel_creation
		FROM servers
		WHERE id = ?
	FOR UPDATE
	`, []any{in.SpaceID}, &serverRow); err != nil {
		return nil, fmt.Errorf("load server for channel creation: %w", err)
	}
	if !serverRow.AllowChannelCreation {
		return nil, fmt.Errorf("channel creation disabled for server")
	}

	var role string
	if err = fetchOne(ctx, tx, `
		SELECT role
		FROM server_members
		WHERE space_id = ? AND user_id = ? AND is_banned = 0
		LIMIT 1
		FOR UPDATE
	`, []any{serverRow.ID, in.CreatorUserID}, &role); err != nil {
		return nil, fmt.Errorf("load space membership: %w", err)
	}
	if role != string(space.RoleOwner) && role != string(space.RoleAdmin) {
		return nil, fmt.Errorf("admin role required")
	}

	var sortOrder int
	if err = tx.GetContext(ctx, &sortOrder, `
		SELECT COALESCE(MAX(sort_order), 0) + 10
		FROM channels
		WHERE space_id = ?
	`, serverRow.ID); err != nil {
		return nil, fmt.Errorf("calculate channel sort order: %w", err)
	}

	now := s.now()
	res, execErr := tx.ExecContext(ctx, `
		INSERT INTO channels (
			space_id, type, name, description, visibility, slow_mode_seconds,
			auto_delete_after_seconds,
			message_seq, message_count, subscriber_count, member_count, created_by, status, sort_order, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, ?, 1, ?, ?, ?)
	`, serverRow.ID, string(in.Type), name, strings.TrimSpace(in.Description), string(in.Visibility), in.SlowModeSeconds, 0, in.CreatorUserID, sortOrder, now, now)
	if execErr != nil {
		err = fmt.Errorf("insert channel: %w", execErr)
		return nil, err
	}

	channelDBID, execErr := res.LastInsertId()
	if execErr != nil {
		err = fmt.Errorf("get inserted channel id: %w", execErr)
		return nil, err
	}

	if _, execErr = tx.ExecContext(ctx, `
		INSERT INTO channel_members (
			channel_id, user_id, role, can_view, can_send_message, can_send_image, can_send_file,
			can_delete_message, can_manage_channel, joined_at, created_at, updated_at
		)
		VALUES (?, ?, 'owner', 1, 1, 1, 1, 1, 1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE role = 'owner', can_send_message = 1, can_send_image = 1, can_send_file = 1,
			can_delete_message = 1, can_manage_channel = 1, updated_at = VALUES(updated_at)
	`, channelDBID, in.CreatorUserID, now, now, now); execErr != nil {
		err = fmt.Errorf("insert channel owner membership: %w", execErr)
		return nil, err
	}

	if err = refreshAggregateCounts(ctx, tx, serverRow.ID); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create channel tx: %w", err)
	}

	item := &channel.Channel{
		ID:                     uint32(channelDBID),
		SpaceDBID:              serverRow.ID,
		Type:                   in.Type,
		Name:                   name,
		Description:            strings.TrimSpace(in.Description),
		Visibility:             in.Visibility,
		SlowModeSeconds:        in.SlowModeSeconds,
		AutoDeleteAfterSeconds: 0,
		MessageSeq:             0,
		MessageCount:           0,
		MemberCount:            1,
		CreatedBy:              in.CreatorUserID,
		Status:                 1,
		SortOrder:              sortOrder,
		Permission: channel.Permission{
			Role:             space.RoleOwner,
			CanView:          true,
			CanSendMessage:   true,
			CanSendImage:     true,
			CanSendFile:      true,
			CanDeleteMessage: true,
			CanManageChannel: true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	return item, nil
}

func (s *Store) SetGroupAutoDeleteAfterSeconds(ctx context.Context, channelID uint32, seconds uint32) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set group auto delete tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var row struct {
		ID   uint32 `db:"id"`
		Type string `db:"type"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, type
		FROM channels
		WHERE id = ?
		FOR UPDATE
	`, []any{channelID}, &row); err != nil {
		return fmt.Errorf("load channel for auto delete: %w", err)
	}
	if row.Type != string(channel.TypeSpace) {
		return fmt.Errorf("auto delete is only supported for group channels")
	}

	now := s.now()
	if _, err = tx.ExecContext(ctx, `
		UPDATE channels
		SET auto_delete_after_seconds = ?, updated_at = ?
		WHERE id = ?
	`, seconds, now, row.ID); err != nil {
		return fmt.Errorf("update channel auto delete: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit set group auto delete tx: %w", err)
	}
	return nil
}

var _ repository.ChannelRepository = (*Store)(nil)
