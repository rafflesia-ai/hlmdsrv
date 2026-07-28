# MDsrv Headless

This CLI treats headless MDsrv as trajectory dataset and session management, not as Mol* image rendering.

Current upstream MDsrv surfaces are split:

- The older Python `mdsrv` command starts an NGL-based local server/viewer and accepts `[structure] [trajectory]`, `--script`, `--configure`, `--host`, and `--port`.
- The newer Mol*-based MDsrv deployment is documented through Docker images and a persistent server directory. Manual server updates are done by placing an XTC file in the trajectory folder and editing `trajectory_index.json`; saved sessions use the session folder plus `session_index.json`.

This implementation builds the missing non-GUI layer between raw trajectory files and a publishable server store.

The initial automation contract is frozen in `docs/mdsrv-v0-contract.md`. Use that document for the stable command, JSON, archive, Docker, and dogfood surfaces that scripts can depend on.

## Core Commands

```sh
go run ./cmd/hlmdsrv quickstart --out outputs/mdsrv-quickstart
go run ./cmd/hlmdsrv self-test --out-dir outputs/mdsrv-self-test --json
go run ./cmd/hlmdsrv version --json
go run ./cmd/hlmdsrv capabilities --json
go run ./cmd/hlmdsrv doctor --store ./mdsrv-data
go run ./cmd/hlmdsrv doctor --strict --json --store ./mdsrv-data --cache ./.cache/mdsrv --static-out ./public-mdsrv
go run ./cmd/hlmdsrv init --store ./mdsrv-data
go run ./cmd/hlmdsrv init job \
  --id run1 \
  --topology structure.gro \
  --trajectory traj.xtc \
  --out job.yaml
go run ./cmd/hlmdsrv demo create --out outputs/mdsrv-demo --id demo --frames 5
go run ./cmd/hlmdsrv config init \
  --profile local \
  --store ./mdsrv-data \
  --backend gromacs \
  --cache ./.cache/mdsrv \
  --timeout 10m \
  --force
go run ./cmd/hlmdsrv --profile local run examples/mdsrv.job.yaml --plan
go run ./cmd/hlmdsrv --profile local run examples/mdsrv.job.yaml --dry-run
go run ./cmd/hlmdsrv explain examples/mdsrv.job.yaml --store ./mdsrv-data --json
go run ./cmd/hlmdsrv run examples/mdsrv.job.yaml --store ./mdsrv-data --force --report run-report.json
go run ./cmd/hlmdsrv run examples/mdsrv.job.yaml \
  --store ./mdsrv-data \
  --probe-timeout 30s \
  --analysis-timeout 2m \
  --force

go run ./cmd/hlmdsrv demo gromacs \
  --out outputs/mdsrv-gromacs-demo \
  --store outputs/mdsrv-gromacs-store \
  --id demo-gromacs \
  --frames 5

go run ./cmd/hlmdsrv gromacs doctor
go run ./cmd/hlmdsrv gromacs probe outputs/mdsrv-gromacs-demo/trajectory.xtc
go run ./cmd/hlmdsrv gromacs convert outputs/mdsrv-gromacs-demo/frames.gro --out trajectory.xtc
go run ./cmd/hlmdsrv gromacs extract \
  --topology outputs/mdsrv-gromacs-demo/structure.gro \
  --trajectory outputs/mdsrv-gromacs-demo/trajectory.xtc \
  --frame 2

go run ./cmd/hlmdsrv fixtures list
go run ./cmd/hlmdsrv fixtures mdanalysis-adk --store ./mdsrv-data --id adk --force

go run ./cmd/hlmdsrv ingest structure.gro traj.xtc \
  --store ./mdsrv-data \
  --id run1 \
  --name "Run 1"

go run ./cmd/hlmdsrv ingest \
  --store ./mdsrv-data \
  --name "Run 1" \
  --topology structure.gro \
  --trajectory traj.xtc \
  --source "doi:10.xxxx/yyyy" \
  --stride 10 \
  --atom-subset "not water and not hydrogen"

go run ./cmd/hlmdsrv ingest \
  --store ./mdsrv-data \
  --id remote-run \
  --topology-url https://example.org/structure.gro \
  --trajectory-url https://example.org/traj.xtc \
  --cache ./.cache/mdsrv

go run ./cmd/hlmdsrv dataset update run1 --store ./mdsrv-data --name "Updated Run 1"
go run ./cmd/hlmdsrv selection save run1 --store ./mdsrv-data --id first-two --expr 1-2
go run ./cmd/hlmdsrv selection resolve run1 first-two --store ./mdsrv-data --target python
go run ./cmd/hlmdsrv selection export-index run1 first-two --store ./mdsrv-data --out first-two.ndx
go run ./cmd/hlmdsrv index build run1 --store ./mdsrv-data --chunk-size 128 --max-frames 100000
go run ./cmd/hlmdsrv index chunks run1 --store ./mdsrv-data --chunk-size 128 --encoding bin-zstd --max-chunk-bytes 104857600 --force
go run ./cmd/hlmdsrv dataset inspect run1 --store ./mdsrv-data --backend gromacs
go run ./cmd/hlmdsrv frames count run1 --store ./mdsrv-data --backend auto
go run ./cmd/hlmdsrv export run1 --store ./mdsrv-data --frames 0:100:10 --out run1-slice.xtc
go run ./cmd/hlmdsrv visualize run1 --store ./mdsrv-data --frame 0 --include-selections --out run1.mvsj
go run ./cmd/hlmdsrv schema job
go run ./cmd/hlmdsrv schema manifest
go run ./cmd/hlmdsrv schema openapi
go run ./cmd/hlmdsrv install backends
go run ./cmd/hlmdsrv install local --bin-dir "$HOME/.local/bin" --completion-dir "$HOME/.local/share/hlmdsrv/completions" --force
go run ./cmd/hlmdsrv install completions --shell zsh --force
go run ./cmd/hlmdsrv completion zsh > completions/_hlmdsrv
go run ./cmd/hlmdsrv list datasets --store ./mdsrv-data
go run ./cmd/hlmdsrv probe run1 --store ./mdsrv-data
go run ./cmd/hlmdsrv frames count run1 --store ./mdsrv-data
go run ./cmd/hlmdsrv frames extract run1 2 --store ./mdsrv-data --out frame-2.gro
go run ./cmd/hlmdsrv frames get run1 2 --store ./mdsrv-data --format json
go run ./cmd/hlmdsrv analyze distance run1 --store ./mdsrv-data --a 1 --b 2 --out traces/distance.csv
go run ./cmd/hlmdsrv analyze rgyr run1 --store ./mdsrv-data --selection first-two --out traces/rgyr.csv
go run ./cmd/hlmdsrv session publish --store ./mdsrv-data --dataset run1 --id run1-session --file session.molj
go run ./cmd/hlmdsrv session list --store ./mdsrv-data
go run ./cmd/hlmdsrv pack run1 --store ./mdsrv-data --out run1.mdsrvx
go run ./cmd/hlmdsrv unpack run1.mdsrvx --store ./restored-mdsrv-data
go run ./cmd/hlmdsrv publish static --store ./mdsrv-data --out ./public-mdsrv --force --verify
go run ./cmd/hlmdsrv demo create --out examples/raw --id run1 --frames 4
go run ./cmd/hlmdsrv batch examples/mdsrv.batch.jsonl --store ./mdsrv-data --concurrency 4
go run ./cmd/hlmdsrv compat check --store ./mdsrv-data
go run ./cmd/hlmdsrv validate run1 --store ./mdsrv-data --deep
go run ./cmd/hlmdsrv validate run1 --store ./mdsrv-data --strict --json
go run ./cmd/hlmdsrv debug bundle run1 --store ./mdsrv-data --out run1-debug.zip --json
go run ./cmd/hlmdsrv serve smoke --store ./mdsrv-data --read-only
go run ./cmd/hlmdsrv serve --store ./mdsrv-data --port 1337
go run ./cmd/hlmdsrv serve \
  --store ./mdsrv-data \
  --host 0.0.0.0 \
  --port 1337 \
  --read-only \
  --log-requests \
  --request-timeout 30s \
  --max-frame-range 128 \
  --max-atoms 250000 \
  --max-frames 100000 \
  --max-chunk-bytes 104857600 \
  --workers 4 \
  --max-queue 64 \
  --job-timeout 10m \
  --allow-path "$PWD/mdsrv-data" \
  --allow-host files.example.org \
  --auth-token "$MDSRV_AUTH_TOKEN"
```

