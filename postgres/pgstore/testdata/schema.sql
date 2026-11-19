CREATE TABLE IF NOT EXISTS iris_webhook_inbox (
    id bigserial PRIMARY KEY,
    scope text NOT NULL,
    message_id text NOT NULL,
    ordering_key text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    claim_token text,
    lease_until timestamptz,
    terminal_at timestamptz,
    terminal_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_iris_webhook_inbox_identity UNIQUE (scope, message_id),
    CONSTRAINT chk_iris_webhook_inbox_message_id
        CHECK (length(btrim(message_id)) > 0),
    CONSTRAINT chk_iris_webhook_inbox_ordering_key
        CHECK (length(btrim(ordering_key)) > 0),
    CONSTRAINT chk_iris_webhook_inbox_payload
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_iris_webhook_inbox_attempts
        CHECK (attempts >= 0),
    CONSTRAINT chk_iris_webhook_inbox_status
        CHECK (status IN ('pending', 'processing', 'completed', 'manual_review')),
    CONSTRAINT chk_iris_webhook_inbox_terminal_reason
        CHECK (
            (status = 'manual_review' AND length(btrim(coalesce(terminal_reason, ''))) > 0)
            OR (status = 'completed' AND (terminal_reason IS NULL OR length(btrim(terminal_reason)) > 0))
            OR (status NOT IN ('manual_review', 'completed') AND terminal_reason IS NULL)
        ),
    CONSTRAINT chk_iris_webhook_inbox_lease
        CHECK (
            (status = 'pending' AND claim_token IS NULL AND lease_until IS NULL AND terminal_at IS NULL)
            OR (status = 'processing' AND claim_token IS NOT NULL AND lease_until IS NOT NULL AND terminal_at IS NULL)
            OR (status IN ('completed', 'manual_review') AND claim_token IS NULL AND lease_until IS NULL AND terminal_at IS NOT NULL)
        )
);

CREATE INDEX IF NOT EXISTS idx_iris_webhook_inbox_claim
    ON iris_webhook_inbox (scope, available_at, created_at, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_iris_webhook_inbox_head
    ON iris_webhook_inbox (scope, ordering_key, created_at, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_iris_webhook_inbox_head_by_id
    ON iris_webhook_inbox (scope, ordering_key, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_iris_webhook_inbox_prune
    ON iris_webhook_inbox (scope, terminal_at, id)
    WHERE status = 'completed';

CREATE INDEX IF NOT EXISTS idx_iris_webhook_inbox_prune_manual_review
    ON iris_webhook_inbox (scope, terminal_at, id)
    WHERE status = 'manual_review';

CREATE TABLE IF NOT EXISTS iris_nonce (
    scope text NOT NULL,
    nonce_key text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pk_iris_nonce PRIMARY KEY (scope, nonce_key),
    CONSTRAINT chk_iris_nonce_key
        CHECK (length(btrim(nonce_key)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_iris_nonce_prune
    ON iris_nonce (scope, expires_at);

CREATE TABLE IF NOT EXISTS iris_reply_outbox (
    id bigserial PRIMARY KEY,
    scope text NOT NULL,
    message_id text NOT NULL,
    phase text NOT NULL,
    ordinal integer NOT NULL,
    room_id text NOT NULL,
    client_request_id text NOT NULL,
    payload jsonb,
    payload_hash char(64) NOT NULL,
    payload_divergence_seen boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    first_attempt_at timestamptz,
    iris_request_id text,
    claim_token text,
    lease_until timestamptz,
    terminal_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT uq_iris_reply_outbox_identity UNIQUE (scope, message_id, phase, ordinal),
    CONSTRAINT chk_iris_reply_outbox_message_id
        CHECK (length(btrim(message_id)) > 0),
    CONSTRAINT chk_iris_reply_outbox_phase
        CHECK (length(btrim(phase)) > 0),
    CONSTRAINT chk_iris_reply_outbox_ordinal
        CHECK (ordinal >= 0),
    CONSTRAINT chk_iris_reply_outbox_attempts
        CHECK (attempts >= 0),
    CONSTRAINT chk_iris_reply_outbox_payload_hash
        CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_iris_reply_outbox_client_request_id
        CHECK (client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'),
    CONSTRAINT chk_iris_reply_outbox_status
        CHECK (status IN (
            'pending',
            'submitting',
            'accepted',
            'retryable_pre_dispatch',
            'outcome_unknown',
            'dead',
            'permanent_conflict',
            'manual_review'
        )),
    CONSTRAINT chk_iris_reply_outbox_payload_replayable
        CHECK (payload IS NOT NULL OR status IN ('accepted', 'dead', 'permanent_conflict')),
    CONSTRAINT chk_iris_reply_outbox_room_scrub
        CHECK (length(btrim(room_id)) > 0 OR status IN ('accepted', 'dead', 'permanent_conflict')),
    CONSTRAINT chk_iris_reply_outbox_terminal_reason
        CHECK (
            (status = 'manual_review' AND length(btrim(coalesce(terminal_reason, ''))) > 0)
            OR (status <> 'manual_review' AND terminal_reason IS NULL)
        )
);

CREATE INDEX IF NOT EXISTS idx_iris_reply_outbox_claimable
    ON iris_reply_outbox (scope, available_at, created_at, id)
    WHERE status IN ('pending', 'submitting', 'retryable_pre_dispatch', 'outcome_unknown');

CREATE INDEX IF NOT EXISTS idx_iris_reply_outbox_prune
    ON iris_reply_outbox (scope, expires_at, id)
    WHERE status <> 'manual_review';
