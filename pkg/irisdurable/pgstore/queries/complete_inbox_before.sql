WITH preselected AS MATERIALIZED (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1 AND status = 'pending' AND created_at < $2
    ORDER BY created_at, id
    LIMIT $3
), candidates AS (
    -- 잠긴 오래된 행 뒤로 무제한 탐색하지 않는다. 이번 batch가 비어도 다음 유지보수가 재관측한다.
    SELECT id FROM iris_webhook_inbox
    WHERE id = ANY(ARRAY(SELECT id FROM preselected))
      AND scope = $1 AND status = 'pending' AND created_at < $2
    ORDER BY created_at, id
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
UPDATE iris_webhook_inbox AS inbox
SET status = 'completed', payload = '{}'::jsonb,
    terminal_at = now(), terminal_reason = $4, updated_at = now()
WHERE inbox.id = ANY(ARRAY(SELECT id FROM candidates))
