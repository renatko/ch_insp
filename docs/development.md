# Development and releases

Run these commands from the root of a source checkout.

## Tests and CI

Pull requests run the Go test suite, cross-platform release builds and a Docker
demo smoke test in GitHub Actions. The demo job checks the data and logs on all
three nodes, runs the ClickHouse-local query tests, builds the TUI image and
opens it against the replica. The separate disposable cluster fixture described
below is not run by this workflow.

The optional cluster integration check is intended for a disposable test cluster with at least two nodes. Set `CHINSPECTOR_TEST_CLUSTER_URL` in the environment and `CHINSPECTOR_TEST_CLUSTER_NAME` to the configured cluster, then run:

```sh
go test ./internal/clickhouse -run '^TestClusterSurveyIntegration$' -v
```

The test uses the existing credentials to discover and contact nodes and checks read access to the two system logs. It does not insert data or flush logs. It verifies connectivity and coverage only; it does not establish logical deduplication or validate cluster-wide aggregation. Do not put credentials in command-line arguments or commit them.

## Release artifacts

Publishing a GitHub release for a tag containing the release workflow builds
and attaches archives for Linux, macOS and Windows on amd64 and arm64, plus a
`SHA256SUMS` file. Each archive contains the executable, README, project license
documentation and notices for its Go dependencies. The binaries are not signed or notarized.

Release builds can be reproduced and checked locally:

```sh
python3 scripts/build_release.py /tmp/chinspector-dist
python3 tests/release/check_archives.py /tmp/chinspector-dist
```

Use an empty output directory. The release workflow will not overwrite existing
assets; remove conflicting assets before rerunning a partially completed release.

## Record the demo GIF

On macOS or Linux, the recording script builds the TUI, connects to the local
synthetic demo, and drives a short interactive tour. It requires Go, Docker
Compose, Python 3, `pyte` and [asciinema agg](https://github.com/asciinema/agg)
(the GIF in this repository was rendered with agg 1.9.0).

```sh
docker compose -f demo/compose.yaml up -d --wait
python3 -m venv /tmp/chinspector-recording-venv
/tmp/chinspector-recording-venv/bin/python -m pip install pyte==0.8.2
/tmp/chinspector-recording-venv/bin/python scripts/record_demo.py
```

The script writes `docs/assets/demo.gif` and leaves the demo running. If `agg`
is not on `PATH`, pass `--agg /path/to/agg`. Use `--port` for a non-default
standalone demo port, `--font-family` to select an installed monospace font,
or `--output` to write elsewhere. The script connects as the demo's default
user and does not record credentials.
