WITH ordering_lock AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtext($1::text), hashtext($3::text))
)
INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, attempts, available_at, created_at, updated_at)
SELECT $1::text, $2::text, $3::text, $4::jsonb, 'pending', 0, clock_timestamp(), clock_timestamp(), clock_timestamp()
FROM ordering_lock
ON CONFLICT ON CONSTRAINT uq_iris_webhook_inbox_identity DO NOTHING
RETURNING id