`demo create` creates a tiny real GROMACS trajectory and a runnable `job.yaml` that points at it. It is the fastest way to get a known-good local manifest without bringing your own data. `demo gromacs` is the lower-level form: it creates a real multi-frame `.gro`, converts it to `.xtc` with `gmx trjconv`, and can ingest the result into a store. The regression path is:

```sh
go run ./cmd/hlmdsrv demo gromacs --out outputs/mdsrv-demo/raw --frames 6
go run ./cmd/hlmdsrv ingest outputs/mdsrv-demo/raw/structure.gro outputs/mdsrv-demo/raw/trajectory.xtc --store outputs/mdsrv-demo/store --id run1 --force
go run ./cmd/hlmdsrv index build run1 --store outputs/mdsrv-demo/store --chunk-size 2
go run ./cmd/hlmdsrv index chunks run1 --store outputs/mdsrv-demo/store --chunk-size 2 --encoding bin-zstd --force
go run ./cmd/hlmdsrv frames get run1 3 --store outputs/mdsrv-demo/store --backend auto --format json
go run ./cmd/hlmdsrv selection save run1 --store outputs/mdsrv-demo/store --id first-two --expression 1-2
go run ./cmd/hlmdsrv analyze distance run1 --store outputs/mdsrv-demo/store --backend gromacs --a 1 --b 2 --out outputs/mdsrv-demo/distance.csv
go run ./cmd/hlmdsrv export run1 --store outputs/mdsrv-demo/store --backend gromacs --frames 1:4:2 --out outputs/mdsrv-demo/slice.xtc --force
go run ./cmd/hlmdsrv visualize run1 --store outputs/mdsrv-demo/store --frame 2 --include-selections --focus first-two
go run ./cmd/hlmdsrv compat check --store outputs/mdsrv-demo/store
go run ./cmd/hlmdsrv pack run1 --store outputs/mdsrv-demo/store --out outputs/mdsrv-demo/run1.mdsrvx --force
go run ./cmd/hlmdsrv unpack outputs/mdsrv-demo/run1.mdsrvx --store outputs/mdsrv-demo/unpacked --force
```

