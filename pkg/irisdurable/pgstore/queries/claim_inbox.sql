-- 후보는 ordering key별 head뿐이므로 같은 key의 후속 행은 head가 끝날 때까지 잡히지 않아 FIFO가 유지된다.
-- head 판정은 문 스냅샷 기준이고, 잠근 행의 적격 조건은 FOR UPDATE가 최신 행에서 다시 검사한다.
WITH RECURSIVE {{heads}},
candidate AS (
    SELECT current_row.id, {{eligible_at}} AS eligible_at
    FROM heads
    JOIN iris_webhook_inbox AS current_row ON current_row.id = heads.id
    WHERE {{eligible}}
    ORDER BY current_row.available_at, current_row.created_at, current_row.id
    LIMIT $4
    FOR UPDATE OF current_row SKIP LOCKED
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
