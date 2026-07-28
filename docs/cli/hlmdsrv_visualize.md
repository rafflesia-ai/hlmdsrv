## hlmdsrv visualize

Generate a static MVS scene from a dataset topology

```
hlmdsrv visualize DATASET_ID [flags]
```

### Options

```
      --background string       canvas background (default "white")
      --color string            explicit color or high-level theme
      --component string        component selector (default "all")
      --focus string            camera focus component or selector
      --frame int               extract this trajectory frame before generating the static scene; -1 uses topology (default -1)
      --gmx-command string      GROMACS command override for --frame
  -h, --help                    help for visualize
      --include-selections      include all saved named selections as extra components
      --json                    write machine-readable output
  -o, --out string              output .mvsj path
      --repr string             representation type (default "cartoon")
      --selection stringArray   include a named selection id or raw MVS selector as an extra component; repeatable
      --store string            MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv](hlmdsrv.md)	 - Headless MDsrv dataset and session manager
