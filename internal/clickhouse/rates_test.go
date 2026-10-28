package clickhouse

import "testing"

func TestRatesForViewKeepsOverviewObservations(t *testing.T) {
	view := View{Database: "db", Name: "mv"}
	data := OverviewRates{Tables: map[string]float64{"db.src": 2}, TableDiagnostics: map[string]MetricDiagnostic{"db.src": {State: MetricAvailable}}, ViewDiagnostics: map[string]MetricDiagnostic{"db.mv": {State: MetricLogError, Detail: "denied"}}, StatusError: "status denied"}
	got := ratesForView(data, view)
	if got.MVAvailable || got.MVDiagnostic.State != MetricLogError || got.MVDiagnostic.Detail != "denied" || got.TableDiagnostics["db.src"].State != MetricAvailable || got.StatusError != "status denied" {
		t.Fatalf("lost diagnostics: %#v", got)
	}
	data.Views = map[string]ViewRate{"db.mv": {Read: 4, Written: 5}}
	data.History = map[string]ViewHistory{"db.mv": {Read: [5]uint64{1, 2, 3, 4, 5}}}
	data.Statuses = map[string]ViewStatus{"db.mv": {Errors: 2}}
	got = ratesForView(data, view)
	if !got.MVAvailable || got.MVRead != 4 || got.MVWritten != 5 || got.MVHistory == nil || got.MVHistory.Read[4] != 5 || got.Status == nil || got.Status.Errors != 2 {
		t.Fatalf("lost observed values: %#v", got)
	}
}
