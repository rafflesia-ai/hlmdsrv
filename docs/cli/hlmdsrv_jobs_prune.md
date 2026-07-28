## hlmdsrv jobs prune

Prune persisted local job records

```
hlmdsrv jobs prune [flags]
```

### Options

```
      --dry-run          report matching jobs without deleting them
  -h, --help             help for prune
      --status strings   job statuses to prune; repeat or comma-separate (default [succeeded,failed,canceled])
      --store string     MDsrv store root (default "./mdsrv-data")
      --ttl duration     remove jobs older than this duration; 0 removes all matching statuses (default 24h0m0s)
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
