## hlmdsrv dataset delete

Delete a dataset manifest and optionally its files

```
hlmdsrv dataset delete DATASET_ID [flags]
```

### Options

```
      --files          also delete topology, trajectory, indexes, and traces referenced by the dataset
  -h, --help           help for delete
      --json           write machine-readable output
      --store string   MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv dataset](hlmdsrv_dataset.md)	 - Manage dataset lifecycle
