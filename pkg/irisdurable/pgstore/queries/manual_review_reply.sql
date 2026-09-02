UPDATE iris_reply_outbox AS outbox
SET status = 'manual_review',
    terminal_reason = $6,
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE outbox.scope = $1
  AND outbox.message_id = $2
  AND outbox.phase = $3
  AND outbox.ordinal = $4
  AND outbox.claim_token = $5
