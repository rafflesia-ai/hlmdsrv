## hlmdsrv index chunks

Materialize static frame chunks

```
hlmdsrv index chunks DATASET_ID [flags]
```

### Options

```
      --chunk-size int        frames per static chunk (default 128)
      --encoding string       chunk encoding: json, bin, or bin-zstd (default "json")
      --force                 overwrite existing chunk files
      --gmx-command string    GROMACS command override
  -h, --help                  help for chunks
      --json                  write machine-readable output
      --max-atoms int         fail if the dataset or decoded frame exceeds this atom count; 0 disables
      --max-chunk-bytes int   fail if an encoded chunk exceeds this byte count; 0 disables
      --max-frames int        fail if the dataset exceeds this frame count; 0 disables
      --store string          MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv index](hlmdsrv_index.md)	 - Build static frame indexes
