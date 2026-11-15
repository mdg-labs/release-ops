-- name: PruneWebhookDeliveries :execrows
DELETE FROM webhook_deliveries WHERE received_at < ?;

-- name: InsertWebhookDelivery :execrows
INSERT INTO webhook_deliveries (guid, event, installation_id, received_at)
VALUES (?, ?, (SELECT id FROM installations WHERE github_installation_id = ?), ?)
ON CONFLICT (guid) DO NOTHING;
