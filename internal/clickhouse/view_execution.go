package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ViewExecution represents a logged terminal event, not a complete execution history.
type ViewExecution struct {
	Time                              time.Time
	Status                            string
	DurationMS, ReadRows, WrittenRows uint64
	Code                              int32
	Message, QueryID                  string
}

func viewExecutionsQuery(hours int) string {
	hours = validStatusHours(hours)
	return fmt.Sprintf(`SELECT event_time, toString(status), view_duration_ms, read_rows, written_rows, exception_code, exception, initial_query_id
 FROM system.query_views_log
 WHERE event_date >= today() - %d AND event_time >= now() - INTERVAL %d HOUR AND event_time <= now()
 AND view_type = 'Materialized' AND status IN ('QueryFinish','ExceptionBeforeStart','ExceptionWhileProcessing') AND view_name = ?
 ORDER BY event_time_microseconds DESC LIMIT 50`, statusDays(hours), hours)
}

// LoadViewExecutions reads terminal events on the connected node only.
func LoadViewExecutions(ctx context.Context, dsn string, view View, hours int) ([]ViewExecution, error) {
	result, err := loadViewExecutions(ctx, dsn, view, hours)
	return result, RedactConnectionError(err, dsn)
}
func loadViewExecutions(ctx context.Context, dsn string, view View, hours int) ([]ViewExecution, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, viewExecutionsQuery(hours), view.Database+"."+view.Name)
	if err != nil {
		return nil, fmt.Errorf("read MV executions: %w", err)
	}
	defer rows.Close()
	var result []ViewExecution
	for rows.Next() {
		var e ViewExecution
		if err := rows.Scan(&e.Time, &e.Status, &e.DurationMS, &e.ReadRows, &e.WrittenRows, &e.Code, &e.Message, &e.QueryID); err != nil {
			return nil, fmt.Errorf("read MV execution: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read MV executions: %w", err)
	}
	return result, nil
}
