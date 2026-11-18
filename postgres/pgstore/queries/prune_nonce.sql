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
