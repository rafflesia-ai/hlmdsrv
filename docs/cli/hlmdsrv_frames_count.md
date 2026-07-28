## hlmdsrv frames count

Print trajectory frame count

```
hlmdsrv frames count DATASET_ID [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --gmx-command string   GROMACS command override
  -h, --help                 help for count
      --json                 write machine-readable output
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv frames](hlmdsrv_frames.md)	 - Inspect and extract trajectory frames
