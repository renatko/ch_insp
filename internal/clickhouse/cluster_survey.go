package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

const clusterSurveyLimit = 64

type NodeSurvey struct {
	Node                       Node
	Server, Version            string
	Error                      string
	PartLogError, ViewLogError string
}
type ClusterSurvey struct {
	Cluster   string
	Nodes     []NodeSurvey
	Total     int
	Truncated bool
}
type nodeProbe func(context.Context, string, Node) NodeSurvey

// SurveyCluster connects only to explicitly selected cluster nodes, performs
// read-only metadata/log permission checks and never computes cluster totals.
func SurveyCluster(ctx context.Context, dsn string, nodes []Node, cluster string) ClusterSurvey {
	return surveyCluster(ctx, dsn, nodes, cluster, probeNode)
}
func surveyCluster(ctx context.Context, dsn string, nodes []Node, cluster string, probe nodeProbe) ClusterSurvey {
	var selected []Node
	for _, n := range nodes {
		if n.Cluster == cluster {
			selected = append(selected, n)
		}
	}
	out := ClusterSurvey{Cluster: cluster, Total: len(selected)}
	if len(selected) > clusterSurveyLimit {
		selected = selected[:clusterSurveyLimit]
		out.Truncated = true
	}
	out.Nodes = make([]NodeSurvey, len(selected))
	for i, n := range selected {
		out.Nodes[i] = NodeSurvey{Node: n, Error: "not checked (cancelled or timed out)"}
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := 4
	if len(selected) < workers {
		workers = len(selected)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				perNode, cancel := context.WithTimeout(ctx, 5*time.Second)
				result := probe(perNode, dsn, selected[index])
				cancel()
				out.Nodes[index] = result
			}
		}()
	}
	for i := range selected {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return out
		}
	}
	close(jobs)
	wg.Wait()
	return out
}
func probeNode(ctx context.Context, dsn string, node Node) NodeSurvey {
	result := NodeSurvey{Node: node}
	nodeDSN, err := NodeDSN(dsn, node)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	db, err := sql.Open("clickhouse", nodeDSN)
	if err != nil {
		result.Error = RedactConnectionError(err, nodeDSN).Error()
		return result
	}
	defer db.Close()
	if err = db.QueryRowContext(ctx, "SELECT hostName(), version()").Scan(&result.Server, &result.Version); err != nil {
		result.Error = RedactConnectionError(err, nodeDSN).Error()
		return result
	}
	check := func(table string) string {
		var value uint8
		err := db.QueryRowContext(ctx, "SELECT 1 FROM system."+table+" LIMIT 1").Scan(&value)
		if err == nil || errors.Is(err, sql.ErrNoRows) {
			return ""
		}
		return RedactConnectionError(err, nodeDSN).Error()
	}
	result.PartLogError = check("part_log")
	result.ViewLogError = check("query_views_log")
	return result
}

func (s NodeSurvey) Summary() string {
	if s.Error != "" {
		return "unavailable"
	}
	if s.PartLogError != "" || s.ViewLogError != "" {
		return "logs unavailable"
	}
	return fmt.Sprintf("%s · v%s · logs readable", s.Server, s.Version)
}
