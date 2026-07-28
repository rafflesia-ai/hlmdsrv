## hlmdsrv pack

Pack a dataset into a .mdsrvx archive

```
hlmdsrv pack DATASET_ID [flags]
```

### Options

```
      --force          overwrite existing archive
  -h, --help           help for pack
      --json           write machine-readable output
  -o, --out string     output .mdsrvx path
      --store string   MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
