{{row}}.scope = $1
AND {{row}}.status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown')
AND {{row}}.payload IS NOT NULL
AND {{row}}.attempts < {{max_attempts}}
AND {{row}}.available_at <= now()
AND {{row}}.expires_at > now()
AND ({{row}}.claim_token IS NULL OR {{row}}.lease_until IS NULL OR {{row}}.lease_until <= now())
AND COALESCE({{row}}.first_attempt_at, {{row}}.created_at) > now() - make_interval(secs => {{replay_horizon}})
AND NOT EXISTS (
    SELECT 1
    FROM iris_reply_outbox AS predecessor
    WHERE predecessor.scope = {{row}}.scope
      AND predecessor.message_id = {{row}}.message_id
      AND predecessor.phase = {{row}}.phase
      AND predecessor.ordinal < {{row}}.ordinal
      AND predecessor.status <> 'accepted'
)
