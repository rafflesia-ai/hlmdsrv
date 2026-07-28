## hlmdsrv frames get

Get one frame as JSON or MDSF binary

```
hlmdsrv frames get DATASET_ID FRAME_INDEX [flags]
```

### Options

```
      --atom-subset string   override atom subset selection for this frame
      --backend string       trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --format string        output format: json or bin; inferred from --out when omitted
      --gmx-command string   GROMACS fallback command override
  -h, --help                 help for get
  -o, --out string           output path; stdout when omitted
      --store string         MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv frames](hlmdsrv_frames.md)	 - Inspect and extract trajectory frames
