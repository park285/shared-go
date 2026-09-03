SELECT status, count(1)
FROM iris_reply_outbox
WHERE scope = $1
GROUP BY status
