## hlmdsrv jobs submit

Submit an async chunking or analysis job

```
hlmdsrv jobs submit [flags]
```

### Options

```
      --a string                analysis selection a
      --analysis-id string      analysis id
      --analysis-type string    analysis type: distance, angle, dihedral, rmsd, rmsf, radius-of-gyration, contacts
      --b string                analysis selection b
      --backend string          analysis backend override
      --c string                analysis selection c
      --chunk-size int          chunk size in frames for chunks jobs
      --d string                analysis selection d
      --dataset string          dataset id
      --encoding string         chunk encoding: json, bin, or bin-zstd
      --force                   overwrite existing output
      --format string           analysis output format
  -h, --help                    help for submit
      --interval duration       poll interval for --wait (default 500ms)
      --out string              analysis output path
      --request string          JSON file containing the full job request
      --selection string        single analysis selection
      --timeout-seconds int     per-job timeout in seconds
      --type string             job type: chunks or analysis
      --wait                    wait until the job reaches a terminal status
      --wait-timeout duration   maximum time to wait; 0 means use command context
```

### Options inherited from parent commands

```
      --json               write machine-readable output
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --server string      MDsrv server URL; defaults to MDSRV_SERVER_URL or http://127.0.0.1:1337
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
      --token string       bearer token or X-MDSRV-Token value; defaults to MDSRV_AUTH_TOKEN
```

### SEE ALSO

* [hlmdsrv jobs](hlmdsrv_jobs.md)	 - Submit and inspect async server jobs
