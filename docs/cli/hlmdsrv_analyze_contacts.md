## hlmdsrv analyze contacts

Run contacts analysis

```
hlmdsrv analyze contacts DATASET_ID [flags]
```

### Options

```
      --a string              first selection
      --b string              second selection
      --backend string        trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs (default "auto")
      --c string              third selection for angle/dihedral
      --cutoff float          contact cutoff in nm (default 0.5)
      --d string              fourth selection for dihedral
      --format string         output format: csv or json
      --gmx-command string    GROMACS fallback command override; fallback selections are 1-based atom indices
  -h, --help                  help for contacts
      --id string             analysis id
      --json                  write a machine-readable completion report
  -o, --out string            trace output path
      --record                record analysis metadata into the dataset manifest (default true)
      --reference-frame int   reference frame for RMSD
      --selection string      atom selection for single-group analyses (rmsd, rmsf, rgyr, sasa)
      --store string          MDsrv store root (default "./mdsrv-data")
```

### Options inherited from parent commands

```
      --profile string     load defaults from a named config profile or MDSRV_PROFILE
      --timeout duration   overall command timeout, for example 5m; profile timeout is used when unset
```

### SEE ALSO

* [hlmdsrv analyze](hlmdsrv_analyze.md)	 - Run trajectory analysis through the backend bridge
