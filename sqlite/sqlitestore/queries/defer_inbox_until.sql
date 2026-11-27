UPDATE iris_webhook_inbox
SET status = 'pending',
    attempts = max(attempts - 1, 0),
    claim_token = NULL,
    lease_until = NULL,
    available_at = ?5,
    updated_at = ?6
WHERE scope = ?1
  AND id = ?2
  AND status = 'processing'
  AND claim_token = ?3
  AND message_id = ?4
