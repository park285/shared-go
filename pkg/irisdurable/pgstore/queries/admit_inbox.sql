INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, attempts, available_at, created_at, updated_at)
VALUES ($1, $2, $3, $4::jsonb, 'pending', 0, clock_timestamp(), clock_timestamp(), clock_timestamp())
ON CONFLICT ON CONSTRAINT uq_iris_webhook_inbox_identity DO NOTHING
RETURNING id
