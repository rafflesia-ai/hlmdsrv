## hlmdsrv gromacs extract

Extract one frame from raw topology and trajectory files

```
hlmdsrv gromacs extract [flags]
```

### Options

```
      --command string       alias for --gmx-command
      --force                overwrite an existing output path
      --frame int            zero-based frame index; mapped to time with gmx check (default -1)
      --gmx-command string   GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi
  -h, --help                 help for extract
      --json                 write machine-readable output
  -o, --out string           output frame path; defaults to TRAJECTORY-frame-N.gro or TRAJECTORY-time-T.gro
      --time string          trajectory time passed directly to -dump
      --topology string      topology/reference structure path passed to -s
      --trajectory string    trajectory path passed to -f
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv gromacs](hlmdsrv_gromacs.md)	 - Raw GROMACS bridge helpers
