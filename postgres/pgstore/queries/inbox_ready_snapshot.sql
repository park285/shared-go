-- 논리적 획득 가능성은 일시적 행 잠금을 무시하되 순서 제약은 지킨다. Claim과 같은 head 집합을 쓴다.
WITH RECURSIVE {{heads}},
ready AS (
    SELECT {{eligible_at}} AS eligible_at
    FROM heads
    JOIN iris_webhook_inbox AS current_row ON current_row.id = heads.id
    WHERE {{eligible}}
)
SELECT count(*),
       coalesce(greatest(extract(epoch FROM now() - min(eligible_at)), 0), 0)
FROM ready
