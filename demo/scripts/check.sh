#!/usr/bin/env bash
# Read-only verification of the running demo.
set -euo pipefail
demo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
compose=(docker compose --file "$demo_dir/compose.yaml")
query() { "${compose[@]}" exec -T "$1" clickhouse-client --query "$2" </dev/null; }
for node in standalone replica1 replica2; do
  printf '%s: ' "$node"
  query "$node" "SELECT version(), (SELECT count() FROM system.tables WHERE database = 'demo' AND engine = 'MaterializedView'), (SELECT count() FROM demo.activity), (SELECT count() FROM demo.daily_activity) FORMAT TSV"
  for attempt in $(seq 1 60); do
    ok=$(query "$node" "SELECT uniqExact(view_name) >= 16 FROM system.query_views_log WHERE view_name LIKE 'demo.%' AND status = 'QueryFinish'" 2>/dev/null || true)
    if [[ $ok == 1 ]]; then break; fi
    if [[ $attempt == 60 ]]; then printf 'Not all MV events observed on %s\n' "$node" >&2; exit 1; fi
    sleep 1
  done
  [[ $(query "$node" "SELECT count() = 16 FROM system.tables WHERE database = 'demo' AND engine = 'MaterializedView'") == 1 ]]
  [[ $(query "$node" 'SELECT count() > 0 FROM demo.activity') == 1 ]]
  [[ $(query "$node" 'SELECT count() > 0 FROM demo.daily_activity') == 1 ]]
  [[ $(query "$node" 'SELECT count() = 0 FROM demo.heartbeat') == 1 ]]
  [[ $(query "$node" "SELECT count() > 0 FROM demo.activity WHERE kind = 'heartbeat'") == 1 ]]
  for table in customer_ledger value_alerts page_metrics audit_events revenue_daily audit_summary ledger_hourly; do
    [[ $(query "$node" "SELECT count() > 0 FROM demo.$table") == 1 ]]
  done
done
[[ $(query replica1 "SELECT count() = 2 FROM system.clusters WHERE cluster = 'demo_cluster'") == 1 ]]
printf 'Demo checks passed.\n'
