## hlmdsrv export

Export a trajectory slice with GROMACS

```
hlmdsrv export DATASET_ID [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --force                overwrite existing output
      --format string        output format; inferred from --out
      --frames string        frame/time range START:STOP:STRIDE using frame indexes
      --gmx-command string   GROMACS command override
      --group string         GROMACS group name/index to pass to trjconv (default "0")
  -h, --help                 help for export
      --json                 write machine-readable output
  -o, --out string           output trajectory path
      --selection string     1-based atom-index selection; writes a temporary index file
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
