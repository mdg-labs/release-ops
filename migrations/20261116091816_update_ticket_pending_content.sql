-- sqlite-migrate: checksum 559cd41cf7a14dec355908be23acf669f87c27529292189645f302b76df5eece

ALTER TABLE update_tickets ADD COLUMN pending_content_hash text;
