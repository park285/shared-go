INSERT INTO {{ledger_table}} (filename)
VALUES ({{filename_literal}})
ON CONFLICT (filename) DO NOTHING
