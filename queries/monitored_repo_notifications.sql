-- name: ListNotificationTargetIDsForRepo :many
SELECT notification_target_id
FROM monitored_repo_notifications
WHERE monitored_repo_id = ?
ORDER BY notification_target_id;

-- name: LinkMonitoredRepoNotification :exec
INSERT INTO monitored_repo_notifications (
  monitored_repo_id,
  notification_target_id
) VALUES (
  ?,
  ?
);

-- name: DeleteMonitoredRepoNotificationsByRepo :exec
DELETE FROM monitored_repo_notifications
WHERE monitored_repo_id = ?;

-- name: DeleteMonitoredRepoNotification :exec
DELETE FROM monitored_repo_notifications
WHERE monitored_repo_id = ?
  AND notification_target_id = ?;
