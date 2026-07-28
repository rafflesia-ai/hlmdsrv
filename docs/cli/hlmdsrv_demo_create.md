## hlmdsrv demo create

Create a tiny trajectory and runnable job manifest

```
hlmdsrv demo create [flags]
```

### Options

```
      --force                overwrite generated files and job manifest (default true)
      --frames int           number of frames to generate (default 5)
      --gmx-command string   GROMACS command override
  -h, --help                 help for create
      --id string            dataset id (default "demo")
      --job string           job manifest path; relative paths are resolved under --out (default "job.yaml")
      --json                 write machine-readable output
      --name string          dataset name (default "MDsrv demo trajectory")
      --out string           output directory for generated files (default "outputs/mdsrv-demo")
      --store string         optional store root to ingest into
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv demo](hlmdsrv_demo.md)	 - Create small real datasets for local testing
