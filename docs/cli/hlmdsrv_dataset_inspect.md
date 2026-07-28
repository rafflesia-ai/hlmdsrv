## hlmdsrv dataset inspect

Inspect dataset files, backend metadata, and frame index status

```
hlmdsrv dataset inspect DATASET_ID [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --gmx-command string   GROMACS command override
  -h, --help                 help for inspect
      --json                 write machine-readable output
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv dataset](hlmdsrv_dataset.md)	 - Manage dataset lifecycle
