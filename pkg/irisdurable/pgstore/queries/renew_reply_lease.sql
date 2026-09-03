UPDATE iris_reply_outbox
SET lease_until = now() + make_interval(secs => $6),
    updated_at = now()
WHERE scope = $1
  AND message_id = $2
  AND phase = $3
  AND ordinal = $4
  AND status = 'submitting'
  AND claim_token = $5
