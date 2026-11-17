DELETE FROM iris_reply_outbox
WHERE id IN (
    SELECT id
    FROM iris_reply_outbox
    WHERE scope = $1
      AND status <> 'manual_review'
      AND expires_at <= now()
    ORDER BY expires_at, id
    LIMIT $2
)
