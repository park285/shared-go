UPDATE iris_reply_outbox AS outbox
SET status = 'submitting',
    attempts = outbox.attempts + 1,
    first_attempt_at = COALESCE(outbox.first_attempt_at, now()),
    claim_token = $5,
    lease_until = now() + make_interval(secs => $6),
    updated_at = now()
WHERE outbox.scope = $1
  AND outbox.message_id = $2
  AND outbox.phase = $3
  AND outbox.ordinal = $4
  AND outbox.status NOT IN ('accepted', 'dead', 'permanent_conflict', 'manual_review')
  AND outbox.attempts < $7
  AND outbox.available_at <= now()
  AND (outbox.claim_token IS NULL OR outbox.lease_until IS NULL OR outbox.lease_until <= now())
  AND outbox.expires_at > now()
  AND COALESCE(outbox.first_attempt_at, outbox.created_at) > now() - make_interval(secs => $8)
  AND NOT EXISTS (
        SELECT 1
        FROM iris_reply_outbox AS predecessor
        WHERE predecessor.scope = outbox.scope
          AND predecessor.message_id = outbox.message_id
          AND predecessor.phase = outbox.phase
          AND predecessor.ordinal < outbox.ordinal
          AND predecessor.status <> 'accepted'
      )
RETURNING outbox.attempts, outbox.client_request_id
