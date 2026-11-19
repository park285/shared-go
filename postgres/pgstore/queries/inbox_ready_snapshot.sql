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
