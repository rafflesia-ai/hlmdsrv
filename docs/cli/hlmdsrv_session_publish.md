## hlmdsrv session publish

Publish a Mol* session/state file into the MDsrv store

```
hlmdsrv session publish [flags]
```

### Options

```
      --dataset string       dataset id
      --description string   session description
      --file string          session file, usually .molj
      --force                overwrite existing session file
  -h, --help                 help for publish
      --id string            session id
      --json                 write machine-readable output
      --name string          display name
      --source string        source URL, DOI, or provenance label
      --sticky               mark as sticky in session_index.json
      --store string         MDsrv store root (default "./mdsrv-data")
      --version string       MDsrv viewer version (default "3.4.0")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv session](hlmdsrv_session.md)	 - Manage published MDsrv sessions
