WITH candidates AS (
    SELECT id
    FROM iris_webhook_inbox
    WHERE scope = $1 AND status = 'processing'
      AND lease_until < LEAST($2::timestamptz, now())
    ORDER BY lease_until, id
    LIMIT $4
    FOR UPDATE SKIP LOCKED
), recovered AS (
    UPDATE iris_webhook_inbox AS inbox
    SET status = CASE WHEN $3 > 0 AND attempts >= $3 THEN 'manual_review' ELSE 'pending' END,
        payload = CASE WHEN $3 > 0 AND attempts >= $3 AND $5::boolean THEN '{}'::jsonb ELSE payload END,
        claim_token = NULL,
        lease_until = NULL,
        terminal_at = CASE WHEN $3 > 0 AND attempts >= $3 THEN now() ELSE NULL END,
        terminal_reason = CASE WHEN $3 > 0 AND attempts >= $3 THEN 'processing lease expired; max attempts exhausted' ELSE NULL END,
        available_at = now(),
        updated_at = now()
    WHERE inbox.id = ANY(ARRAY(SELECT id FROM candidates))
    RETURNING inbox.id, inbox.status
)
SELECT count(*) FILTER (WHERE status = 'pending'),
       count(*) FILTER (WHERE status = 'manual_review'),
       coalesce(array_agg(id) FILTER (WHERE status = 'pending'), ARRAY[]::bigint[]),
       coalesce(array_agg(id) FILTER (WHERE status = 'manual_review'), ARRAY[]::bigint[])
FROM recovered
