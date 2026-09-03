-- 재시도 상한을 넘겼거나 자동 replay 지평을 벗어났거나 선행 순번이 종단이면 더 보낼 수 없다.
--
-- BeginAttempt가 보내기 전에 attempts를 올리므로 마지막 시도는 시작하는 순간 이미 상한을
-- 만족한다. 그래서 소진·선행 종단 판정에서 submitting을 뺀다. lease가 끊긴 submitting은 전송
-- 중인지 작업자가 죽은 것인지 구분할 수 없고, dead로 보내면서 payload를 지우면 실제로 전달된
-- 응답을 미전달로 기록하거나 아직 보낼 수 있는 응답을 복구 불가능하게 잃는다. 그런 행은
-- 결과를 알 수 없는 상태 그대로 두고, 지평을 넘긴 뒤에만 정리한다.
WITH retiring AS (
    SELECT candidate.id
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
