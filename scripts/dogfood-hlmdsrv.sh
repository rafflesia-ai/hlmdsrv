#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEEP="${DOGFOOD_KEEP:-0}"
if [ -n "${DOGFOOD_OUT_DIR:-}" ]; then
  WORKDIR="$DOGFOOD_OUT_DIR"
  mkdir -p "$WORKDIR"
else
  WORKDIR="$(mktemp -d)"
fi
cleanup() {
  local status="$?"
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$KEEP" = "1" ] || [ -n "${DOGFOOD_OUT_DIR:-}" ]; then
    echo "dogfood artifacts kept at $WORKDIR" >&2
  else
    rm -rf "$WORKDIR"
  fi
  exit "$status"
}
trap cleanup EXIT

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

wait_for_health() {
  local port="$1"
  local out="$2"
  for _ in $(seq 1 50); do
    if curl --fail --silent "http://127.0.0.1:${port}/health" >"$out"; then
      return 0
    fi
    sleep 0.1
  done
  curl --fail --silent "http://127.0.0.1:${port}/health" >"$out"
}

stop_server() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    unset SERVER_PID
  fi
}

if ! command -v gmx >/dev/null 2>&1 && ! command -v gmx_mpi >/dev/null 2>&1; then
  echo "GROMACS is required for MDsrv dogfood; install gmx/gmx_mpi or set MDSRV_GMX" >&2
  exit 2
fi

BIN_DIR="$WORKDIR/bin"
COMPLETION_DIR="$WORKDIR/completions"
mkdir -p "$BIN_DIR" "$COMPLETION_DIR"

go run ./cmd/hlmdsrv install local \
  --home "$ROOT" \
  --bin-dir "$BIN_DIR" \
  --completion-dir "$COMPLETION_DIR" \
  --force \
  --json >"$WORKDIR/install.json"

export PATH="$BIN_DIR:$PATH"

hlmdsrv version --json >"$WORKDIR/version.json"
hlmdsrv capabilities --store "$WORKDIR/capabilities-store" --json >"$WORKDIR/capabilities.json"
hlmdsrv doctor --strict --store "$WORKDIR/doctor-store" --json >"$WORKDIR/doctor.json"

hlmdsrv quickstart \
  --out "$WORKDIR/quickstart" \
  --id dogfood \
  --frames 4 \
  --json >"$WORKDIR/quickstart.json"

STORE="$WORKDIR/quickstart/store"
hlmdsrv serve smoke --store "$STORE" --read-only --backend gromacs --json >"$WORKDIR/serve-smoke.json"

PORT="$(free_port)"
hlmdsrv serve \
  --store "$STORE" \
  --host 127.0.0.1 \
  --port "$PORT" \
  --read-only \
  --backend gromacs >"$WORKDIR/server.log" 2>&1 &
SERVER_PID="$!"
wait_for_health "$PORT" "$WORKDIR/server-health.json"
curl --fail --silent "http://127.0.0.1:${PORT}/capabilities" >"$WORKDIR/server-capabilities.json"
curl --fail --silent "http://127.0.0.1:${PORT}/datasets" >"$WORKDIR/server-datasets.json"
READ_ONLY_STATUS="$(curl --silent --show-error -o "$WORKDIR/read-only-post.json" -w "%{http_code}" \
  -X POST "http://127.0.0.1:${PORT}/datasets" \
  -H "Content-Type: application/json" \
  --data '{"id":"blocked"}')"
if [ "$READ_ONLY_STATUS" != "403" ]; then
  echo "expected read-only POST to return 403, got $READ_ONLY_STATUS" >&2
  cat "$WORKDIR/read-only-post.json" >&2 || true
  exit 1
fi
RANGE_STATUS="$(curl --silent --show-error -o "$WORKDIR/invalid-range.json" -w "%{http_code}" \
  "http://127.0.0.1:${PORT}/datasets/dogfood/frames/range?start=3&stop=1&backend=gromacs")"
