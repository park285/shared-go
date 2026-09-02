-- 재시도 상한을 넘겼거나 자동 replay 지평을 벗어났거나 선행 순번이 종단이면 더 보낼 수 없다.
WITH retiring AS (
    SELECT candidate.id
    FROM iris_reply_outbox AS candidate
    WHERE candidate.scope = $1
      AND candidate.status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
      AND (candidate.claim_token IS NULL OR candidate.lease_until IS NULL OR candidate.lease_until <= now())
      AND (
            candidate.attempts >= $2
            OR COALESCE(candidate.first_attempt_at, candidate.created_at) <= now() - make_interval(secs => $3)
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
    ORDER BY candidate.created_at, candidate.id
    LIMIT $4
    FOR UPDATE SKIP LOCKED
)
UPDATE iris_reply_outbox AS outbox
SET status = 'dead',
    payload = NULL,
    room_id = '',
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
FROM retiring
WHERE outbox.id = retiring.id
