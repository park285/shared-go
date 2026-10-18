WITH ready AS (
    SELECT candidate.id, {{eligible_at}} AS eligible_at
    FROM iris_reply_outbox AS candidate
    WHERE {{eligible}}
)
SELECT count(id),
       coalesce(greatest(extract(epoch FROM now() - min(eligible_at)), 0), 0)
FROM ready
