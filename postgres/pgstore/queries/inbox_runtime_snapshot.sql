WITH sampled AS (
    SELECT status,
           CASE
               WHEN status = 'pending' THEN available_at
               WHEN status = 'processing' THEN lease_until
               ELSE NULL
           END AS due_at
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND status IN ('pending', 'processing')
    UNION ALL
    SELECT status, NULL::timestamptz AS due_at
    FROM iris_webhook_inbox
    WHERE scope = $1
      AND status = 'manual_review'
)
SELECT count(status) FILTER (WHERE status = 'pending'),
       count(status) FILTER (WHERE status = 'processing'),
       count(status) FILTER (WHERE status = 'manual_review'),
       count(status) FILTER (WHERE due_at <= now()),
       coalesce(
           greatest(
               extract(epoch FROM now() - min(due_at) FILTER (WHERE due_at <= now())),
               0
           ),
           0
       )
FROM sampled
