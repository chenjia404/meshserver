CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id VARCHAR(64) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  pubkey VARBINARY(255) NULL,
  display_name VARCHAR(100) NOT NULL,
  avatar_url VARCHAR(255) NULL,
  bio VARCHAR(255) NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  last_login_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_user_id (user_id),
  UNIQUE KEY uk_users_peer_id (peer_id),
  KEY idx_users_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS nodes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  node_id VARCHAR(64) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  name VARCHAR(100) NOT NULL,
  public_addrs JSON NULL,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_nodes_node_id (node_id),
  UNIQUE KEY uk_nodes_peer_id (peer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

