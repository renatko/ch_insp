package clickhouse

import (
	"context"
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
