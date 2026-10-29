package clickhouse

import (
	"context"
	"database/sql"
	"strings"
)

type MetricState string

const (
	MetricUnknown             MetricState = "unknown"
	MetricAvailable           MetricState = "available"
	MetricNoEvents            MetricState = "no logged events"
	MetricUnsupported         MetricState = "unsupported engine"
	MetricMetadataUnavailable MetricState = "metadata unavailable"
	MetricLogError            MetricState = "log query failed"
	MetricOffline             MetricState = "offline"
)

type MetricDiagnostic struct {
	State  MetricState
	Detail string
}

// tableMetricDiagnostics checks only metadata; no events does not establish
// whether log collection was enabled for the writers.
func tableMetricDiagnostics(ctx context.Context, db *sql.DB, names []string) (map[string]MetricDiagnostic, error) {
	result := map[string]MetricDiagnostic{}
	for _, n := range names {
		result[n] = MetricDiagnostic{State: MetricMetadataUnavailable}
	}
	for start := 0; start < len(names); start += 100 {
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		args, marks := queryArgs(names[start:end])
		rows, err := db.QueryContext(ctx, `SELECT database, name, engine FROM system.tables WHERE concat(database, '.', name) IN (`+marks+`)`, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var database, name, engine string
			if err = rows.Scan(&database, &name, &engine); err != nil {
				break
			}
			state := MetricNoEvents
			if !strings.Contains(engine, "MergeTree") {
				state = MetricUnsupported
			}
			result[database+"."+name] = MetricDiagnostic{State: state, Detail: engine}
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
