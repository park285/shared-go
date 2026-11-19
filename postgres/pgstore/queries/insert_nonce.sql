INSERT INTO iris_nonce (scope, nonce_key, expires_at, created_at)
VALUES ($1, $2, now() + make_interval(secs => $3), now())
ON CONFLICT ON CONSTRAINT pk_iris_nonce DO UPDATE
SET expires_at = now() + make_interval(secs => $3),
    created_at = now()
WHERE iris_nonce.expires_at <= now()
