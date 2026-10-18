UPDATE iris_reply_outbox AS outbox
SET status = 'submitting',
    attempts = outbox.attempts + 1,
    first_attempt_at = COALESCE(outbox.first_attempt_at, now()),
    claim_token = $5,
    lease_until = now() + make_interval(secs => $6),
    updated_at = now()
WHERE {{eligible}}
  AND outbox.message_id = $2
  AND outbox.phase = $3
  AND outbox.ordinal = $4
RETURNING outbox.attempts, outbox.client_request_id
