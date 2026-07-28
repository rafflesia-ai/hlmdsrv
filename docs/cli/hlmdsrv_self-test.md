## hlmdsrv self-test

Run local MDsrv headless smoke checks

```
hlmdsrv self-test [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --force                overwrite self-test quickstart artifacts (default true)
      --frames int           number of quickstart trajectory frames (default 4)
      --gmx-command string   GROMACS command override
  -h, --help                 help for self-test
      --json                 write machine-readable output (default true)
      --out string           self-test output directory; defaults to a temporary directory
      --out-dir string       self-test output directory; alias for --out
      --quickstart           run the full GROMACS quickstart path when GROMACS is available (default true)
      --require-gromacs      fail when the quickstart path cannot run because GROMACS is unavailable
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
