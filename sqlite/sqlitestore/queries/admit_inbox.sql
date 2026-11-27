INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, attempts, available_at, created_at, updated_at)
VALUES (?1, ?2, ?3, ?4, 'pending', 0, ?5, ?5, ?5)
ON CONFLICT (scope, message_id) DO NOTHING
RETURNING id
