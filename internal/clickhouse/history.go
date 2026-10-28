package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ViewHistory contains observed row counts in five consecutive 60-second
// windows, ordered oldest to newest. Empty buckets mean no logged events.
type ViewHistory struct{ Read, Written [5]uint64 }

func loadViewHistories(ctx context.Context, db *sql.DB, names []string) (map[string]ViewHistory, error) {
	result := make(map[string]ViewHistory)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		query := viewHistoryQuery(marks)
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var name string
			var history ViewHistory
			values := []any{&name}
			for i := 0; i < 5; i++ {
				values = append(values, &history.Read[i], &history.Written[i])
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
	return result, nil
}

// loadTableHistories counts rows in successfully created MergeTree parts.
func loadTableHistories(ctx context.Context, db *sql.DB, names []string) (map[string][5]uint64, error) {
	result := make(map[string][5]uint64)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		query := tableHistoryQuery(marks)
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var name string
			var history [5]uint64
			values := []any{&name}
			for i := range history {
				values = append(values, &history[i])
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
	return result, nil
}

func viewHistoryQuery(marks string) string {
	fields := []string{"view_name"}
	for i := 5; i >= 1; i-- {
		condition := fmt.Sprintf("event_time > now() - INTERVAL %d SECOND AND event_time <= now() - INTERVAL %d SECOND", i*60, (i-1)*60)
		fields = append(fields, "sumIf(read_rows, "+condition+")", "sumIf(written_rows, "+condition+")")
	}
	return `SELECT ` + strings.Join(fields, ", ") + ` FROM system.query_views_log
 WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 300 SECOND AND event_time <= now()
 AND view_type = 'Materialized' AND status = 'QueryFinish' AND view_name IN (` + marks + `) GROUP BY view_name`
}

func tableHistoryQuery(marks string) string {
	fields := []string{"concat(database, '.', table)"}
	for i := 5; i >= 1; i-- {
		fields = append(fields, fmt.Sprintf("sumIf(rows, event_time > now() - INTERVAL %d SECOND AND event_time <= now() - INTERVAL %d SECOND)", i*60, (i-1)*60))
	}
	return `SELECT ` + strings.Join(fields, ", ") + ` FROM system.part_log
 WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 300 SECOND AND event_time <= now()
 AND event_type = 'NewPart' AND error = 0 AND concat(database, '.', table) IN (` + marks + `) GROUP BY database, table`
}
