#!/usr/bin/env bash
set -u
trap 'exit 0' INT TERM
while true; do
  for host in standalone replica1 replica2; do
    for table in page_views purchases refunds heartbeat; do
      case $table in
        page_views) value="if(number = 0, '/page/1', concat('/page/', toString(rand() % 10)))" ;;
        purchases) value='if(number = 0, toFloat64(180), toFloat64(rand() % 200 + 1))' ;;
        refunds) value='toFloat64(rand() % 20 + 10)' ;;
        heartbeat) value="'background'" ;;
      esac
      inserts=$((1 + RANDOM % 3))
      for ((i = 0; i < inserts; i++)); do
        rows=$((1 + RANDOM % 20))
        clickhouse-client --host "$host" --query "INSERT INTO demo.$table SELECT now(), toUInt32(rand() % 100 + 1), $value FROM numbers($rows)" </dev/null || printf 'Traffic insert failed: %s %s\n' "$host" "$table" >&2
        delay_ms=$((250 + RANDOM % 1501))
        printf -v delay '%d.%03d' "$((delay_ms / 1000))" "$((delay_ms % 1000))"
        sleep "$delay"
      done
    done
  done
done
