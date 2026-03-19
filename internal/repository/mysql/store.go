package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"meshserver/internal/repository"
)

// Store is the concrete MySQL-backed repository implementation.
type Store struct {
	db *sqlx.DB
}

// NewStore creates a MySQL repository store.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

func (s *Store) now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

func newExternalID(prefix string) string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", prefix, strings.ToLower(hex.EncodeToString(buf)))
}

func normalizeNotFound(err error) error {
	if err == nil {
		return nil
	}
	if err == sql.ErrNoRows {
		return repository.ErrNotFound
	}
	return err
}

func marshalJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func fetchOne[T any](ctx context.Context, exec sqlx.ExtContext, query string, args []any, dest *T) error {
	if err := sqlx.GetContext(ctx, exec, dest, query, args...); err != nil {
		return normalizeNotFound(err)
	}
	return nil
}
