-- 한 claim 호출이 잡은 행 가운데 호출자가 넘긴 id($3)를 뺀 나머지를 되돌린다. 처리를 시작하지 않은
-- 반납이므로 Claim이 올린 attempts를 빼고 available_at은 그대로 둔다. 종단이거나 다른 token이 잡은 행은
-- 조건에서 빠진다. $3이 NULL이면 id = ANY가 NULL이 되어 아무 행도 고르지 않으므로 빈 배열로 바꾼다.
UPDATE iris_webhook_inbox
SET status = 'pending',
    attempts = GREATEST(attempts - 1, 0),
    claim_token = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE scope = $1
  AND status = 'processing'
  AND claim_token = $2
  AND id <> ALL(COALESCE($3::bigint[], '{}'::bigint[]))
