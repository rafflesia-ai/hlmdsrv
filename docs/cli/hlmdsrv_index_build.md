## hlmdsrv index build

Build a JSON frame/chunk index

```
hlmdsrv index build DATASET_ID [flags]
```

### Options

```
      --chunk-size int       frames per logical chunk (default 128)
      --gmx-command string   GROMACS command override
  -h, --help                 help for build
      --json                 write machine-readable output
      --max-atoms int        fail if the dataset exceeds this atom count; 0 disables
      --max-frames int       fail if the dataset exceeds this frame count; 0 disables
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv index](hlmdsrv_index.md)	 - Build static frame indexes
