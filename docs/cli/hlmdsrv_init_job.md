## hlmdsrv init job

Create a starter mdsrv.job/v1 manifest

```
hlmdsrv init job [flags]
```

### Options

```
      --chunk-size int          default streaming chunk size (default 128)
      --chunks                  request materialized static frame chunks
      --force                   overwrite an existing output file
  -h, --help                    help for job
      --id string               dataset id; defaults to the trajectory filename stem
      --json                    write machine-readable output
      --name string             dataset display name
  -o, --out string              output job manifest path (default "mdsrv.job.yaml")
      --topology string         topology file path
      --topology-url string     topology URL
      --trajectory string       trajectory file path
      --trajectory-url string   trajectory URL
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv init](hlmdsrv_init.md)	 - Initialize an MDsrv store
