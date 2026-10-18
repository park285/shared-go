-- 같은 ordering key에 더 오래된 미종단 행이 있으면 그 행이 먼저 끝나야 하므로 FIFO가 유지된다.
WITH candidate AS (
    SELECT current_row.id, {{eligible_at}} AS eligible_at
    FROM iris_webhook_inbox AS current_row
    WHERE {{eligible}}
    ORDER BY current_row.available_at, current_row.created_at, current_row.id
    FOR UPDATE OF current_row SKIP LOCKED
    LIMIT $4
)
UPDATE iris_webhook_inbox AS inbox
SET status = 'processing',
    attempts = inbox.attempts + 1,
    claim_token = $2,
    lease_until = now() + make_interval(secs => $3),
    updated_at = now()
FROM candidate
WHERE inbox.id = candidate.id
RETURNING inbox.id, inbox.message_id, inbox.ordering_key, inbox.payload::text, inbox.claim_token, inbox.attempts, inbox.created_at, candidate.eligible_at
