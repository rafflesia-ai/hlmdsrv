## hlmdsrv probe

Refresh trajectory metadata with GROMACS

```
hlmdsrv probe DATASET_ID [flags]
```

### Options

```
      --gmx-command string   GROMACS command override
  -h, --help                 help for probe
      --json                 write machine-readable output
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
