-- 살아 있는 키와 충돌하면 DO UPDATE의 WHERE가 걸러 0행이 되고, 만료된 키는 같은 문에서
-- 최초 관측으로 갱신된다. 그래서 영향 행 수 하나로 중복 여부가 결정된다.
INSERT INTO iris_nonce (scope, nonce_key, expires_at, created_at)
VALUES ($1, $2, now() + make_interval(secs => $3), now())
ON CONFLICT ON CONSTRAINT pk_iris_nonce DO UPDATE
SET expires_at = now() + make_interval(secs => $3),
    created_at = now()
WHERE iris_nonce.expires_at <= now()
