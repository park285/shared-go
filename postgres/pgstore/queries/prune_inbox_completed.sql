WITH candidates AS MATERIALIZED (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND status = 'completed'
      AND terminal_at <= LEAST($4::timestamptz, now() - make_interval(secs => $2))
    ORDER BY terminal_at, id
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
DELETE FROM iris_webhook_inbox
WHERE id = ANY(ARRAY(SELECT id FROM candidates))
