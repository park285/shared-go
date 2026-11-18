heads AS (
    (
        SELECT head_row.id, head_row.ordering_key
        FROM iris_webhook_inbox AS head_row
        WHERE head_row.scope = $1
          AND head_row.status IN ('pending', 'processing')
        ORDER BY head_row.ordering_key, head_row.created_at, head_row.id
        LIMIT 1
    )
    UNION ALL
    SELECT next_head.id, next_head.ordering_key
    FROM heads
    CROSS JOIN LATERAL (
        SELECT head_row.id, head_row.ordering_key
        FROM iris_webhook_inbox AS head_row
        WHERE head_row.scope = $1
          AND head_row.status IN ('pending', 'processing')
          AND head_row.ordering_key > heads.ordering_key
        ORDER BY head_row.ordering_key, head_row.created_at, head_row.id
        LIMIT 1
    ) AS next_head
)
