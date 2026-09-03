-- list_redrivable_replies.sql의 후보 술어와 같아야 한다. 다르면 "밀린 건수"가 실제로 redrive할 수
-- 있는 행 수와 어긋나 워커 수 판단이 틀어진다. 다른 점은 LIMIT과 payload 반환뿐이다.
WITH ready AS (
    SELECT candidate.id, candidate.created_at
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
)
SELECT count(id),
       coalesce(greatest(extract(epoch FROM now() - min(created_at)), 0), 0)
FROM ready
