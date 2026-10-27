-- manual_review 갈래만 지운다. 보존이 0이면 Go가 이 문을 실행하지 않는다. 술어가
-- idx_iris_webhook_inbox_prune_manual_review의 순서와 같고, 후보 고정 이유는 prune_inbox_completed.sql과 같다.
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND status = 'manual_review'
      AND terminal_at <= LEAST($4::timestamptz, now() - make_interval(secs => $2))
    ORDER BY terminal_at, id
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
DELETE FROM iris_webhook_inbox
WHERE id = ANY(ARRAY(SELECT id FROM candidates))
