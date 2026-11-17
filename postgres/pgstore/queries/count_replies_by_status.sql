-- 아직 보낼 수 있는 행만 센다. 종단 행까지 세면 보존 기간(168h) 전체를 순차로 훑어야 하고,
-- 그 수는 backlog 관측에 쓸 값도 아니다. 이 술어는 idx_iris_reply_outbox_claimable과 같으므로
-- 부분 인덱스만 읽는다.
SELECT status, count(1)
FROM iris_reply_outbox
WHERE scope = $1
  AND status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
GROUP BY status