The automated local regression command is:

```sh
npm run test:mdsrv:gromacs
npm run test:mdsrv:python-backend
npm run bench:mdsrv
```

The GROMACS regression is intentionally GROMACS-gated. CI also runs two isolated Python backend jobs: one installs only `mdtraj`, and one installs only `MDAnalysis`, so each backend proves it can carry the Python frame/analysis path without the other package. The benchmark command writes a persistent JSON report to `outputs/benchmarks/mdsrv.json` and measures JSON, binary, and zstd-compressed binary chunk encode/decode behavior.

## Docker

The default Docker image includes both CLIs, GROMACS, Python, and `mdtraj`. That keeps rebuilds practical while still exercising the Python atom-subset backend. Build with `MDSRV_PYTHON_BACKENDS=full` when you also need `MDAnalysis` and `MDAnalysisTests`, or `MDSRV_PYTHON_BACKENDS=none` for a GROMACS-only runtime image.

```sh
docker build -t headlessmolstar:local .
docker build --build-arg MDSRV_PYTHON_BACKENDS=full -t headlessmolstar:local-full .
docker run --rm headlessmolstar:local doctor
npm run docker:verify
npm run docker:verify:full
docker run --rm -p 1337:1337 -v "$PWD/mdsrv-data:/data" headlessmolstar:local \
  serve --store /data --host 0.0.0.0 --read-only --log-requests
```

`npm run docker:verify` fails fast when the Docker daemon is unreachable, then exercises recipe commands, GROMACS demo generation, MDsrv `self-test`, `run --report`, `publish static --verify`, Python atom-subset frame extraction when a Python backend is present, and archive packing inside the image. `npm run docker:verify:full` additionally requires the full Python fixture stack and runs the MDAnalysisTests AdK fixture.

The image defaults to `hlmdsrv`. The Mol* renderer is still available as `molstar`:

```sh
docker run --rm --entrypoint molstar headlessmolstar:local doctor
```

`gromacs` is the raw adapter namespace. It is useful for diagnosing or converting files before they enter a store:

- `gromacs doctor`: reports the resolved `gmx` command and version.
- `gromacs probe TRAJECTORY`: runs `gmx check` and returns clean frame metadata.
- `gromacs convert INPUT --out OUTPUT`: runs `gmx trjconv` for direct conversion.
- `gromacs extract --topology TOP --trajectory TRAJ --frame N`: maps a frame index to trajectory time and runs `gmx trjconv -dump`.
- `gromacs extract ... --time T`: passes an explicit GROMACS time directly to `-dump`.

Most workflows should still use `ingest`, `frames`, and `serve`; the raw `gromacs` namespace is the bridge/debug surface.

`quickstart` is the fastest first-contact path. It creates a tiny real GROMACS trajectory, writes a `mdsrv.job/v1` file, runs it through the normal `run` pipeline, materializes frame chunks, writes a trace, generates an MVS companion scene, packs an `.mdsrvx` archive, publishes a static store, verifies the static output, smoke-tests the HTTP server surface in-process, and prints next commands for serving and frame access. It requires GROMACS because it intentionally proves the real trajectory path.

