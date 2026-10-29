# Disposable replica fixture

This fixture is for validating cluster metric semantics before enabling any cluster-wide aggregation in ch_insp. It starts two ClickHouse servers in one shard with two replicas and an embedded single-node Keeper. It is intentionally a disposable test setup, not a production configuration.

Run it only where Docker Compose can start containers and pull the ClickHouse image:

```sh
./tests/cluster/verify.sh
```

To check the script's success and cleanup paths without starting Docker, run `bash tests/cluster/test_verify.sh`. This uses a fake Docker command and does not validate ClickHouse behavior.

The script creates a uniquely named Compose project, writes synthetic tables and rows **inside that project only**, and removes that project's containers and volumes on exit. It never connects to `CLICKHOUSE_URL` or an existing ClickHouse server. The application itself remains read-only. The test user has no password, and native ports bind only to loopback; do not expose the fixture on a shared network.

Default host ports are `19001` and `19002`. Override them with `CHINSPECTOR_TEST_PORT1` and `CHINSPECTOR_TEST_PORT2` if needed. `CHINSPECTOR_TEST_IMAGE` selects an alternative official server image. The default is the ClickHouse 25.2 image; no cross-version compatibility is claimed without running the fixture on each version.

The fixture performs inserts on both replicas, a foreground Distributed-table insert, and a deliberately failing MV. It waits for replicated source and target rows, MV success and failure records, and a `DownloadPart` event. It then prints version/topology, local view events, `NewPart` versus `DownloadPart`, and view query IDs observed on more than one node. It cross-compiles and runs the Go cluster metric integration test inside the Docker network, then stops one replica and repeats the test for partial coverage. Go must be installed locally; set `CHINSPECTOR_FIXTURE_GO_TEST=0` to omit the Go integration checks. Log writes are asynchronous; the script polls rather than calling `SYSTEM FLUSH LOGS`.

These observations cover only the fixture's topology and sample operations. They do not prove a universal logical deduplication rule. Run additional cases against the intended versions and topology. The fixture was exercised with ClickHouse 25.2.2.39, one shard and two replicas; other versions and topologies require separate verification.
