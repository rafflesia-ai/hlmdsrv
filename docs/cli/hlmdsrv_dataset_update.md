## hlmdsrv dataset update

Update dataset metadata

```
hlmdsrv dataset update DATASET_ID [flags]
```

### Options

```
      --description string   dataset description
  -h, --help                 help for update
      --json                 write machine-readable output
      --license string       dataset license
      --name string          dataset name
      --source string        source URL, DOI, or label
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv dataset](hlmdsrv_dataset.md)	 - Manage dataset lifecycle
