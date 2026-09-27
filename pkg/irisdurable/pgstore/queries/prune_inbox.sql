-- completed와 manual_review는 보존이 다르다. manual_review는 사람이 볼 payload를 남기므로 더
-- 길게 두고, $3이 0이면 그 갈래를 아예 지우지 않는다.
-- 후보는 한 번만 계산해 고정한다. `id IN (... LIMIT ... SKIP LOCKED)`는 planner가 부분 질의를
-- nested loop 안쪽에서 바깥 행마다 다시 실행할 수 있고, 그때마다 이번 문이 이미 지운 행을
-- 건너뛰고 다음 행을 내주므로 한 호출이 LIMIT보다 많이 지운다.
WITH candidates AS MATERIALIZED (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND (
          (status = 'completed' AND terminal_at <= LEAST($5::timestamptz, now() - make_interval(secs => $2)))
          OR ($3 > 0 AND status = 'manual_review' AND terminal_at <= LEAST($5::timestamptz, now() - make_interval(secs => $3)))
      )
    ORDER BY terminal_at, id
    LIMIT $4
    FOR UPDATE SKIP LOCKED
)
DELETE FROM iris_webhook_inbox
WHERE id = ANY(ARRAY(SELECT id FROM candidates))
