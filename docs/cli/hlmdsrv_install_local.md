## hlmdsrv install local

Build and install hlmdsrv plus shell completions

```
hlmdsrv install local [flags]
```

### Options

```
      --bin-dir string          directory to install the hlmdsrv binary into
      --completion-dir string   directory to install bash, zsh, and fish completions into
      --force                   overwrite an existing executable and completions
  -h, --help                    help for local
      --home string             source checkout root; auto-detected when omitted
      --json                    write machine-readable output
      --name string             installed executable name (default "hlmdsrv")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv install](hlmdsrv_install.md)	 - Install local CLI extras and print backend setup guidance
