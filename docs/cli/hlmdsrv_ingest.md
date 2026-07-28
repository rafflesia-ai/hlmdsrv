## hlmdsrv ingest

Add a topology and trajectory to a headless MDsrv store

```
hlmdsrv ingest [TOPOLOGY] [TRAJECTORY] [flags]
```

### Options

```
      --atom-subset string       atom subset expression hint
      --cache string             download cache directory
      --coordinate-unit string   trajectory coordinate unit (default "nm")
      --created-by string        creator or pipeline name
      --description string       dataset description
      --force                    overwrite an existing dataset with the same id
      --gmx-command string       GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi
  -h, --help                     help for ingest
      --id string                dataset id; defaults to the trajectory filename stem
      --json                     write machine-readable output
      --license string           dataset license
      --max-atoms int            fail after probe if the dataset exceeds this atom count; 0 disables
      --max-frames int           fail after probe if the dataset exceeds this frame count; 0 disables
      --name string              display name
      --probe                    probe trajectory metadata with GROMACS (default true)
      --source string            source URL, DOI, or provenance label
      --store string             MDsrv store root (default "./mdsrv-data")
      --stride int               frame stride hint
      --time-unit string         trajectory time unit (default "ps")
      --topology string          topology file path
      --topology-url string      topology URL to download before ingest
      --trajectory string        trajectory file path
      --trajectory-url string    trajectory URL to download before ingest
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
