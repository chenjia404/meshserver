package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"

	"meshserver/internal/repository"
	"meshserver/internal/space"
)

// BootstrapDefaultAdmin ensures the user for adminPeerID exists and has admin (or owner) role in targetSpaceID.
// When targetSpaceID is 0, it defaults to 1.
// Does not create any space: if the space does not exist yet, bootstrap is skipped (restart after the space exists).
// Peer IDs are stored in canonical form (Decode→String) so they match authenticated client_peer_id strings.
func (s *Store) BootstrapDefaultAdmin(ctx context.Context, logger *slog.Logger, adminPeerID string, targetSpaceID uint32) error {
	adminPeerID = strings.TrimSpace(adminPeerID)
	if adminPeerID == "" {
		return nil
	}
	decoded, err := peer.Decode(adminPeerID)
	if err != nil {
		return fmt.Errorf("default admin peer id: %w", err)
	}
	adminPeerID = decoded.String()

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
		if err := s.ensureAdminMembership(ctx, targetSpaceID, usr.ID); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("default admin bootstrap applied", "peer_id", adminPeerID, "space_id", targetSpaceID, "user_db_id", usr.ID)
		}
		return nil
	case err == repository.ErrNotFound:
		if logger != nil {
			logger.Warn("default admin bootstrap skipped: space does not exist yet; create the space and restart, or set MESHSERVER_DEFAULT_SPACE_ID",
				"peer_id", adminPeerID, "space_id", targetSpaceID)
		}
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
