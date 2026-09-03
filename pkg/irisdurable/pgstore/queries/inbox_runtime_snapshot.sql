-- pending은 available_at이, processing은 lease_until이 "지금 손이 가야 하는 시각"이다. 둘을
-- due_at으로 합쳐 밀린 건수와 가장 오래 밀린 시간을 한 번에 센다. manual_review는 자동 처리
-- 대상이 아니므로 due_at이 없다.
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
