CREATE TABLE IF NOT EXISTS blobs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  blob_id VARCHAR(80) NOT NULL,
  sha256 CHAR(64) NOT NULL,
  size BIGINT UNSIGNED NOT NULL,
  mime_type VARCHAR(128) NULL,
  storage_path VARCHAR(255) NOT NULL,
  ref_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_blobs_blob_id (blob_id),
  UNIQUE KEY uk_blobs_sha256 (sha256),
  KEY idx_blobs_size (size)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS media_objects (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  media_id VARCHAR(80) NOT NULL,
  blob_id BIGINT UNSIGNED NOT NULL,
  kind ENUM('image','file') NOT NULL,
  original_name VARCHAR(255) NULL,
  mime_type VARCHAR(128) NULL,
  size BIGINT UNSIGNED NOT NULL,
  width INT UNSIGNED NULL,
  height INT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_media_objects_media_id (media_id),
  KEY idx_media_objects_blob_id (blob_id),
  KEY idx_media_objects_kind (kind),
  KEY idx_media_objects_created_by (created_by)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS message_attachments (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  message_id BIGINT UNSIGNED NOT NULL,
  media_id BIGINT UNSIGNED NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_message_attachments_message_media (message_id, media_id),
  KEY idx_message_attachments_message_id (message_id),
  KEY idx_message_attachments_media_id (media_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

