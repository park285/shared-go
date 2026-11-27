DELETE FROM iris_webhook_inbox
WHERE id IN (
    SELECT candidate.id
    FROM iris_webhook_inbox AS candidate
    WHERE candidate.scope = ?1
      AND candidate.status = 'manual_review'
      AND candidate.terminal_at <= min(?4, ?5 - ?2)
    ORDER BY candidate.terminal_at, candidate.id
    LIMIT ?3
)
