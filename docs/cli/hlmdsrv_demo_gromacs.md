## hlmdsrv demo gromacs

Create a tiny real GROMACS .gro/.xtc trajectory

```
hlmdsrv demo gromacs [flags]
```

### Options

```
      --force                overwrite generated files and an existing ingested demo dataset (default true)
      --frames int           number of frames to generate (default 5)
      --gmx-command string   GROMACS command override
  -h, --help                 help for gromacs
      --id string            dataset id (default "demo-gromacs")
      --json                 write machine-readable output
      --name string          dataset name (default "GROMACS demo trajectory")
      --out string           output directory for generated files (default "outputs/mdsrv-gromacs-demo")
      --store string         optional store root to ingest into
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv demo](hlmdsrv_demo.md)	 - Create small real datasets for local testing
