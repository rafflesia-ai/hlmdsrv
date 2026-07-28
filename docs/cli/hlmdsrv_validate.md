## hlmdsrv validate

Validate a manifest or store dataset

```
hlmdsrv validate MANIFEST_OR_DATASET_ID [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --deep                 decode trajectory metadata with mdtraj or MDAnalysis
      --gmx-command string   GROMACS fallback command override
  -h, --help                 help for validate
      --json                 write machine-readable output
      --store string         store root; when set, argument is a dataset id
      --strict               fail on missing optional artifacts, output conflicts, and unavailable requested backends
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
