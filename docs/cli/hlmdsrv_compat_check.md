## hlmdsrv compat check

Check store layout and optionally smoke-test mdsrv-remote Docker

```
hlmdsrv compat check [flags]
```

### Options

```
      --docker             run upstream mdsrv-remote container smoke test
  -h, --help               help for check
      --image string       Docker image for upstream streaming server (default "dwiegreffe/mdsrv-remote")
      --json               write machine-readable output
      --port int           temporary host port for Docker smoke test (default 18087)
      --store string       MDsrv store root (default "./mdsrv-data")
      --timeout duration   Docker smoke test timeout (default 30s)
```

### Options inherited from parent commands

```
      --profile string   load defaults from a named config profile or MDSRV_PROFILE
```

### SEE ALSO

* [hlmdsrv compat](hlmdsrv_compat.md)	 - Check upstream MDsrv compatibility
