## hlmdsrv bench

Run synthetic MDsrv frame chunk benchmarks

```
hlmdsrv bench [flags]
```

### Options

```
      --atoms int        synthetic atom count (default 1024)
      --frames int       synthetic frame count (default 128)
  -h, --help             help for bench
      --iterations int   iterations per codec (default 3)
      --json             write machine-readable output
      --out string       write benchmark report JSON to this path
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
