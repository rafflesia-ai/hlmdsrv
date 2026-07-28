## hlmdsrv run

Run an end-to-end MDsrv headless job manifest

```
hlmdsrv run JOB.yaml [flags]
```

### Options

```
      --analysis-timeout duration   timeout for each analysis step; 0 uses the command context
      --backend string              trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --cache string                download cache directory for URL inputs
      --chunk-encoding string       chunk encoding override: json, bin, or bin-zstd; defaults to job streaming.encoding or json
      --chunks                      materialize static frame chunks after indexing
      --dry-run                     alias for --plan with dry_run=true in JSON output
      --force                       overwrite existing datasets and generated outputs
      --gmx-command string          GROMACS command override
  -h, --help                        help for run
      --index                       build a frame index after ingest (default true)
      --json                        write machine-readable output (default true)
      --max-atoms int               fail if the dataset or decoded frame exceeds this atom count; 0 uses runtime.max_atoms
      --max-chunk-bytes int         fail if an encoded chunk exceeds this byte count; 0 uses runtime.max_chunk_bytes
      --max-frames int              fail if the dataset exceeds this frame count; 0 uses runtime.max_frames
      --plan                        print the job steps without touching the store
      --probe                       probe trajectory metadata after ingest (default true)
      --probe-timeout duration      timeout for probe/index steps; 0 uses the command context
      --report string               write a durable JSON run report to this path
      --store string                MDsrv store root (default "./mdsrv-data")
      --strict                      fail if optional job artifacts such as sessions are referenced but missing
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
