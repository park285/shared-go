-- completed와 manual_review는 보존이 다르다. manual_review는 사람이 볼 payload를 남기므로 더
-- 길게 두고, $3이 0이면 그 갈래를 아예 지우지 않는다.
DELETE FROM iris_webhook_inbox
WHERE id IN (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND (
          (status = 'completed' AND terminal_at <= now() - make_interval(secs => $2))
          OR ($3 > 0 AND status = 'manual_review' AND terminal_at <= now() - make_interval(secs => $3))
      )
    ORDER BY terminal_at, id
    LIMIT $4
)
