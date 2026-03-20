package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"

	"meshserver/internal/repository"
	"meshserver/internal/space"
)

// BootstrapDefaultAdmin ensures the user for adminPeerID exists and has admin (or owner) role in targetSpaceID.
// When targetSpaceID is 0, it defaults to 1.
// If the space is missing, only space id 1 may be auto-created when the servers table is empty (first deploy).
func (s *Store) BootstrapDefaultAdmin(ctx context.Context, adminPeerID string, targetSpaceID uint32, hostNodeID uint64) error {
	adminPeerID = strings.TrimSpace(adminPeerID)
	if adminPeerID == "" {
		return nil
	}
	if _, err := peer.Decode(adminPeerID); err != nil {
		return fmt.Errorf("default admin peer id: %w", err)
	}
	if targetSpaceID == 0 {
		targetSpaceID = 1
	}

	usr, err := s.CreateIfNotExistsByPeerID(ctx, adminPeerID, nil)
	if err != nil {
		return fmt.Errorf("bootstrap default admin user: %w", err)
	}

	_, err = s.GetBySpaceID(ctx, targetSpaceID)
	switch {
	case err == nil:
		return s.ensureAdminMembership(ctx, targetSpaceID, usr.ID)
	case err == repository.ErrNotFound:
		return s.ensureDefaultSpace(ctx, targetSpaceID, hostNodeID, usr.ID)
	default:
		return fmt.Errorf("bootstrap default admin: load space: %w", err)
	}
}

func (s *Store) ensureDefaultSpace(ctx context.Context, targetSpaceID uint32, hostNodeID uint64, creatorUserID uint64) error {
	if targetSpaceID != 1 {
		return fmt.Errorf("bootstrap default admin: space %d not found", targetSpaceID)
	}
	var n uint64
	if err := s.db.GetContext(ctx, &n, `SELECT COUNT(1) FROM servers WHERE status = 1`); err != nil {
		return fmt.Errorf("bootstrap default admin: count spaces: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("bootstrap default admin: space 1 not found but other spaces exist")
	}
	_, err := s.CreateSpace(ctx, repository.CreateSpaceInput{
		HostNodeID:           hostNodeID,
		CreatorUserID:        creatorUserID,
		Name:                 "Default",
		Description:          "",
		Visibility:           space.VisibilityPublic,
		AllowChannelCreation: true,
	})
	if err != nil {
		return fmt.Errorf("bootstrap default admin: create default space: %w", err)
	}
	return nil
}

func (s *Store) ensureAdminMembership(ctx context.Context, targetSpaceID uint32, userID uint64) error {
	role, err := s.GetMemberRole(ctx, targetSpaceID, userID)
	if err != nil {
		if err != repository.ErrNotFound {
			return fmt.Errorf("bootstrap default admin: member role: %w", err)
		}
		if _, err := s.InviteSpaceMember(ctx, targetSpaceID, userID); err != nil {
			return fmt.Errorf("bootstrap default admin: invite member: %w", err)
		}
		if err := s.SetSpaceMemberRole(ctx, targetSpaceID, userID, space.RoleAdmin); err != nil {
			return fmt.Errorf("bootstrap default admin: set admin role: %w", err)
		}
		return nil
	}

	switch role {
	case space.RoleOwner, space.RoleAdmin:
		return nil
	default:
		if err := s.SetSpaceMemberRole(ctx, targetSpaceID, userID, space.RoleAdmin); err != nil {
			return fmt.Errorf("bootstrap default admin: promote to admin: %w", err)
		}
		return nil
	}
}
