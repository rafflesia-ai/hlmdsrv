## hlmdsrv explain

Explain a concept or resolve a job manifest plan

```
hlmdsrv explain TOPIC_OR_JOB [flags]
```

### Options

```
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --cache string         download cache directory override
      --gmx-command string   GROMACS command override used by planned backend steps
  -h, --help                 help for explain
      --json                 write machine-readable output
      --store string         MDsrv store root used for planned outputs (default "./mdsrv-data")
      --strict               fail when the explained job has missing inputs
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
