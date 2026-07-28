## hlmdsrv serve

Serve a headless MDsrv store over HTTP

```
hlmdsrv serve [flags]
```

### Options

```
      --allow-host stringArray     allow remote ingest URLs from this host; repeatable
      --allow-path stringArray     allow local ingest/session paths under this directory; repeatable
      --auth-token string          require this bearer token or X-MDSRV-Token value for HTTP API requests; defaults to MDSRV_AUTH_TOKEN
      --backend string             trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --gmx-command string         GROMACS command override
  -h, --help                       help for serve
      --host string                listen host (default "127.0.0.1")
      --job-prune-on-start         prune old terminal job records before starting workers
      --job-timeout duration       per async job timeout, for example 10m; 0 disables
      --job-ttl duration           age threshold for --job-prune-on-start; 0 prunes all terminal jobs (default 168h0m0s)
      --log-requests               write structured JSON request logs to stderr
      --max-atoms int              maximum dataset/frame atom count for index, chunks, and frame range operations; 0 disables
      --max-chunk-bytes int        maximum encoded chunk size in bytes; 0 disables
      --max-frame-range int        maximum number of frames returned by one HTTP frame range request (default 256)
      --max-frames int             maximum dataset frame count for index and chunks operations; 0 disables
      --max-queue int              maximum queued async jobs when --workers is enabled (default 64)
      --port int                   listen port (default 1337)
      --read-only                  reject HTTP requests that mutate datasets, selections, indexes, analyses, or sessions
      --request-timeout duration   per-request timeout, for example 30s; 0 disables the timeout wrapper
      --store string               MDsrv store root (default "./mdsrv-data")
      --workers int                background workers for /jobs chunking and analysis requests; 0 disables async jobs
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
* [hlmdsrv serve smoke](hlmdsrv_serve_smoke.md)	 - Start the HTTP handler in-process and verify key routes