if [ "$RANGE_STATUS" != "400" ]; then
  echo "expected invalid frame range to return 400, got $RANGE_STATUS" >&2
  cat "$WORKDIR/invalid-range.json" >&2 || true
  exit 1
fi
stop_server

AUTH_PORT="$(free_port)"
hlmdsrv serve \
  --store "$STORE" \
  --host 127.0.0.1 \
  --port "$AUTH_PORT" \
  --auth-token secret \
  --read-only \
  --backend gromacs >"$WORKDIR/auth-server.log" 2>&1 &
SERVER_PID="$!"
wait_for_health "$AUTH_PORT" "$WORKDIR/auth-health.json"
AUTH_DENIED_STATUS="$(curl --silent --show-error -o "$WORKDIR/auth-denied.json" -w "%{http_code}" \
  "http://127.0.0.1:${AUTH_PORT}/datasets")"
if [ "$AUTH_DENIED_STATUS" != "401" ]; then
  echo "expected unauthenticated datasets request to return 401, got $AUTH_DENIED_STATUS" >&2
  cat "$WORKDIR/auth-denied.json" >&2 || true
  exit 1
fi
curl --fail --silent \
  -H "Authorization: Bearer secret" \
  "http://127.0.0.1:${AUTH_PORT}/datasets" >"$WORKDIR/auth-datasets.json"
stop_server

JOBS_PORT="$(free_port)"
hlmdsrv serve \
  --store "$STORE" \
  --host 127.0.0.1 \
  --port "$JOBS_PORT" \
  --workers 1 \
  --max-queue 2 \
  --backend gromacs >"$WORKDIR/jobs-server.log" 2>&1 &
SERVER_PID="$!"
wait_for_health "$JOBS_PORT" "$WORKDIR/jobs-health.json"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" submit \
  --type chunks \
  --dataset dogfood \
  --chunk-size 2 \
  --encoding json \
  --force \
  --wait \
  --json >"$WORKDIR/jobs-submit.json"
JOB_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$WORKDIR/jobs-submit.json")"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" status "$JOB_ID" --json >"$WORKDIR/jobs-status.json"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" stats --json >"$WORKDIR/jobs-stats.json"
curl --fail --silent "http://127.0.0.1:${JOBS_PORT}/jobs/metrics" >"$WORKDIR/jobs-metrics.prom"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" logs "$JOB_ID" >"$WORKDIR/jobs.log"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" events "$JOB_ID" --json >"$WORKDIR/jobs-events.json"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" retry "$JOB_ID" --wait --json >"$WORKDIR/jobs-retry.json"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" submit \
  --type analysis \
  --dataset dogfood \
  --analysis-id impossible-distance \
  --analysis-type distance \
  --a 1 \
  --b 999 \
  --backend gromacs \
  --wait \
  --json >"$WORKDIR/jobs-failed.json"
hlmdsrv jobs --server "http://127.0.0.1:${JOBS_PORT}" stats --json >"$WORKDIR/jobs-stats-final.json"
stop_server
hlmdsrv jobs prune --store "$STORE" --ttl 0 --dry-run --json >"$WORKDIR/jobs-prune.json"

hlmdsrv frames count dogfood --store "$STORE" --backend gromacs --json >"$WORKDIR/frames-count.json"
hlmdsrv frames get dogfood 1 --store "$STORE" --backend gromacs --format json >"$WORKDIR/frame-1.json"
hlmdsrv analyze distance dogfood \
  --store "$STORE" \
  --backend gromacs \
  --a 1 \
  --b 2 \
  --out "$WORKDIR/distance.csv" \
  --format csv >"$WORKDIR/analyze-distance.out"
