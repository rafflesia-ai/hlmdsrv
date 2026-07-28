#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

cd "$ROOT"

mdsrv() {
  if [ -n "${MDSRV_HEADLESS_BIN:-}" ]; then
    "$MDSRV_HEADLESS_BIN" "$@"
  else
    go run ./cmd/hlmdsrv "$@"
  fi
}

mdsrv version --json >"$WORKDIR/version.json"
mdsrv capabilities --gmx-command definitely-missing-gmx-for-contract-test --json >"$WORKDIR/capabilities-no-gromacs.json"
mdsrv doctor --gmx-command definitely-missing-gmx-for-contract-test --json >"$WORKDIR/doctor-no-gromacs.json"
mdsrv self-test --quickstart=false --out-dir "$WORKDIR/self-test-core" --json >"$WORKDIR/self-test-core.json"
mdsrv docs --out "$WORKDIR/docs" >/dev/null
test -s "$WORKDIR/docs/hlmdsrv.md"
go test ./internal/mdsrvcli -run 'TestMDSrv.*ContractSnapshots' -count=1

for example in mdsrv-demo.job.yaml mdsrv.job.yaml; do
  mdsrv explain "$ROOT/examples/$example" --store "$WORKDIR/store-$example" --json >"$WORKDIR/explain-$example.json"
  mdsrv run "$ROOT/examples/$example" --store "$WORKDIR/plan-$example" --plan --json >"$WORKDIR/plan-$example.json"
done

python3 - "$WORKDIR" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def load(name):
    with (root / name).open() as f:
        return json.load(f)

version = load("version.json")
assert version["ok"], version
assert version["service"] == "hlmdsrv", version
assert version["manifest_version"] == "mdsrv.job/v1", version
assert version["cli"]["version"], version

capabilities = load("capabilities-no-gromacs.json")
assert capabilities["ok"], capabilities
assert capabilities["manifest_version"] == "mdsrv.job/v1", capabilities
assert capabilities["backends"]["gromacs"]["available"] is False, capabilities
assert capabilities["backends"]["gromacs"]["source"] == "option", capabilities
assert "install GROMACS" in capabilities["backends"]["gromacs"]["hint"], capabilities
for key in ("datasets", "dataset_writes", "selections", "sessions", "chunk_encodings"):
    assert key in capabilities["features"], capabilities

doctor = load("doctor-no-gromacs.json")
assert doctor["ok"] is False, doctor
assert doctor["checks"], doctor
gromacs = next((check for check in doctor["checks"] if check["name"] == "gromacs"), None)
assert gromacs and gromacs["ok"] is False, doctor
assert gromacs["level"] == "recommended", gromacs
assert gromacs.get("remediation"), gromacs

selftest = load("self-test-core.json")
assert selftest["ok"], selftest
assert selftest["quickstart_status"] == "disabled", selftest
assert selftest["checks"], selftest

for example in ("mdsrv-demo.job.yaml", "mdsrv.job.yaml"):
    explanation = load(f"explain-{example}.json")
    assert explanation["id"], explanation
    assert explanation["plan"], explanation
    plan = load(f"plan-{example}.json")
    assert plan["id"], plan
    assert plan["steps"], plan
    assert plan["steps"][0]["action"] == "ingest", plan
PY

if command -v gmx >/dev/null 2>&1 || command -v gmx_mpi >/dev/null 2>&1; then
  cp "$ROOT/examples/mdsrv.batch.jsonl" "$WORKDIR/mdsrv.batch.jsonl"
  mdsrv demo create --out "$WORKDIR/raw" --id run1 --frames 3 --json >"$WORKDIR/demo-create.json"
  mdsrv batch "$WORKDIR/mdsrv.batch.jsonl" --store "$WORKDIR/batch-store" --force --json >"$WORKDIR/batch.jsonl"
  python3 - "$WORKDIR/batch.jsonl" <<'PY'
import json
import sys

reports = [json.loads(line) for line in open(sys.argv[1]) if line.strip()]
assert reports, "batch produced no reports"
for report in reports:
    assert not report.get("error"), report
    assert report["id"], report
PY
else
  echo "warning: GROMACS not found; skipped executable JSONL batch example" >&2
fi

printf 'mdsrv contracts verified\n'
