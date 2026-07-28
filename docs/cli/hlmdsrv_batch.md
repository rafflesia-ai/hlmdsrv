## hlmdsrv batch

Ingest a JSONL/YAML/JSON batch of trajectory datasets

```
hlmdsrv batch JOBS [flags]
```

### Options

```
      --concurrency int     number of ingest jobs to run concurrently (default 1)
      --continue-on-error   continue after failed jobs
      --force               overwrite existing datasets
  -h, --help                help for batch
      --json                write JSON report lines to stdout (default true)
      --store string        MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
