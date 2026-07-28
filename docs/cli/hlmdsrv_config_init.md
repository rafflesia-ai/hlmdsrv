## hlmdsrv config init

Create or update a CLI profile

```
hlmdsrv config init [flags]
```

### Options

```
      --auth-token string    default server auth token
      --backend string       default backend: auto, python, or gromacs (default "auto")
      --cache string         default download cache
      --config string        config file path; defaults to $XDG_CONFIG_HOME/hlmdsrv/config.yaml
      --force                overwrite an existing profile
      --gmx-command string   default GROMACS command
  -h, --help                 help for init
      --job-prune-on-start   default serve startup pruning behavior
      --job-ttl duration     default serve job TTL for startup pruning (default 168h0m0s)
      --json                 write machine-readable output
      --profile string       profile name (default "local")
      --store string         default store root (default "./mdsrv-data")
      --timeout duration     default command timeout
```

### SEE ALSO

* [hlmdsrv config](hlmdsrv_config.md)	 - Manage MDsrv headless CLI profiles
