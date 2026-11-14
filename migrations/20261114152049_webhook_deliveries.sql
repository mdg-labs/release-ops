-- sqlite-migrate: checksum cf30e66f28f4061d88c85c0fcc093c9d0ac62c4f47f8520e44b48fb7f9d807e4

CREATE TABLE webhook_deliveries (
  guid TEXT PRIMARY KEY,
  event TEXT NOT NULL,
  installation_id TEXT REFERENCES installations(id) ON DELETE SET NULL,
  received_at TEXT NOT NULL
) STRICT;

CREATE INDEX idx_webhook_deliveries_received_at ON webhook_deliveries (received_at);