`self-test` runs the headless confidence path as separate steps. It always checks doctor, schema validation, explain, and run planning; when GROMACS is available it also runs the full quickstart path and reports `demo_create`, `run`, strict validation, static publish verification, and `serve_smoke`. Use `--out-dir` to keep a stable output directory, `--quickstart=false` for the lightweight core path, or `--require-gromacs` when CI should fail if the full trajectory path cannot run.

`debug bundle DATASET_ID` writes a small zip file for issue reports and CI artifacts. The bundle includes `summary.json`, command context, doctor output, backend versions, validation output, manifest JSON/YAML, store index files, a frame-index summary, in-process `serve smoke` output, and recent job status/log files. It intentionally does not copy large topology, trajectory, or chunk payloads by default. Use `--skip-smoke` when the HTTP handler checks are not relevant, `--deep` to decode trajectory metadata during validation, `--strict` to make optional missing artifacts visible as validation errors, and `--max-file-bytes` / `--log-bytes` to control bundle size.

`version --json` reports CLI build provenance plus the supported `mdsrv.job/v1` manifest version. `capabilities --json` reports local backend availability, feature support, chunk encodings, and whether frame decoding/indexing can run through Python or GROMACS.

`capabilities --json` and `gromacs doctor --json` include the resolved GROMACS command, where it came from (`--gmx-command`, `MDSRV_GMX`, `PATH`, or fallback), version when runnable, and an actionable install/remediation hint when it is missing. This is the machine-readable contract CI should use to distinguish “GROMACS unavailable” from “GROMACS present but broken.”

`init job` creates a starter `mdsrv.job/v1` manifest from topology and trajectory paths or URLs. It infers basic formats, derives the id from the trajectory when `--id` is omitted, and can preconfigure static chunk generation with `--chunks`.

`examples/mdsrv-demo.job.yaml` is the canonical tiny workflow manifest. It expects files generated by `demo create` under `raw/` and is useful for docs, CI fixtures, and comparing hand-authored jobs to the CLI-generated shape.

`config init` writes named CLI profiles to `$XDG_CONFIG_HOME/hlmdsrv/config.yaml`, `$HOME/.config/hlmdsrv/config.yaml`, or `MDSRV_CONFIG` when set. Profiles can provide defaults such as `store`, `backend`, `gmx_command`, `cache`, `auth_token`, and `timeout`. Use `--profile NAME` or `MDSRV_PROFILE=NAME` to load those defaults; explicit command flags still win.

`explain JOB.yaml` resolves the job without touching the store. It prints the planned steps, resolved input paths, inferred formats, cache/store/backend defaults, selections, analyses, outputs, and warnings such as missing local files or output collisions. `explain job`, `explain chunks`, and similar topic names still print short conceptual help.

`run JOB.yaml` is the preferred end-to-end path. It treats an `mdsrv.job/v1` manifest as a job recipe: ingest topology/trajectory, save named selections, optionally probe metadata, build a frame index, optionally materialize static frame chunks, generate a companion MVS scene, run listed analyses, publish referenced session artifacts when they exist, and pack `.mdsrvx` outputs. Use `--plan` to print the planned steps without touching the store, or `--dry-run` for the same plan with `dry_run: true` in JSON output. Use `streaming.materialize_chunks: true` or `run --chunks` to write `chunks/<id>/chunk-*`; set `streaming.encoding` or `run --chunk-encoding` to `json`, `bin`, or `bin-zstd`. Use `runtime.max_atoms`, `runtime.max_frames`, `runtime.max_chunk_bytes`, and `runtime.timeout_seconds` for reproducible resource ceilings. Use `--probe=false` or `--index=false` for fixture-only runs against placeholder files, and `--strict` when missing optional artifacts should fail the job. `run` validates the job against `schema job` before touching the store.

`run --report report.json` writes a durable report with artifact paths, SHA-256 digests, byte counts, per-step timings, warnings, and total duration. This is intended for CI and batch logs where stdout may be consumed by another tool.

`examples/mdsrv.batch.jsonl` is the default JSONL batch shape for CI. Create demo files under `examples/raw`, then run `batch examples/mdsrv.batch.jsonl --store ./mdsrv-data --concurrency 4 --json`. Relative paths in a batch file are resolved from the batch file directory, so the same JSONL works from any current working directory.

