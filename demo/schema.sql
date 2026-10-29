-- Demo schema captured from the standalone ClickHouse 26.8.36.3 instance.
-- Definitions only; contains no event data.
CREATE DATABASE IF NOT EXISTS demo;

CREATE TABLE demo.activity (`event_time` DateTime, `user_id` UInt32, `kind` String, `amount` Float64, `segment` String) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.audit_events (`event_time` DateTime, `user_id` UInt32, `action` String, `detail` String) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.audit_summary (`event_time` DateTime, `action` String, `events` UInt64) ENGINE = MergeTree ORDER BY (event_time, action) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.customer_ledger (`event_time` DateTime, `user_id` UInt32, `operation` String, `delta` Float64) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.daily_activity (`event_time` DateTime, `kind` String, `events` UInt64, `amount` Float64) ENGINE = MergeTree ORDER BY (event_time, kind) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.heartbeat (`event_time` DateTime, `user_id` UInt32, `origin` String) ENGINE = `Null`;

CREATE TABLE demo.ledger_hourly (`event_time` DateTime, `user_id` UInt32, `operations` UInt64, `balance_delta` Float64) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.page_metrics (`event_time` DateTime, `page` String, `views` UInt64) ENGINE = MergeTree ORDER BY (event_time, page) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.page_views (`event_time` DateTime, `user_id` UInt32, `page` String) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.purchases (`event_time` DateTime, `user_id` UInt32, `amount` Float64) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.refunds (`event_time` DateTime, `user_id` UInt32, `amount` Float64) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.revenue_daily (`event_time` DateTime, `kind` String, `events` UInt64, `amount` Float64) ENGINE = MergeTree ORDER BY (event_time, kind) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE TABLE demo.segments (`user_id` UInt32, `segment` String) ENGINE = MergeTree ORDER BY user_id SETTINGS index_granularity = 8192;

CREATE TABLE demo.value_alerts (`event_time` DateTime, `user_id` UInt32, `category` String, `amount` Float64) ENGINE = MergeTree ORDER BY (event_time, user_id) TTL event_time + toIntervalDay(1) SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW demo.audit_summary_mv TO demo.audit_summary (`event_time` DateTime, `action` String, `events` UInt64) AS SELECT toStartOfMinute(event_time) AS event_time, action, count() AS events FROM demo.audit_events GROUP BY event_time, action;

CREATE MATERIALIZED VIEW demo.daily_activity_mv TO demo.daily_activity (`event_time` DateTime, `kind` String, `events` UInt64, `amount` Float64) AS SELECT event_time, kind, count() AS events, sum(amount) AS amount FROM demo.activity GROUP BY event_time, kind;

CREATE MATERIALIZED VIEW demo.heartbeat_audit_mv TO demo.audit_events (`event_time` DateTime, `user_id` UInt32, `action` String, `detail` String) AS SELECT event_time, user_id, 'heartbeat' AS action, origin AS detail FROM demo.heartbeat;

CREATE MATERIALIZED VIEW demo.heartbeat_mv TO demo.activity (`event_time` DateTime, `user_id` UInt32, `kind` String, `amount` Float64, `segment` String) AS SELECT event_time, user_id, 'heartbeat' AS kind, toFloat64(0) AS amount, origin AS segment FROM demo.heartbeat;

CREATE MATERIALIZED VIEW demo.high_value_purchase_mv TO demo.value_alerts (`event_time` DateTime, `user_id` UInt32, `category` String, `amount` Float64) AS SELECT event_time, user_id, 'large purchase' AS category, amount FROM demo.purchases WHERE amount >= 150;

CREATE MATERIALIZED VIEW demo.ledger_hourly_mv TO demo.ledger_hourly (`event_time` DateTime, `user_id` UInt32, `operations` UInt64, `balance_delta` Float64) AS SELECT toStartOfHour(event_time) AS event_time, user_id, count() AS operations, sum(delta) AS balance_delta FROM demo.customer_ledger GROUP BY event_time, user_id;

CREATE MATERIALIZED VIEW demo.page_audit_mv TO demo.audit_events (`event_time` DateTime, `user_id` UInt32, `action` String, `detail` String) AS SELECT event_time, user_id, 'page' AS action, page AS detail FROM demo.page_views WHERE page != '/page/0';

CREATE MATERIALIZED VIEW demo.page_metrics_mv TO demo.page_metrics (`event_time` DateTime, `page` String, `views` UInt64) AS SELECT toStartOfMinute(event_time) AS event_time, page, count() AS views FROM demo.page_views GROUP BY event_time, page;

CREATE MATERIALIZED VIEW demo.page_views_mv TO demo.activity (`event_time` DateTime, `user_id` UInt32, `kind` String, `amount` Float64, `segment` String) AS SELECT p.event_time AS event_time, p.user_id AS user_id, 'view' AS kind, toFloat64(0) AS amount, s.segment AS segment FROM demo.page_views AS p LEFT JOIN demo.segments AS s ON p.user_id = s.user_id;

CREATE MATERIALIZED VIEW demo.purchase_audit_mv TO demo.audit_events (`event_time` DateTime, `user_id` UInt32, `action` String, `detail` String) AS SELECT event_time, user_id, 'purchase' AS action, segment AS detail FROM demo.activity WHERE kind = 'purchase';

CREATE MATERIALIZED VIEW demo.purchase_ledger_mv TO demo.customer_ledger (`event_time` DateTime, `user_id` UInt32, `operation` String, `delta` Float64) AS SELECT event_time, user_id, 'purchase' AS operation, round(amount, 2) AS delta FROM demo.purchases;

CREATE MATERIALIZED VIEW demo.purchases_mv TO demo.activity (`event_time` DateTime, `user_id` UInt32, `kind` String, `amount` Float64, `segment` String) AS SELECT event_time, user_id, 'purchase' AS kind, amount, 'customer' AS segment FROM demo.purchases;

CREATE MATERIALIZED VIEW demo.refund_alert_mv TO demo.value_alerts (`event_time` DateTime, `user_id` UInt32, `category` String, `amount` Float64) AS SELECT event_time, user_id, 'refund' AS category, -amount AS amount FROM demo.activity WHERE (kind = 'refund') AND (amount <= -10);

CREATE MATERIALIZED VIEW demo.refund_ledger_mv TO demo.customer_ledger (`event_time` DateTime, `user_id` UInt32, `operation` String, `delta` Float64) AS SELECT event_time, user_id, 'refund' AS operation, -round(amount, 2) AS delta FROM demo.refunds;

CREATE MATERIALIZED VIEW demo.refunds_mv TO demo.activity (`event_time` DateTime, `user_id` UInt32, `kind` String, `amount` Float64, `segment` String) AS SELECT event_time, user_id, 'refund' AS kind, -amount AS amount, 'customer' AS segment FROM demo.refunds;

CREATE MATERIALIZED VIEW demo.revenue_daily_mv TO demo.revenue_daily (`event_time` DateTime, `kind` String, `events` UInt64, `amount` Float64) AS SELECT toStartOfHour(event_time) AS event_time, kind, sum(events) AS events, sum(amount) AS amount FROM demo.daily_activity WHERE kind IN ('purchase', 'refund') GROUP BY event_time, kind;
