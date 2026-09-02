SELECT status, client_request_id, attempts, payload IS NOT NULL
FROM iris_reply_outbox
WHERE scope = $1 AND message_id = $2 AND phase = $3 AND ordinal = $4
