-- IsDuplicate의 만료 행 갱신과 같은 PK 행을 다루므로, 후보를 잠근 뒤 지운다. 이미 갱신이
-- 진행 중인 행은 건너뛰고, DELETE에서도 현재 만료 시각을 다시 확인해 살아난 nonce를 보존한다.
WITH candidates AS MATERIALIZED (
    SELECT scope, nonce_key
    FROM iris_nonce
    WHERE scope = $1
      AND expires_at <= now()
    ORDER BY expires_at, nonce_key
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM iris_nonce AS nonce
USING candidates
WHERE nonce.scope = candidates.scope
  AND nonce.nonce_key = candidates.nonce_key
  AND nonce.expires_at <= now()
