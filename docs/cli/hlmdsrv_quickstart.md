## hlmdsrv quickstart

Create, run, publish, and summarize a tiny MDsrv dataset

```
hlmdsrv quickstart [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --force                overwrite quickstart artifacts (default true)
      --frames int           number of demo trajectory frames (default 5)
      --gmx-command string   GROMACS command override
  -h, --help                 help for quickstart
      --id string            dataset id (default "quickstart")
      --json                 write machine-readable output (default true)
      --out string           quickstart output directory (default "outputs/mdsrv-quickstart")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
