CREATE TABLE iris_webhook_inbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    message_id TEXT NOT NULL CHECK (length(trim(message_id)) > 0),
    ordering_key TEXT NOT NULL CHECK (length(trim(ordering_key)) > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload) AND json_type(payload) = 'object'),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'manual_review')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at INTEGER NOT NULL,
    claim_token TEXT,
    lease_until INTEGER,
    terminal_at INTEGER,
    terminal_reason TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (scope, message_id),
    CHECK (
        (status = 'manual_review' AND length(trim(coalesce(terminal_reason, ''))) > 0)
        OR (status = 'completed' AND (terminal_reason IS NULL OR length(trim(terminal_reason)) > 0))
        OR (status NOT IN ('manual_review', 'completed') AND terminal_reason IS NULL)
    ),
    CHECK (
        (status = 'pending' AND claim_token IS NULL AND lease_until IS NULL AND terminal_at IS NULL)
        OR (status = 'processing' AND claim_token IS NOT NULL AND lease_until IS NOT NULL AND terminal_at IS NULL)
        OR (status IN ('completed', 'manual_review') AND claim_token IS NULL AND lease_until IS NULL AND terminal_at IS NOT NULL)
    )
) STRICT;

CREATE INDEX idx_iris_webhook_inbox_head ON iris_webhook_inbox (scope, ordering_key, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX idx_iris_webhook_inbox_pending_age ON iris_webhook_inbox (scope, created_at, id)
    WHERE status = 'pending';

CREATE INDEX idx_iris_webhook_inbox_prune ON iris_webhook_inbox (scope, terminal_at, id)
    WHERE status = 'completed';

CREATE INDEX idx_iris_webhook_inbox_prune_manual_review ON iris_webhook_inbox (scope, terminal_at, id)
    WHERE status = 'manual_review';

CREATE TABLE iris_nonce (
    scope TEXT NOT NULL,
    nonce_key TEXT NOT NULL CHECK (length(trim(nonce_key)) > 0),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (scope, nonce_key)
) STRICT;

CREATE INDEX idx_iris_nonce_prune ON iris_nonce (scope, expires_at);
