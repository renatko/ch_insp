package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ViewHistory contains observed row counts in five completed calendar
// intervals, ordered oldest to newest. Empty buckets mean no logged events.
type ViewHistory struct{ Read, Written, Events [5]uint64 }
type TableHistory struct{ Rows, Events [5]uint64 }

func loadViewHistories(ctx context.Context, db *sql.DB, names []string, seconds int, dsn string, windowEnd time.Time) (map[string]ViewHistory, error) {
	key := metricWindowKey(dsn, names, seconds, windowEnd)
	if value, ok := viewHistoryCache.get(key); ok {
		return value, nil
	}
	result := make(map[string]ViewHistory)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		query := viewHistoryQueryAt(marks, seconds, windowEnd)
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var name string
			var history ViewHistory
			values := []any{&name}
			for i := 0; i < 5; i++ {
				values = append(values, &history.Read[i], &history.Written[i], &history.Events[i])
			}
			if err = rows.Scan(values...); err != nil {
				break
			}
			result[name] = history
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	viewHistoryCache.put(key, result, 20*time.Second)
	return result, nil
}

// loadTableHistories counts rows in successfully created MergeTree parts.
func loadTableHistories(ctx context.Context, db *sql.DB, names []string, seconds int, dsn string, windowEnd time.Time) (map[string]TableHistory, error) {
	key := metricWindowKey(dsn, names, seconds, windowEnd)
	if value, ok := tableHistoryCache.get(key); ok {
		return value, nil
	}
	result := make(map[string]TableHistory)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		query := tableHistoryQueryAt(marks, seconds, windowEnd)
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var name string
			var history TableHistory
			values := []any{&name}
			for i := range history.Rows {
				values = append(values, &history.Rows[i], &history.Events[i])
			}
			if err = rows.Scan(values...); err != nil {
				break
			}
			result[name] = history
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	tableHistoryCache.put(key, result, 20*time.Second)
	return result, nil
}

func historyPeriod(seconds int) (int, string) {
	switch seconds {
	case 300:
		return 300, "toStartOfInterval(now(), INTERVAL 5 MINUTE)"
	case 3600:
		return 3600, "toStartOfHour(now())"
	default:
		return 60, "toStartOfMinute(now())"
	}
}
func viewHistoryQueryAt(marks string, period int, end time.Time) string {
	seconds, anchor := historyPeriod(period)
	if !end.IsZero() {
		anchor = fmt.Sprintf("toDateTime(%d)", end.Unix())
	}
	fields := []string{"view_name"}
	for i := 5; i >= 1; i-- {
		condition := fmt.Sprintf("event_time >= %s - INTERVAL %d SECOND AND event_time < %s - INTERVAL %d SECOND", anchor, i*seconds, anchor, (i-1)*seconds)
		fields = append(fields, "sumIf(read_rows, "+condition+")", "sumIf(written_rows, "+condition+")", "countIf("+condition+")")
	}
	return `SELECT ` + strings.Join(fields, ", ") + ` FROM system.query_views_log
 WHERE event_date >= today() - 1 AND event_time >= ` + anchor + fmt.Sprintf(" - INTERVAL %d SECOND", 5*seconds) + ` AND event_time < ` + anchor + `
 AND view_type = 'Materialized' AND status = 'QueryFinish' AND view_name IN (` + marks + `) GROUP BY view_name`
}

func tableHistoryQueryAt(marks string, period int, end time.Time) string {
	seconds, anchor := historyPeriod(period)
	if !end.IsZero() {
		anchor = fmt.Sprintf("toDateTime(%d)", end.Unix())
	}
	fields := []string{"concat(database, '.', table)"}
	for i := 5; i >= 1; i-- {
		condition := fmt.Sprintf("event_time >= %s - INTERVAL %d SECOND AND event_time < %s - INTERVAL %d SECOND", anchor, i*seconds, anchor, (i-1)*seconds)
		fields = append(fields, "sumIf(rows, "+condition+")", "countIf("+condition+")")
	}
	return `SELECT ` + strings.Join(fields, ", ") + ` FROM system.part_log
 WHERE event_date >= today() - 1 AND event_time >= ` + anchor + fmt.Sprintf(" - INTERVAL %d SECOND", 5*seconds) + ` AND event_time < ` + anchor + `
 AND event_type = 'NewPart' AND error = 0 AND concat(database, '.', table) IN (` + marks + `) GROUP BY database, table`
}
