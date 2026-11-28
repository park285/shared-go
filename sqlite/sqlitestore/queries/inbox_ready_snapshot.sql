WITH heads AS (
    SELECT min(head_row.id) AS id
    FROM iris_webhook_inbox AS head_row
    WHERE head_row.scope = ?1
      AND head_row.status IN ('pending', 'processing')
    GROUP BY head_row.ordering_key
)
SELECT count(*), min(current_row.available_at)
FROM heads
CROSS JOIN iris_webhook_inbox AS current_row NOT INDEXED ON current_row.id = heads.id
WHERE current_row.available_at <= ?2
  AND current_row.status = 'pending'
