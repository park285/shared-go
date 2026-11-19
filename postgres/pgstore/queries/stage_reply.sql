WITH inserted AS (
    INSERT INTO iris_reply_outbox (
        scope, message_id, phase, ordinal, room_id, client_request_id, payload, payload_hash,
        status, available_at, created_at, updated_at, expires_at
    )
    SELECT $1, $2, $3, $4, $5, $6, $7::jsonb, $8,
           'pending', now(), now(), now(), now() + make_interval(secs => $9)
    WHERE NOT EXISTS (
        SELECT 1
        FROM iris_reply_outbox AS successor
        WHERE successor.scope = $1
          AND successor.message_id = $2
          AND successor.phase = $3
          AND successor.ordinal > $4
    )
    ON CONFLICT ON CONSTRAINT uq_iris_reply_outbox_identity DO NOTHING
    RETURNING id, status, client_request_id, payload::text AS payload, payload_hash::text AS payload_hash,
              attempts, true AS inserted
)
SELECT id, status, client_request_id, payload, payload_hash, attempts, inserted
FROM inserted
UNION ALL
SELECT id, status, client_request_id, payload::text, payload_hash::text, attempts, false
FROM iris_reply_outbox
WHERE scope = $1 AND message_id = $2 AND phase = $3 AND ordinal = $4
  AND NOT EXISTS (SELECT 1 FROM inserted)
