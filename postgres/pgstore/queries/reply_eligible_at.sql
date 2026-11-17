CASE WHEN {{row}}.claim_token IS NOT NULL AND {{row}}.lease_until IS NOT NULL
     THEN greatest({{row}}.available_at, {{row}}.lease_until)
     ELSE {{row}}.available_at END
