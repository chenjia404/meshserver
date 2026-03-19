CREATE TABLE IF NOT EXISTS user_channel_reads (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  channel_id INT UNSIGNED NOT NULL,
  last_delivered_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_read_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_user_channel_reads_user_channel (user_id, channel_id),
  KEY idx_user_channel_reads_channel_id (channel_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS auth_nonces (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  nonce_hash CHAR(64) NOT NULL,
  client_peer_id VARCHAR(128) NOT NULL,
  node_peer_id VARCHAR(128) NOT NULL,
  issued_at DATETIME(3) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_auth_nonces_nonce_hash (nonce_hash),
  KEY idx_auth_nonces_client_peer_id (client_peer_id),
  KEY idx_auth_nonces_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
