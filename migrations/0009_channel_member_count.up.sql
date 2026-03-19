ALTER TABLE channels
  ADD COLUMN member_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER subscriber_count;

UPDATE channels
SET member_count = subscriber_count;
