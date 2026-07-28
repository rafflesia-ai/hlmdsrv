## hlmdsrv doctor

Check local MDsrv headless prerequisites

```
hlmdsrv doctor [flags]
```

### Options

```
      --cache string         optional cache directory to check
      --gmx-command string   GROMACS command override
  -h, --help                 help for doctor
      --json                 write machine-readable output
      --static-out string    optional static publish output directory to check
      --store string         optional store path to check
      --strict               fail when required checks or GROMACS are unavailable
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
