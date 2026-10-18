SELECT candidate.message_id,
       candidate.phase,
       candidate.ordinal,
       candidate.room_id,
       candidate.client_request_id,
       candidate.payload::text,
       candidate.attempts
FROM iris_reply_outbox AS candidate
WHERE {{eligible}}
ORDER BY candidate.created_at, candidate.id
LIMIT $4