Use `npm run test:mdsrv:contracts` to verify the stable JSON contracts and documented examples. The verifier exercises CLI contracts (`version`, `capabilities`, `doctor`, `store doctor`, core `self-test`, docs generation, and example planning) plus HTTP contracts for `/health`, `/capabilities`, `/datasets`, frame counts, job lists/events, read-only errors, and auth errors. It does not require GROMACS for the core path; when `gmx` or `gmx_mpi` is available it also creates the demo raw files and runs the JSONL batch example end to end.

Use `npm run dogfood:mdsrv` for the installed-CLI story: install `hlmdsrv` into a temporary bin directory, run `doctor`, create a real quickstart dataset, start the HTTP server, verify read-only and invalid-range errors, verify auth failure/success, submit/retry/fail async jobs, inspect job events/metrics/pruning, fetch frame/metadata endpoints, run analysis, publish a static store, pack `.mdsrvx`, unpack it, and validate both the dataset and restored store. Set `DOGFOOD_KEEP=1` to retain the output directory.

Use `npm run dogfood:mdsrv:real` for the larger real-data path. It runs the same installed-Docker CLI against the MDAnalysisTests AdK GRO/XTC fixture, materializes chunks, submits async server jobs, publishes a static store, packs/unpacks `.mdsrvx`, and writes a debug bundle.

Timeouts are split by scope. The global `--timeout` wraps the whole command, `runtime.timeout_seconds` wraps one manifest run, `run --probe-timeout` wraps probe/index/chunk work, and `run --analysis-timeout` wraps each analysis. A profile timeout is used only when the command does not set `--timeout`.

`fixtures mdanalysis-adk` ingests a real AdK GRO/XTC trajectory from the `MDAnalysisTests` Python package. The default Docker image skips that package for rebuild speed; use `npm run docker:build:full` and `npm run docker:verify:full` to exercise the real public fixture path without committing binary data to this repository.

The GROMACS bridge is implemented as an independent `internal/gromacs` adapter rather than as MDsrv store code. That package owns command discovery, injectable command execution for fake-`gmx` tests, `gmx check` parsing, exact frame-time handling, conversion, extraction, export, typed command-availability errors, capability reports, and the tiny demo trajectory fixture used by quickstart/dogfood tests. MDsrv only maps adapter probe results into manifests and uses the adapter as one backend for indexing, frame extraction, and trajectory export.

`init` creates the store directory layout, writes `store.json` with the current `mdsrv.store/v1` metadata version, and initializes empty `trajectory_index.json` / `session_index.json` files. `store doctor --store ./mdsrv-data --json --strict` checks the store version, required directories, index files, and migration status; use `store doctor --init` to initialize missing metadata before checking.

`ingest` writes:

- `datasets/<id>.yaml`: durable headless manifest.
- `topology/<id>.<ext>`: copied topology file.
- `trajectory/<id>.<ext>`: copied trajectory file.
- `trajectory_index.json`: current MDsrv streaming-server compatible trajectory catalog.
- `session_index.json`: initialized for later session publishing.

By default, `ingest` derives the dataset id from the trajectory filename when `--id` is omitted, then runs a GROMACS probe when `gmx` is available. The manifest records atom count, frame count, first/last time, and timestep on the trajectory entry. Use `--probe=false` to skip this, or `probe DATASET_ID` to refresh it later. The positional form is equivalent to `--topology` plus `--trajectory`.

`ingest` also accepts `--topology-url` and `--trajectory-url`. Downloads are cached with `--cache`, copied into the store, checksummed, and kept with the original URL in the manifest. Batch jobs support the same `topology_url`, `trajectory_url`, and `cache` fields.

`dataset` manages lifecycle operations: `update`, `rename`, `delete`, and `gc`. `delete --files` removes referenced topology, trajectory, index, and trace files; `gc` removes unreferenced store files.

`selection` persists named selections in the manifest. The CLI selection DSL supports `all`, `atom:1`, `atom:1-10`, comma-separated 1-based atom ids, and ranges such as `1-10`. Saved selection ids can be reused by `analyze`, `export`, and HTTP requests; prefixing with `@` is also accepted. Raw mdtraj or MDAnalysis selections still pass through to the Python backend.

`index build` writes `indexes/<id>-frame-index.json`, records it in the manifest, and lets frame extraction map frame indexes to exact trajectory times when GROMACS reports them. `index chunks` materializes static frame chunks under `chunks/<id>/`, updates the chunk paths in the frame index, and includes those chunks in `.mdsrvx` archives. Use `--encoding json`, `--encoding bin`, or `--encoding bin-zstd`; decoded JSON inspection still works through `LoadFrameChunk` and the HTTP API. `--max-atoms`, `--max-frames`, and `--max-chunk-bytes` fail early when a job exceeds configured ceilings.

