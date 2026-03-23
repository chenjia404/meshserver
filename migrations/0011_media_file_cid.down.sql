ALTER TABLE media_objects
  DROP KEY idx_media_objects_file_cid,
  DROP COLUMN file_cid;
