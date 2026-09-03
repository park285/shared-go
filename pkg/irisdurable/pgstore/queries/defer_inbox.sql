-- Release와 달리 attempts를 되돌린다. 처리를 시작하지 못하고 소유권만 반납하는 경로이므로
-- 이번 claim은 시도가 아니었고, 재시도 예산을 쓰면 안 된다. Claim이 올린 1을 그대로 뺀다.
UPDATE iris_webhook_inbox
SET status = 'pending',
    attempts = GREATEST(attempts - 1, 0),
    claim_token = NULL,
    lease_until = NULL,
    available_at = clock_timestamp() + make_interval(secs => $5),
    updated_at = now()
WHERE scope = $1
  AND id = $2
  AND status = 'processing'
  AND claim_token = $3
  AND message_id = $4
