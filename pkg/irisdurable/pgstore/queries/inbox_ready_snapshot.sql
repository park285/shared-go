-- Logical claim eligibility excludes transient row locks, not ordering fences.
WITH ready AS (
    SELECT current_row.id, {{eligible_at}} AS eligible_at
    FROM iris_webhook_inbox AS current_row
    WHERE {{eligible}}
)
SELECT count(id),
       coalesce(greatest(extract(epoch FROM now() - min(eligible_at)), 0), 0)
FROM ready