`export` slices a dataset trajectory through GROMACS. `--frames START:STOP:STRIDE` uses frame indexes and maps them to trajectory times using probed metadata.

`visualize` generates a static `.mvsj` companion scene and records it under `visualization.mvs.scene`. By default it uses the topology file; `--frame N` extracts a trajectory snapshot to `visualization/<id>-frame-N.gro` first, and `--include-selections` or repeated `--selection` flags add saved named selections as separate MVS components.

`schema` prints JSON Schema for `job`/`manifest` and batches, plus an OpenAPI 3.1 document for the HTTP server. `install backends` prints setup guidance for GROMACS, `mdtraj`, and `MDAnalysis`; `completion bash|zsh|fish` prints shell completions, while `install completions` writes one shell completion to a local completion path. `install local` builds `hlmdsrv` from the checkout, installs it into a bin directory, and writes bash, zsh, and fish completions together.

`doctor` reports GROMACS, Docker, Python, `mdtraj`, and `MDAnalysis` separately, with `required`, `recommended`, and `optional` levels plus remediation text in JSON output. `doctor --strict` fails when required checks or GROMACS fail. `--cache` and `--static-out` make the command verify writable cache and publish paths up front. GROMACS is enough for metadata, frame extraction, trajectory export, and fallback full-frame analysis. Python trajectory packages are optional, but needed for atom-subset JSON/binary frames and Python-native trajectory decoding.

Backend flags are explicit. `--backend python` auto-selects the first available Python backend, while `--backend mdtraj` and `--backend mdanalysis` force that package and fail if it is unavailable.

`frames count` uses the manifest metadata when available and falls back to `gmx check`. `frames extract` uses `gmx trjconv -dump` and writes a real structure file such as `.gro` or `.pdb`.

`frames get` returns structured frame data as JSON or a compact `MDSF` binary payload. It tries the Python bridge first (`mdtraj` or `MDAnalysis`), then falls back to GROMACS extraction plus `.gro` parsing. The fallback supports full-frame output; atom-subset JSON extraction requires the Python bridge.

`analyze` supports `distance`, `angle`, `dihedral`, `rmsd`, `rgyr`, `rmsf`, `contacts`, `sasa`, and `hbonds`. With `mdtraj` or `MDAnalysis`, selections use the installed library's selection syntax. Without those packages, the GROMACS fallback supports distance, angle, dihedral, RMSD, radius of gyration, RMSF, and contact counts with 1-based atom-index selections such as `--a 1 --b 2`, ranges such as `--selection 1-10`, and `all`.

`session publish` copies `.molj` or other saved session artifacts to `session/<id>.<ext>`, updates `session_index.json`, and records the session in the dataset manifest.

`validate --strict` checks more than syntax: topology/trajectory files and checksums, unsafe paths, referenced analysis/index/visualization/session artifacts, output collisions, obvious topology/trajectory atom-count mismatches, and availability of the requested backend. Non-strict mode keeps optional artifacts as warnings so partially-authored jobs are still inspectable.

`publish static` copies a store into a read-only deployment directory with the current catalogs and referenced artifact trees. It is useful when `index chunks` has materialized chunks and the result will be served by a static file host or copied into a container image. Add `--verify` to validate the copied catalogs, dataset manifests, topology/trajectory files, frame indexes, chunks, traces, visualization files, and sessions as a standalone static output.

CLI failures are classified for automation. `hlmdsrv` prints `code: message` on stderr and exits with stable codes: `invalid_manifest` = 2, `missing_input` = 3, `missing_backend` = 4, `backend_timeout` = 5, `unsafe_path` = 6, `validation_failed` = 7, `conflict` = 8, `render_failed` = 9, and `canceled` = 130.

`pack` writes a `.mdsrvx` ZIP archive containing `index.yaml`, the dataset manifest, topology, trajectory, session files, trace outputs, and MDsrv index files.

`unpack` restores a `.mdsrvx` archive into a store. Archive entries are resolved under the store root and `../` escapes are rejected.

`compat check` validates the store layout against the currently documented `mdsrv-remote` conventions: trajectory files live under `trajectory/`, session files under `session/`, and index ids match filename stems. `compat check --docker` attempts to run `dwiegreffe/mdsrv-remote` with the store mounted at `/mdsrv/server`.

