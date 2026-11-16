-- A bounded batch of the delivery records older than the cutoff, the oldest first
-- (specs.html section 2.10).
-- name: PruneWebhookDeliveries :execrows
DELETE FROM webhook_deliveries
WHERE guid IN (
  SELECT old.guid FROM webhook_deliveries old
  WHERE old.received_at < sqlc.arg(cutoff)
  ORDER BY old.received_at, old.guid
  LIMIT sqlc.arg(batch)
);

-- name: InsertWebhookDelivery :execrows
INSERT INTO webhook_deliveries (guid, event, installation_id, received_at)
VALUES (?, ?, (SELECT id FROM installations WHERE github_installation_id = ?), ?)
ON CONFLICT (guid) DO NOTHING;
