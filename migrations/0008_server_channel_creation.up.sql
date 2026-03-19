ALTER TABLE servers
  ADD COLUMN allow_channel_creation TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER visibility;

UPDATE servers
SET allow_channel_creation = 1
WHERE id = 1;

UPDATE servers s
SET s.channel_count = (
  SELECT COUNT(1)
  FROM channels c
  WHERE c.space_id = s.id AND c.status = 1
);

UPDATE channels c
SET c.subscriber_count = (
  SELECT COUNT(1)
  FROM channel_members cm
  WHERE cm.channel_id = c.id
);
