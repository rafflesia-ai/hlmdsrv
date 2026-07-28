## hlmdsrv unpack

Unpack a .mdsrvx archive into a store

```
hlmdsrv unpack ARCHIVE.mdsrvx [flags]
```

### Options

```
      --force          overwrite existing store files
  -h, --help           help for unpack
      --json           write machine-readable output
      --store string   MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
