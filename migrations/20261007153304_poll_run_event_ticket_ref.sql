-- sqlite-migrate: checksum cde83403914f8c3df1524e9b5272a2a6604eee97f89e1728f682af8d376a6318

ALTER TABLE poll_run_events ADD COLUMN ticket_external_id text;

ALTER TABLE poll_run_events ADD COLUMN ticket_url text;

ALTER TABLE poll_run_events ADD COLUMN release_tag text;
