package mysql

import (
	"context"
	"fmt"

	"meshserver/internal/repository"
)

func (s *Store) GetBySHA256(ctx context.Context, sha256 string) (*repository.Blob, error) {
	const query = `
		SELECT
			id,
			blob_id,
			sha256,
			size,
			COALESCE(mime_type, '') AS mime_type,
			storage_path,
			ref_count,
			created_at
		FROM blobs
		WHERE sha256 = ?
		LIMIT 1
	`
	var blob repository.Blob
	if err := fetchOne(ctx, s.db, query, []any{sha256}, &blob); err != nil {
		return nil, err
	}
	return &blob, nil
}

func (s *Store) CreateBlob(ctx context.Context, in repository.CreateBlobInput) (*repository.Blob, error) {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO blobs (
			blob_id, sha256, size, mime_type, storage_path, ref_count, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, in.BlobID, in.SHA256, in.Size, nullableString(in.MIMEType), in.StoragePath, in.RefCount, now)
	if err != nil {
		return nil, fmt.Errorf("insert blob: %w", err)
	}
	return s.GetBySHA256(ctx, in.SHA256)
}

func (s *Store) IncRef(ctx context.Context, blobID uint64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE blobs
		SET ref_count = ref_count + 1
		WHERE id = ?
	`, blobID)
	if err != nil {
		return fmt.Errorf("increment blob ref count: %w", err)
	}
	return nil
}

func (s *Store) CreateMedia(ctx context.Context, in repository.CreateMediaInput) (*repository.MediaObject, error) {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_objects (
			media_id, blob_id, kind, original_name, mime_type, file_cid, size, width, height, created_by, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, in.MediaID, in.BlobID, in.Kind, nullableString(in.OriginalName), nullableString(in.MIMEType), nullableString(in.FileCID), in.Size, nullableUint32(in.Width), nullableUint32(in.Height), in.CreatedBy, now)
	if err != nil {
		return nil, fmt.Errorf("insert media object: %w", err)
	}
	return s.GetByMediaID(ctx, in.MediaID)
}

func (s *Store) GetByMediaID(ctx context.Context, mediaID string) (*repository.MediaObject, error) {
	const query = `
		SELECT
			mo.id,
			mo.media_id,
			mo.blob_id,
			b.blob_id AS blob_external_id,
			mo.kind,
			COALESCE(mo.original_name, '') AS original_name,
			COALESCE(mo.mime_type, '') AS mime_type,
			COALESCE(mo.file_cid, '') AS file_cid,
			mo.size,
			COALESCE(mo.width, 0) AS width,
			COALESCE(mo.height, 0) AS height,
			COALESCE(mo.created_by, 0) AS created_by,
			mo.created_at,
			b.sha256,
			b.storage_path
		FROM media_objects mo
		INNER JOIN blobs b ON b.id = mo.blob_id
		WHERE mo.media_id = ?
		LIMIT 1
	`
	var item repository.MediaObject
	if err := fetchOne(ctx, s.db, query, []any{mediaID}, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func nullableUint32(value *uint32) any {
	if value == nil {
		return nil
	}
	return *value
}

var _ repository.BlobRepository = (*Store)(nil)
var _ repository.MediaRepository = (*Store)(nil)
