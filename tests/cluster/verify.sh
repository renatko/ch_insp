#!/usr/bin/env bash
# Creates and removes only a uniquely named disposable Docker Compose project.
# It writes synthetic data inside that project; chinspector itself remains read-only.
set -euo pipefail

fixture_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
project="chinspector-fixture-$(date +%s)-$$"
compose=(docker compose --project-name "$project" --file "$fixture_dir/compose.yaml")
cleanup() {
  local status=$?
  if (( status != 0 )); then
    "${compose[@]}" logs --tail=60 >&2 || true
  fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

query() {
  local service=$1 sql=$2
  shift 2
  "${compose[@]}" exec -T "$service" clickhouse-client --query "$sql" "$@"
}
wait_for() {
  local description=$1 service=$2 sql=$3
  local value
  for _ in $(seq 1 60); do
    value=$(query "$service" "$sql" 2>/dev/null || true)
    if [[ "$value" == "1" ]]; then return 0; fi
    sleep 1
  done
  printf 'Timed out waiting for %s\n' "$description" >&2
  "${compose[@]}" ps >&2 || true
  return 1
}

printf 'Starting disposable project %s\n' "$project"
"${compose[@]}" up --detach --wait --wait-timeout 150
wait_for 'Keeper availability' ch1 "SELECT count() >= 0 FROM system.zookeeper WHERE path = '/'"

for node in ch1 ch2; do
  query "$node" 'CREATE DATABASE flow_test'
  query "$node" "CREATE TABLE flow_test.src (value UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/chinspector_fixture/src', '{replica}') ORDER BY value"
  query "$node" "CREATE TABLE flow_test.dst (value UInt64) ENGINE = ReplicatedMergeTree('/clickhouse/chinspector_fixture/dst', '{replica}') ORDER BY value"
  query "$node" 'CREATE MATERIALIZED VIEW flow_test.mv TO flow_test.dst AS SELECT value FROM flow_test.src'
done
query ch1 "CREATE TABLE flow_test.src_distributed AS flow_test.src ENGINE = Distributed('chinspector_test', 'flow_test', 'src', rand())"
query ch1 'CREATE TABLE flow_test.bad_src (value UInt64) ENGINE = MergeTree ORDER BY value'
query ch1 'CREATE TABLE flow_test.bad_dst (value UInt64) ENGINE = MergeTree ORDER BY value'
query ch1 'CREATE MATERIALIZED VIEW flow_test.bad_mv TO flow_test.bad_dst AS SELECT value + throwIf(value = 999) AS value FROM flow_test.bad_src'

query ch1 'INSERT INTO flow_test.src VALUES (1), (2), (3)' --query_id=chinspector_fixture_direct_1
query ch2 'INSERT INTO flow_test.src VALUES (4), (5)' --query_id=chinspector_fixture_direct_2
query ch1 'INSERT INTO flow_test.src_distributed SETTINGS distributed_foreground_insert = 1 VALUES (6)' --query_id=chinspector_fixture_distributed
if query ch1 'INSERT INTO flow_test.bad_src VALUES (999)' --query_id=chinspector_fixture_failure >/dev/null 2>&1; then
  printf 'Expected the deliberately failing test MV to reject the insert\n' >&2
  exit 1
fi

for node in ch1 ch2; do
  wait_for "replicated source on $node" "$node" 'SELECT count() = 6 FROM flow_test.src'
  wait_for "replicated target on $node" "$node" 'SELECT count() = 6 FROM flow_test.dst'
done
wait_for 'MV success events' ch1 "SELECT count() > 0 FROM clusterAllReplicas('chinspector_test', system.query_views_log) WHERE view_name = 'flow_test.mv' AND status = 'QueryFinish'"
wait_for 'MV failure event' ch1 "SELECT count() > 0 FROM system.query_views_log WHERE view_name = 'flow_test.bad_mv' AND status IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing')"
wait_for 'replica part downloads' ch1 "SELECT count() > 0 FROM clusterAllReplicas('chinspector_test', system.part_log) WHERE database = 'flow_test' AND table = 'src' AND event_type = 'DownloadPart' AND error = 0"

printf '\nVersions and cluster topology:\n'
query ch1 "SELECT hostName(), version() FROM clusterAllReplicas('chinspector_test', system.one) ORDER BY hostName()"
query ch1 "SELECT cluster, shard_num, replica_num, host_name, port FROM system.clusters WHERE cluster = 'chinspector_test' ORDER BY shard_num, replica_num"
printf '\nSuccessful and failed view events (per node):\n'
query ch1 "SELECT hostname, view_name, status, count() AS events, sum(read_rows) AS read_rows, sum(written_rows) AS written_rows FROM clusterAllReplicas('chinspector_test', system.query_views_log) WHERE view_name IN ('flow_test.mv', 'flow_test.bad_mv') GROUP BY hostname, view_name, status ORDER BY hostname, view_name, status"
printf '\nNewPart and DownloadPart rows (per node):\n'
query ch1 "SELECT hostname, table, event_type, count() AS events, sum(rows) AS rows FROM clusterAllReplicas('chinspector_test', system.part_log) WHERE database = 'flow_test' AND table IN ('src', 'dst') AND event_type IN ('NewPart', 'DownloadPart') AND error = 0 GROUP BY hostname, table, event_type ORDER BY hostname, table, event_type"
printf '\nInitial query IDs observed on more than one node (inspect before deduplicating):\n'
query ch1 "SELECT initial_query_id, view_name, groupUniqArray(hostname) AS hosts, count() AS events FROM clusterAllReplicas('chinspector_test', system.query_views_log) WHERE view_name IN ('flow_test.mv', 'flow_test.bad_mv') AND initial_query_id != '' GROUP BY initial_query_id, view_name HAVING length(hosts) > 1 ORDER BY initial_query_id"
printf '\nChecking partial availability after stopping the second replica:\n'
"${compose[@]}" stop ch2
wait_for 'one responding replica' ch1 "SELECT count() = 1 FROM clusterAllReplicas('chinspector_test', system.one) SETTINGS skip_unavailable_shards = 1, connect_timeout_with_failover_ms = 1000"
printf 'Only one of two configured replicas responded; these results are incomplete.\n'
printf '\nFixture checks passed. Results describe only these synthetic operations, not every ClickHouse topology.\n'
