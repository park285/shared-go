-- 같은 (scope, message_id, phase)의 stage를 직렬화한다. 이 lock이 없으면 stage_reply.sql의 후속
-- ordinal 가드가 READ COMMITTED에서 성립하지 않는다. 두 stage가 동시에 오면 서로의 미commit
-- 행을 보지 못해 양쪽 다 가드를 통과하고, 늦게 도착한 낮은 순번이 행으로 남아 이미 보낸 응답
-- 뒤에 앞 순번이 다시 나간다. lock은 감싼 트랜잭션이 끝날 때 풀린다.
SELECT pg_advisory_xact_lock(hashtext($1::text || ':' || $2::text), hashtext($3::text))
