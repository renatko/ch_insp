#!/usr/bin/env bash
# Exercise fixture orchestration with a fake Docker CLI; this is not a cluster test.
set -euo pipefail
fixture_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export FIXTURE_TEST_LOG="$tmp/docker.log"
cat > "$tmp/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE_TEST_LOG"
if [[ " $* " == *" up --detach "* ]] && [[ ${FIXTURE_FAIL_START:-0} == 1 ]]; then
  exit 1
fi
if [[ " $* " == *" INSERT INTO flow_test.bad_src "* ]]; then
  exit 1
fi
if [[ " $* " == *"SELECT count()"* ]]; then
  printf '1\n'
fi
EOF
chmod +x "$tmp/docker"
export PATH="$tmp:$PATH"

"$fixture_dir/verify.sh" > "$tmp/success.out"
grep -Fq 'Fixture checks passed' "$tmp/success.out"
grep -Fq 'stop ch2' "$FIXTURE_TEST_LOG"
grep -Fq 'down --volumes --remove-orphans' "$FIXTURE_TEST_LOG"

: > "$FIXTURE_TEST_LOG"
if FIXTURE_FAIL_START=1 "$fixture_dir/verify.sh" > "$tmp/failure.out" 2>&1; then
  printf 'Expected simulated startup failure\n' >&2
  exit 1
fi
grep -Fq 'logs --tail=60' "$FIXTURE_TEST_LOG"
grep -Fq 'down --volumes --remove-orphans' "$FIXTURE_TEST_LOG"
printf 'Fixture orchestration checks passed (mock Docker only).\n'