hlmdsrv publish static --store "$STORE" --out "$WORKDIR/static" --force --verify --json >"$WORKDIR/publish-static.json"
hlmdsrv pack dogfood --store "$STORE" --out "$WORKDIR/dogfood.mdsrvx" --force --json >"$WORKDIR/pack.json"
hlmdsrv unpack "$WORKDIR/dogfood.mdsrvx" --store "$WORKDIR/unpacked" --force --json >"$WORKDIR/unpack.json"
hlmdsrv validate dogfood --store "$WORKDIR/unpacked" --json >"$WORKDIR/validate-unpacked.json"
hlmdsrv validate "$WORKDIR/unpacked" --strict --json >"$WORKDIR/validate-store.json"

python3 - "$WORKDIR" >"$WORKDIR/summary.json" <<'PY'
import csv
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def load(name):
    with (root / name).open() as f:
        return json.load(f)

quickstart = load("quickstart.json")
doctor = load("doctor.json")
serve_smoke = load("serve-smoke.json")
frame = load("frame-1.json")
publish = load("publish-static.json")
pack = load("pack.json")
unpack = load("unpack.json")
validate = load("validate-unpacked.json")
validate_store = load("validate-store.json")
jobs_submit = load("jobs-submit.json")
jobs_status = load("jobs-status.json")
jobs_stats = load("jobs-stats.json")
jobs_stats_final = load("jobs-stats-final.json")
jobs_events = load("jobs-events.json")
jobs_retry = load("jobs-retry.json")
jobs_failed = load("jobs-failed.json")
jobs_prune = load("jobs-prune.json")

with (root / "distance.csv").open() as f:
    rows = list(csv.DictReader(f))
with (root / "jobs-metrics.prom").open() as f:
    jobs_metrics = f.read()
with (root / "read-only-post.json").open() as f:
    read_only_error = json.load(f)
with (root / "auth-denied.json").open() as f:
    auth_error = json.load(f)

assert quickstart["id"] == "dogfood", quickstart
assert quickstart["serve_smoke"]["ok"], quickstart
assert doctor["ok"] and doctor["checks"], doctor
assert serve_smoke["ok"], serve_smoke
assert frame["frame"] == 1 and frame["coordinates"], frame
assert rows, rows
assert publish["verification"]["ok"], publish
paths = [check.get("path") for check in publish["verification"].get("checks", []) if check.get("path")]
assert len(paths) == len(set(paths)), publish["verification"]
assert pathlib.Path(pack["path"]).exists(), pack
assert unpack["id"] == "dogfood", unpack
assert validate["id"] == "dogfood", validate
assert validate_store["ok"] and validate_store["checks"], validate_store
assert read_only_error["code"] == "forbidden", read_only_error
assert auth_error["code"] == "unauthorized", auth_error
assert jobs_submit["status"] == "succeeded", jobs_submit
assert jobs_status["id"] == jobs_submit["id"] and jobs_status["status"] == "succeeded", jobs_status
assert jobs_stats["total"] >= 1 and jobs_stats["counts"].get("succeeded", 0) >= 1, jobs_stats
assert jobs_stats_final["total"] >= 3 and jobs_stats_final["counts"].get("failed", 0) >= 1, jobs_stats_final
assert jobs_events["events"] and jobs_events["events"][0]["version"] == "mdsrv.job_event/v1", jobs_events
assert jobs_retry["status"] == "succeeded", jobs_retry
assert jobs_failed["status"] == "failed" and jobs_failed.get("error"), jobs_failed
assert jobs_prune["dry_run"] and jobs_prune["would_remove"], jobs_prune
assert "mdsrv_jobs_by_status" in jobs_metrics, jobs_metrics

print(json.dumps({
    "ok": True,
    "root": str(root),
    "store": quickstart["store"],
    "archive": pack["path"],
    "static": publish["out"],
    "checks": [
        "install",
        "doctor",
        "quickstart",
        "serve",
        "server-errors",
        "auth",
        "jobs",
        "frames",
        "analyze",
        "publish",
        "pack",
        "unpack",
        "validate-dataset",
        "validate-store",
    ],
}, indent=2))
PY

cat "$WORKDIR/summary.json"
