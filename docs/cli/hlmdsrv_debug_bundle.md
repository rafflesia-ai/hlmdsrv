## hlmdsrv debug bundle

Write a small zip archive with store, backend, validation, and server diagnostics

```
hlmdsrv debug bundle DATASET_ID [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --deep                 decode trajectory metadata during validation when a Python backend is available
      --gmx-command string   GROMACS command override
  -h, --help                 help for bundle
      --json                 write machine-readable output
      --log-bytes int        maximum bytes kept from each job.log (default 65536)
      --max-file-bytes int   maximum store metadata file bytes copied into the bundle (default 2097152)
      --max-logs int         maximum recent job log directories to include (default 5)
  -o, --out string           output zip path; defaults to DATASET_ID-debug-bundle.zip
      --skip-smoke           skip in-process HTTP serve smoke diagnostics
      --store string         MDsrv store root (default "./mdsrv-data")
      --strict               treat optional missing artifacts and unavailable requested backends as validation errors
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv debug](hlmdsrv_debug.md)	 - Collect MDsrv headless diagnostics
