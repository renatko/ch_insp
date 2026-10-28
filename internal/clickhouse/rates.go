package clickhouse

import (
	"context"
	"time"
)

// Rates are local-node log observations over the last minute. A missing rate
// does not mean zero: logging may be disabled or delayed.
type Rates struct {
	Tables            map[string]float64
	TableDiagnostics  map[string]MetricDiagnostic
	MVDiagnostic      MetricDiagnostic
	TableHistory      map[string][5]uint64
	MVRead, MVWritten float64
	MVAvailable       bool
	MVHistory         *ViewHistory
	Errors            []string
	Status            *ViewStatus
	StatusError       string
	ChartWindowEnd    time.Time
	StatusHours       int
}

// LoadRates uses the same batched collection and diagnostics as overview.
func LoadRates(ctx context.Context, dsn string, view View, period int, hours int) Rates {
	return ratesForView(LoadOverviewRates(ctx, dsn, []View{view}, period, hours), view)
}

func ratesForView(all OverviewRates, view View) Rates {
	key := view.Database + "." + view.Name
	result := Rates{Tables: all.Tables, TableDiagnostics: all.TableDiagnostics, TableHistory: all.TableHistory, Errors: all.Errors, StatusError: all.StatusError, ChartWindowEnd: all.ChartWindowEnd, StatusHours: all.StatusHours}
	if diagnostic, ok := all.ViewDiagnostics[key]; ok {
		result.MVDiagnostic = diagnostic
	} else {
		result.MVDiagnostic = MetricDiagnostic{State: MetricUnknown}
	}
	if rate, ok := all.Views[key]; ok {
		result.MVAvailable = true
		result.MVRead = rate.Read
		result.MVWritten = rate.Written
	}
	if history, ok := all.History[key]; ok {
		result.MVHistory = &history
	}
	if status, ok := all.Statuses[key]; ok {
		result.Status = &status
	}
	return result
}
