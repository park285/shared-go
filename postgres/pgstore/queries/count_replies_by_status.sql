SELECT status, count(1)
FROM iris_reply_outbox
WHERE scope = $1
  AND status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
GROUP BY status
