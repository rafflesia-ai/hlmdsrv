## hlmdsrv store doctor

Check store layout, version metadata, and migration status

```
hlmdsrv store doctor [flags]
```

### Options

```
  -h, --help           help for doctor
      --init           initialize missing store directories and metadata before checking
      --json           write machine-readable output
      --store string   MDsrv store root (default "./mdsrv-data")
      --strict         fail when any store check fails
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv store](hlmdsrv_store.md)	 - Inspect and maintain an MDsrv store
