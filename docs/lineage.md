# Target columns and column lineage

Press `t` (or click **target**) from a selected MV or the overview to inspect its target table. The target screen shows a compact list of target columns and matching materialized views. Use ↑/↓ or click a column to show its expressions and source columns below; PgUp/PgDn or the mouse wheel scrolls the details. Click a view in the left panel to inspect a different target. Long types are shortened in the list; the selected column shows the full target type and, when known and different, the MV schema type alongside it. Different types do not necessarily imply an error. On narrow screens the list shows a count of matching views when their names do not fit. It considers visible MVs across databases, independently of the current filters. Columns without an identified MV output and their defaults are shown too. These are potential mappings inferred from definitions, not observed writes: direct inserts, inaccessible views and unsupported SQL can make the picture incomplete. Press `t` again to return. Views without an explicit `TO` have internal storage and do not have a separate target table to inspect.

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
transformations are shown as a chain such as `orders.price → cost: price * 2`. Ambiguous or unresolved
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
`prefer_column_name_to_alias`. The app reads this setting from the current
connection to resolve collisions when a query does not override settings, and
warns when the setting was used. If the setting is unavailable or the query
overrides settings, the collision remains unresolved. Table and column aliases
are traced to physical source names when possible, with intermediate expressions
shown as arrow-separated steps. When safe to do so, the app also uses the current
ClickHouse analyzer to resolve uncertain output columns.
This may differ from settings in effect when the view was created. Server analysis
is restricted to supported query scopes without function calls or scalar
subqueries, a matching current database, and local supported table engines;
otherwise static analysis remains in use. Query-tree output varies by ClickHouse
version, and unrecognized or incomplete trees are left unresolved.
Simple `SELECT *` inside a CTE or FROM subquery can be traced when a single
physical source and its column metadata are available. Wildcards over multiple
inputs, wildcard modifiers and settings-dependent columns remain uncertain.
