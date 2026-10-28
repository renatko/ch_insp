package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

// View contains the metadata needed to inspect a ClickHouse materialized view.
type View struct {
	Database   string
	Name       string
	Engine     string
	Definition string
}

type Catalog struct {
	Databases       []string
	CurrentDatabase string
	Views           []View
}

// LoadCatalog performs read-only discovery of databases and materialized views.
func LoadCatalog(ctx context.Context, dsn string) (Catalog, error) {
	var catalog Catalog
	if strings.TrimSpace(dsn) == "" {
		return catalog, fmt.Errorf("CLICKHOUSE_URL is not set")
	}

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return catalog, fmt.Errorf("open ClickHouse connection: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return catalog, fmt.Errorf("connect to ClickHouse: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT currentDatabase()").Scan(&catalog.CurrentDatabase); err != nil {
		return catalog, fmt.Errorf("read current database: %w", err)
	}
	dbRows, err := db.QueryContext(ctx, `SELECT name FROM system.databases WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') ORDER BY name`)
	if err != nil {
		return catalog, fmt.Errorf("discover databases: %w", err)
	}
	for dbRows.Next() {
		var name string
		if err := dbRows.Scan(&name); err != nil {
			dbRows.Close()
			return catalog, fmt.Errorf("read databases: %w", err)
		}
		catalog.Databases = append(catalog.Databases, name)
	}
	if err := dbRows.Err(); err != nil {
		dbRows.Close()
		return catalog, fmt.Errorf("read databases: %w", err)
	}
	if err := dbRows.Close(); err != nil {
		return catalog, fmt.Errorf("read databases: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT database, name, engine_full, create_table_query
		FROM system.tables
		WHERE engine = 'MaterializedView'
		  AND database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		ORDER BY database, name
	`)
	if err != nil {
		return catalog, fmt.Errorf("discover materialized views: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var view View
		if err := rows.Scan(&view.Database, &view.Name, &view.Engine, &view.Definition); err != nil {
			return catalog, fmt.Errorf("read materialized view metadata: %w", err)
		}
		catalog.Views = append(catalog.Views, view)
	}
	if err := rows.Err(); err != nil {
		return catalog, fmt.Errorf("read materialized view metadata: %w", err)
	}
	return catalog, nil
}
