ALTER TABLE channels
  ADD COLUMN auto_delete_after_seconds INT UNSIGNED NOT NULL DEFAULT 0 AFTER slow_mode_seconds;
