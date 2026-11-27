CREATE TABLE IF NOT EXISTS sqlitedb_migrations (
    version INTEGER PRIMARY KEY CHECK (version > 0),
    name TEXT NOT NULL UNIQUE CHECK (name <> ''),
    sha256 BLOB NOT NULL CHECK (length(sha256) = 32),
    applied_at_ms INTEGER NOT NULL
) STRICT
