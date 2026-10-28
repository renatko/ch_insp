package clickhouse

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
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