`serve smoke` starts the same HTTP handler stack in-process, checks `/health`, `/version`, `/capabilities`, catalogs, dataset metadata, and a frame index/count route, then exits with a JSON report. When `--workers` is greater than zero it also checks `/jobs/stats`. It is the CI-safe counterpart to starting a long-running `serve` process.

`serve` can be hardened for published stores. `--read-only` rejects `POST`, `PATCH`, `PUT`, and `DELETE` mutations. `--allow-path` restricts local ingest and session upload paths to approved roots. `--allow-host` restricts URL ingest to approved hosts. `--request-timeout` wraps every request, `--max-frame-range` caps range response size before backend work starts, and `--max-atoms`, `--max-frames`, and `--max-chunk-bytes` enforce dataset and chunk ceilings on server-side work. `--workers` enables the `/jobs` queue for long-running chunk and analysis jobs, `--max-queue` bounds queued work, and `--job-timeout` caps each async job. `--job-prune-on-start --job-ttl 168h` prunes old terminal job records before workers load persisted jobs. `--log-requests` writes JSON request logs to stderr. `--auth-token` or `MDSRV_AUTH_TOKEN` protects API routes with `Authorization: Bearer ...` or `X-MDSRV-Token`; `/health` and `/version` stay open for operational checks. Error responses include `error`, `code`, and `request_id` when a request id is available.

Profile config can store the same retention defaults: `config init --job-prune-on-start --job-ttl 168h` records them under the selected profile so repeated `serve` and `serve smoke` runs do not need those flags.

## HTTP Surface

```text
GET /health
GET /version
GET /capabilities
GET /metrics
GET /schema/manifest
GET /schema/batch
GET /schema/openapi
GET /datasets
POST /datasets
GET /datasets/{dataset_id}
PATCH /datasets/{dataset_id}
DELETE /datasets/{dataset_id}
POST /datasets/{dataset_id}/rename
GET /datasets/{dataset_id}/metadata
GET /datasets/{dataset_id}/topology
GET /datasets/{dataset_id}/trajectory
GET /datasets/{dataset_id}/frames/count
GET /datasets/{dataset_id}/frames/{frame}
GET /datasets/{dataset_id}/frames/range
GET /datasets/{dataset_id}/frames/index
POST /datasets/{dataset_id}/frames/index
GET /datasets/{dataset_id}/frames/chunks
POST /datasets/{dataset_id}/frames/chunks
GET /datasets/{dataset_id}/frames/chunks/{chunk}
GET /datasets/{dataset_id}/selections
POST /datasets/{dataset_id}/selections
GET /datasets/{dataset_id}/selections/{selection_id}
DELETE /datasets/{dataset_id}/selections/{selection_id}
GET /datasets/{dataset_id}/analyses
POST /datasets/{dataset_id}/analyses
GET /jobs
POST /jobs
GET /jobs/{job_id}
GET /jobs/stats
GET /jobs/metrics
GET /jobs/{job_id}/logs
GET /jobs/{job_id}/events
POST /jobs/{job_id}/cancel
POST /jobs/{job_id}/retry
GET /sessions
POST /sessions
GET /trajectory_index.json
GET /session_index.json
GET /trajectory/{file}
GET /session/{file}
```

Frame endpoints support both structure-file and structured data responses:

```sh
curl http://127.0.0.1:1337/datasets/run1/frames/count
curl 'http://127.0.0.1:1337/datasets/run1/frames/2?format=gro' > frame-2.gro
curl 'http://127.0.0.1:1337/datasets/run1/frames/2?format=json'
curl -H 'Accept: application/vnd.mdsrv.frame+bin' http://127.0.0.1:1337/datasets/run1/frames/2 > frame-2.bin
curl -X POST 'http://127.0.0.1:1337/datasets/run1/frames/index?chunk_size=128'
curl -X POST 'http://127.0.0.1:1337/datasets/run1/frames/chunks?chunk_size=128&encoding=bin-zstd'
curl 'http://127.0.0.1:1337/datasets/run1/frames/chunks/0'
curl 'http://127.0.0.1:1337/datasets/run1/frames/chunks/0?format=raw' > chunk-0.bin.zst
curl 'http://127.0.0.1:1337/datasets/run1/frames/range?start=0&stop=10&stride=2'
curl -X POST 'http://127.0.0.1:1337/jobs' \
  -H 'Content-Type: application/json' \
  -d '{"type":"chunks","dataset_id":"run1","chunk_size":128,"encoding":"bin-zstd","timeout_seconds":600}'
curl -X POST 'http://127.0.0.1:1337/jobs' \
  -H 'Content-Type: application/json' \
  -d '{"type":"analysis","dataset_id":"run1","analysis":{"type":"distance","selections":{"a":"1","b":"2"},"output":"traces/run1-distance.csv"}}'
curl 'http://127.0.0.1:1337/jobs/job_...'
curl 'http://127.0.0.1:1337/jobs/stats'
curl 'http://127.0.0.1:1337/jobs/metrics'
curl 'http://127.0.0.1:1337/jobs/job_.../logs'
curl 'http://127.0.0.1:1337/jobs/job_.../events'
curl -X POST 'http://127.0.0.1:1337/jobs/job_.../cancel'
curl -X POST 'http://127.0.0.1:1337/jobs/job_.../retry'
curl -H "Authorization: Bearer $MDSRV_AUTH_TOKEN" http://127.0.0.1:1337/datasets
```

