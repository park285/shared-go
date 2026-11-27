UPDATE iris_webhook_inbox
SET status = 'processing',
    attempts = attempts + 1,
    claim_token = ?3,
    lease_until = ?4,
    updated_at = ?5
WHERE scope = ?1
  AND id = ?2
RETURNING id, message_id, ordering_key, payload, claim_token, attempts, created_at
