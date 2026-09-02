UPDATE iris_webhook_inbox
SET status = 'pending',
    claim_token = NULL,
    lease_until = NULL,
    available_at = now() + make_interval(secs => $4),
    updated_at = now()
WHERE scope = $1
  AND id = $2
  AND status = 'processing'
  AND claim_token = $3