The same async job API is exposed through the CLI:

```sh
hlmdsrv jobs --server http://127.0.0.1:1337 submit --type chunks --dataset run1 --chunk-size 128 --encoding bin-zstd --wait
hlmdsrv jobs --server http://127.0.0.1:1337 status job_...
hlmdsrv jobs --server http://127.0.0.1:1337 stats
hlmdsrv jobs --server http://127.0.0.1:1337 logs job_...
hlmdsrv jobs --server http://127.0.0.1:1337 events job_...
hlmdsrv jobs --server http://127.0.0.1:1337 cancel job_...
hlmdsrv jobs --server http://127.0.0.1:1337 retry job_... --wait
hlmdsrv jobs prune --store ./mdsrv-data --ttl 24h --status succeeded --status failed --dry-run
```

## Buckets

Core:

- Store initialization and diagnostics.
- Strict diagnostics with cache/static output path checks.
- Known-good demo job generation with `demo create`.
- Canonical committed demo job manifest.
- One-command `self-test` for the local headless workflow.
- Starter job manifest generation with `init job`.
- Named CLI profiles for repeated local, CI, and server defaults.
- Resolved job explanations with `explain JOB.yaml`.
- Manifest-driven `run JOB.yaml` orchestration.
- Non-mutating `run --plan` and `run --dry-run` previews.
- Whole-command, probe/index, and per-analysis timeout controls.
- Durable run reports with artifact checksums and step timings.
- Stable CLI error codes for automation.
- Local and URL topology plus trajectory ingest.
- Checksums, byte counts, creation timestamp, source/provenance fields.
- Headless YAML/JSON manifest.
- Current MDsrv-compatible trajectory and session index files.
- Dataset listing, validation, update, rename, delete, and garbage collection.
- Strict validation for referenced artifacts, output conflicts, backend availability, and topology/trajectory compatibility.
- Named selections with GROMACS index export.
- Metadata-first HTTP server with write APIs, GROMACS-backed frame count, frame ranges, and `.gro`/`.pdb` extraction.
- Server hardening flags for read-only mode, request logging, request timeouts, local path allowlists, remote host allowlists, and frame-range limits.
- Bounded async server job queue for long-running chunk materialization and analysis jobs.
- In-process `serve smoke` checks for CI and release verification.
- Bearer-token auth, request IDs, and structured error codes for server mode.
- Real fixture workflow through `fixtures mdanalysis-adk` when `MDAnalysisTests` is installed.
- Session publishing into `session_index.json`.
- Session listing.
- Batch ingest.
- MDSRVX archive writer.
- MDSRVX archive unpacking.
- Static store publishing and verification for read-only hosts.
- JSON/binary frame responses.
- Distance, angle, dihedral, RMSD, radius of gyration, RMSF, contact, SASA, and hydrogen-bond trace generation where supported by the selected backend.
- Dataset trajectory export/slicing.
- Static frame indexes and materialized JSON, binary, or zstd-compressed binary frame chunks for chunked streaming metadata.
- Mol*/MVS companion scene generation from topology files or extracted trajectory-frame snapshots.
- JSON Schema, OpenAPI, and shell completions.
- Local binary installation with generated MDsrv completions.
- Docker, CI, isolated Python backend CI, GoReleaser/Homebrew release packaging metadata, and a local release verification script.
- Large frame-index and frame-chunk codec benchmarks.
- Explicit warning for non-XTC trajectories, because the current Mol*-based MDsrv streaming-server docs describe XTC as the streaming baseline.

Exhaustive:

- Atom-subset JSON/binary extraction through the Python backend bridge.
- Analysis backends for clustering, PCA/tICA, and interaction fingerprints.
- Chunked object storage and CDN/static publishing with immutable signed chunks.
- Multi-tenant auth, quotas, Zenodo-specific import helpers, and external worker backends.
