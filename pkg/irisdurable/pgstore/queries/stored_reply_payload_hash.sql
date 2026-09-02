SELECT payload_hash
FROM iris_reply_outbox
WHERE scope = $1 AND message_id = $2 AND phase = $3 AND ordinal = $4
