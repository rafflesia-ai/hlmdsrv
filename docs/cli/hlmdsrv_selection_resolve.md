## hlmdsrv selection resolve

Resolve a saved selection into a target backend dialect

```
hlmdsrv selection resolve DATASET_ID SELECTION_OR_EXPR [flags]
```

### Options

```
  -h, --help            help for resolve
      --json            write machine-readable output
      --store string    MDsrv store root (default "./mdsrv-data")
      --target string   target dialect: gromacs, mdtraj, mdanalysis, python, mvs (default "gromacs")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv selection](hlmdsrv_selection.md)	 - Manage named dataset selections
