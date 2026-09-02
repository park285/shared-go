UPDATE iris_webhook_inbox
SET status = 'pending',
    claim_token = NULL,
    lease_until = NULL,
    available_at = now(),
    updated_at = now()
WHERE scope = $1
  AND status = 'processing'
  AND lease_until <= now()
