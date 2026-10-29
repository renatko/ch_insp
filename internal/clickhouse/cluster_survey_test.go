package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClusterSurveyReportsPartialResultsAndLimit(t *testing.T) {
	nodes := make([]Node, 66)
	for i := range nodes {
		nodes[i] = Node{Cluster: "chosen", Host: "node", Port: 9000}
	}
	nodes = append(nodes, Node{Cluster: "other", Host: "other", Port: 9000})
	var calls atomic.Int32
	survey := surveyCluster(context.Background(), "dsn", nodes, "chosen", func(_ context.Context, _ string, n Node) NodeSurvey {
		i := calls.Add(1)
		if i == 2 {
			return NodeSurvey{Node: n, Error: "denied"}
		}
		return NodeSurvey{Node: n, Server: "node", Version: "1", ViewLogError: "permission denied"}
	})
	if survey.Total != 66 || !survey.Truncated || len(survey.Nodes) != 64 || calls.Load() != 64 {
		t.Fatalf("wrong scope: %#v", survey)
	}
	failures := 0
	for _, n := range survey.Nodes {
		if n.Error != "" {
			failures++
		}
	}
	if failures != 1 || !strings.Contains(survey.Nodes[0].Summary(), "logs unavailable") && survey.Nodes[0].Error == "" {
		t.Fatalf("wrong partial results: %#v", survey.Nodes[:2])
	}
}
func TestClusterSurveyCancelledBeforeProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	survey := surveyCluster(ctx, "dsn", []Node{{Cluster: "c", Host: "n"}}, "c", func(context.Context, string, Node) NodeSurvey {
		calls.Add(1)
		return NodeSurvey{Error: errors.New("unexpected").Error()}
	})
	if calls.Load() != 0 || len(survey.Nodes) != 1 || !strings.Contains(survey.Nodes[0].Error, "not checked") {
		t.Fatal("cancelled survey ran a probe")
	}
}

func TestClusterSurveyCancellationStopsQueuedProbes(t *testing.T) {
	nodes := make([]Node, 12)
	for i := range nodes {
		nodes[i] = Node{Cluster: "c", Host: "node", Replica: uint32(i + 1)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 12)
	done := make(chan ClusterSurvey, 1)
	go func() {
		done <- surveyCluster(ctx, "dsn", nodes, "c", func(ctx context.Context, _ string, n Node) NodeSurvey {
			started <- struct{}{}
			<-ctx.Done()
			return NodeSurvey{Node: n, Error: ctx.Err().Error()}
		})
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("four workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("more than four probes ran concurrently")
	default:
	}
	cancel()
	select {
	case result := <-done:
		if len(result.Nodes) != len(nodes) || result.Total != len(nodes) {
			t.Fatalf("lost configured nodes after cancellation: %#v", result)
		}
		for _, node := range result.Nodes {
			if node.Error == "" {
				t.Fatalf("cancelled or unchecked node has no error: %#v", node)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("survey did not return after cancellation")
	}
	select {
	case <-started:
		t.Fatal("a queued probe started after cancellation")
	default:
	}
}

// This opt-in integration check only reads system tables. It requires a
// disposable cluster explicitly selected by the operator; ordinary tests do
// not access external servers.
func TestClusterSurveyIntegration(t *testing.T) {
	dsn := os.Getenv("CHINSPECTOR_TEST_CLUSTER_URL")
	cluster := os.Getenv("CHINSPECTOR_TEST_CLUSTER_NAME")
	if dsn == "" || cluster == "" {
		t.Skip("set CHINSPECTOR_TEST_CLUSTER_URL and CHINSPECTOR_TEST_CLUSTER_NAME for the read-only cluster check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	nodes, err := LoadNodes(ctx, dsn)
	if err != nil {
		t.Fatalf("discover nodes: %v", RedactConnectionError(err, dsn))
	}
	survey := SurveyCluster(ctx, dsn, nodes, cluster)
	if survey.Total < 2 {
		t.Fatalf("test cluster %q has %d configured nodes; at least two are required", cluster, survey.Total)
	}
	if survey.Truncated || len(survey.Nodes) != survey.Total {
		t.Fatalf("incomplete survey: checked %d of %d", len(survey.Nodes), survey.Total)
	}
	for _, n := range survey.Nodes {
		if n.Error != "" {
			t.Errorf("node %s:%d: %s", n.Node.Host, n.Node.Port, n.Error)
			continue
		}
		if n.Version == "" || n.Server == "" {
			t.Errorf("node %s:%d did not identify itself", n.Node.Host, n.Node.Port)
		}
		if n.PartLogError != "" {
			t.Errorf("node %s:%d part_log: %s", n.Node.Host, n.Node.Port, n.PartLogError)
		}
		if n.ViewLogError != "" {
			t.Errorf("node %s:%d query_views_log: %s", n.Node.Host, n.Node.Port, n.ViewLogError)
		}
	}
}

// The disposable fixture provides the expected values and runs this test from
// inside its Docker network, where the advertised hostnames are reachable.
func TestClusterMetricsFixtureIntegration(t *testing.T) {
	dsn := os.Getenv("CHINSPECTOR_FIXTURE_URL")
	if dsn == "" {
		t.Skip("requires the disposable replica fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodes, err := LoadNodes(ctx, dsn)
	if err != nil {
		t.Fatal(RedactConnectionError(err, dsn))
	}
	survey := SurveyClusterMetrics(ctx, dsn, nodes, "chinspector_test", "flow_test")
	if survey.Total != 2 || len(survey.Nodes) != 2 || survey.Truncated {
		t.Fatalf("unexpected coverage: %#v", survey)
	}
	var parts, reads, writes uint64
	available := 0
	for _, n := range survey.Nodes {
		if n.Error != "" {
			continue
		}
		if n.PartLogError != "" || n.ViewLogError != "" {
			t.Fatalf("log error: %#v", n)
		}
		available++
		parts += n.PartRows
		reads += n.ViewReadRows
		writes += n.ViewWrittenRows
	}
	if os.Getenv("CHINSPECTOR_FIXTURE_PARTIAL") == "1" {
		if available != 1 {
			t.Fatalf("expected one available node, got %d", available)
		}
		return
	}
	if available != 2 || parts < 12 || reads != 6 || writes != 6 {
		t.Fatalf("unexpected observed events: nodes=%d parts=%d read=%d write=%d", available, parts, reads, writes)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		t.Fatal(RedactConnectionError(err, dsn))
	}
	defer db.Close()
	statuses, err := loadViewStatuses(ctx, db, []string{"flow_test.mv", "flow_test.bad_mv", "flow_test.missing"}, dsn, 24)
	if err != nil {
		t.Fatal(RedactConnectionError(err, dsn))
	}
	if statuses["flow_test.mv"].Successes == 0 || statuses["flow_test.bad_mv"].Errors != 1 || statuses["flow_test.bad_mv"].Successes != 0 {
		t.Fatalf("incorrect local MV statuses: %#v", statuses)
	}
	if _, ok := statuses["flow_test.missing"]; ok {
		t.Fatal("missing events reported as an observed status")
	}
	failures, err := LoadViewFailures(ctx, dsn, View{Database: "flow_test", Name: "bad_mv"}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].Status != "ExceptionWhileProcessing" || failures[0].Code == 0 || failures[0].Message == "" || failures[0].QueryID != "chinspector_fixture_failure" {
		t.Fatalf("incorrect MV failure detail: %#v", failures)
	}
}
