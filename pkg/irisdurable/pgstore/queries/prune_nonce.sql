DELETE FROM iris_nonce
WHERE (scope, nonce_key) IN (
    SELECT scope, nonce_key
    FROM iris_nonce
    WHERE scope = $1
      AND expires_at <= now()
    ORDER BY expires_at
    LIMIT $2
)
