## hlmdsrv gromacs convert

Convert a trajectory-like file with gmx trjconv

```
hlmdsrv gromacs convert INPUT [flags]
```

### Options

```
      --command string       alias for --gmx-command
      --force                overwrite an existing output path
      --gmx-command string   GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi
  -h, --help                 help for convert
      --json                 write machine-readable output
  -o, --out string           output path
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv gromacs](hlmdsrv_gromacs.md)	 - Raw GROMACS bridge helpers
