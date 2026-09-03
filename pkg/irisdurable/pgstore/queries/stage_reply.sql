-- 삽입과 기존 행 조회를 한 문에 담아, stage 직후의 행 상태를 추가 왕복 없이 돌려준다.
-- 후속 ordinal 가드는 새 행에만 건다. 이미 있는 행은 재stage가 멱등해야 하므로 UNION ALL
-- 갈래가 가드와 무관하게 저장본을 돌려준다.
WITH inserted AS (
    INSERT INTO iris_reply_outbox (
        scope, message_id, phase, ordinal, room_id, client_request_id, payload, payload_hash,
        status, available_at, created_at, updated_at, expires_at
    )
    SELECT $1, $2, $3, $4, $5, $6, $7::jsonb, $8,
           'pending', now(), now(), now(), now() + make_interval(secs => $9)
    WHERE NOT EXISTS (
        SELECT 1
        FROM iris_reply_outbox AS successor
        WHERE successor.scope = $1
          AND successor.message_id = $2
          AND successor.phase = $3
          AND successor.ordinal > $4
    )
    ON CONFLICT ON CONSTRAINT uq_iris_reply_outbox_identity DO NOTHING
    RETURNING id, status, client_request_id, payload::text AS payload, payload_hash::text AS payload_hash,
              attempts, true AS inserted
)
SELECT id, status, client_request_id, payload, payload_hash, attempts, inserted
FROM inserted
UNION ALL
SELECT id, status, client_request_id, payload::text, payload_hash::text, attempts, false
FROM iris_reply_outbox
WHERE scope = $1 AND message_id = $2 AND phase = $3 AND ordinal = $4
  AND NOT EXISTS (SELECT 1 FROM inserted)
