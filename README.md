# hlmdsrv — headless MDsrv, built for agents

[MDsrv](https://github.com/nglviewer/mdsrv) serves molecular dynamics trajectories to a browser
viewer. This is a CLI for the part that has no GUI: turning raw topology and trajectory files into a
versioned, indexed, servable dataset store — and doing it without a viewer, a display, or a human.

It exists because the tedious half of MD visualization is not the picture. It's ingesting an XTC,
working out how many frames it really has, chunking it so a client can seek, keeping a manifest
honest across reruns, and publishing something a server can actually read. Agents are increasingly
the ones doing that, so every command speaks JSON, fails with a branchable code, and can check its
own environment before asking a human.

```bash
hlmdsrv ingest --topology structure.gro --trajectory traj.xtc --id run1 --store ./mdsrv-data --json
```

## Why an agent can actually use this

**Every command speaks JSON.** `--json` (or `--format json`) returns a structured envelope on stdout;
stderr is for progress and diagnostics. Never parse a human-readable string.

**Failures carry a stable code.** Errors report `error.code` and a distinct exit status. Branch on the
code, never on the message text:

| `code` | Exit | Retryable | What it means |
| --- | --- | --- | --- |
| `invalid_manifest` | 2 | no | The job manifest is malformed or failed schema validation |
| `missing_input` | 3 | no | A topology, trajectory, dataset, or required flag is absent |
| `missing_backend` | 4 | yes | GROMACS or the Python backend is not installed — run `doctor` |
| `backend_timeout` | 5 | yes | The backend exceeded `--probe-timeout` / `--analysis-timeout` |
| `unsafe_path` | 6 | no | A path escaped the store root or an allowed directory |
| `validation_failed` | 7 | no | Store or artifact verification failed |
| `conflict` | 8 | no | The target already exists — pass `--force` if overwriting is intended |
| `render_failed` | 9 | yes | Visualization or MVS scene generation failed |
| `canceled` | 130 | no | The context deadline or a signal ended the run |
| `internal_error` | 1 | no | Unclassified — capture the envelope and report it |

**Cost a run before paying for it.** `run --plan` and `run --dry-run` resolve inputs, outputs, and
backend commands without touching GROMACS, so a plan can be validated for free. `explain` does the
same for a concept or a manifest.

**It diagnoses itself.** `doctor` checks prerequisites and grades them required vs. optional,
`self-test` runs an end-to-end smoke over a synthetic dataset, and `debug bundle` writes a zip to
hand to a human or another environment.

The frozen automation contract — stable command, JSON, archive, and Docker surfaces that scripts may
depend on — is [docs/mdsrv-v0-contract.md](docs/mdsrv-v0-contract.md).

## Quickstart

Requires Go 1.22+. GROMACS and Python are optional and only needed for real trajectory work.

```bash
go build -o bin/hlmdsrv ./cmd/hlmdsrv
bin/hlmdsrv doctor --json
bin/hlmdsrv self-test --json
```

No trajectory data on hand? Everything below runs against a generated fixture:

```bash
bin/hlmdsrv quickstart --out outputs/quickstart
```

## The agent loop

1. `hlmdsrv doctor --json` — once per environment; grades GROMACS and the Python backend.
2. `hlmdsrv validate JOB --json` — cheap, catches manifest shape errors.
3. `hlmdsrv explain JOB --json` — resolves what *would* happen.
4. `hlmdsrv run JOB --plan --json` — the resolved plan, still without touching the backend.
5. `hlmdsrv run JOB --store ./mdsrv-data --report run-report.json --json` — the actual work.
6. `hlmdsrv store doctor --json` / `hlmdsrv validate --store ...` — verify what landed.
7. `hlmdsrv debug bundle` when something breaks.

## A job

```yaml
version: mdsrv.job/v1
metadata:
  id: run1
inputs:
  topology:
    path: topology/run1.gro
    format: gro
  trajectories:
    - path: trajectory/run1.xtc
      format: xtc
      time_unit: ps
      coordinate_unit: nm
processing:
  stride: 10
  atom_subset: not water and not hydrogen
streaming:
  encoding: mdsrv-frames-bin-zstd-v1
  chunk_size_frames: 128
outputs:
  - type: manifest
    path: datasets/run1.yaml
  - type: server-store
    path: .
```

`runtime.max_atoms`, `runtime.max_frames`, and `runtime.max_chunk_bytes` bound a job for CI or
untrusted input. A fuller manifest, with analyses and visualization, is in
[examples/mdsrv.job.yaml](examples/mdsrv.job.yaml).

Full schema: `hlmdsrv schema job`, or
[schema/hlmdsrv-job-v1.schema.json](schema/hlmdsrv-job-v1.schema.json). Working examples are in
[examples/](examples/).

## Command surface

| | |
| --- | --- |
| **Ingest** | `ingest`, `batch`, `run`, `probe`, `export`, `fixtures`, `demo` |
| **Store** | `init`, `store`, `dataset`, `list`, `selection`, `session`, `validate` |
| **Index** | `index`, `frames`, `pack`, `unpack`, `publish` |
| **Analyze** | `analyze`, `visualize`, `bench` |
| **Understand** | `explain`, `capabilities`, `schema`, `docs` |
| **Diagnose** | `doctor`, `self-test`, `debug`, `compat` |
| **Serve** | `serve`, `jobs` |
| **Install** | `install`, `config`, `version`, `completion` |

Generated per-command reference lives in [docs/cli/](docs/cli/).

## Analysis

`analyze` bridges to a Python backend for trajectory observables — RMSD, RMSF, radius of gyration,
SASA, hydrogen bonds, contacts, distances, angles, dihedrals:

```bash
hlmdsrv analyze rmsd run1 --store ./mdsrv-data --json
```

The backend auto-detects [MDTraj](https://www.mdtraj.org) or
[MDAnalysis](https://www.mdanalysis.org), whichever is importable. Point `MDSRV_PYTHON` at a specific
interpreter to control which environment is used. Neither library is required to build or to run the
store, index, and serve paths.

## Server mode

```bash
hlmdsrv serve --store ./mdsrv-data --read-only
```

`GET /health`, `GET /capabilities`, `GET /datasets`, frame and chunk endpoints for seeking, and
`GET /jobs/{job_id}/events` for streaming async work. Discover the contract with `hlmdsrv schema openapi`,
and check a running server with `hlmdsrv serve smoke --url ... --json`.

## GROMACS

The GROMACS bridge is optional and isolated behind `hlmdsrv gromacs`:

```bash
hlmdsrv gromacs doctor
hlmdsrv gromacs probe traj.xtc
hlmdsrv gromacs convert frames.gro --out traj.xtc
```

Set `--gmx-command` (or the profile equivalent) if `gmx` is not on `PATH`. Commands that need it fail
with `missing_backend`, never a silent wrong answer.

## Docker

```bash
docker build -t hlmdsrv:local .
docker run --rm hlmdsrv:local doctor --json
```

The image bundles the CLI, Python 3, MDTraj, and MDAnalysis. GROMACS is not included — mount a host
install or extend the image if you need the GROMACS bridge.

## Provenance

Extracted from [sacha-ichbiah/headlessmolstar](https://github.com/sacha-ichbiah/headlessmolstar),
which retains a fleet of standalone structural-biology tool CLIs. The headless Mol\* renderer carved
out of the same monorepo lives at [rafflesia-ai/molstar](https://github.com/rafflesia-ai/molstar).
MDsrv itself is a separate upstream project — this repo is a headless dataset layer for that
ecosystem, not a fork of it.
