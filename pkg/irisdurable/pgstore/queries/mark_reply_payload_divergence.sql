UPDATE iris_reply_outbox
SET payload_divergence_seen = true,
    updated_at = now()
WHERE scope = $1 AND message_id = $2 AND phase = $3 AND ordinal = $4
