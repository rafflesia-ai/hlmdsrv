## hlmdsrv jobs

Submit and inspect async server jobs

### Options

```
  -h, --help            help for jobs
      --json            write machine-readable output
      --server string   MDsrv server URL; defaults to MDSRV_SERVER_URL or http://127.0.0.1:1337
      --token string    bearer token or X-MDSRV-Token value; defaults to MDSRV_AUTH_TOKEN
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
* [hlmdsrv jobs cancel](hlmdsrv_jobs_cancel.md)	 - Cancel a queued or running job
* [hlmdsrv jobs events](hlmdsrv_jobs_events.md)	 - Print structured job events as JSON Lines
* [hlmdsrv jobs list](hlmdsrv_jobs_list.md)	 - List async jobs
* [hlmdsrv jobs logs](hlmdsrv_jobs_logs.md)	 - Print job logs
* [hlmdsrv jobs prune](hlmdsrv_jobs_prune.md)	 - Prune persisted local job records
* [hlmdsrv jobs retry](hlmdsrv_jobs_retry.md)	 - Retry a terminal job using its original request
* [hlmdsrv jobs stats](hlmdsrv_jobs_stats.md)	 - Show server job queue statistics
* [hlmdsrv jobs status](hlmdsrv_jobs_status.md)	 - Show job status
* [hlmdsrv jobs submit](hlmdsrv_jobs_submit.md)	 - Submit an async chunking or analysis job
* [hlmdsrv jobs wait](hlmdsrv_jobs_wait.md)	 - Wait for a job to finish
