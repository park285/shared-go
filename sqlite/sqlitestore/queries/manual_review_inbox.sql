UPDATE iris_webhook_inbox
SET status = 'manual_review',
    payload = CASE WHEN ?5 THEN '{}' ELSE payload END,
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = ?6,
    terminal_reason = ?4,
    updated_at = ?6
WHERE scope = ?1
  AND id = ?2
  AND status = 'processing'
  AND claim_token = ?3
