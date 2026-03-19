package db

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"meshserver/internal/config"
)

// Open opens a MySQL connection pool with retry logic for container startup.
func Open(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*sqlx.DB, error) {
	dsn := cfg.MySQLDSN
	if cfg.MySQLServerPubKeyPath != "" {
		if err := registerServerPubKey(cfg.MySQLServerPubKeyPath); err != nil {
			return nil, err
		}
		dsn = addDSNParam(dsn, "serverPubKey", cfg.MySQLServerPubKeyPath)
		logger.Info("mysql server public key registered", "path", cfg.MySQLServerPubKeyPath)
	}

	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetConnMaxLifetime(cfg.MySQLConnMaxLifetime)
	db.SetMaxIdleConns(cfg.MySQLMaxIdleConns)
	db.SetMaxOpenConns(cfg.MySQLMaxOpenConns)

	deadline := time.Now().Add(cfg.MySQLMaxRetryDuration)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, cfg.MySQLConnectTimeout)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			if cfg.MySQLAdminDSN != "" {
				if err := ensureCachingSha2Auth(ctx, cfg, logger); err != nil {
					_ = db.Close()
					return nil, err
				}
			}
			logger.Info("mysql connection established")
			return db, nil
		}

		if time.Now().After(deadline) {
			_ = db.Close()
			return nil, fmt.Errorf("ping mysql: %w", err)
		}

		logger.Warn("mysql not ready yet", "error", err, "retry_in", cfg.MySQLRetryInterval.String())
		select {
		case <-time.After(cfg.MySQLRetryInterval):
		case <-ctx.Done():
			_ = db.Close()
			return nil, ctx.Err()
		}
	}
}

func ensureCachingSha2Auth(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	adminDSN := cfg.MySQLAdminDSN
	if cfg.MySQLServerPubKeyPath != "" {
		adminDSN = addDSNParam(adminDSN, "serverPubKey", cfg.MySQLServerPubKeyPath)
	}

	adminDB, err := sqlx.Open("mysql", adminDSN)
	if err != nil {
		return fmt.Errorf("open mysql admin connection: %w", err)
	}
	defer adminDB.Close()

	adminDB.SetConnMaxLifetime(cfg.MySQLConnMaxLifetime)
	adminDB.SetMaxIdleConns(cfg.MySQLMaxIdleConns)
	adminDB.SetMaxOpenConns(1)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.MySQLConnectTimeout)
	if err := adminDB.PingContext(pingCtx); err != nil {
		cancel()
		return fmt.Errorf("ping mysql admin connection: %w", err)
	}
	cancel()

	adminCfg, err := mysql.ParseDSN(adminDSN)
	if err != nil {
		return fmt.Errorf("parse mysql admin dsn: %w", err)
	}
	appCfg, err := mysql.ParseDSN(cfg.MySQLDSN)
	if err != nil {
		return fmt.Errorf("parse mysql dsn: %w", err)
	}

	if err := alterMySQLUserAuth(ctx, adminDB, appCfg.User, appCfg.Passwd); err != nil {
		return err
	}
	if err := alterMySQLUserAuth(ctx, adminDB, adminCfg.User, adminCfg.Passwd); err != nil {
		return err
	}

	logger.Info("mysql authentication plugins normalized to caching_sha2_password")
	return nil
}

func alterMySQLUserAuth(ctx context.Context, db *sqlx.DB, user, password string) error {
	if user == "" {
		return nil
	}

	var rows []struct {
		Host string `db:"host"`
	}
	if err := db.SelectContext(ctx, &rows, "SELECT host FROM mysql.user WHERE user = ?", user); err != nil {
		return fmt.Errorf("list mysql accounts for user %q: %w", user, err)
	}
	if len(rows) == 0 {
		return nil
	}

	for _, row := range rows {
		stmt := fmt.Sprintf(
			"ALTER USER %s@%s IDENTIFIED WITH caching_sha2_password BY %s",
			sqlStringLiteral(user),
			sqlStringLiteral(row.Host),
			sqlStringLiteral(password),
		)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("alter mysql user %q@%q: %w", user, row.Host, err)
		}
	}
	return nil
}

func addDSNParam(dsn, key, value string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + key + "=" + url.QueryEscape(value)
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func registerServerPubKey(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read mysql server public key %q: %w", path, err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return fmt.Errorf("decode mysql server public key %q: no PEM block found", path)
	}

	var pubKey *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse mysql server public key %q: %w", path, err)
		}
		var ok bool
		pubKey, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("parse mysql server public key %q: not an RSA public key", path)
		}
	case "RSA PUBLIC KEY":
		pubKey, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse mysql server public key %q: %w", path, err)
		}
	default:
		return fmt.Errorf("parse mysql server public key %q: unsupported PEM block type %q", path, block.Type)
	}

	mysql.RegisterServerPubKey(path, pubKey)
	return nil
}
