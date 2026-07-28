#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_BIN="${DOCKER_BIN:-docker}"
IMAGE="${MDSRV_REAL_FIXTURE_IMAGE:-hlmdsrv:local-full}"
BUILD_IMAGE="${MDSRV_REAL_FIXTURE_BUILD:-0}"
KEEP="${DOGFOOD_KEEP:-0}"
if [ -n "${DOGFOOD_OUT_DIR:-}" ]; then
  WORKDIR="$DOGFOOD_OUT_DIR"
  mkdir -p "$WORKDIR"
else
  WORKDIR="$(mktemp -d)"
fi
SERVER_PID=""

cleanup() {
  local status="$?"
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$KEEP" = "1" ] || [ -n "${DOGFOOD_OUT_DIR:-}" ] || [ "$status" -ne 0 ]; then
    echo "real-fixture dogfood artifacts kept at $WORKDIR" >&2
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

docker_cli() {
  "$DOCKER_BIN" run --rm -v "$WORKDIR:/work" "$IMAGE" "$@"
}

if ! "$DOCKER_BIN" image inspect "$IMAGE" >/dev/null 2>&1; then
  if [ "$BUILD_IMAGE" = "1" ]; then
    "$DOCKER_BIN" build --build-arg MDSRV_PYTHON_BACKENDS=full -t "$IMAGE" "$ROOT"
  else
    echo "Docker image $IMAGE not found. Set MDSRV_REAL_FIXTURE_BUILD=1 to build it, or build manually with: docker build --build-arg MDSRV_PYTHON_BACKENDS=full -t hlmdsrv:local-full ." >&2
    exit 2
  fi
fi

docker_cli doctor --strict --json --store /work/doctor-store >"$WORKDIR/doctor.json"
docker_cli capabilities --store /work/capabilities-store --json >"$WORKDIR/capabilities.json"
docker_cli fixtures list >"$WORKDIR/fixtures-list.json"
docker_cli fixtures mdanalysis-adk --store /work/store --id adk-real --force --json >"$WORKDIR/fixture.json"
docker_cli validate adk-real --store /work/store --strict --deep --backend python --json >"$WORKDIR/validate.json"
docker_cli frames count adk-real --store /work/store --backend python --json >"$WORKDIR/frames-count.json"
docker_cli frames get adk-real 0 --store /work/store --backend python --atom-subset atom:1-10 --format json >"$WORKDIR/frame-0-subset.json"
docker_cli selection save adk-real --store /work/store --id first-ten --expr 1-10 --json >"$WORKDIR/selection.json"
docker_cli selection resolve adk-real first-ten --store /work/store --target gromacs --json >"$WORKDIR/selection-resolve.json"
docker_cli index build adk-real --store /work/store --chunk-size 5 --json >"$WORKDIR/index.json"
docker_cli index chunks adk-real --store /work/store --chunk-size 5 --encoding bin-zstd --force --json >"$WORKDIR/chunks.json"
docker_cli analyze distance adk-real --store /work/store --backend gromacs --a 1 --b 2 --out /work/distance.csv --format csv >"$WORKDIR/distance.out"
docker_cli analyze rmsd adk-real --store /work/store --backend gromacs --selection first-ten --out /work/rmsd.csv --format csv >"$WORKDIR/rmsd.out"
docker_cli visualize adk-real --store /work/store --frame 0 --include-selections --focus first-ten --out /work/adk-real.mvsj --json >"$WORKDIR/visualize.json"

PORT="$(free_port)"
"$DOCKER_BIN" run --rm \
  -v "$WORKDIR:/work" \
  -p "127.0.0.1:${PORT}:1337" \
  "$IMAGE" serve \
    --store /work/store \
    --host 0.0.0.0 \
    --port 1337 \
    --workers 1 \
    --max-queue 4 \
    --backend gromacs \
    --max-frame-range 8 \
    >"$WORKDIR/server.log" 2>&1 &
SERVER_PID="$!"
for _ in $(seq 1 120); do
  if curl --fail --silent "http://127.0.0.1:${PORT}/health" >"$WORKDIR/server-health.json"; then
    break
  fi
  sleep 0.25
done
curl --fail --silent "http://127.0.0.1:${PORT}/datasets/adk-real/frames/count?backend=gromacs" >"$WORKDIR/server-frame-count.json"
curl --fail --silent "http://127.0.0.1:${PORT}/datasets/adk-real/frames/range?start=0&stop=2&backend=gromacs" >"$WORKDIR/server-frame-range.json"
curl --fail --silent -X POST \
  -H "Content-Type: application/json" \
  --data '{"type":"chunks","dataset_id":"adk-real","chunk_size":5,"encoding":"bin-zstd","force":true}' \
  "http://127.0.0.1:${PORT}/jobs" >"$WORKDIR/server-job-submit.json"
python3 - "$PORT" "$WORKDIR/server-job-submit.json" "$WORKDIR/server-job-status.json" <<'PY'
import json
import sys
import time
import urllib.request

port, submit_path, status_path = sys.argv[1:]
with open(submit_path) as f:
    job = json.load(f)
job_id = job["id"]
url = f"http://127.0.0.1:{port}/jobs/{job_id}"
last = None
for _ in range(120):
    with urllib.request.urlopen(url) as response:
        last = json.load(response)
    if last.get("status") in {"succeeded", "failed", "canceled"}:
        break
    time.sleep(0.25)
with open(status_path, "w") as f:
    json.dump(last, f, indent=2)
    f.write("\n")
if last.get("status") != "succeeded":
    raise SystemExit(f"job did not succeed: {last}")
PY
curl --fail --silent "http://127.0.0.1:${PORT}/jobs/metrics" >"$WORKDIR/server-jobs-metrics.prom"
kill "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""

docker_cli publish static --store /work/store --out /work/static --force --verify --json >"$WORKDIR/publish-static.json"
docker_cli pack adk-real --store /work/store --out /work/adk-real.mdsrvx --force --json >"$WORKDIR/pack.json"
docker_cli unpack /work/adk-real.mdsrvx --store /work/unpacked --force --json >"$WORKDIR/unpack.json"
docker_cli validate /work/unpacked --strict --json >"$WORKDIR/validate-store.json"
docker_cli debug bundle adk-real --store /work/store --out /work/adk-real-debug.zip --json >"$WORKDIR/debug-bundle.json"

python3 - "$WORKDIR" >"$WORKDIR/summary.json" <<'PY'
import csv
import json
import os
import pathlib
import sys

root = pathlib.Path(sys.argv[1])

def load(name):
    with (root / name).open() as f:
        return json.load(f)

def mounted_path(path):
    path = pathlib.PurePosixPath(str(path))
    return root / path.name

fixture = load("fixture.json")
validate = load("validate.json")
frame = load("frame-0-subset.json")
index = load("index.json")
chunks = load("chunks.json")
server_job = load("server-job-status.json")
publish = load("publish-static.json")
pack = load("pack.json")
unpack = load("unpack.json")
store = load("validate-store.json")
debug = load("debug-bundle.json")

with (root / "distance.csv").open() as f:
    distance_rows = list(csv.DictReader(f))
with (root / "rmsd.csv").open() as f:
    rmsd_rows = list(csv.DictReader(f))
with (root / "server-jobs-metrics.prom").open() as f:
    metrics = f.read()

assert fixture["id"] == "adk-real", fixture
assert fixture["frames"] > 0 and fixture["atoms"] > 0, fixture
assert validate["ok"], validate
assert frame["coordinates"] and len(frame["coordinates"]) <= 10, frame
assert index["frame_count"] == fixture["frames"], index
assert chunks["chunks"], chunks
assert all(chunk.get("encoding") == "mdsrv-frames-bin-zstd-v1" for chunk in chunks["chunks"]), chunks
assert all(str(chunk.get("path", "")).endswith(".bin.zst") for chunk in chunks["chunks"]), chunks
assert distance_rows and rmsd_rows, (distance_rows, rmsd_rows)
assert server_job["status"] == "succeeded", server_job
assert "mdsrv_jobs_by_status" in metrics, metrics
assert publish["verification"]["ok"], publish
assert mounted_path(pack["path"]).exists(), pack
assert any(str(path).endswith(".bin.zst") for path in pack["files"]), pack
assert unpack["id"] == "adk-real", unpack
assert store["ok"], store
assert mounted_path(debug["path"]).exists() and os.path.getsize(mounted_path(debug["path"])) > 0, debug

print(json.dumps({
    "ok": True,
    "fixture": fixture,
    "workdir": str(root),
    "archive": pack["path"],
    "checks": [
        "real-fixture-ingest",
        "python-subset-frame",
        "gromacs-index",
        "zstd-chunks",
        "distance",
        "rmsd",
        "visualize",
        "serve",
        "async-job",
        "publish",
        "pack",
        "unpack",
        "debug-bundle",
    ],
}, indent=2))
PY

cat "$WORKDIR/summary.json"
