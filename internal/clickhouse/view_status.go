package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ViewStatus describes events observed in a bounded window on this node.
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

func loadViewStatuses(ctx context.Context, db *sql.DB, names []string, dsn string, hours int) (map[string]ViewStatus, error) {
	hours = validStatusHours(hours)
	key := metricCacheKey(dsn, names, hours)
	if value, ok := statusCache.get(key); ok {
		return value, nil
	}
	result := make(map[string]ViewStatus)
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		rows, err := db.QueryContext(ctx, viewStatusQuery(marks, hours), args...)
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
	statusCache.put(key, result, 30*time.Second)
	return result, nil
}

// LoadViewFailures returns recent logged failures and preserves the server error.
func LoadViewFailures(ctx context.Context, dsn string, view View, hours int) ([]ViewFailure, error) {
	failures, err := loadViewFailures(ctx, dsn, view, hours)
	return failures, RedactConnectionError(err, dsn)
}
func loadViewFailures(ctx context.Context, dsn string, view View, hours int) ([]ViewFailure, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	hours = validStatusHours(hours)
	query := fmt.Sprintf(`SELECT event_time, toString(status), exception_code, exception, initial_query_id FROM system.query_views_log
 WHERE event_date >= today() - %d AND event_time >= now() - INTERVAL %d HOUR AND event_time <= now()
 AND view_type = 'Materialized' AND status IN ('ExceptionBeforeStart','ExceptionWhileProcessing') AND view_name = ?
 ORDER BY event_time_microseconds DESC LIMIT 50`, statusDays(hours), hours)
	rows, err := db.QueryContext(ctx, query, view.Database+"."+view.Name)
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

func validStatusHours(hours int) int {
	switch hours {
	case 1, 24, 168:
		return hours
	default:
		return 24
	}
}
func statusDays(hours int) int { return (hours + 23) / 24 }
func viewStatusQuery(marks string, hours int) string {
	hours = validStatusHours(hours)
	return fmt.Sprintf(`SELECT view_name, maxIf(event_time, status = 'QueryFinish'), countIf(status = 'QueryFinish'), countIf(status IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing'))
 FROM system.query_views_log WHERE event_date >= today() - %d AND event_time >= now() - INTERVAL %d HOUR AND event_time <= now()
 AND view_type = 'Materialized' AND view_name IN (%s) GROUP BY view_name`, statusDays(hours), hours, marks)
}
