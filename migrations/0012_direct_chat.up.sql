CREATE TABLE IF NOT EXISTS direct_conversations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  conversation_id VARCHAR(64) NOT NULL,
  user_low_id BIGINT UNSIGNED NOT NULL,
  user_high_id BIGINT UNSIGNED NOT NULL,
  last_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_message_at_ms BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_dm_conv_external (conversation_id),
  UNIQUE KEY uk_dm_pair (user_low_id, user_high_id),
  KEY idx_dm_user_low (user_low_id),
  KEY idx_dm_user_high (user_high_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS direct_messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  message_id VARCHAR(64) NOT NULL,
  conversation_id VARCHAR(64) NOT NULL,
  seq BIGINT UNSIGNED NOT NULL,
  sender_user_id BIGINT UNSIGNED NOT NULL,
  recipient_user_id BIGINT UNSIGNED NOT NULL,
  client_msg_id VARCHAR(128) NOT NULL,
  message_type ENUM('text','image','file','system') NOT NULL DEFAULT 'text',
  text_content TEXT NULL,
  created_at_ms BIGINT UNSIGNED NOT NULL,
  recipient_acked_at_ms BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_dm_msg_id (message_id),
  UNIQUE KEY uk_dm_conv_seq (conversation_id, seq),
  UNIQUE KEY uk_dm_idempotent (conversation_id, sender_user_id, client_msg_id),
  KEY idx_dm_recipient_pending (recipient_user_id, recipient_acked_at_ms, seq),
  KEY idx_dm_conv (conversation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
