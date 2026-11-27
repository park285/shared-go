DELETE FROM iris_nonce
WHERE (scope, nonce_key) IN (
    SELECT candidate.scope, candidate.nonce_key
    FROM iris_nonce AS candidate
    WHERE candidate.scope = ?1
      AND candidate.expires_at <= ?3
    ORDER BY candidate.expires_at, candidate.nonce_key
    LIMIT ?2
)
