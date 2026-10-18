CASE WHEN {{row}}.status = 'processing'
     THEN greatest({{row}}.available_at, {{row}}.lease_until)
     ELSE {{row}}.available_at END
