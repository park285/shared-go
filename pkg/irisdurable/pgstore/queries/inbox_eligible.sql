{{row}}.scope = $1
AND {{row}}.available_at <= now()
AND (
    {{row}}.status = 'pending'
    OR (NOT {{explicit_recovery}}::boolean AND {{row}}.status = 'processing' AND {{row}}.lease_until <= now())
)
AND NOT EXISTS (
    SELECT 1
    FROM iris_webhook_inbox AS older_row
    WHERE older_row.scope = {{row}}.scope
      AND older_row.ordering_key = {{row}}.ordering_key
      AND older_row.status IN ('pending', 'processing')
      AND (older_row.created_at, older_row.id) < ({{row}}.created_at, {{row}}.id)
)
