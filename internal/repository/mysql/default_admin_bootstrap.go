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
// Does not create any space: if the space does not exist yet, bootstrap is skipped (restart after the space exists).
func (s *Store) BootstrapDefaultAdmin(ctx context.Context, adminPeerID string, targetSpaceID uint32) error {
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
		return nil
	default:
		return fmt.Errorf("bootstrap default admin: load space: %w", err)
	}
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
