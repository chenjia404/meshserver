package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// RunMigrations applies pending .up.sql files from the configured directory.
func RunMigrations(ctx context.Context, conn *sqlx.DB, migrationsDir string, logger *slog.Logger) error {
	if err := ensureMigrationTable(ctx, conn); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		applied, err := isMigrationApplied(ctx, conn, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		fullPath := filepath.Join(migrationsDir, name)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", fullPath, err)
		}

		tx, err := conn.BeginTxx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %q: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(raw)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %q: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO schema_migrations (name, applied_at)
			VALUES (?, ?)
		`, name, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %q: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %q: %w", name, err)
		}

		logger.Info("migration applied", "name", name)
	}

	return nil
}

func ensureMigrationTable(ctx context.Context, conn *sqlx.DB) error {
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			name VARCHAR(255) NOT NULL,
			applied_at DATETIME(3) NOT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY uk_schema_migrations_name (name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func isMigrationApplied(ctx context.Context, conn *sqlx.DB, name string) (bool, error) {
	var count int
	if err := conn.GetContext(ctx, &count, `SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name); err != nil {
		return false, fmt.Errorf("check migration %q: %w", name, err)
	}
	return count > 0, nil
}
