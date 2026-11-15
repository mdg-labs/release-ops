-- sqlite-migrate: checksum 88494ef566c378f9dc2a3fa5d27b59cb6c115f873b497a5572225f23e16b5d19

ALTER TABLE jobs ADD COLUMN rerun_trigger text CHECK (rerun_trigger is null or rerun_trigger in ('dashboard_checkbox', 'pr_checkbox', 'pr_closed', 'push', 'repository_added', 'schedule', 'polling', 'run_now'));

ALTER TABLE jobs ADD COLUMN rerun_markers text CHECK (rerun_markers is null or json_valid(rerun_markers));

ALTER TABLE jobs ADD COLUMN rerun_webhook_received_at text;
