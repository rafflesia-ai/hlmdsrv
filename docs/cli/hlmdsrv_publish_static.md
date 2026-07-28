## hlmdsrv publish static

Copy a store into a read-only static directory

```
hlmdsrv publish static [flags]
```

### Options

```
      --force          overwrite existing files
  -h, --help           help for static
      --json           write machine-readable output
  -o, --out string     output directory
      --store string   MDsrv store root (default "./mdsrv-data")
      --verify         verify copied catalogs and referenced artifacts
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv publish](hlmdsrv_publish.md)	 - Publish MDsrv artifacts for deployment
