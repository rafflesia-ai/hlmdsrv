## hlmdsrv selection save

Save a named selection

```
hlmdsrv selection save DATASET_ID [flags]
```

### Options

```
      --description string   selection description
      --expr string          selection expression
      --expression string    selection expression
  -h, --help                 help for save
      --id string            selection id
      --json                 write machine-readable output
      --kind string          selection kind: atom-index, mdtraj, mdanalysis, mvs (default "atom-index")
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv selection](hlmdsrv_selection.md)	 - Manage named dataset selections
