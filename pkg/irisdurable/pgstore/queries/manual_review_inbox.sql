UPDATE iris_webhook_inbox
SET status = 'manual_review',
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = now(),
    terminal_reason = $4,
    updated_at = now()
WHERE scope = $1
  AND id = $2
  AND status = 'processing'
  AND claim_token = $3
