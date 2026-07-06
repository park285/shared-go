SELECT EXISTS (
    SELECT 1
    FROM {{ledger_table}}
    WHERE filename = $1
)
