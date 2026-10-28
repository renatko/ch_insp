package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Node struct {
	Cluster, Host  string
	Port           uint16
	Shard, Replica uint32
}

func LoadNodes(ctx context.Context, dsn string) ([]Node, error) {
	nodes, err := loadNodes(ctx, dsn)
	return nodes, RedactConnectionError(err, dsn)
}
func loadNodes(ctx context.Context, dsn string) ([]Node, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT cluster, host_name, port, shard_num, replica_num FROM system.clusters ORDER BY cluster, shard_num, replica_num`)
	if err != nil {
		return nil, fmt.Errorf("read cluster nodes: %w", err)
	}
	defer rows.Close()
	var nodes []Node
	for rows.Next() {
		var n Node
		if err = rows.Scan(&n.Cluster, &n.Host, &n.Port, &n.Shard, &n.Replica); err != nil {
			return nil, fmt.Errorf("read cluster node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read cluster nodes: %w", err)
	}
	return nodes, nil
}

// NodeDSN keeps authentication and connection settings private and unchanged.
// The caller validates the new connection before replacing the current one.
func NodeDSN(dsn string, node Node) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid connection URL")
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return "", fmt.Errorf("node switching requires a native TCP connection; system.clusters advertises native ports")
	}
	if node.Host == "" || node.Port == 0 {
		return "", fmt.Errorf("node address is incomplete")
	}
	u.Host = net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	return u.String(), nil
}

// Redact the complete URL and password while retaining the underlying error.
func RedactConnectionError(err error, dsn string) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), dsn, "[connection]")
	if u, parseErr := url.Parse(dsn); parseErr == nil && u.User != nil {
		if password, ok := u.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
			message = strings.ReplaceAll(message, url.QueryEscape(password), "[redacted]")
		}
	}
	if message == err.Error() {
		return err
	}
	return fmt.Errorf("%s", message)
}
