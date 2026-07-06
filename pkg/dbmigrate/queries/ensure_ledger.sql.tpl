CREATE TABLE IF NOT EXISTS {{ledger_table}} (
    filename TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
