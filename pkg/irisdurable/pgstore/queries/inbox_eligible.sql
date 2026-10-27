{{row}}.scope = $1
AND {{row}}.available_at <= now()
AND (
    {{row}}.status = 'pending'
    OR (NOT {{explicit_recovery}}::boolean AND {{row}}.status = 'processing' AND {{row}}.lease_until <= now())
)
