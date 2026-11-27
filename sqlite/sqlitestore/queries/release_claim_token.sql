UPDATE iris_webhook_inbox
SET status = 'pending',
    attempts = max(attempts - 1, 0),
    claim_token = NULL,
    lease_until = NULL,
    updated_at = ?4
WHERE scope = ?1
  AND status = 'processing'
  AND claim_token = ?2
  AND id NOT IN (SELECT kept.value FROM json_each(?3) AS kept)
