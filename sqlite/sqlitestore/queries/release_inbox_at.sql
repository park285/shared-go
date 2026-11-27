UPDATE iris_webhook_inbox
SET status = 'pending',
    claim_token = NULL,
    lease_until = NULL,
    available_at = ?4,
    updated_at = ?5
WHERE scope = ?1
  AND id = ?2
  AND status = 'processing'
  AND claim_token = ?3
