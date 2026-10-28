# chinspector

Terminal UI for inspecting ClickHouse materialized views and their definitions.

## Run

Set a ClickHouse DSN in `CLICKHOUSE_URL`, then start the TUI:

```sh
export CLICKHOUSE_URL='clickhouse://user:password@localhost:9000/default'
go run .
```

The application reads `system.databases` and `system.tables` to list databases and materialized views and show each
view's inferred source-to-view-to-target diagram and `CREATE` definition. It only runs read-only queries. The
configured ClickHouse user should have permission to read the relevant system
metadata.

The initial database is the connection's current database. Press `b` to choose
another database, including one with no materialized views. The active database
is shown in the header; names in the list and diagram are unqualified. A view
can reference another database, so identical short names can represent different
tables. The SQL definition remains unchanged and shows the full references.

The diagram recognizes simple table references after `FROM` and `JOIN` and a
target after `TO` in the stored SQL. Complex expressions, table functions,
aliases and other SQL constructs may not be resolved completely. Sources are
query inputs, not necessarily insert triggers: for incremental materialized
views a joined table does not trigger the view when it receives inserts. A
view without `TO` uses internal storage. Check the SQL definition with `d`.

The diagram refreshes rates approximately every 5 seconds. Rates are local-node
averages over the preceding 60 seconds, derived from asynchronously flushed
system logs, so they can lag and undercount (including when query logging is
sampled). Table `parts` rates count rows in successful `NewPart` events from
`system.part_log`; they cover MergeTree tables and include all writers to the
table, not only the selected view. The target displays both this total rate and
the selected MV's write rate; either can be unavailable independently. MV `read/write` rates come from successful
executions in `system.query_views_log`; reads can include JOIN inputs. The
internal target of a view without `TO` does not have a separate table rate.

Press `o` to open a flow overview for every MV in the selected database. Views
that write to the same target are grouped and connect to one target box. Inputs
may be repeated for different views. Each input and MV shows its rates; the
target shows its total rate and the selected MV's write rate when selection is
in that group.
Select an MV with `↑` / `↓` and press `Enter` to open its diagram. The
overview supports horizontal and page scrolling. Metrics are queried in
batches and have the same local-node and log-delay limits described above.

Both system logs must be configured and readable by the user. View logging
also requires `log_query_views` for the queries that execute the view. An
empty window is shown as `no data` because disabled or delayed logging cannot
be distinguished from inactivity. Errors appear below the diagram. Metrics
cover the connected server only, not the entire cluster or replicas.

## Keys

- `↑` / `↓` or `j` / `k`: select a view
- `b`: choose a database; use `↑` / `↓` and `Enter` to switch, `Esc` to cancel
- `o`: switch between the overview and selected MV; `Enter` opens the selected MV
- `r`: reload view metadata
- `d`: switch between the diagram and SQL definition
- `Page Up` / `Page Down`: scroll the current detail panel
- `←` / `→` or `h` / `l`: scroll the diagram or SQL horizontally
- `q` or `Ctrl+C`: quit
