package mysql

import (
	"context"
	"fmt"
	"strings"

	"meshserver/internal/repository"
	"meshserver/internal/space"
)

func (s *Store) ListByUserID(ctx context.Context, userID uint64) ([]*space.Space, error) {
	const query = `
		SELECT
			s.id,
			s.host_node_id,
			s.owner_user_id,
			s.name,
			COALESCE(s.avatar_url, '') AS avatar_url,
			COALESCE(s.description, '') AS description,
			s.visibility,
			s.member_count,
			s.channel_count,
			s.allow_channel_creation,
			s.status,
			s.created_at,
			s.updated_at
		FROM servers s
		INNER JOIN server_members sm ON sm.space_id = s.id
		WHERE sm.user_id = ? AND sm.is_banned = 0 AND s.status = 1
		ORDER BY s.name ASC
	`

	var items []*space.Space
	if err := s.db.SelectContext(ctx, &items, query, userID); err != nil {
		return nil, fmt.Errorf("list spaces by user id: %w", err)
	}
	return items, nil
}

func (s *Store) GetBySpaceID(ctx context.Context, spaceID uint32) (*space.Space, error) {
	const query = `
		SELECT
			id,
			host_node_id,
			owner_user_id,
			name,
			COALESCE(avatar_url, '') AS avatar_url,
			COALESCE(description, '') AS description,
			visibility,
			member_count,
			channel_count,
			allow_channel_creation,
			status,
			created_at,
			updated_at
		FROM servers
		WHERE id = ?
		LIMIT 1
	`
	var item space.Space
	if err := fetchOne(ctx, s.db, query, []any{spaceID}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) GetMemberRole(ctx context.Context, spaceID uint32, userID uint64) (space.Role, error) {
	const query = `
		SELECT sm.role
		FROM server_members sm
		INNER JOIN servers s ON s.id = sm.space_id
		WHERE s.id = ? AND sm.user_id = ? AND sm.is_banned = 0
		LIMIT 1
	`
	var role string
	if err := fetchOne(ctx, s.db, query, []any{spaceID, userID}, &role); err != nil {
		return "", err
	}
	return space.Role(role), nil
}

func (s *Store) CanCreateSpace(ctx context.Context, userID uint64) (bool, error) {
	const query = `
		SELECT COUNT(1)
		FROM server_members sm
		INNER JOIN servers s ON s.id = sm.space_id
		WHERE sm.user_id = ? AND sm.is_banned = 0 AND s.status = 1 AND sm.role IN ('owner', 'admin')
	`
	var count uint64
	if err := s.db.GetContext(ctx, &count, query, userID); err != nil {
		return false, fmt.Errorf("check create space permission: %w", err)
	}
	return count > 0, nil
}

func (s *Store) CanCreateGroup(ctx context.Context, spaceID uint32, userID uint64) (bool, error) {
	const query = `
		SELECT COUNT(1)
		FROM server_members sm
		INNER JOIN servers s ON s.id = sm.space_id
		WHERE s.id = ? AND sm.user_id = ? AND sm.is_banned = 0 AND s.status = 1
			AND sm.role IN ('owner', 'admin') AND s.allow_channel_creation = 1
	`
	var count uint64
	if err := s.db.GetContext(ctx, &count, query, spaceID, userID); err != nil {
		return false, fmt.Errorf("check create group permission: %w", err)
	}
	return count > 0, nil
}

func (s *Store) CreateSpace(ctx context.Context, in repository.CreateSpaceInput) (*space.Space, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("create space: name is required")
	}
	switch in.Visibility {
	case space.VisibilityPublic, space.VisibilityPrivate:
	default:
		return nil, fmt.Errorf("create space: unsupported visibility %q", in.Visibility)
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create space tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := s.now()
	allowChannelCreation := 0
	if in.AllowChannelCreation {
		allowChannelCreation = 1
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO servers (
			host_node_id, owner_user_id, name, avatar_url, description, visibility,
			member_count, channel_count, allow_channel_creation, status, created_at, updated_at
		)
		VALUES (?, ?, ?, NULL, ?, ?, 1, 0, ?, 1, ?, ?)
	`, in.HostNodeID, in.CreatorUserID, name, strings.TrimSpace(in.Description), string(in.Visibility), allowChannelCreation, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert space: %w", err)
	}

	spaceDBID64, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert space id: %w", err)
	}
	spaceDBID := uint32(spaceDBID64)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO server_members (
			space_id, user_id, role, nickname, is_muted, is_banned, joined_at, last_seen_at, created_at, updated_at
		)
		VALUES (?, ?, 'owner', NULL, 0, 0, ?, ?, ?, ?)
	`, spaceDBID, in.CreatorUserID, now, now, now, now); err != nil {
		return nil, fmt.Errorf("insert space owner membership: %w", err)
	}

	if err = refreshAggregateCounts(ctx, tx, spaceDBID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create space tx: %w", err)
	}
	return s.GetBySpaceID(ctx, spaceDBID)
}

func (s *Store) JoinSpace(ctx context.Context, spaceID uint32, userID uint64) (*space.Space, error) {
	return s.addSpaceMember(ctx, spaceID, userID, true)
}

func (s *Store) InviteSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error) {
	return s.addSpaceMember(ctx, spaceID, targetUserID, false)
}

