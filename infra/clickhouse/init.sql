-- =============================================================================
-- EventStream ClickHouse schema initialisation
-- Runs automatically on first container start via /docker-entrypoint-initdb.d/
-- =============================================================================

CREATE DATABASE IF NOT EXISTS eventstream;

CREATE TABLE IF NOT EXISTS eventstream.events
(
    event_id        String,
    event_type      LowCardinality(String),
    channel         LowCardinality(String),
    source          LowCardinality(String),
    correlation_id  String,
    timestamp       DateTime64(3, 'UTC'),
    version         String           DEFAULT '1.0',
    metadata        String           DEFAULT '{}',   -- JSON serialised
    payload         String           DEFAULT '{}',   -- JSON serialised
    processed_at    DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (toDate(timestamp), event_id)
TTL toDateTime(timestamp) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;
