DELETE FROM iris_webhook_inbox
WHERE id IN (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND status = 'completed'
      AND terminal_at <= now() - make_interval(secs => $2)
    ORDER BY terminal_at, id
    LIMIT $3
)
