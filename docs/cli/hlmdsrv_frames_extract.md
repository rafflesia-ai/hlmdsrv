## hlmdsrv frames extract

Extract one frame with GROMACS

```
hlmdsrv frames extract DATASET_ID FRAME_INDEX [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --gmx-command string   GROMACS command override
  -h, --help                 help for extract
      --json                 write machine-readable output
  -o, --out string           output frame file; extension controls GROMACS output format
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv frames](hlmdsrv_frames.md)	 - Inspect and extract trajectory frames
