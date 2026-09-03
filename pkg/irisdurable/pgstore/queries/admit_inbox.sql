-- 같은 ordering key의 admit을 직렬화한다. 이 lock이 없으면 Claim이 아직 commit되지 않은 더
-- 오래된 행을 보지 못해 뒤 메시지를 head로 잡고, 앞 행이 commit된 뒤에는 그 행도 head가 되어
-- 같은 key의 두 메시지가 동시에 처리된다. lock은 이 문의 암묵 트랜잭션이 끝날 때 풀린다.
WITH ordering_lock AS (
    SELECT pg_advisory_xact_lock(hashtext($1::text), hashtext($3::text))
)
INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, status, attempts, available_at, created_at, updated_at)
SELECT $1::text, $2::text, $3::text, $4::jsonb, 'pending', 0, clock_timestamp(), clock_timestamp(), clock_timestamp()
FROM ordering_lock
ON CONFLICT ON CONSTRAINT uq_iris_webhook_inbox_identity DO NOTHING
RETURNING id
