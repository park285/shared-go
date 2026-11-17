-- 더 보낼 것이 없는 상태는 payload와 room을 지우고, 결과 미상은 같은 clientRequestId로 다시
-- 보내야 하므로 둘 다 보존한다.
-- 전진한 시도는 재시도 예산을 쓰지 않는다. $10이 그 판정이고, 이유는 irisdurable.ReplyOutcome의
-- Progressed 주석이 적는다.
UPDATE iris_reply_outbox AS outbox
SET status = $6,
    client_request_id = COALESCE(NULLIF($7, ''), outbox.client_request_id),
    iris_request_id = COALESCE(NULLIF($8, ''), outbox.iris_request_id),
    payload = CASE WHEN $6 IN ('accepted', 'dead', 'permanent_conflict') THEN NULL ELSE outbox.payload END,
    room_id = CASE WHEN $6 IN ('accepted', 'dead', 'permanent_conflict') THEN '' ELSE outbox.room_id END,
    attempts = CASE
        WHEN $10 AND $6 NOT IN ('accepted', 'dead', 'permanent_conflict') THEN 0
        ELSE outbox.attempts
    END,
    available_at = now() + make_interval(secs => $9),
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE outbox.scope = $1
  AND outbox.message_id = $2
  AND outbox.phase = $3
  AND outbox.ordinal = $4
  AND outbox.claim_token = $5
