WITH retiring AS (
    SELECT candidate.id,
           candidate.message_id,
           candidate.phase,
           candidate.ordinal,
           candidate.status,
           candidate.room_id,
           candidate.client_request_id,
           candidate.payload::text AS payload,
           candidate.attempts
    FROM iris_reply_outbox AS candidate
    WHERE candidate.scope = $1
      AND candidate.status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
      AND (candidate.claim_token IS NULL OR candidate.lease_until IS NULL OR candidate.lease_until <= now())
      AND (
            COALESCE(candidate.first_attempt_at, candidate.created_at) <= now() - make_interval(secs => $3)
            OR (
                candidate.status <> 'submitting'
                AND (
                    candidate.attempts >= $2
                    OR EXISTS (
                        SELECT 1
                        FROM iris_reply_outbox AS predecessor
                        WHERE predecessor.scope = candidate.scope
                          AND predecessor.message_id = candidate.message_id
                          AND predecessor.phase = candidate.phase
                          AND predecessor.ordinal < candidate.ordinal
                          AND predecessor.status IN ('dead', 'permanent_conflict')
                    )
                )
            )
          )
    ORDER BY candidate.created_at, candidate.id
    LIMIT $4
    FOR UPDATE SKIP LOCKED
), retired AS (
    UPDATE iris_reply_outbox AS outbox
    SET status = 'dead',
        payload = NULL,
        room_id = '',
        claim_token = NULL,
        lease_until = NULL,
        updated_at = now()
    FROM retiring
    WHERE outbox.id = retiring.id
    RETURNING outbox.id
)
SELECT retiring.message_id, retiring.phase, retiring.ordinal, retiring.status,
       retiring.room_id, retiring.client_request_id, retiring.payload, retiring.attempts
FROM retiring
JOIN retired ON retired.id = retiring.id
