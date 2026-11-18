UPDATE iris_webhook_inbox
SET status = 'pending',
    attempts = GREATEST(attempts - 1, 0),
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE scope = $1
  AND status = 'processing'
  AND claim_token = $2
  AND id <> ALL(COALESCE($3::bigint[], '{}'::bigint[]))
