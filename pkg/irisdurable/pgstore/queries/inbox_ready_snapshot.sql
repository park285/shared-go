-- 논리적 획득 가능성은 일시적 행 잠금을 무시하되 순서 제약은 지킨다.
WITH ready AS (
    SELECT current_row.id, {{eligible_at}} AS eligible_at
    FROM iris_webhook_inbox AS current_row
    WHERE {{eligible}}
)
SELECT count(id),
       coalesce(greatest(extract(epoch FROM now() - min(eligible_at)), 0), 0)
FROM ready
