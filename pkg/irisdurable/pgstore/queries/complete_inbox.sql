-- message_id와 살아 있는 lease를 함께 대조한다. claim token만 보면 lease가 만료된 뒤 아직
-- ReclaimInbox가 지나가지 않은 창에서 complete이 성공하고, 같은 순간 reclaim이 행을 되돌리면
-- 같은 메시지가 두 번 처리된다. 처리가 lease보다 길어질 수 있으면 RenewInbox로 연장한다.
UPDATE iris_webhook_inbox
SET status = 'completed',
    payload = '{}'::jsonb,
    claim_token = NULL,
    lease_until = NULL,
    terminal_at = now(),
    terminal_reason = NULLIF($4, ''),
    updated_at = now()
WHERE scope = $1
  AND id = $2
  AND status = 'processing'
  AND claim_token = $3
  AND message_id = $5
  AND lease_until > clock_timestamp()
