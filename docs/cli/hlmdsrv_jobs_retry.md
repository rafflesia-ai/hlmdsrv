## hlmdsrv jobs retry

Retry a terminal job using its original request

```
hlmdsrv jobs retry JOB_ID [flags]
```

### Options

```
  -h, --help                    help for retry
      --interval duration       poll interval for --wait (default 500ms)
      --wait                    wait until the retried job reaches a terminal status
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
