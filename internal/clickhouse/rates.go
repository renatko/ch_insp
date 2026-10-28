package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Rates are local-node log observations over the last minute. A missing rate
// does not mean zero: logging may be disabled or delayed.
type Rates struct {
	Tables            map[string]float64
	MVRead, MVWritten float64
	MVAvailable       bool
	Errors            []string
}

func LoadRates(ctx context.Context, dsn string, view View) Rates {
	rates := Rates{Tables: make(map[string]float64)}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		rates.Errors = append(rates.Errors, "open metrics connection: "+err.Error())
		return rates
	}
	defer db.Close()
	flow := InferFlow(view)
	tables := append([]string{}, flow.Sources...)
	if flow.Target != "internal storage (no TO)" && flow.Target != "unknown target" {
		tables = append(tables, flow.Target)
	}
	seen := make(map[string]bool)
	for _, name := range tables {
		if seen[name] {
			continue
		}
		seen[name] = true
		parts := strings.Split(name, ".")
		if len(parts) != 2 {
			continue
		}
		var engine string
		err = db.QueryRowContext(ctx, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", parts[0], parts[1]).Scan(&engine)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			rates.Errors = append(rates.Errors, fmt.Sprintf("%s metadata: %v", parts[1], err))
			continue
		}
		if !strings.Contains(engine, "MergeTree") {
			continue
		}
		var rows, events uint64
		err = db.QueryRowContext(ctx, `SELECT sum(rows), count() FROM system.part_log
			WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 60 SECOND
			AND event_type = 'NewPart' AND error = 0 AND database = ? AND table = ?`, parts[0], parts[1]).Scan(&rows, &events)
		if err != nil {
			rates.Errors = append(rates.Errors, fmt.Sprintf("%s part log: %v", parts[1], err))
			continue
		}
		if events > 0 {
			rates.Tables[name] = float64(rows) / 60
		}
	}
	var read, written, events uint64
	err = db.QueryRowContext(ctx, `SELECT sum(read_rows), sum(written_rows), count() FROM system.query_views_log
		WHERE event_date >= today() - 1 AND event_time > now() - INTERVAL 60 SECOND
		AND view_type = 'Materialized' AND status = 'QueryFinish' AND view_name = ?`, view.Database+"."+view.Name).Scan(&read, &written, &events)
	if err != nil {
		rates.Errors = append(rates.Errors, "view log: "+err.Error())
	} else if events > 0 {
		rates.MVAvailable = true
		rates.MVRead = float64(read) / 60
		rates.MVWritten = float64(written) / 60
	}
	return rates
}
