# Changelog

## Unreleased

### Fixed — first dogfood round

The CLI was excluded from the monorepo's dogfood rounds and never adopted its
`toolcli` hardening, so it carried the fleet's known defect taxonomy intact.
Eleven findings, all with regression tests.

- **`--json` now emits a failure envelope.** Every error path previously wrote
  zero bytes to stdout and a human string to stderr, so a consumer told to branch
  on `error.code` got nothing to parse. Failures now write
  `{ok:false, command, error:{code,message,exit_code}, timestamp}` to stdout.
  Without `--json` the old stderr behavior is unchanged.
- **SIGINT and SIGTERM are handled.** There was no signal handling anywhere in
  the CLI: `serve` ignored SIGINT entirely and died only on SIGTERM with a raw
  exit 143, making the documented `canceled`/130 contract unreachable. Both
  signals now shut down gracefully and exit 130.
- **`export --out <one of its own inputs> --force` no longer destroys the input.**
  It truncated the source (a store's own topology) and reported success, leaving
  the dataset failing its own checksum validation. Identity is tested with
  `os.SameFile`, so hardlinks and symlinks are caught, not just literal paths.
- **An existing FIFO, device, or socket is refused as `--out`.** `pack` unlinked
  the pipe and left a regular file in its place at exit 0.
- **A macOS AppleDouble sidecar no longer breaks a valid store.** `ListDatasets`
  globbed `datasets/*.yaml` and hard-failed on the first unparseable match, so
  the `._<name>.yaml` files macOS writes on exFAT/FAT/SMB — where large
  trajectory stores live — made `list datasets` exit 2 and `publish static
  --verify` exit 7. Dotfiles are skipped; a genuinely corrupt manifest still
  fails.
- **`--timeout` is enforced.** An exhausted budget produced a full success report
  at exit 0 because the pure-Go commands never consult the context. It now fails
  with `backend_timeout` before the command runs. A deadline expiring mid-run
  still only interrupts GROMACS subprocess work.
- **`gromacs doctor` verifies identity.** Any binary that exited 0 on
  `--version` passed as GROMACS (`--gmx-command /usr/bin/true` reported
  `available:true`). The "GROMACS version:" banner is now required.
- **Usage and input errors are `invalid_input` (exit 2), not `internal_error`.**
  An unknown flag, unknown subcommand, bad flag value, or a missing/directory
  input previously exited 1, whose documented meaning is "unclassified, report
  it" — so a typo told the caller to file a bug. Adds the `invalid_input` code,
  sharing exit 2 with `invalid_manifest`.
- **`publish static` rejects a store that does not exist** instead of exiting 0
  with `files:null` and creating an empty output directory.
- **`validate <store> --strict` verifies dataset integrity.** It reported
  `ok:true` on a store whose dataset failed its own sha256 check.
- **GROMACS error messages carry the diagnosis, not the banner.** The `Fatal
  error:` section is extracted instead of ~15 lines of executable path, data
  prefix, working dir and command line.

Verified clean in the same round and left alone: concurrent index builds and
ingests, BOM handling, `missing_backend` reporting when no Python backend is
installed, and non-finite `.gro` coordinates (no `NaN` leaks into JSON).

### Fixed — completeness sweep

Auditing the fixes above for sibling code paths showed the guards had only been
wired where they were probed by hand.

- **`frames get`, `pack`, and `debug bundle` still truncated a store's own
  topology** when `--out` named it. They know a dataset id rather than resolved
  input paths, so they need a dataset-aware check.
- **Ten commands could still be pointed at a FIFO**, including `frames get`,
  `bench`, `debug bundle`, and `config --config`. Reading one blocks in `open(2)`
  just as writing one does, so the config path is now screened in both
  directions.
- **A second SIGINT was ignored.** Handling signals turns a process parked in a
  blocking syscall into an unkillable one, because cancellation is cooperative
  and nothing is polling — before the previous change, the default disposition
  would have killed it. A second signal now terminates immediately. Relying on
  `signal.Stop` to restore the default was tried first and does not work: it can
  leave the signal ignored rather than fatal.
- **`export` and the GROMACS bridge fabricated success.** Availability was only a
  PATH lookup, so a stub binary passed and the commands reported an `output` path
  that had never been created. The run paths now require a *verified* GROMACS
  (matching `doctor` and `capabilities`) and verify that the file was actually
  produced. An unusable backend reports `missing_backend` consistently across
  `probe`, `convert`, `extract`, and `export`, where it previously produced a mix
  of exit 0, 1, and 9.
- **`ingest` validates its local `--topology`/`--trajectory`** instead of
  surfacing a directory as `internal_error`.

### Fixed — third-pass audit

A pass over surfaces the first two rounds never touched.

- **A directory passed as a successfully produced file.** A directory stats with
  a non-zero size, so `export --out <existing dir>` reported success against an
  empty directory. Both the produced-file check and the shared output-path guard
  now reject a directory outright.
- **`--store` pointing at a missing path or a regular file read as an empty
  store**: `list datasets` printed `null` and exited 0, indistinguishable from a
  real store with no datasets. It now fails with `missing_input` or
  `invalid_input`.
- **More `internal_error` misclassification**: `publish static --out <regular
  file>`, `pack --out <dir>`, and malformed or empty batch JSONL all exited 1.

### Fixed — seventh pass (units, empty collections)

First round with MDAnalysis installed, so the second Python engine was exercised.

- **The engines report different units.** MDTraj emits `nm`, MDAnalysis emits
  `angstrom`, for the same analysis. Only the CSV carried the unit, so a manifest
  could hold two analyses of one type whose values were an order of magnitude
  apart with nothing at the manifest or report level saying so. Stored analyses
  and the `--json` report now carry `unit` alongside `backend`.
- **Empty lists marshalled to `null`.** `list datasets` and `selection list`
  returned `null` when empty while `session list` returned `[]` — one CLI
  disagreeing with itself about the shape of "nothing", forcing a consumer to
  null-check some list endpoints but not others. All now emit `[]`.

Measured while comparing engines, worth recording: on a demo trajectory
**MDAnalysis and GROMACS agree on rmsd** (0.12910 Å vs 0.01291 nm — the same
value), and **MDTraj is the outlier**, differing by ~2.45×. All three are now
self-describing via `backend` and `unit`.

Probed and found correct: `--profile` precedence is exactly right — an explicit
`--store` beats the profile, `MDSRV_PROFILE` works as an env alternative to
`--profile`, and an unknown profile fails with a message naming both the profile
and the config file it looked in.

### Fixed — sixth pass (analyze bridge, selections)

First round with a real MDTraj install, so the Python analysis bridge was
exercised end to end rather than only through its unavailable path.

- **An argument mistake reported as a missing backend.** Every python-bridge
  error carries a generic `python backend failed:` prefix, and that prefix was
  matched as `missing_backend` — so `analyze sasa` without a selection told the
  caller to install MDTraj while MDTraj was installed and working. Only the
  genuine unavailability signals now map to `missing_backend`; a missing
  selection is `invalid_input`, consistently across all nine analyses.
- **Analyses record which backend produced them.** MDTraj and GROMACS do not
  always compute the same quantity under one name: on a demo trajectory their
  `rgyr` agrees to ~9 significant figures, but their `rmsd` differs by a
  consistent factor of ~2.45 (different mass-weighting and fitting conventions).
  Stored analyses now carry a `backend` field, so a trace can be attributed.
- **Two analyses of one type overwrote each other's trace.** The trace path was
  keyed on the analysis *type* while manifest entries are keyed on the *id*, so
  `--id rmsd-py` and `--id rmsd-gmx` both wrote `traces/<ds>-rmsd.csv`; the
  manifest then held two entries with different backends pointing at one file.
  Paths are now keyed on the id, matching the manifest.
- **`selection save --kind` accepted anything.** An unrecognized kind was stored
  verbatim, leaving a selection nothing could interpret (no `atom_count`, no
  usable dialect). Now validated against the known set.
- **Selection errors leaked Go internals**: a malformed expression surfaced the
  raw `strconv.Atoi: parsing "!!!": invalid syntax`. Now names the offending term
  and its expected form. Out-of-range indices, bad ranges and unknown resolve
  targets are `invalid_input` rather than `internal_error`.

### Fixed — fifth pass (codec, limits, envelope hole)

- **The error envelope could produce two JSON documents.** Commands that write
  their `--json` report and *then* fail (`compat check`, `publish static
  --verify`) had an envelope appended to the report, so stdout carried two
  concatenated documents and failed a consumer's parse outright — worse than the
  original bug of emitting nothing. stdout is now tracked; when a command has
  already reported, the failure goes to stderr and the report (which carries its
  own `"ok": false`) stands alone.
- **A non-positive `--chunk-size` was silently coerced** to "everything in one
  chunk", so a caller asking for specific chunking got something else with no
  signal. Now `invalid_input`.
- **Tripped resource limits reported `internal_error`.** `max_atoms`,
  `max_frames` and `max_chunk_bytes` exist to bound a job, so exceeding one is
  the policy working — now `validation_failed`. Likewise `compat check` failing
  its checks, and `install local --bin-dir <regular file>`.

Verified against ground truth in this pass: all six frames of a demo trajectory
decode to coordinates matching raw `gmx trjconv` exactly, through both the JSON
and the zstd chunk encodings; chunk sizes of 1, 2, 4, 6 and 100 over six frames
all produce correct chunk counts and correct frame data.

### Fixed — fourth pass (orchestration, jobs, lifecycle)

- **The job queue's backpressure response had no typed code.** Every other server
  status maps to one (`bad_request`, `forbidden`, …), but 429 fell through to a
  generic `"error"` and 502 to `"internal_error"` — and 429 is the one response a
  client most needs to branch on, being the retryable one. Now
  `too_many_requests` and `bad_gateway`.
- **Flag-range validation was `internal_error`.** `--frames must be at least 2`,
  `--workers cannot be negative` and the rest of that class exited 1, telling the
  caller to file a bug about their own typo. Now `invalid_input`.
- **A dead `--server` was `internal_error`.** A connection refused is a backend
  that is not reachable, so it now reports `missing_backend` like any other
  unavailable backend.
- **`unpack` on a file that is not a zip** was `internal_error`; now
  `invalid_input` with a message naming the archive.

After these, a sweep of 15 ordinary misuse cases produces **no `internal_error`
at all** — that code is now reserved for genuinely unclassified faults, as the
table says.

Probed and found correct in this pass, so unchanged: `run --plan` is faithful to
what `run` executes (same index path, same chunking); the queue applies real
backpressure (3 accepted / 7 rejected at `--max-queue 2`) rather than blocking;
the jobs lifecycle (status, logs, events, retry, cancel, prune) behaves; and
pack→unpack round-trips with matching frame counts.

Probed and found correct, so deliberately unchanged: `.mdsrvx` archive extraction
rejects path traversal (`../`, absolute, and nested forms) with `unsafe_path` and
creates nothing outside the store; the HTTP server enforces auth (401), read-only
(403), and does not serve store files through a traversed path; eight concurrent
exports to one `--out` produce a valid trajectory.

Initial extraction of the headless MDsrv CLI from
[sacha-ichbiah/headlessmolstar](https://github.com/sacha-ichbiah/headlessmolstar) into its own repo.

### Breaking

- The binary is now `hlmdsrv`, renamed from `mdsrv-headless`. This changes the `service` field in
  `version --json`, `capabilities --json`, and the server `/health` payload; the XDG config path
  (`~/.config/mdsrv-headless/config.yaml` → `~/.config/hlmdsrv/config.yaml`); and the shipped
  completion and `docs/cli/` filenames. Consumers branching on `service` must be updated.
- The Go module is `github.com/rafflesia-ai/hlmdsrv`.
- The job schema file is `schema/hlmdsrv-job-v1.schema.json`, renamed from
  `schema/mdsrv-headless-job-v1.schema.json`. Its contents are unchanged.

### Unchanged contract

The upstream domain vocabulary is deliberately untouched: the `mdsrv.job/v1` manifest version, the
`mdsrv-frames-bin-zstd-v1` chunk encoding, the `.mdsrvx` archive format, and the `MDSRV_*`
environment variables all keep their names.

### Not carried over

- `scripts/verify-artifact-mdsrv.sh` and `scripts/verify-artifact-lib.sh` verified the monorepo's
  npm-packaged artifact layout and depended on `cmd/molstar install-artifact`, which does not exist
  here. Artifact verification for the goreleaser archives is not yet reimplemented.
- The `internal/job` JSON-schema golden test, which asserted against a schema generated by
  `cmd/molstar`.