func (s *Store) addSpaceMember(ctx context.Context, spaceID uint32, userID uint64, publicOnly bool) (*space.Space, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin add space member tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID         uint32           `db:"id"`
		Visibility space.Visibility `db:"visibility"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, visibility
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return nil, fmt.Errorf("load space for member add: %w", err)
	}

	var existing struct {
		Role     string `db:"role"`
		IsBanned bool   `db:"is_banned"`
	}
	err = fetchOne(ctx, tx, `
		SELECT role, is_banned
		FROM server_members
		WHERE space_id = ? AND user_id = ?
		FOR UPDATE
	`, []any{spaceRow.ID, userID}, &existing)
	if err == nil {
		if existing.IsBanned {
			return nil, fmt.Errorf("user is banned")
		}
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit add space member tx (existing): %w", err)
		}
		return s.GetBySpaceID(ctx, spaceRow.ID)
	}
	if err != repository.ErrNotFound {
		return nil, fmt.Errorf("check existing space membership: %w", err)
	}
	if publicOnly && spaceRow.Visibility != space.VisibilityPublic {
		return nil, fmt.Errorf("space is private")
	}

	now := s.now()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO server_members (
			space_id, user_id, role, nickname, is_muted, is_banned, joined_at, last_seen_at, created_at, updated_at
		)
		VALUES (?, ?, 'member', NULL, 0, 0, ?, ?, ?, ?)
	`, spaceRow.ID, userID, now, now, now, now); err != nil {
		return nil, fmt.Errorf("insert space membership: %w", err)
	}

	if err = refreshAggregateCounts(ctx, tx, spaceRow.ID); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit add space member tx: %w", err)
	}
	return s.GetBySpaceID(ctx, spaceRow.ID)
}

