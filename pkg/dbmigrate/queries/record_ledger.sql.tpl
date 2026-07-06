INSERT INTO {{ledger_table}} (filename)
VALUES ($1)
ON CONFLICT (filename) DO NOTHING
