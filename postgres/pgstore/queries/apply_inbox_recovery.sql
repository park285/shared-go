-- 호출자는 같은 트랜잭션에서 선택·업무 잠금을 유지하며 변경 행 수와 결정 수가 다르면 롤백한다.
UPDATE iris_webhook_inbox AS inbox
SET status = decision.status,
    payload = CASE
        WHEN decision.status = 'completed'
          OR (decision.status = 'manual_review' AND $9::boolean) THEN '{}'::jsonb
        ELSE inbox.payload
    END,
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = CASE WHEN decision.status = 'pending' THEN NULL ELSE now() END,
    terminal_reason = NULLIF(decision.reason, ''),
    available_at = now(),
    updated_at = now()
FROM unnest($2::bigint[], $3::text[], $4::text[], $5::integer[],
            $6::timestamptz[], $7::text[], $8::text[])
    AS decision(id, message_id, claim_token, attempts, lease_until, status, reason)
WHERE inbox.scope = $1
  AND inbox.id = decision.id
  AND inbox.message_id = decision.message_id
  AND inbox.attempts = decision.attempts
  AND (
      (decision.claim_token <> '' AND inbox.status = 'processing'
       AND inbox.claim_token = decision.claim_token
       AND inbox.lease_until = decision.lease_until)
      OR
      (decision.claim_token = '' AND inbox.status = 'pending'
       AND inbox.claim_token IS NULL AND inbox.lease_until IS NULL)
  )
