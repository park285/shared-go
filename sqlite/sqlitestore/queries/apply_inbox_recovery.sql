UPDATE iris_webhook_inbox
SET status = ?6,
    payload = CASE
        WHEN ?6 = 'completed' OR (?6 = 'manual_review' AND ?8) THEN '{}'
        ELSE payload
    END,
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = CASE WHEN ?6 = 'pending' THEN NULL ELSE ?9 END,
    terminal_reason = NULLIF(?7, ''),
    available_at = ?9,
    updated_at = ?9
WHERE scope = ?1
  AND id = ?2
  AND message_id = ?3
  AND attempts = ?5
  AND (
      (?4 <> '' AND status = 'processing' AND claim_token = ?4 AND lease_until = ?10)
      OR (?4 = '' AND status = 'pending' AND claim_token IS NULL AND lease_until IS NULL)
  )
