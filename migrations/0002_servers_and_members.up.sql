CREATE TABLE IF NOT EXISTS servers (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  host_node_id BIGINT UNSIGNED NOT NULL,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  avatar_url VARCHAR(255) NULL,
  description VARCHAR(500) NULL,
  visibility ENUM('public','private') NOT NULL DEFAULT 'private',
  member_count INT UNSIGNED NOT NULL DEFAULT 0,
  channel_count INT UNSIGNED NOT NULL DEFAULT 0,
  status TINYINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_servers_owner_user_id (owner_user_id),
  KEY idx_servers_host_node_id (host_node_id),
  KEY idx_servers_visibility (visibility)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS server_members (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  space_id INT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  role ENUM('owner','admin','member','subscriber') NOT NULL DEFAULT 'member',
  nickname VARCHAR(100) NULL,
  is_muted TINYINT UNSIGNED NOT NULL DEFAULT 0,
  is_banned TINYINT UNSIGNED NOT NULL DEFAULT 0,
  joined_at DATETIME(3) NOT NULL,
  last_seen_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_server_members_server_user (space_id, user_id),
  KEY idx_server_members_user_id (user_id),
  KEY idx_server_members_server_role (space_id, role),
  KEY idx_server_members_server_banned (space_id, is_banned)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
