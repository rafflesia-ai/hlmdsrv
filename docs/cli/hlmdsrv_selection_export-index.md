## hlmdsrv selection export-index

Export an atom-index selection as a GROMACS .ndx file

```
hlmdsrv selection export-index DATASET_ID SELECTION_ID [flags]
```

### Options

```
      --force          overwrite existing output
  -h, --help           help for export-index
  -o, --out string     output .ndx path
      --store string   MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv selection](hlmdsrv_selection.md)	 - Manage named dataset selections
