INSERT INTO iris_reply_outbox (
    scope, message_id, phase, ordinal, room_id, client_request_id, payload, payload_hash,
    status, available_at, created_at, updated_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, 'pending', now(), now(), now(), now() + make_interval(secs => $9))
ON CONFLICT ON CONSTRAINT uq_iris_reply_outbox_identity DO NOTHING
RETURNING id
