-- 같은 ordering key에 더 오래된 미종단 행이 있으면 그 행이 먼저 끝나야 하므로 FIFO가 유지된다.
WITH candidate AS (
    SELECT current_row.id
    FROM iris_webhook_inbox AS current_row
    WHERE current_row.scope = $1
      AND current_row.available_at <= now()
      AND (
          current_row.status = 'pending'
          OR (current_row.status = 'processing' AND current_row.lease_until <= now())
      )
      AND NOT EXISTS (
          SELECT 1
          FROM iris_webhook_inbox AS older_row
          WHERE older_row.scope = current_row.scope
            AND older_row.ordering_key = current_row.ordering_key
            AND older_row.status IN ('pending', 'processing')
            AND (older_row.created_at, older_row.id) < (current_row.created_at, current_row.id)
      )
    ORDER BY current_row.available_at, current_row.created_at, current_row.id
    FOR UPDATE OF current_row SKIP LOCKED
    LIMIT 1
)
UPDATE iris_webhook_inbox AS inbox
SET status = 'processing',
    attempts = inbox.attempts + 1,
    claim_token = $2,
    lease_until = now() + make_interval(secs => $3),
    updated_at = now()
FROM candidate
WHERE inbox.id = candidate.id
RETURNING inbox.id, inbox.message_id, inbox.ordering_key, inbox.payload::text, inbox.claim_token, inbox.attempts
