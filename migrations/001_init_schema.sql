-- Create tables for event fanout service

CREATE TABLE IF NOT EXISTS events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type VARCHAR(255) NOT NULL,
    source VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_events_type (type),
    INDEX idx_events_source (source),
    INDEX idx_events_created_at (created_at)
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_url VARCHAR(2048) NOT NULL,
    rules JSONB NOT NULL DEFAULT '{}',
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_subscriptions_active (active),
    INDEX idx_subscriptions_created_at (created_at)
);

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    attempt_number INT DEFAULT 1,
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, success, failed
    http_code INT,
    error_message TEXT,
    next_retry_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_delivery_event_id (event_id),
    INDEX idx_delivery_sub_id (subscription_id),
    INDEX idx_delivery_status (status),
    INDEX idx_delivery_next_retry (next_retry_at),
    UNIQUE KEY uk_delivery_event_sub (event_id, subscription_id)
);

CREATE INDEX idx_delivery_attempts_created_at ON delivery_attempts(created_at);
