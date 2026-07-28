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