func (s *Store) KickSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin kick space member tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID          uint32 `db:"id"`
		OwnerUserID uint64 `db:"owner_user_id"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, owner_user_id
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return nil, fmt.Errorf("load space for kick: %w", err)
	}

	if targetUserID == spaceRow.OwnerUserID {
		return nil, fmt.Errorf("current owner cannot be removed")
	}

	var existing struct {
		Role     string `db:"role"`
		IsBanned bool   `db:"is_banned"`
	}
	err = fetchOne(ctx, tx, `
		SELECT role, is_banned
		FROM server_members
		WHERE space_id = ? AND user_id = ?
		FOR UPDATE
	`, []any{spaceRow.ID, targetUserID}, &existing)
	if err != nil {
		return nil, fmt.Errorf("load target space membership: %w", err)
	}
	if existing.IsBanned {
		return nil, fmt.Errorf("user is banned")
	}

	if _, err = tx.ExecContext(ctx, `
		DELETE FROM server_members
		WHERE space_id = ? AND user_id = ?
	`, spaceRow.ID, targetUserID); err != nil {
		return nil, fmt.Errorf("delete space membership: %w", err)
	}
	if err = refreshAggregateCounts(ctx, tx, spaceRow.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit kick space member tx: %w", err)
	}
	return s.GetBySpaceID(ctx, spaceRow.ID)
}

func (s *Store) BanSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ban space member tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID          uint32 `db:"id"`
		OwnerUserID uint64 `db:"owner_user_id"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, owner_user_id
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return nil, fmt.Errorf("load space for ban: %w", err)
	}

	if targetUserID == spaceRow.OwnerUserID {
		return nil, fmt.Errorf("current owner cannot be banned")
	}

	var existing struct {
		Role     string `db:"role"`
		IsBanned bool   `db:"is_banned"`
	}
	err = fetchOne(ctx, tx, `
		SELECT role, is_banned
		FROM server_members
		WHERE space_id = ? AND user_id = ?
		FOR UPDATE
	`, []any{spaceRow.ID, targetUserID}, &existing)

	now := s.now()
	switch {
	case err == nil && existing.IsBanned:
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit ban space member tx (existing banned): %w", err)
		}
		return s.GetBySpaceID(ctx, spaceRow.ID)
	case err == nil:
		if _, err = tx.ExecContext(ctx, `
			UPDATE server_members
			SET is_banned = 1, role = 'member', updated_at = ?
			WHERE space_id = ? AND user_id = ?
		`, now, spaceRow.ID, targetUserID); err != nil {
			return nil, fmt.Errorf("mark space member banned: %w", err)
		}
	case err == repository.ErrNotFound:
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO server_members (
				space_id, user_id, role, nickname, is_muted, is_banned, joined_at, last_seen_at, created_at, updated_at
			)
			VALUES (?, ?, 'member', NULL, 0, 1, ?, ?, ?, ?)
			`, spaceRow.ID, targetUserID, now, now, now, now); err != nil {
			return nil, fmt.Errorf("insert banned space member: %w", err)
		}
	default:
		return nil, fmt.Errorf("load target space membership: %w", err)
	}

	if err = refreshAggregateCounts(ctx, tx, spaceRow.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ban space member tx: %w", err)
	}
	return s.GetBySpaceID(ctx, spaceRow.ID)
}

func (s *Store) UnbanSpaceMember(ctx context.Context, spaceID uint32, targetUserID uint64) (*space.Space, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin unban space member tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID          uint32 `db:"id"`
		OwnerUserID uint64 `db:"owner_user_id"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, owner_user_id
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return nil, fmt.Errorf("load space for unban: %w", err)
	}

	if targetUserID == spaceRow.OwnerUserID {
		return nil, fmt.Errorf("current owner cannot be unbanned")
	}

	var existing struct {
		Role     string `db:"role"`
		IsBanned bool   `db:"is_banned"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT role, is_banned
		FROM server_members
		WHERE space_id = ? AND user_id = ?
		FOR UPDATE
	`, []any{spaceRow.ID, targetUserID}, &existing); err != nil {
		return nil, fmt.Errorf("load target space membership: %w", err)
	}
	if !existing.IsBanned {
		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit unban space member tx (already unbanned): %w", err)
		}
		return s.GetBySpaceID(ctx, spaceRow.ID)
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE server_members
		SET is_banned = 0, updated_at = ?
		WHERE space_id = ? AND user_id = ?
	`, s.now(), spaceRow.ID, targetUserID); err != nil {
		return nil, fmt.Errorf("mark space member unbanned: %w", err)
	}

	if err = refreshAggregateCounts(ctx, tx, spaceRow.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit unban space member tx: %w", err)
	}
	return s.GetBySpaceID(ctx, spaceRow.ID)
}

func (s *Store) ListSpaceMembers(ctx context.Context, spaceID uint32, afterMemberID uint64, limit uint32) ([]*repository.SpaceMember, error) {
	if limit == 0 {
		limit = 20
	}
	const query = `
		SELECT
			sm.id AS member_id,
			u.user_id,
			COALESCE(u.display_name, '') AS display_name,
			COALESCE(u.avatar_url, '') AS avatar_url,
			sm.role,
			COALESCE(sm.nickname, '') AS nickname,
			sm.is_muted,
			sm.is_banned,
			sm.joined_at,
			COALESCE(sm.last_seen_at, sm.joined_at) AS last_seen_at
		FROM server_members sm
		INNER JOIN servers s ON s.id = sm.space_id
		INNER JOIN users u ON u.id = sm.user_id
		WHERE s.id = ? AND s.status = 1 AND sm.is_banned = 0 AND sm.id > ?
		ORDER BY sm.id ASC
		LIMIT ?
	`
	var items []*repository.SpaceMember
	if err := s.db.SelectContext(ctx, &items, query, spaceID, afterMemberID, limit); err != nil {
		return nil, fmt.Errorf("list space members: %w", err)
	}
	return items, nil
}

func (s *Store) SetSpaceAllowChannelCreation(ctx context.Context, spaceID uint32, enabled bool) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update space allow-channel-creation tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID uint32 `db:"id"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return fmt.Errorf("load space for allow-channel-creation update: %w", err)
	}

	allowValue := 0
	if enabled {
		allowValue = 1
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE servers
		SET allow_channel_creation = ?, updated_at = ?
		WHERE id = ?
	`, allowValue, s.now(), spaceRow.ID)
	if err != nil {
		return fmt.Errorf("update space allow-channel-creation: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit update space allow-channel-creation tx: %w", err)
	}
	return nil
}

func (s *Store) SetSpaceMemberRole(ctx context.Context, spaceID uint32, targetUserID uint64, role space.Role) error {
	if strings.TrimSpace(string(role)) == "" {
		return fmt.Errorf("set member role: role is required")
	}
	switch role {
	case space.RoleOwner, space.RoleAdmin, space.RoleMember, space.RoleSubscriber:
	default:
		return fmt.Errorf("set member role: unsupported role %q", role)
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update space member role tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var spaceRow struct {
		ID          uint32 `db:"id"`
		OwnerUserID uint64 `db:"owner_user_id"`
	}
	if err = fetchOne(ctx, tx, `
		SELECT id, owner_user_id
		FROM servers
		WHERE id = ?
		FOR UPDATE
	`, []any{spaceID}, &spaceRow); err != nil {
		return fmt.Errorf("load space for member role update: %w", err)
	}

	if err = fetchOne(ctx, tx, `
		SELECT role
		FROM server_members
		WHERE space_id = ? AND user_id = ?
		FOR UPDATE
	`, []any{spaceRow.ID, targetUserID}, new(string)); err != nil {
		return fmt.Errorf("load target space membership: %w", err)
	}

	if targetUserID == spaceRow.OwnerUserID && role != space.RoleOwner {
		return fmt.Errorf("current owner role cannot be changed directly")
	}

	now := s.now()
	if role == space.RoleOwner {
		if targetUserID != spaceRow.OwnerUserID {
			if _, err = tx.ExecContext(ctx, `
				UPDATE server_members
				SET role = 'admin', updated_at = ?
				WHERE space_id = ? AND user_id = ?
			`, now, spaceRow.ID, spaceRow.OwnerUserID); err != nil {
				return fmt.Errorf("demote previous owner: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE server_members
				SET role = 'owner', updated_at = ?
				WHERE space_id = ? AND user_id = ?
			`, now, spaceRow.ID, targetUserID); err != nil {
				return fmt.Errorf("promote target owner: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE servers
				SET owner_user_id = ?, updated_at = ?
				WHERE id = ?
			`, targetUserID, now, spaceRow.ID); err != nil {
				return fmt.Errorf("update space owner: %w", err)
			}
		} else if _, err = tx.ExecContext(ctx, `
			UPDATE server_members
			SET role = 'owner', updated_at = ?
			WHERE space_id = ? AND user_id = ?
			`, now, spaceRow.ID, targetUserID); err != nil {
			return fmt.Errorf("refresh owner membership: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			UPDATE server_members
			SET role = ?, updated_at = ?
			WHERE space_id = ? AND user_id = ?
			`, string(role), now, spaceRow.ID, targetUserID); err != nil {
			return fmt.Errorf("update member role: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit update space member role tx: %w", err)
	}
	return nil
}

var _ repository.SpaceRepository = (*Store)(nil)
