UPDATE iris_reply_outbox AS outbox
SET status = $6,
    client_request_id = COALESCE(NULLIF($7, ''), outbox.client_request_id),
    iris_request_id = COALESCE(NULLIF($8, ''), outbox.iris_request_id),
    payload = CASE WHEN $6 IN ('accepted', 'dead', 'permanent_conflict') THEN NULL ELSE outbox.payload END,
    room_id = CASE WHEN $6 IN ('accepted', 'dead', 'permanent_conflict') THEN '' ELSE outbox.room_id END,
    attempts = CASE
        WHEN $10 AND $6 NOT IN ('accepted', 'dead', 'permanent_conflict') THEN 0
        ELSE outbox.attempts
    END,
    available_at = now() + make_interval(secs => $9),
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE outbox.scope = $1
  AND outbox.message_id = $2
  AND outbox.phase = $3
  AND outbox.ordinal = $4
  AND outbox.claim_token = $5
