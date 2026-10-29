#!/usr/bin/env bash
set -u
while true; do
  for host in standalone replica1 replica2; do
    for table in page_views purchases refunds heartbeat; do
      case $table in
        page_views) value="if(number = 0, '/page/1', concat('/page/', toString(rand() % 10)))" ;;
        purchases) value='if(number = 0, toFloat64(180), toFloat64(rand() % 200 + 1))' ;;
        refunds) value='toFloat64(rand() % 20 + 10)' ;;
        heartbeat) value="'background'" ;;
      esac
      clickhouse-client --host "$host" --query "INSERT INTO demo.$table SELECT now(), toUInt32(rand() % 100 + 1), $value FROM numbers(5)" </dev/null || printf 'Traffic insert failed: %s %s\n' "$host" "$table" >&2
    done
  done
  sleep 5
done
