#!/usr/bin/env bash
set -euo pipefail

query() { clickhouse-client --host "$1" --query "$2" </dev/null; }
for attempt in $(seq 1 60); do
  if [[ $(query replica1 "SELECT count() >= 0 FROM system.zookeeper WHERE path = '/'" 2>/dev/null || true) == 1 ]]; then break; fi
  if [[ $attempt == 60 ]]; then echo 'Keeper did not become ready' >&2; exit 1; fi
  sleep 1
done

for host in standalone replica1 replica2; do
  query "$host" 'CREATE DATABASE IF NOT EXISTS demo'
  for table in page_views purchases refunds activity daily_activity customer_ledger value_alerts page_metrics audit_events revenue_daily audit_summary ledger_hourly; do
    if [[ $host == standalone ]]; then
      engine=MergeTree
    else
      engine="ReplicatedMergeTree('/clickhouse/chinspector_demo/$table', '{replica}')"
    fi
    case $table in
      page_views) columns='event_time DateTime, user_id UInt32, page String'; order='(event_time, user_id)' ;;
      purchases|refunds) columns='event_time DateTime, user_id UInt32, amount Float64'; order='(event_time, user_id)' ;;
      activity) columns='event_time DateTime, user_id UInt32, kind String, amount Float64, segment String'; order='(event_time, user_id)' ;;
      daily_activity) columns='event_time DateTime, kind String, events UInt64, amount Float64'; order='(event_time, kind)' ;;
      customer_ledger) columns='event_time DateTime, user_id UInt32, operation String, delta Float64'; order='(event_time, user_id)' ;;
      value_alerts) columns='event_time DateTime, user_id UInt32, category String, amount Float64'; order='(event_time, user_id)' ;;
      page_metrics) columns='event_time DateTime, page String, views UInt64'; order='(event_time, page)' ;;
      audit_events) columns='event_time DateTime, user_id UInt32, action String, detail String'; order='(event_time, user_id)' ;;
      revenue_daily) columns='event_time DateTime, kind String, events UInt64, amount Float64'; order='(event_time, kind)' ;;
      audit_summary) columns='event_time DateTime, action String, events UInt64'; order='(event_time, action)' ;;
      ledger_hourly) columns='event_time DateTime, user_id UInt32, operations UInt64, balance_delta Float64'; order='(event_time, user_id)' ;;
    esac
    query "$host" "CREATE TABLE IF NOT EXISTS demo.$table ($columns) ENGINE = $engine ORDER BY $order TTL event_time + INTERVAL 1 DAY DELETE"
  done
  query "$host" 'CREATE TABLE IF NOT EXISTS demo.segments (user_id UInt32, segment String) ENGINE = MergeTree ORDER BY user_id'
  query "$host" 'CREATE TABLE IF NOT EXISTS demo.heartbeat (event_time DateTime, user_id UInt32, origin String) ENGINE = Null'
  query "$host" "INSERT INTO demo.segments SELECT number + 1, if(number % 2 = 0, 'new', 'returning') FROM numbers(100) WHERE NOT EXISTS (SELECT 1 FROM demo.segments LIMIT 1)"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.page_views_mv TO demo.activity AS SELECT p.event_time AS event_time, p.user_id AS user_id, 'view' AS kind, toFloat64(0) AS amount, s.segment AS segment FROM demo.page_views AS p LEFT JOIN demo.segments AS s ON p.user_id = s.user_id"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.purchases_mv TO demo.activity AS SELECT event_time, user_id, 'purchase' AS kind, amount, 'customer' AS segment FROM demo.purchases"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.refunds_mv TO demo.activity AS SELECT event_time, user_id, 'refund' AS kind, -amount AS amount, 'customer' AS segment FROM demo.refunds"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.heartbeat_mv TO demo.activity AS SELECT event_time, user_id, 'heartbeat' AS kind, toFloat64(0) AS amount, origin AS segment FROM demo.heartbeat"
  query "$host" 'CREATE MATERIALIZED VIEW IF NOT EXISTS demo.daily_activity_mv TO demo.daily_activity AS SELECT event_time, kind, count() AS events, sum(amount) AS amount FROM demo.activity GROUP BY event_time, kind'
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.purchase_ledger_mv TO demo.customer_ledger AS SELECT event_time, user_id, 'purchase' AS operation, round(amount, 2) AS delta FROM demo.purchases"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.refund_ledger_mv TO demo.customer_ledger AS SELECT event_time, user_id, 'refund' AS operation, -round(amount, 2) AS delta FROM demo.refunds"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.high_value_purchase_mv TO demo.value_alerts AS SELECT event_time, user_id, 'large purchase' AS category, amount FROM demo.purchases WHERE amount >= 150"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.refund_alert_mv TO demo.value_alerts AS SELECT event_time, user_id, 'refund' AS category, -amount AS amount FROM demo.activity WHERE kind = 'refund' AND amount <= -10"
  query "$host" 'CREATE MATERIALIZED VIEW IF NOT EXISTS demo.page_metrics_mv TO demo.page_metrics AS SELECT toStartOfMinute(event_time) AS event_time, page, count() AS views FROM demo.page_views GROUP BY event_time, page'
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.page_audit_mv TO demo.audit_events AS SELECT event_time, user_id, 'page' AS action, page AS detail FROM demo.page_views WHERE page != '/page/0'"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.heartbeat_audit_mv TO demo.audit_events AS SELECT event_time, user_id, 'heartbeat' AS action, origin AS detail FROM demo.heartbeat"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.purchase_audit_mv TO demo.audit_events AS SELECT event_time, user_id, 'purchase' AS action, segment AS detail FROM demo.activity WHERE kind = 'purchase'"
  query "$host" "CREATE MATERIALIZED VIEW IF NOT EXISTS demo.revenue_daily_mv TO demo.revenue_daily AS SELECT toStartOfHour(event_time) AS event_time, kind, sum(events) AS events, sum(amount) AS amount FROM demo.daily_activity WHERE kind IN ('purchase', 'refund') GROUP BY event_time, kind"
  query "$host" 'CREATE MATERIALIZED VIEW IF NOT EXISTS demo.audit_summary_mv TO demo.audit_summary AS SELECT toStartOfMinute(event_time) AS event_time, action, count() AS events FROM demo.audit_events GROUP BY event_time, action'
  query "$host" 'CREATE MATERIALIZED VIEW IF NOT EXISTS demo.ledger_hourly_mv TO demo.ledger_hourly AS SELECT toStartOfHour(event_time) AS event_time, user_id, count() AS operations, sum(delta) AS balance_delta FROM demo.customer_ledger GROUP BY event_time, user_id'
  printf 'Initialized %s\n' "$host"
done
