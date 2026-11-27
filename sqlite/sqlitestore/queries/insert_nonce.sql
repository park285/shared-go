INSERT INTO iris_nonce (scope, nonce_key, expires_at, created_at)
VALUES (?1, ?2, ?4 + ?3, ?4)
ON CONFLICT (scope, nonce_key) DO UPDATE
SET expires_at = excluded.expires_at,
    created_at = excluded.created_at
WHERE iris_nonce.expires_at <= ?4
