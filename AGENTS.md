# Agent Instructions

- Store expensive generated/runtime artifacts under `/Volumes/ExtremeSSD/hlmdsrv/`.
- Use repo-local symlinks only when tools require the original path. `bin` and `outputs` are
  symlinks into that directory.
- Everything here is reproducible from source: `go build -o bin/hlmdsrv ./cmd/hlmdsrv`. There is no
  npm/node dependency — the analysis backend is the embedded
  `internal/mdsrv/scripts/mdsrv_backend.py`, which soft-imports MDTraj or MDAnalysis at runtime.

## Git workflow

- Single-committer play repo — **no branches, no pull requests**.
- Commit and push directly to `main`. This overrides the usual "branch before committing to the
  default branch" default; stay on `main` and push there.

## Repo scope

This repo is the headless MDsrv dataset/session CLI only. It was carved out of `headlessmolstar`,
which retains the standalone structural-biology tool CLIs — don't add those back here. The headless
Mol\* renderer from the same monorepo lives at `rafflesia-ai/molstar`.

`internal/job` and `internal/mvs` are shared vocabulary packages inherited from the monorepo. mdsrv
uses them for their **types only** (`job.Job`, `job.Input`, `job.Scene`, …). `job.JSONSchema()` and
`job.ValidateSchemaBytes()` describe the *molstar* render-job schema and are unreachable from any
hlmdsrv command — their golden test was dropped in the carve-out because the generator
(`cmd/molstar job schema`) does not exist here. Treat them as dead weight, not as contract.

## Naming and contract surface

The binary is `hlmdsrv`; the Go module is `github.com/rafflesia-ai/hlmdsrv`.

The carve-out **deliberately renamed** the CLI from `mdsrv-headless` to `hlmdsrv`. This was a
breaking change, not a cosmetic one — it moved:

- the `service` field in `version --json`, `capabilities --json`, and the server `/health` payload,
- the XDG config path (`~/.config/mdsrv-headless/config.yaml` → `~/.config/hlmdsrv/config.yaml`),
- the shipped completion and `docs/cli/` filenames.

Anything still reading `mdsrv` is the **upstream domain name and is contract** — do not rename it:
the `mdsrv.job/v1` manifest version, the `mdsrv-frames-bin-zstd-v1` chunk encoding, the `.mdsrvx`
archive format, and the `MDSRV_*` environment variables (`MDSRV_PYTHON`, `MDSRV_GMX`,
`MDSRV_PROFILE`).

## Generated assets

`docs/cli/**` and `completions/**` are generated from the binary and verified in CI. After changing
any command, flag, or help string, regenerate them or CI fails on drift:

```sh
go build -o bin/hlmdsrv ./cmd/hlmdsrv
bin/hlmdsrv docs --out docs/cli
perl -0pi -e 's/\n+\z/\n/' docs/cli/*.md
bin/hlmdsrv completion bash --out completions/hlmdsrv.bash --force
bin/hlmdsrv completion zsh  --out completions/_hlmdsrv     --force
bin/hlmdsrv completion fish --out completions/hlmdsrv.fish --force
```

The frozen JSON contracts live in `internal/mdsrvcli/testdata/contracts/` and are checked by
`scripts/verify-mdsrv-contracts.sh`.
