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
	Database       string
	Name           string
	Engine         string
	Definition     string
	TargetDatabase string
	TargetTable    string
}

type Catalog struct {
	Databases       []string
	CurrentDatabase string
	Node            string
	ServerVersion   string
	Views           []View
	Relations       []SchemaRelation
}

// LoadCatalog performs read-only discovery of databases and materialized views.
func LoadCatalog(ctx context.Context, dsn string) (Catalog, error) {
	catalog, err := loadCatalog(ctx, dsn)
	return catalog, RedactConnectionError(err, dsn)
}
func loadCatalog(ctx context.Context, dsn string) (Catalog, error) {
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
	if err := db.QueryRowContext(ctx, "SELECT currentDatabase(), hostName(), version()").Scan(&catalog.CurrentDatabase, &catalog.Node, &catalog.ServerVersion); err != nil {
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

	var targetColumns uint64
	if err := db.QueryRowContext(ctx, `SELECT count() FROM system.columns WHERE database = 'system' AND table = 'tables' AND name IN ('target_database', 'target_table')`).Scan(&targetColumns); err != nil {
		return catalog, fmt.Errorf("discover materialized view target metadata: %w", err)
	}
	targetSelect := "'' AS target_database, '' AS target_table"
	if targetColumns == 2 {
		targetSelect = "target_database, target_table"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT database, name, engine_full, create_table_query, `+targetSelect+`
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
		if err := rows.Scan(&view.Database, &view.Name, &view.Engine, &view.Definition, &view.TargetDatabase, &view.TargetTable); err != nil {
			return catalog, fmt.Errorf("read materialized view metadata: %w", err)
		}
		catalog.Views = append(catalog.Views, view)
	}
	if err := rows.Err(); err != nil {
		return catalog, fmt.Errorf("read materialized view metadata: %w", err)
	}
	if err := rows.Close(); err != nil {
		return catalog, fmt.Errorf("read materialized view metadata: %w", err)
	}
	relationRows, err := db.QueryContext(ctx, `SELECT database, name, engine, engine_full, create_table_query FROM system.tables WHERE engine IN ('Distributed', 'Dictionary') AND database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')`)
	if err != nil {
		return catalog, fmt.Errorf("discover schema relations: %w", err)
	}
	defer relationRows.Close()
	for relationRows.Next() {
		var database, name, engine, engineFull, definition string
		if err := relationRows.Scan(&database, &name, &engine, &engineFull, &definition); err != nil {
			return catalog, fmt.Errorf("read schema relations: %w", err)
		}
		catalog.Relations = append(catalog.Relations, ParseSchemaRelation(database, name, engine, engineFull, definition))
	}
	if err := relationRows.Err(); err != nil {
		return catalog, fmt.Errorf("read schema relations: %w", err)
	}
	return catalog, nil
}
