## hlmdsrv fixtures mdanalysis-adk

Ingest the real AdK GRO/XTC fixture from MDAnalysisTests

```
hlmdsrv fixtures mdanalysis-adk [flags]
```

### Options

```
      --force                overwrite existing fixture dataset
      --gmx-command string   GROMACS command override
  -h, --help                 help for mdanalysis-adk
      --id string            dataset id (default "mdanalysis-adk")
      --json                 write machine-readable output (default true)
      --probe                probe fixture with GROMACS after ingest (default true)
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv fixtures](hlmdsrv_fixtures.md)	 - Fetch or ingest known trajectory fixtures
