ALTER TABLE media_objects
  ADD COLUMN file_cid VARCHAR(255) NULL AFTER mime_type,
  KEY idx_media_objects_file_cid (file_cid);
