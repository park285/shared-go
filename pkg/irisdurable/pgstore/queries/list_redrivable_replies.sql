SELECT candidate.message_id,
       candidate.phase,
       candidate.ordinal,
       candidate.room_id,
       candidate.client_request_id,
       candidate.payload::text,
       candidate.attempts
FROM iris_reply_outbox AS candidate
WHERE candidate.scope = $1
  AND candidate.status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
  AND candidate.payload IS NOT NULL
  AND candidate.attempts < $2
  AND candidate.available_at <= now()
  AND candidate.expires_at > now()
  AND (candidate.claim_token IS NULL OR candidate.lease_until IS NULL OR candidate.lease_until <= now())
  AND COALESCE(candidate.first_attempt_at, candidate.created_at) > now() - make_interval(secs => $3)
  AND NOT EXISTS (
        SELECT 1
        FROM iris_reply_outbox AS predecessor
        WHERE predecessor.scope = candidate.scope
          AND predecessor.message_id = candidate.message_id
          AND predecessor.phase = candidate.phase
          AND predecessor.ordinal < candidate.ordinal
          AND predecessor.status <> 'accepted'
      )
ORDER BY candidate.created_at, candidate.id
LIMIT $4
