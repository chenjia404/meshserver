CREATE TABLE IF NOT EXISTS server_invites (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  invite_code VARCHAR(64) NOT NULL,
  space_id INT UNSIGNED NOT NULL,
  created_by BIGINT UNSIGNED NOT NULL,
  max_uses INT UNSIGNED NULL,
  used_count INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at DATETIME(3) NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_server_invites_invite_code (invite_code),
  KEY idx_server_invites_space_id (space_id),
  KEY idx_server_invites_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
