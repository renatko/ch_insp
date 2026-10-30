# Metrics and node surveys

## Nodes and cluster survey

The header shows the connected node and ClickHouse server version. Press `n` to inspect entries from `system.clusters` and connect directly to an advertised native TCP node using the current credentials and TLS settings. The current connection remains active if validation fails. Addresses must be reachable from the client; HTTP connections cannot use this switch because the cluster advertises native ports. In the node selector, press `v` to survey the selected cluster: up to 64 nodes are checked with bounded concurrency and timeouts for connectivity, version and read access to both logs. The survey sums observed events for the active database over the preceding 60 seconds, and reports coverage separately for each log. Press `e` for a node's counts and errors. The sum is partial when any configured node or log is unavailable, omitted by the limit, or duplicated by address. This is a snapshot; press `v` again to refresh it.

Cluster counts are sums of physical events reported by nodes, not deduplicated logical inserts: `NewPart` includes source, target and other tables in the database, while MV read/write counts include successful materialized view executions. Replica downloads are excluded. Distributed queries, replicated tables, multiple views, duplicate addresses advertised under different names, server clock skew, and disabled, delayed or sampled logs can make these numbers differ from logical flow. A zero means no rows were observed in the available logs, not proof of inactivity. The diagram and its histories continue to show only the connected node; they are not mixed with the survey.

## Rates, history and errors

The diagram refreshes rates approximately every 5 seconds. Completed-interval histories are cached for up to 20 seconds and MV status for up to 30 seconds to limit repeated scans of system logs. Rates are local-node
averages over the preceding 60 seconds, derived from asynchronously flushed
system logs, so they can lag and undercount (including when query logging is
sampled). Metrics use blue for `read` and `inserted`, and amber for `write` and
target `total`. Table `inserted` rates count rows in successful `NewPart` events from
`system.part_log`; they cover MergeTree tables and include all writers to the
table, not only the selected view. The target displays both this total rate and
the selected MV's write rate; either can be unavailable independently. MV `read/write` rates come from successful
executions in `system.query_views_log`; reads can include JOIN inputs. The
internal target of a view without `TO` does not have a separate table rate.

Next to MV read/write and target total, a five-bar chart shows observed rows in
five completed calendar intervals, oldest on the left. Press `p` to switch between five 1-minute, 5-minute or 1-hour intervals; the current partial interval is excluded. The displayed time range is anchored to ClickHouse server time, and history cache entries do not cross interval boundaries. Press `s` to switch overview charts between individual scales and shared scales for read, write and target totals separately. The overview header shows the shared maxima in rows per interval. A dot means no event was observed in that interval; `0` means an event reported zero rows. Missing events can also result from disabled, sampled or delayed logging. The charts
use the same local-node successful MV events and successful `NewPart` events as
the rates; they do not include other cluster nodes, failed view executions or
replica part downloads. Target totals include all observed writers.

The overview and MV detail show observed execution status on the connected node. Press `w` to switch the error window between 24 hours, 1 hour and 7 days; press `e` to inspect up to 50 logged MV errors, then Enter to open the full ClickHouse message. A missing success or error is only an observation within that window: logging may be disabled, sampled or delayed. Query failures are shown separately.

## Log requirements

Both system logs must be configured and readable by the user. View logging
also requires `log_query_views` for the queries that execute the view. Diagnostics distinguish unsupported engines, unavailable metadata, log query errors and no observed events. No observed events still cannot distinguish inactivity from disabled, sampled or delayed logging. Errors appear below the diagram. Metrics
in the diagrams cover the connected server only; the separate node survey reports its cluster coverage explicitly.
