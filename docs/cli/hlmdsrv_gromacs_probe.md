## hlmdsrv gromacs probe

Probe trajectory metadata with gmx check

```
hlmdsrv gromacs probe TRAJECTORY [flags]
```

### Options

```
      --command string       alias for --gmx-command
      --gmx-command string   GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi
  -h, --help                 help for probe
      --json                 write machine-readable output
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv gromacs](hlmdsrv_gromacs.md)	 - Raw GROMACS bridge helpers
