package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ViewStatus describes events observed in the last 24 hours on this node.
// A missing status is unknown, not evidence that the view has never run.
type ViewStatus struct {
	LastSuccess       time.Time
	Successes, Errors uint64
}
type ViewFailure struct {
	Time             time.Time
	Status           string
	Code             int32
	Message, QueryID string
}

func loadViewStatuses(ctx context.Context, db *sql.DB, names []string) (map[string]ViewStatus, error) {
	result := make(map[string]ViewStatus)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		rows, err := db.QueryContext(ctx, viewStatusQuery(marks), args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var name string
			var status ViewStatus
			if err = rows.Scan(&name, &status.LastSuccess, &status.Successes, &status.Errors); err != nil {
				break
			}
			result[name] = status
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

// LoadViewFailures returns recent logged failures and preserves the server error.
func LoadViewFailures(ctx context.Context, dsn string, view View) ([]ViewFailure, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT event_time, toString(status), exception_code, exception, initial_query_id FROM system.query_views_log
 WHERE event_date >= today() - 1 AND event_time >= now() - INTERVAL 24 HOUR AND event_time <= now()
 AND view_type = 'Materialized' AND status IN ('ExceptionBeforeStart','ExceptionWhileProcessing') AND view_name = ?
 ORDER BY event_time_microseconds DESC LIMIT 50`, view.Database+"."+view.Name)
	if err != nil {
		return nil, fmt.Errorf("read MV errors: %w", err)
	}
	defer rows.Close()
	var failures []ViewFailure
	for rows.Next() {
		var failure ViewFailure
		if err = rows.Scan(&failure.Time, &failure.Status, &failure.Code, &failure.Message, &failure.QueryID); err != nil {
			return nil, fmt.Errorf("read MV error: %w", err)
		}
		failures = append(failures, failure)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read MV errors: %w", err)
	}
	return failures, nil
}

func viewStatusQuery(marks string) string {
	return `SELECT view_name, maxIf(event_time, status = 'QueryFinish'), countIf(status = 'QueryFinish'), countIf(status IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing'))
 FROM system.query_views_log WHERE event_date >= today() - 1 AND event_time >= now() - INTERVAL 24 HOUR AND event_time <= now()
 AND view_type = 'Materialized' AND view_name IN (` + marks + `) GROUP BY view_name`
}
