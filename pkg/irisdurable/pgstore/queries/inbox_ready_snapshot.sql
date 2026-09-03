-- claim_inbox.sql의 후보 술어와 같아야 한다. 다르면 "밀린 건수"가 실제로 집을 수 있는 행 수와
-- 어긋나 워커 수 판단이 틀어진다.
WITH ready AS (
    SELECT current_row.id, current_row.created_at
    FROM iris_webhook_inbox AS current_row
    WHERE current_row.scope = $1
      AND current_row.available_at <= now()
      AND (
          current_row.status = 'pending'
          OR (current_row.status = 'processing' AND current_row.lease_until <= now())
      )
      AND NOT EXISTS (
          SELECT 1
          FROM iris_webhook_inbox AS older_row
          WHERE older_row.scope = current_row.scope
            AND older_row.ordering_key = current_row.ordering_key
            AND older_row.status IN ('pending', 'processing')
            AND (older_row.created_at, older_row.id) < (current_row.created_at, current_row.id)
      )
)
SELECT count(id),
       coalesce(greatest(extract(epoch FROM now() - min(created_at)), 0), 0)
FROM ready
