# chinspector

Terminal UI for inspecting ClickHouse materialized views and their definitions.

## Run

Set a ClickHouse DSN in `CLICKHOUSE_URL`, then start the TUI:

```sh
export CLICKHOUSE_URL='clickhouse://user:password@localhost:9000/default'
go run .
```

Alternatively, pass connection options on the command line:

```sh
go run . --host localhost --port 9000 --user default --database default
```

Available options are `--url`, `--host`, `--port`, `--user`, `--password`,
`--database` and `--secure` (TLS). Run `go run . --help` for usage.
`--url` overrides `CLICKHOUSE_URL`, and individual options override the
corresponding URL fields. With individual options and no URL, defaults are
`localhost:9000`, user `default` and database `default`; without any connection
settings the application asks for a URL. A password passed with `--password`
or `--url` can be visible in process arguments and shell history. For a
password, prefer setting `CLICKHOUSE_URL` in the environment.

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

In the detailed view, press `c` (or click **columns**) to inspect column flows.
For each SELECT output it shows the expression, referenced input columns,
available MV and target types, and whether the target has a matching column
name. The top-level JOIN, PREWHERE, WHERE, GROUP BY and HAVING clauses are
shown explicitly, formatted by the parser; the original SQL remains available
with `d`. Additional references from JOIN, WHERE, GROUP BY and similar clauses and
target columns not identified in the SELECT are listed separately. The feature
reads `system.columns` and parses the stored definition. For a restricted subset
of queries it also uses `EXPLAIN QUERY TREE`; it does not execute the view's SELECT. It needs permission to read the relevant column metadata.

Column lineage is a best-effort analysis, not an execution trace. It
follows supported SELECT and WITH aliases, CTEs, FROM subqueries, scalar
subqueries and lambda parameters back to source columns; intermediate
transformations are shown as `via` expressions. Ambiguous or unresolved
identifiers are marked explicitly. Correlated subqueries, UNION, ARRAY JOIN,
table functions, wildcard modifiers and other complex syntax can require manual
inspection; wildcard expansion needs unambiguous source and MV metadata. A
missing target match or type can also mean metadata is unavailable. Use `d`
to inspect the original SQL.

When a reference cannot be resolved, the column view distinguishes missing
`system.columns` metadata, multiple possible sources, alias/name collisions,
and unsupported wildcard or query constructs. For missing metadata it names
the affected table and preserves ClickHouse errors in the warnings. A collision
between an alias and a source column can depend on
`prefer_column_name_to_alias`. When safe to do so, the app uses the current
ClickHouse analyzer and connection settings to resolve uncertain output columns.
This may differ from settings in effect when the view was created. Server analysis
is restricted to supported query scopes without function calls or scalar
subqueries, a matching current database, and local supported table engines;
otherwise static analysis remains in use. Query-tree output varies by ClickHouse
version, and unrecognized or incomplete trees are left unresolved.
Simple `SELECT *` inside a CTE or FROM subquery can be traced when a single
physical source and its column metadata are available. Wildcards over multiple
inputs, wildcard modifiers and settings-dependent columns remain uncertain.

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
that write to the same target are grouped and connect to one target. Each MV
occupies two lines: the flow, then rates directly below its inputs and MV. The
target's total rate is shown directly below the target. Inputs may be repeated
for different views. Sources are bold, MV names italic, and targets regular;
the terminal and font determine whether these styles are visible. Each target
group has its own color, including its inputs and
views. Colors remain stable when filtering; with more than eight groups the
palette repeats, and refreshing after schema changes can reassign colors.
Select an MV with `↑` / `↓` and press `Enter` to open its diagram. The
selected flow's inputs, MV, and their statistics are highlighted. The
overview supports horizontal and page scrolling. Metrics are queried in
batches and have the same local-node and log-delay limits described above.

Press `f` to filter views by source and/or target tables. The lists are built
from all discovered views in the current database. Use Tab to switch lists,
arrows or the mouse wheel to navigate, and Space or a click to toggle tables.
Multiple choices within one list match any selected table; when both lists have
choices, a view must match both. Enter applies the selection; Ctrl+X clears both,
and Esc cancels. `(any table)` selects all entries in the current list; select
it again to clear that list. An empty selection applies no restriction for that
list. Selecting all entries matches only those inferred table names, so views
without a recognized table in that list may be excluded. Identical table names from
different databases include their database in the list. The filter applies to
the list and overview and remains active when switching databases. Filtering
uses the SQL-inferred relationships described above; complex SQL or internal
targets may therefore not be matched.

Both system logs must be configured and readable by the user. View logging
also requires `log_query_views` for the queries that execute the view. An
empty window is shown as `no data` because disabled or delayed logging cannot
be distinguished from inactivity. Errors appear below the diagram. Metrics
cover the connected server only, not the entire cluster or replicas.

## Keys

- `↑` / `↓` or `j` / `k`: select a view
- `b`: choose a database; use `↑` / `↓` and `Enter` to switch, `Esc` to cancel
- `f`: edit source and target table filters
- `o`: switch between the overview and selected MV; `Enter` opens the selected MV
- `r`: reload view metadata
- `d`: switch between the diagram and SQL definition
- `c`: switch between the diagram and column flow in the detailed view
- `Page Up` / `Page Down`: scroll the current detail panel
- `←` / `→` or `h` / `l`: scroll the diagram or SQL horizontally
- `q` or `Ctrl+C`: quit

Mouse support depends on the terminal. Click an MV in the list to select it,
or click a flow in the overview to select it and click it again to open it.
Click a database in the database selector to switch. The bracketed actions in
the footer are clickable. The wheel scrolls the area under the pointer: it
moves the selection over the MV list and scrolls the diagram or SQL elsewhere.
Use horizontal wheel scrolling, or Shift + wheel when supported, to scroll
horizontally. Hold Shift while selecting text if your terminal supports that
override for mouse capture.
