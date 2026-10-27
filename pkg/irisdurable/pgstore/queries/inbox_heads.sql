-- ordering key마다 가장 오래된 미종단 행(head) 하나만 idx_iris_webhook_inbox_head를 건너뛰며 꺼낸다
-- (loose index scan). 후속 행은 head가 끝나야 적격이므로 후보에서 아예 빠지고, 비용은 활성 key 수에 비례한다.
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
