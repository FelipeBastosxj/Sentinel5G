-- =============================================================================
-- Open Telecom Webhook Observability — PostgreSQL Schema
-- =============================================================================
-- Executed once on first container start via docker-entrypoint-initdb.d.
-- Re-running is safe: every statement uses IF NOT EXISTS.
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Extensions
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "pg_trgm";    -- GIN trigram index for full-text search

-- ---------------------------------------------------------------------------
-- workspaces
-- Each workspace has a unique public endpoint for receiving webhooks.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS workspaces (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    endpoint_token TEXT       NOT NULL UNIQUE,   -- URL token:  /{workspaceId}/{endpointToken}
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- webhook_events
-- Raw events received from telecom providers.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS webhook_events (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,

    -- Provider metadata
    provider       TEXT        NOT NULL DEFAULT 'twilio',  -- 'twilio' | 'vonage' | …
    event_type     TEXT        NOT NULL,                   -- 'message.received' | 'message.status' | …

    -- Telecom-specific identifiers (nullable — depends on event type)
    message_sid    TEXT,
    call_sid       TEXT,
    from_number    TEXT,
    to_number      TEXT,
    status         TEXT,

    -- Full HTTP request
    request_headers JSONB     NOT NULL DEFAULT '{}',
    request_payload JSONB     NOT NULL DEFAULT '{}',

    -- Processing
    received_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at   TIMESTAMPTZ,
    processing_ms  INTEGER                          -- wall-clock processing time
);

-- ---------------------------------------------------------------------------
-- Indexes — common query patterns
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_webhook_events_workspace_id
    ON webhook_events (workspace_id);

CREATE INDEX IF NOT EXISTS idx_webhook_events_received_at
    ON webhook_events (received_at DESC);

CREATE INDEX IF NOT EXISTS idx_webhook_events_provider_event_type
    ON webhook_events (provider, event_type);

CREATE INDEX IF NOT EXISTS idx_webhook_events_message_sid
    ON webhook_events (message_sid)
    WHERE message_sid IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_events_call_sid
    ON webhook_events (call_sid)
    WHERE call_sid IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_events_from_number
    ON webhook_events (from_number)
    WHERE from_number IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_events_to_number
    ON webhook_events (to_number)
    WHERE to_number IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_events_status
    ON webhook_events (status)
    WHERE status IS NOT NULL;

-- GIN index for full-text search inside payload
CREATE INDEX IF NOT EXISTS idx_webhook_events_payload_gin
    ON webhook_events USING GIN (request_payload jsonb_path_ops);

-- ---------------------------------------------------------------------------
-- event_timelines (view)
-- Groups related events under the same MessageSid or CallSid.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE VIEW event_timelines AS
SELECT
    COALESCE(message_sid, call_sid)  AS thread_id,
    CASE WHEN message_sid IS NOT NULL THEN 'message' ELSE 'call' END AS thread_type,
    workspace_id,
    provider,
    JSON_AGG(
        JSON_BUILD_OBJECT(
            'id',         id,
            'event_type', event_type,
            'status',     status,
            'received_at', received_at
        )
        ORDER BY received_at ASC
    )                                AS events,
    MIN(received_at)                 AS first_seen,
    MAX(received_at)                 AS last_seen,
    COUNT(*)                         AS event_count
FROM webhook_events
WHERE message_sid IS NOT NULL
   OR call_sid    IS NOT NULL
GROUP BY COALESCE(message_sid, call_sid),
         CASE WHEN message_sid IS NOT NULL THEN 'message' ELSE 'call' END,
         workspace_id,
         provider;

-- ---------------------------------------------------------------------------
-- Seed data — default workspace for local development
-- ---------------------------------------------------------------------------
INSERT INTO workspaces (id, name, endpoint_token)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'Local Dev Workspace',
    'dev-token-local-001'
)
ON CONFLICT (endpoint_token) DO NOTHING;
