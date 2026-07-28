package mdsrvcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type ingestFlags struct {
	store         string
	topology      string
	topologyURL   string
	trajectory    string
	trajectoryURL string
	cache         string
	id            string
	name          string
	description   string
	source        string
	license       string
	createdBy     string
	stride        int
	atomSubset    string
	timeUnit      string
	coordUnit     string
	force         bool
	probe         bool
	gmxCommand    string
	maxAtoms      int
	maxFrames     int
	jsonReport    bool
}

type validateFlags struct {
	store      string
	deep       bool
	strict     bool
	backend    string
	gmxCommand string
	jsonReport bool
}

type listFlags struct {
	store      string
	jsonReport bool
}

type initFlags struct {
	store      string
	jsonReport bool
}

type initJobFlags struct {
	id                string
	name              string
	topology          string
	topologyURL       string
	trajectory        string
	trajectoryURL     string
	out               string
	chunkSize         int
	materializeChunks bool
	force             bool
	jsonReport        bool
}

type serveFlags struct {
	store           string
	host            string
	port            int
	backend         string
	gmxCommand      string
	readOnly        bool
	logRequests     bool
	requestTimeout  time.Duration
	maxFrameRange   int
	maxAtoms        int
	maxFrames       int
	maxChunkBytes   int64
	workers         int
	maxQueue        int
	jobTimeout      time.Duration
	jobTTL          time.Duration
	jobPruneOnStart bool
	allowPaths      []string
	allowHosts      []string
	authToken       string
}

type serveSmokeCheck struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type serveSmokeReport struct {
	Store   string            `json:"store"`
	Checks  []serveSmokeCheck `json:"checks"`
	OK      bool              `json:"ok"`
	Dataset string            `json:"dataset,omitempty"`
}

type probeFlags struct {
	store      string
	gmxCommand string
	jsonReport bool
}

type framesFlags struct {
	store      string
	out        string
	backend    string
	gmxCommand string
	jsonReport bool
}

type demoFlags struct {
	out        string
	store      string
	id         string
	name       string
	frames     int
	job        string
	gmxCommand string
	force      bool
	jsonReport bool
}

type doctorFlags struct {
	store      string
	cache      string
	staticOut  string
	gmxCommand string
	strict     bool
	jsonReport bool
}

type doctorCheck struct {
	Name        string `json:"name"`
	Level       string `json:"level,omitempty"`
	OK          bool   `json:"ok"`
	Message     string `json:"message,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func (a app) doctorCommand() *cobra.Command {
	flags := &doctorFlags{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local MDsrv headless prerequisites",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := a.runDoctor(cmd.Context(), flags)
			if flags.jsonReport {
				report := doctorReport{OK: doctorChecksOK(checks), Checks: checks}
				if err := writeJSON(a.stdout, report); err != nil {
					return err
				}
				return strictDoctorError(checks, flags.strict)
			}
			for _, check := range checks {
				status := "ok"
				if !check.OK {
					status = "warn"
				}
				level := firstNonEmpty(check.Level, "optional")
				if check.Message == "" && check.Hint == "" {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", status, level, check.Name)
				} else if check.Hint == "" {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", status, level, check.Name, check.Message)
				} else if check.Message == "" {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", status, level, check.Name, check.Hint)
				} else {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\n", status, level, check.Name, check.Message, check.Hint)
				}
			}
			return strictDoctorError(checks, flags.strict)
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "", "optional store path to check")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "optional cache directory to check")
	cmd.Flags().StringVar(&flags.staticOut, "static-out", "", "optional static publish output directory to check")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "fail when required checks or GROMACS are unavailable")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) ingestCommand() *cobra.Command {
	flags := &ingestFlags{}
	cmd := &cobra.Command{
		Use:   "ingest [TOPOLOGY] [TRAJECTORY]",
		Short: "Add a topology and trajectory to a headless MDsrv store",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return fmt.Errorf("ingest positional form requires both TOPOLOGY and TRAJECTORY")
			}
			if len(args) == 2 {
				if flags.topology == "" {
					flags.topology = args[0]
				}
				if flags.trajectory == "" {
					flags.trajectory = args[1]
				}
			}
			if flags.topology == "" && flags.topologyURL == "" {
				return fmt.Errorf("one of --topology, --topology-url, or positional TOPOLOGY is required")
			}
			if flags.trajectory == "" && flags.trajectoryURL == "" {
				return fmt.Errorf("one of --trajectory, --trajectory-url, or positional TRAJECTORY is required")
			}
			// Validate local inputs up front. A directory or unreadable path otherwise
			// surfaced from deep in the ingest as internal_error (exit 1), telling the
			// caller to report a bug over a fixable path. URL inputs are not yet local,
			// so they are left to the download step.
			if flags.topology != "" {
				if err := requireInputFile(flags.topology); err != nil {
					return err
				}
			}
			if flags.trajectory != "" {
				if err := requireInputFile(flags.trajectory); err != nil {
					return err
				}
			}
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			manifest, err := store.Ingest(mdsrv.IngestOptions{
				ID:            flags.id,
				Name:          flags.name,
				Description:   flags.description,
				Source:        flags.source,
				License:       flags.license,
				CreatedBy:     flags.createdBy,
				Topology:      flags.topology,
				TopologyURL:   flags.topologyURL,
				Trajectory:    flags.trajectory,
				TrajectoryURL: flags.trajectoryURL,
				Cache:         flags.cache,
				Stride:        flags.stride,
				AtomSubset:    flags.atomSubset,
				TimeUnit:      flags.timeUnit,
				CoordUnit:     flags.coordUnit,
				Force:         flags.force,
			})
			if err != nil {
				return err
			}
			var warnings []string
			if flags.probe {
				probed, err := store.ProbeDataset(cmd.Context(), manifest.Metadata.ID, flags.gmxCommand)
				if err != nil {
					warnings = append(warnings, err.Error())
					fmt.Fprintln(a.stderr, "warning: gromacs probe failed:", err)
				} else {
					if err := (mdsrv.ResourceLimits{MaxAtoms: flags.maxAtoms, MaxFrames: flags.maxFrames}).CheckManifest(probed); err != nil {
						return err
					}
					manifest = probed
				}
			}
			report := struct {
				ID         string   `json:"id"`
				Manifest   string   `json:"manifest"`
				Topology   string   `json:"topology"`
				Trajectory string   `json:"trajectory"`
				FrameCount int      `json:"frame_count,omitempty"`
				AtomCount  int      `json:"atom_count,omitempty"`
				Warnings   []string `json:"warnings,omitempty"`
			}{
				ID:       manifest.Metadata.ID,
				Manifest: filepath.ToSlash(filepath.Join(mdsrv.DatasetsDir, manifest.Metadata.ID+".yaml")),
				Topology: manifest.Inputs.Topology.Path,
				Warnings: warnings,
			}
			if len(manifest.Inputs.Trajectories) > 0 {
				report.Trajectory = manifest.Inputs.Trajectories[0].Path
				report.FrameCount = manifest.Inputs.Trajectories[0].FrameCount
				report.AtomCount = manifest.Inputs.Trajectories[0].AtomCount
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "ingested %s\n", report.ID)
			fmt.Fprintf(a.stdout, "manifest %s\n", report.Manifest)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVar(&flags.topology, "topology", "", "topology file path")
	cmd.Flags().StringVar(&flags.topologyURL, "topology-url", "", "topology URL to download before ingest")
	cmd.Flags().StringVar(&flags.trajectory, "trajectory", "", "trajectory file path")
	cmd.Flags().StringVar(&flags.trajectoryURL, "trajectory-url", "", "trajectory URL to download before ingest")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "download cache directory")
	cmd.Flags().StringVar(&flags.id, "id", "", "dataset id; defaults to the trajectory filename stem")
	cmd.Flags().StringVar(&flags.name, "name", "", "display name")
	cmd.Flags().StringVar(&flags.description, "description", "", "dataset description")
	cmd.Flags().StringVar(&flags.source, "source", "", "source URL, DOI, or provenance label")
	cmd.Flags().StringVar(&flags.license, "license", "", "dataset license")
	cmd.Flags().StringVar(&flags.createdBy, "created-by", "", "creator or pipeline name")
	cmd.Flags().IntVar(&flags.stride, "stride", 0, "frame stride hint")
	cmd.Flags().StringVar(&flags.atomSubset, "atom-subset", "", "atom subset expression hint")
	cmd.Flags().StringVar(&flags.timeUnit, "time-unit", "ps", "trajectory time unit")
	cmd.Flags().StringVar(&flags.coordUnit, "coordinate-unit", "nm", "trajectory coordinate unit")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing dataset with the same id")
	cmd.Flags().BoolVar(&flags.probe, "probe", true, "probe trajectory metadata with GROMACS")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi")
	cmd.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "fail after probe if the dataset exceeds this atom count; 0 disables")
	cmd.Flags().IntVar(&flags.maxFrames, "max-frames", 0, "fail after probe if the dataset exceeds this frame count; 0 disables")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) initCommand() *cobra.Command {
	flags := &initFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize an MDsrv store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			if err := store.Init(); err != nil {
				return err
			}
			report := struct {
				Store           string   `json:"store"`
				Directories     []string `json:"directories"`
				Metadata        string   `json:"metadata"`
				TrajectoryIndex string   `json:"trajectory_index"`
				SessionIndex    string   `json:"session_index"`
			}{
				Store: store.Root,
				Directories: []string{
					filepath.ToSlash(filepath.Join(store.Root, mdsrv.DatasetsDir)),
					filepath.ToSlash(filepath.Join(store.Root, mdsrv.TopologyDir)),
					filepath.ToSlash(filepath.Join(store.Root, mdsrv.TrajectoryDir)),
					filepath.ToSlash(filepath.Join(store.Root, mdsrv.SessionDir)),
				},
				Metadata:        filepath.ToSlash(store.MetadataPath()),
				TrajectoryIndex: filepath.ToSlash(filepath.Join(store.Root, "trajectory_index.json")),
				SessionIndex:    filepath.ToSlash(filepath.Join(store.Root, "session_index.json")),
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, report.Store)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(a.initJobCommand())
	return cmd
}

func (a app) initJobCommand() *cobra.Command {
	flags := &initJobFlags{out: "mdsrv.job.yaml", chunkSize: 128}
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Create a starter mdsrv.job/v1 manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.topology == "" && flags.topologyURL == "" {
				return codedErrorf(codeMissingInput, "one of --topology or --topology-url is required")
			}
			if flags.trajectory == "" && flags.trajectoryURL == "" {
				return codedErrorf(codeMissingInput, "one of --trajectory or --trajectory-url is required")
			}
			if strings.TrimSpace(flags.id) == "" {
				flags.id = mdsrv.DefaultDatasetID(firstNonEmpty(flags.trajectory, flags.trajectoryURL))
			}
			if err := mdsrv.ValidateID(flags.id); err != nil {
				return codedErrorf(codeInvalidManifest, "id: %v", err)
			}
			if err := ensureOutputPath(flags.out, flags.force); err != nil {
				return err
			}
			manifest := starterJobManifest(flags)
			if err := mdsrv.WriteManifestFile(flags.out, manifest); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, map[string]any{"path": flags.out, "manifest": manifest})
			}
			fmt.Fprintln(a.stdout, flags.out)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.id, "id", "", "dataset id; defaults to the trajectory filename stem")
	cmd.Flags().StringVar(&flags.name, "name", "", "dataset display name")
	cmd.Flags().StringVar(&flags.topology, "topology", "", "topology file path")
	cmd.Flags().StringVar(&flags.topologyURL, "topology-url", "", "topology URL")
	cmd.Flags().StringVar(&flags.trajectory, "trajectory", "", "trajectory file path")
	cmd.Flags().StringVar(&flags.trajectoryURL, "trajectory-url", "", "trajectory URL")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "mdsrv.job.yaml", "output job manifest path")
	cmd.Flags().IntVar(&flags.chunkSize, "chunk-size", 128, "default streaming chunk size")
	cmd.Flags().BoolVar(&flags.materializeChunks, "chunks", false, "request materialized static frame chunks")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing output file")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func starterJobManifest(flags *initJobFlags) mdsrv.Manifest {
	topology := mdsrv.FileRef{
		Path:   flags.topology,
		URL:    flags.topologyURL,
		Format: mdsrv.NormalizeFormat(mdsrv.InferFormat(firstNonEmpty(flags.topology, flags.topologyURL))),
	}
	trajectory := mdsrv.FileRef{
		Path:      flags.trajectory,
		URL:       flags.trajectoryURL,
		Format:    mdsrv.NormalizeFormat(mdsrv.InferFormat(firstNonEmpty(flags.trajectory, flags.trajectoryURL))),
		TimeUnit:  "ps",
		CoordUnit: "nm",
	}
	return mdsrv.Manifest{
		Version: mdsrv.ManifestVersion,
		Metadata: mdsrv.Metadata{
			ID:   flags.id,
			Name: flags.name,
		},
		Inputs: mdsrv.Inputs{
			Topology:     topology,
			Trajectories: []mdsrv.FileRef{trajectory},
		},
		Streaming: mdsrv.Streaming{
			ChunkSizeFrames:   flags.chunkSize,
			MaterializeChunks: flags.materializeChunks,
		},
	}
}

func (a app) listCommand() *cobra.Command {
	flags := &listFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List store resources",
	}
	datasets := &cobra.Command{
		Use:   "datasets",
		Short: "List datasets in a store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			items, err := store.ListDatasets()
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, items)
			}
			for _, item := range items {
				if item.FrameCount > 0 {
					fmt.Fprintf(a.stdout, "%s\t%s\tframes=%d\t%s\n", item.ID, item.Name, item.FrameCount, item.Trajectory)
				} else {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", item.ID, item.Name, item.Trajectory)
				}
			}
			return nil
		},
	}
	datasets.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	datasets.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(datasets)
	return cmd
}

func (a app) probeCommand() *cobra.Command {
	flags := &probeFlags{}
	cmd := &cobra.Command{
		Use:   "probe DATASET_ID",
		Short: "Refresh trajectory metadata with GROMACS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			manifest, err := store.ProbeDataset(cmd.Context(), args[0], flags.gmxCommand)
			if err != nil {
				return err
			}
			report := trajectoryReport(manifest)
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "%s\tatoms=%d\tframes=%d\ttimestep=%g\n", report.ID, report.AtomCount, report.FrameCount, report.TimeStep)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) framesCommand() *cobra.Command {
	flags := &framesFlags{}
	getFlags := &frameFlags{}
	cmd := &cobra.Command{
		Use:     "frames",
		Aliases: []string{"trajectory"},
		Short:   "Inspect and extract trajectory frames",
	}
	count := &cobra.Command{
		Use:   "count DATASET_ID",
		Short: "Print trajectory frame count",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			manifest, err := store.LoadDataset(args[0])
			if err != nil {
				return err
			}
			report := trajectoryReport(manifest)
			// Use the cached manifest frame count when present; only spawn a
			// backend probe when the count is missing or the user explicitly
			// asked for a specific backend. (--backend defaults to "auto", so
			// checking flags.backend != "" here would always be true and would
			// wrongly fail on hosts with no backend installed.)
			if report.FrameCount == 0 || cmd.Flags().Changed("backend") {
				info, err := trajectoryInfoWithPolicy(cmd.Context(), store, manifest, args[0], flags.backend, flags.gmxCommand)
				if err != nil {
					return err
				}
				report.AtomCount = info.Atoms
				report.FrameCount = info.Frames
				report.TimeStart = info.FirstTime
				report.TimeEnd = info.LastTime
				report.TimeUnit = info.TimeUnit
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, report.FrameCount)
			return nil
		},
	}
	get := &cobra.Command{
		Use:   "get DATASET_ID FRAME_INDEX",
		Short: "Get one frame as JSON or MDSF binary",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			frameIndex, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid frame index %q", args[1])
			}
			return a.runFrame(cmd.Context(), args[0], frameIndex, getFlags)
		},
	}
	extract := &cobra.Command{
		Use:   "extract DATASET_ID FRAME_INDEX",
		Short: "Extract one frame with GROMACS",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			frameIndex, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid frame index %q", args[1])
			}
			out := flags.out
			if out == "" {
				out = fmt.Sprintf("%s-frame-%d.gro", args[0], frameIndex)
			}
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			manifest, err := store.ExtractFrame(cmd.Context(), args[0], frameIndex, out, flags.gmxCommand)
			if err != nil {
				return err
			}
			report := struct {
				ID    string `json:"id"`
				Frame int    `json:"frame"`
				Path  string `json:"path"`
			}{ID: manifest.Metadata.ID, Frame: frameIndex, Path: out}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, out)
			return nil
		},
	}
	for _, sub := range []*cobra.Command{count, extract} {
		sub.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
		bindBackendFlag(sub, &flags.backend)
		sub.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
		sub.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	}
	extract.Flags().StringVarP(&flags.out, "out", "o", "", "output frame file; extension controls GROMACS output format")
	get.Flags().StringVar(&getFlags.store, "store", "./mdsrv-data", "MDsrv store root")
	get.Flags().StringVarP(&getFlags.out, "out", "o", "", "output path; stdout when omitted")
	get.Flags().StringVar(&getFlags.format, "format", "", "output format: json or bin; inferred from --out when omitted")
	get.Flags().StringVar(&getFlags.atomSubset, "atom-subset", "", "override atom subset selection for this frame")
	bindBackendFlag(get, &getFlags.backend)
	get.Flags().StringVar(&getFlags.gmxCommand, "gmx-command", "", "GROMACS fallback command override")
	cmd.AddCommand(count, get, extract)
	return cmd
}

func (a app) demoCommand() *cobra.Command {
	flags := &demoFlags{}
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Create small real datasets for local testing",
	}
	gromacs := &cobra.Command{
		Use:   "gromacs",
		Short: "Create a tiny real GROMACS .gro/.xtc trajectory",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.runDemoGromacs(cmd.Context(), flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "topology %s\n", report.Topology)
			fmt.Fprintf(a.stdout, "trajectory %s\n", report.Trajectory)
			if report.Manifest != "" {
				fmt.Fprintf(a.stdout, "manifest %s\n", report.Manifest)
			}
			return nil
		},
	}
	gromacs.Flags().StringVar(&flags.out, "out", "outputs/mdsrv-gromacs-demo", "output directory for generated files")
	gromacs.Flags().StringVar(&flags.store, "store", "", "optional store root to ingest into")
	gromacs.Flags().StringVar(&flags.id, "id", "demo-gromacs", "dataset id")
	gromacs.Flags().StringVar(&flags.name, "name", "GROMACS demo trajectory", "dataset name")
	gromacs.Flags().IntVar(&flags.frames, "frames", 5, "number of frames to generate")
	gromacs.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	gromacs.Flags().BoolVar(&flags.force, "force", true, "overwrite generated files and an existing ingested demo dataset")
	gromacs.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(gromacs)

	createFlags := &demoFlags{out: "outputs/mdsrv-demo", id: "demo", name: "MDsrv demo trajectory", frames: 5, job: "job.yaml", force: true}
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a tiny trajectory and runnable job manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.runDemoCreate(cmd.Context(), createFlags)
			if err != nil {
				return err
			}
			if createFlags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "topology %s\n", report.Topology)
			fmt.Fprintf(a.stdout, "trajectory %s\n", report.Trajectory)
			fmt.Fprintf(a.stdout, "job %s\n", report.Job)
			if report.Manifest != "" {
				fmt.Fprintf(a.stdout, "manifest %s\n", report.Manifest)
			}
			return nil
		},
	}
	create.Flags().StringVar(&createFlags.out, "out", createFlags.out, "output directory for generated files")
	create.Flags().StringVar(&createFlags.store, "store", "", "optional store root to ingest into")
	create.Flags().StringVar(&createFlags.id, "id", createFlags.id, "dataset id")
	create.Flags().StringVar(&createFlags.name, "name", createFlags.name, "dataset name")
	create.Flags().IntVar(&createFlags.frames, "frames", createFlags.frames, "number of frames to generate")
	create.Flags().StringVar(&createFlags.job, "job", createFlags.job, "job manifest path; relative paths are resolved under --out")
	create.Flags().StringVar(&createFlags.gmxCommand, "gmx-command", "", "GROMACS command override")
	create.Flags().BoolVar(&createFlags.force, "force", createFlags.force, "overwrite generated files and job manifest")
	create.Flags().BoolVar(&createFlags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(create)
	return cmd
}

func (a app) validateCommand() *cobra.Command {
	flags := &validateFlags{}
	cmd := &cobra.Command{
		Use:   "validate MANIFEST_OR_DATASET_ID",
		Short: "Validate a manifest or store dataset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				store mdsrv.Store
				m     mdsrv.Manifest
				err   error
			)
			if flags.store != "" {
				store, err = mdsrv.OpenStore(flags.store)
				if err != nil {
					return err
				}
				m, err = store.LoadDataset(args[0])
			} else if info, statErr := os.Stat(args[0]); statErr == nil && info.IsDir() {
				store, err = mdsrv.OpenStore(args[0])
				if err != nil {
					return err
				}
				report := store.Doctor()
				// Doctor only inspects store layout, version and metadata. Under
				// --strict also verify each dataset's files, otherwise a store whose
				// dataset fails its own checksum check reported ok:true here while
				// `validate <id>` on the very same store failed — a clean bill of health
				// for a corrupt store.
				datasetFailures := []string{}
				if flags.strict {
					summaries, listErr := store.ListDatasets()
					if listErr != nil {
						return listErr
					}
					for _, summary := range summaries {
						m, loadErr := store.LoadDataset(summary.ID)
						if loadErr != nil {
							datasetFailures = append(datasetFailures, fmt.Sprintf("%s: %v", summary.ID, loadErr))
							continue
						}
						datasetReport, buildErr := buildDatasetValidationReport(cmd.Context(), store, m, store.Root, flags)
						if buildErr != nil {
							datasetFailures = append(datasetFailures, fmt.Sprintf("%s: %v", summary.ID, buildErr))
							continue
						}
						if !datasetReport.OK {
							datasetFailures = append(datasetFailures, summary.ID)
						}
					}
					if len(datasetFailures) > 0 {
						report.OK = false
					}
				}
				if flags.jsonReport {
					if err := writeJSON(a.stdout, report); err != nil {
						return err
					}
					if flags.strict && !report.OK {
						return storeValidationError(datasetFailures)
					}
					return nil
				}
				writeStoreDoctorText(a.stdout, report)
				for _, failure := range datasetFailures {
					fmt.Fprintln(a.stderr, "dataset validation failed:", failure)
				}
				if flags.strict && !report.OK {
					return storeValidationError(datasetFailures)
				}
				return nil
			} else {
				m, err = mdsrv.LoadManifestFile(args[0])
				if err == nil {
					store, err = mdsrv.OpenStore(manifestRoot(args[0]))
				}
			}
			if err != nil {
				return err
			}
			root := store.Root
			if flags.store == "" {
				root = manifestRoot(args[0])
			}
			report, err := buildDatasetValidationReport(cmd.Context(), store, m, root, flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				if err := writeJSON(a.stdout, report); err != nil {
					return err
				}
				if !report.OK {
					return codedErrorf(codeValidationFailed, "validation failed")
				}
				return nil
			}
			fmt.Fprintf(a.stdout, "valid %s\n", m.Metadata.ID)
			for _, file := range report.Files {
				status := "ok"
				if !file.Exists || file.Error != "" {
					status = "fail"
				}
				fmt.Fprintf(a.stdout, "%s\t%s", status, file.Path)
				if file.Error != "" {
					fmt.Fprintf(a.stdout, "\t%s", file.Error)
				}
				fmt.Fprintln(a.stdout)
			}
			for _, warning := range report.Warnings {
				fmt.Fprintln(a.stderr, "warning:", warning)
			}
			for _, issue := range report.Issues {
				if issue.Severity == "error" {
					fmt.Fprintf(a.stderr, "error: %s", issue.Message)
				} else {
					fmt.Fprintf(a.stderr, "warning: %s", issue.Message)
				}
				if issue.Path != "" {
					fmt.Fprintf(a.stderr, "\t%s", issue.Path)
				}
				fmt.Fprintln(a.stderr)
			}
			if report.Trajectory != nil {
				fmt.Fprintf(a.stdout, "frames\t%d\n", report.Trajectory.Frames)
				fmt.Fprintf(a.stdout, "atoms\t%d\n", report.Trajectory.Atoms)
			}
			if !report.OK {
				return codedErrorf(codeValidationFailed, "validation failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "", "store root; when set, argument is a dataset id")
	cmd.Flags().BoolVar(&flags.deep, "deep", false, "decode trajectory metadata with mdtraj or MDAnalysis")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "fail on missing optional artifacts, output conflicts, and unavailable requested backends")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS fallback command override")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) serveCommand() *cobra.Command {
	flags := &serveFlags{}
	cmd := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"server"},
		Short:   "Serve a headless MDsrv store over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			return a.runServe(cmd.Context(), store, flags)
		},
	}
	bindServeFlags(cmd, flags, true)

	smokeFlags := &serveFlags{}
	var smokeJSON bool
	smoke := &cobra.Command{
		Use:   "smoke",
		Short: "Start the HTTP handler in-process and verify key routes",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(smokeFlags.store)
			if err != nil {
				return err
			}
			report, err := a.runServeSmoke(cmd.Context(), store, smokeFlags)
			if err != nil {
				if len(report.Checks) > 0 {
					_ = writeJSON(a.stdout, report)
				}
				return err
			}
			return writeJSON(a.stdout, report)
		},
	}
	bindServeFlags(smoke, smokeFlags, false)
	smoke.Flags().BoolVar(&smokeJSON, "json", false, "write machine-readable output")
	cmd.AddCommand(smoke)
	return cmd
}

func bindServeFlags(cmd *cobra.Command, flags *serveFlags, includeListen bool) {
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	if includeListen {
		cmd.Flags().StringVar(&flags.host, "host", "127.0.0.1", "listen host")
		cmd.Flags().IntVar(&flags.port, "port", 1337, "listen port")
	}
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.readOnly, "read-only", false, "reject HTTP requests that mutate datasets, selections, indexes, analyses, or sessions")
	cmd.Flags().BoolVar(&flags.logRequests, "log-requests", false, "write structured JSON request logs to stderr")
	cmd.Flags().DurationVar(&flags.requestTimeout, "request-timeout", 0, "per-request timeout, for example 30s; 0 disables the timeout wrapper")
	cmd.Flags().IntVar(&flags.maxFrameRange, "max-frame-range", 256, "maximum number of frames returned by one HTTP frame range request")
	cmd.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "maximum dataset/frame atom count for index, chunks, and frame range operations; 0 disables")
	cmd.Flags().IntVar(&flags.maxFrames, "max-frames", 0, "maximum dataset frame count for index and chunks operations; 0 disables")
	cmd.Flags().Int64Var(&flags.maxChunkBytes, "max-chunk-bytes", 0, "maximum encoded chunk size in bytes; 0 disables")
	cmd.Flags().IntVar(&flags.workers, "workers", 0, "background workers for /jobs chunking and analysis requests; 0 disables async jobs")
	cmd.Flags().IntVar(&flags.maxQueue, "max-queue", 64, "maximum queued async jobs when --workers is enabled")
	cmd.Flags().DurationVar(&flags.jobTimeout, "job-timeout", 0, "per async job timeout, for example 10m; 0 disables")
	cmd.Flags().DurationVar(&flags.jobTTL, "job-ttl", 7*24*time.Hour, "age threshold for --job-prune-on-start; 0 prunes all terminal jobs")
	cmd.Flags().BoolVar(&flags.jobPruneOnStart, "job-prune-on-start", false, "prune old terminal job records before starting workers")
	cmd.Flags().StringArrayVar(&flags.allowPaths, "allow-path", nil, "allow local ingest/session paths under this directory; repeatable")
	cmd.Flags().StringArrayVar(&flags.allowHosts, "allow-host", nil, "allow remote ingest URLs from this host; repeatable")
	cmd.Flags().StringVar(&flags.authToken, "auth-token", "", "require this bearer token or X-MDSRV-Token value for HTTP API requests; defaults to MDSRV_AUTH_TOKEN")
}

func (a app) runDoctor(ctx context.Context, flags *doctorFlags) []doctorCheck {
	checks := []doctorCheck{
		{Name: "go runtime", OK: true, Message: runtime.Version()},
		{Name: "host platform", OK: true, Message: runtime.GOOS + "/" + runtime.GOARCH},
	}
	if _, err := exec.LookPath("mdsrv"); err == nil {
		checks = append(checks, doctorCheck{Name: "legacy mdsrv command", OK: true})
	} else {
		checks = append(checks, doctorCheck{Name: "legacy mdsrv command", OK: false, Message: "not required; install Python MDsrv only if you need the old viewer/server"})
	}
	if _, err := exec.LookPath("docker"); err == nil {
		checks = append(checks, doctorCheck{Name: "docker", OK: true, Message: "available for upstream mdsrv-viewer/mdsrv-remote images"})
	} else {
		checks = append(checks, doctorCheck{Name: "docker", OK: false, Message: "optional; needed only for upstream MDsrv containers"})
	}
	gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
	gromacsReport := gmx.Check(ctx)
	if gromacsReport.Available {
		checks = append(checks, doctorCheck{
			Name:    "gromacs",
			OK:      true,
			Message: fmt.Sprintf("%s via %s", gromacsReport.Version, gromacsReport.Source),
			Hint:    gromacsReport.CommandString,
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:    "gromacs",
			OK:      false,
			Message: firstNonEmpty(gromacsReport.Error, "command not found"),
			Hint:    firstNonEmpty(gromacsReport.Hint, "install GROMACS or set MDSRV_GMX=/path/to/gmx for probing, frame extraction, export, and fallback analysis"),
		})
	}
	backend := mdsrv.NewBackend(mdsrv.Store{})
	if path, err := exec.LookPath(backend.Python); err == nil {
		checks = append(checks, doctorCheck{Name: "python command", OK: true, Message: path})
	} else {
		checks = append(checks, doctorCheck{
			Name:    "python command",
			OK:      false,
			Message: err.Error(),
			Hint:    "install python3 or set MDSRV_PYTHON=/path/to/python",
		})
	}
	backendDoctor, err := backend.Doctor(ctx)
	if err == nil {
		checks = append(checks, doctorCheck{
			Name:    "python mdtraj",
			OK:      backendDoctor.MDTraj,
			Message: fmt.Sprintf("python %s", backendDoctor.Python),
			Hint:    "optional: python3 -m pip install mdtraj",
		})
		checks = append(checks, doctorCheck{
			Name:    "python MDAnalysis",
			OK:      backendDoctor.MDAnalysis,
			Message: fmt.Sprintf("python %s", backendDoctor.Python),
			Hint:    "optional: python3 -m pip install MDAnalysis",
		})
	}
	if err == nil && (backendDoctor.MDTraj || backendDoctor.MDAnalysis) {
		checks = append(checks, doctorCheck{
			Name:    "python trajectory backend",
			OK:      true,
			Message: fmt.Sprintf("python %s mdtraj=%t MDAnalysis=%t", backendDoctor.Python, backendDoctor.MDTraj, backendDoctor.MDAnalysis),
		})
	} else if err != nil {
		checks = append(checks, doctorCheck{
			Name:    "python trajectory backend",
			OK:      false,
			Message: err.Error(),
			Hint:    "install mdtraj or MDAnalysis for atom-subset frame JSON and Python-native analyses; GROMACS fallback still works for full-frame paths",
		})
	} else {
		checks = append(checks, doctorCheck{
			Name:    "python trajectory backend",
			OK:      false,
			Message: "mdtraj=false MDAnalysis=false",
			Hint:    "install at least one: python3 -m pip install mdtraj or python3 -m pip install MDAnalysis",
		})
	}
	if flags.cache != "" {
		if err := checkWritableDirectory(flags.cache); err != nil {
			checks = append(checks, doctorCheck{Name: "cache", OK: false, Message: err.Error(), Hint: "choose a writable cache directory or pass --cache"})
		} else {
			checks = append(checks, doctorCheck{Name: "cache", OK: true, Message: flags.cache})
		}
	}
	if flags.staticOut != "" {
		if err := checkWritableDirectory(flags.staticOut); err != nil {
			checks = append(checks, doctorCheck{Name: "static publish output", OK: false, Message: err.Error(), Hint: "choose a writable directory outside the source store"})
		} else {
			checks = append(checks, doctorCheck{Name: "static publish output", OK: true, Message: flags.staticOut})
		}
	}
	if flags.store != "" {
		store, err := mdsrv.OpenStore(flags.store)
		if err != nil {
			checks = append(checks, doctorCheck{Name: "store", OK: false, Message: err.Error()})
		} else if err := store.Init(); err != nil {
			checks = append(checks, doctorCheck{Name: "store", OK: false, Message: err.Error()})
		} else {
			checks = append(checks, doctorCheck{Name: "store", OK: true, Message: store.Root})
		}
	} else if err := checkWritable("."); err != nil {
		checks = append(checks, doctorCheck{Name: "working directory writable", OK: false, Message: err.Error()})
	} else {
		checks = append(checks, doctorCheck{Name: "working directory writable", OK: true})
	}
	return gradeDoctorChecks(checks)
}

func gradeDoctorChecks(checks []doctorCheck) []doctorCheck {
	for i := range checks {
		if checks[i].Level == "" {
			checks[i].Level = doctorLevel(checks[i].Name)
		}
		if checks[i].Remediation == "" && checks[i].Hint != "" {
			checks[i].Remediation = checks[i].Hint
		}
		if checks[i].Remediation == "" && !checks[i].OK {
			checks[i].Remediation = "inspect this prerequisite before running production jobs"
		}
	}
	return checks
}

func doctorLevel(name string) string {
	switch name {
	case "go runtime", "host platform", "working directory writable", "store", "cache", "static publish output":
		return "required"
	case "gromacs", "python command", "python trajectory backend":
		return "recommended"
	case "python mdtraj", "python MDAnalysis":
		return "optional"
	case "docker", "legacy mdsrv command":
		return "optional"
	default:
		return "optional"
	}
}

func strictDoctorError(checks []doctorCheck, strict bool) error {
	if !strict {
		return nil
	}
	var failed []string
	for _, check := range checks {
		if check.OK {
			continue
		}
		if check.Level == "required" || check.Name == "gromacs" {
			failed = append(failed, check.Name)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return codedErrorf(codeValidationFailed, "doctor strict checks failed: %s", strings.Join(failed, ", "))
}

func doctorChecksOK(checks []doctorCheck) bool {
	return strictDoctorError(checks, true) == nil
}

func writeStoreDoctorText(w io.Writer, report mdsrv.StoreDoctorReport) {
	status := "ok"
	if !report.OK {
		status = "failed"
	}
	fmt.Fprintf(w, "%s\t%s\t%s\n", status, report.Store, firstNonEmpty(report.Version, "unversioned"))
	for _, check := range report.Checks {
		checkStatus := "ok"
		if !check.OK {
			checkStatus = "failed"
		}
		if check.Message == "" {
			fmt.Fprintf(w, "%s\t%s\t%s\n", checkStatus, check.Name, check.Path)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", checkStatus, check.Name, check.Path, check.Message)
		}
	}
	for _, migration := range report.Migrations {
		required := "optional"
		if migration.Required {
			required = "required"
		}
		fmt.Fprintf(w, "migration\t%s\t%s\t%s\n", required, migration.ID, migration.Message)
	}
}

func (a app) runServe(ctx context.Context, store mdsrv.Store, flags *serveFlags) error {
	if err := store.Init(); err != nil {
		return err
	}
	handler, _, err := a.serveHTTPHandler(store, flags)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              net.JoinHostPort(flags.host, strconv.Itoa(flags.port)),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(a.stderr, "serving %s at http://%s\n", store.Root, server.Addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a app) serveHTTPHandler(store mdsrv.Store, flags *serveFlags) (http.Handler, serverOptions, error) {
	if flags.maxFrameRange == 0 {
		flags.maxFrameRange = 256
	}
	if flags.maxFrameRange < 1 {
		return nil, serverOptions{}, fmt.Errorf("--max-frame-range must be at least 1")
	}
	if err := (mdsrv.ResourceLimits{MaxAtoms: flags.maxAtoms, MaxFrames: flags.maxFrames, MaxChunkBytes: flags.maxChunkBytes}).Validate(); err != nil {
		return nil, serverOptions{}, err
	}
	if flags.workers < 0 {
		return nil, serverOptions{}, fmt.Errorf("--workers cannot be negative")
	}
	if flags.workers > 0 && flags.maxQueue < 1 {
		return nil, serverOptions{}, fmt.Errorf("--max-queue must be at least 1 when --workers is enabled")
	}
	if flags.jobTimeout < 0 {
		return nil, serverOptions{}, fmt.Errorf("--job-timeout cannot be negative")
	}
	if flags.jobTTL < 0 {
		return nil, serverOptions{}, fmt.Errorf("--job-ttl cannot be negative")
	}
	if flags.jobPruneOnStart {
		if _, err := pruneJobs(jobPruneOptions{
			Store: store.Root,
			TTL:   flags.jobTTL,
			Status: map[string]bool{
				"succeeded": true,
				"failed":    true,
				"canceled":  true,
			},
		}); err != nil {
			return nil, serverOptions{}, err
		}
	}
	mux := http.NewServeMux()
	options := serverOptions{
		Backend:        flags.backend,
		GromacsCommand: flags.gmxCommand,
		ReadOnly:       flags.readOnly,
		LogRequests:    flags.logRequests,
		LogWriter:      a.stderr,
		RequestTimeout: flags.requestTimeout,
		MaxFrameRange:  flags.maxFrameRange,
		Limits: mdsrv.ResourceLimits{
			MaxAtoms:      flags.maxAtoms,
			MaxFrames:     flags.maxFrames,
			MaxChunkBytes: flags.maxChunkBytes,
		},
		Workers:         flags.workers,
		MaxQueue:        flags.maxQueue,
		JobTimeout:      flags.jobTimeout,
		JobTTL:          flags.jobTTL,
		JobPruneOnStart: flags.jobPruneOnStart,
		AllowPaths:      flags.allowPaths,
		AllowHosts:      flags.allowHosts,
		AuthToken:       firstNonEmpty(flags.authToken, os.Getenv("MDSRV_AUTH_TOKEN")),
	}
	registerHandlersWithOptions(mux, store, options)
	handler := maxBodyBytesMiddleware(mux, maxRequestBodyBytes)
	if flags.requestTimeout > 0 {
		handler = http.TimeoutHandler(handler, flags.requestTimeout, "request timed out\n")
	}
	if options.AuthToken != "" {
		handler = authMiddleware(handler, options.AuthToken)
	}
	handler = requestIDMiddleware(handler)
	if flags.logRequests {
		handler = requestLogMiddleware(handler, a.stderr)
	}
	return handler, options, nil
}

func (a app) runServeSmoke(ctx context.Context, store mdsrv.Store, flags *serveFlags) (serveSmokeReport, error) {
	if err := store.Init(); err != nil {
		return serveSmokeReport{}, err
	}
	handler, _, err := a.serveHTTPHandler(store, flags)
	if err != nil {
		return serveSmokeReport{}, err
	}
	token := firstNonEmpty(flags.authToken, os.Getenv("MDSRV_AUTH_TOKEN"))
	report := serveSmokeReport{Store: store.Root, OK: true}
	check := func(method, path string) {
		req := httptest.NewRequest(method, path, nil).WithContext(ctx)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		status := recorder.Code
		item := serveSmokeCheck{Method: method, Path: path, Status: status, OK: status >= 200 && status < 300}
		if !item.OK {
			item.Error = strings.TrimSpace(recorder.Body.String())
			report.OK = false
		}
		report.Checks = append(report.Checks, item)
	}
	check(http.MethodGet, "/health")
	check(http.MethodGet, "/version")
	check(http.MethodGet, "/capabilities")
	check(http.MethodGet, "/metrics")
	check(http.MethodGet, "/datasets")
	check(http.MethodGet, "/trajectory_index.json")
	check(http.MethodGet, "/session_index.json")
	if flags.workers > 0 {
		check(http.MethodGet, "/jobs/stats")
		check(http.MethodGet, "/jobs/metrics")
	}
	datasets, err := store.ListDatasets()
	if err != nil {
		return report, err
	}
	if len(datasets) > 0 {
		report.Dataset = datasets[0].ID
		check(http.MethodGet, "/datasets/"+url.PathEscape(report.Dataset))
		m, err := store.LoadDataset(report.Dataset)
		if err != nil {
			return report, err
		}
		if m.Streaming.FrameIndex != "" {
			check(http.MethodGet, "/datasets/"+url.PathEscape(report.Dataset)+"/frames/index")
		} else {
			check(http.MethodGet, "/datasets/"+url.PathEscape(report.Dataset)+"/frames/count?backend="+url.QueryEscape(firstNonEmpty(flags.backend, "auto")))
		}
	}
	if !report.OK {
		return report, codedErrorf(codeValidationFailed, "serve smoke failed")
	}
	return report, nil
}

func registerHandlers(mux *http.ServeMux, store mdsrv.Store, backend string, gromacsCommand string) {
	registerHandlersWithOptions(mux, store, serverOptions{
		Backend:        backend,
		GromacsCommand: gromacsCommand,
		MaxFrameRange:  256,
	})
}

type serverOptions struct {
	Backend         string
	GromacsCommand  string
	ReadOnly        bool
	LogRequests     bool
	LogWriter       io.Writer
	RequestTimeout  time.Duration
	MaxFrameRange   int
	Limits          mdsrv.ResourceLimits
	Workers         int
	MaxQueue        int
	JobTimeout      time.Duration
	JobTTL          time.Duration
	JobPruneOnStart bool
	AllowPaths      []string
	AllowHosts      []string
	AuthToken       string
}

func (options serverOptions) withDefaults() serverOptions {
	if options.MaxFrameRange < 1 {
		options.MaxFrameRange = 256
	}
	if options.LogWriter == nil {
		options.LogWriter = io.Discard
	}
	if options.MaxQueue < 1 {
		options.MaxQueue = 64
	}
	return options
}

func registerHandlersWithOptions(mux *http.ServeMux, store mdsrv.Store, options serverOptions) {
	options = options.withDefaults()
	jobs := newServerJobQueue(store, options)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, map[string]string{
			"manifest_version": mdsrv.ManifestVersion,
			"service":          "hlmdsrv",
		})
	})
	mux.HandleFunc("/capabilities", func(w http.ResponseWriter, r *http.Request) {
		backendDoctor, _ := mdsrv.NewBackend(store).Doctor(r.Context())
		pythonBackend := backendDoctor.MDTraj || backendDoctor.MDAnalysis
		gromacsAvailable := gromacs.New(gromacs.Options{Command: options.GromacsCommand}).Available()
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"datasets":            true,
			"dataset_writes":      !options.ReadOnly,
			"read_only":           options.ReadOnly,
			"auth_required":       options.AuthToken != "",
			"metadata":            true,
			"file_serving":        true,
			"frame_decoding":      pythonBackend || gromacsAvailable,
			"frame_index":         gromacsAvailable,
			"chunk_encodings":     []string{"json", "bin", "bin-zstd"},
			"frame_ranges":        pythonBackend || gromacsAvailable,
			"max_frame_range":     options.MaxFrameRange,
			"max_atoms":           options.Limits.MaxAtoms,
			"max_frames":          options.Limits.MaxFrames,
			"max_chunk_bytes":     options.Limits.MaxChunkBytes,
			"job_queue":           jobs != nil,
			"workers":             options.Workers,
			"max_queue":           options.MaxQueue,
			"job_timeout_seconds": int(options.JobTimeout.Seconds()),
			"job_ttl_seconds":     int(options.JobTTL.Seconds()),
			"job_prune_on_start":  options.JobPruneOnStart,
			"gromacs_extraction":  gromacsAvailable,
			"analysis":            pythonBackend || gromacsAvailable,
			"selections":          true,
			"sessions":            true,
			"streaming_baseline":  []string{"xtc"},
		})
	})
	writeMetrics := func(w http.ResponseWriter) {
		stats := serverJobStats{Counts: map[string]int{}}
		enabled := jobs != nil
		if jobs != nil {
			stats = jobs.stats()
		} else {
			stats.Workers = options.Workers
			stats.MaxQueue = options.MaxQueue
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(jobMetricsText(stats, enabled)))
	}
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeMetrics(w)
	})
	mux.HandleFunc("/jobs/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeMetrics(w)
	})
	mux.HandleFunc("/schema/manifest", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, manifestSchema())
	})
	mux.HandleFunc("/schema/batch", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, batchSchema())
	})
	mux.HandleFunc("/schema/openapi", func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, openAPISchema())
	})
	mux.HandleFunc("/datasets", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/datasets" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			items, err := store.ListDatasets()
			if err != nil {
				writeHTTPError(w, http.StatusInternalServerError, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, items)
		case http.MethodPost:
			if options.ReadOnly {
				writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
				return
			}
			var opts mdsrv.IngestOptions
			if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			if err := validateIngestAgainstServerPolicy(opts, options); err != nil {
				writeHTTPError(w, http.StatusForbidden, err)
				return
			}
			// Enforce the server allowlist across download redirects too; a
			// client cannot set this (IngestOptions.AllowedHosts is json:"-").
			opts.AllowedHosts = options.AllowHosts
			m, err := store.Ingest(opts)
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, m)
		default:
			methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
			return
		}
	})
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		if jobs == nil {
			writeHTTPError(w, http.StatusServiceUnavailable, errors.New("job queue disabled; start serve with --workers greater than 0"))
			return
		}
		jobs.handleCollection(w, r)
	})
	mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		if jobs == nil {
			writeHTTPError(w, http.StatusServiceUnavailable, errors.New("job queue disabled; start serve with --workers greater than 0"))
			return
		}
		jobs.handleItem(w, r)
	})
	mux.HandleFunc("/datasets/", func(w http.ResponseWriter, r *http.Request) {
		handleDataset(w, r, store, options)
	})
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			items, err := store.ListSessions()
			if err != nil {
				writeHTTPError(w, http.StatusInternalServerError, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, items)
		case http.MethodPost:
			if options.ReadOnly {
				writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
				return
			}
			var opts mdsrv.SessionOptions
			if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			if err := validateSessionAgainstServerPolicy(opts, options); err != nil {
				writeHTTPError(w, http.StatusForbidden, err)
				return
			}
			ref, err := store.PublishSession(opts)
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, ref)
		default:
			methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
			return
		}
	})
	mux.Handle("/trajectory/", http.StripPrefix("/trajectory/", http.FileServer(http.Dir(filepath.Join(store.Root, mdsrv.TrajectoryDir)))))
	mux.Handle("/session/", http.StripPrefix("/session/", http.FileServer(http.Dir(filepath.Join(store.Root, mdsrv.SessionDir)))))
	mux.HandleFunc("/trajectory_index.json", serveStoreFile(store, "trajectory_index.json"))
	mux.HandleFunc("/session_index.json", serveStoreFile(store, "session_index.json"))
}

func handleDataset(w http.ResponseWriter, r *http.Request, store mdsrv.Store, options serverOptions) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/datasets/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	m, err := store.LoadDataset(id)
	if err != nil {
		writeHTTPError(w, http.StatusNotFound, err)
		return
	}
	if options.ReadOnly && isWriteMethod(r.Method) {
		writeHTTPError(w, http.StatusForbidden, errors.New("server is read-only"))
		return
	}
	if r.Method == http.MethodPatch && len(parts) == 1 {
		var opts mdsrv.UpdateOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		updated, err := store.UpdateDataset(id, opts)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, updated)
		return
	}
	if r.Method == http.MethodDelete && len(parts) == 1 {
		deleteFiles := r.URL.Query().Get("files") == "true"
		if err := store.DeleteDataset(id, deleteFiles); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]string{"deleted": id})
		return
	}
	if r.Method != http.MethodGet && !allowedDatasetWriteMethod(r.Method, parts) {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPatch+", "+http.MethodDelete+", "+http.MethodPost)
		return
	}
	if len(parts) == 1 {
		writeHTTPJSON(w, http.StatusOK, m)
		return
	}
	switch parts[1] {
	case "metadata":
		writeHTTPJSON(w, http.StatusOK, m.Metadata)
	case "topology":
		serveRefFile(w, r, store, m.Inputs.Topology)
	case "trajectory":
		if len(m.Inputs.Trajectories) == 0 {
			writeHTTPError(w, http.StatusNotFound, errors.New("dataset has no trajectory"))
			return
		}
		serveRefFile(w, r, store, m.Inputs.Trajectories[0])
	case "frames":
		if len(parts) >= 3 && parts[2] == "chunks" {
			if len(parts) == 3 {
				if r.Method == http.MethodPost {
					chunkSize, err := optionalQueryInt(r, "chunk_size", 128)
					if err != nil {
						writeHTTPError(w, http.StatusBadRequest, err)
						return
					}
					var body struct {
						ChunkSize int    `json:"chunk_size"`
						Encoding  string `json:"encoding"`
						Force     bool   `json:"force"`
					}
					if r.Body != nil {
						if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
							if body.ChunkSize > 0 {
								chunkSize = body.ChunkSize
							}
						}
					}
					encoding := firstNonEmpty(r.URL.Query().Get("encoding"), body.Encoding, "json")
					index, err := store.BuildFrameChunksWithOptions(r.Context(), id, mdsrv.BuildFrameChunksOptions{
						ChunkSize:      chunkSize,
						Encoding:       encoding,
						GromacsCommand: options.GromacsCommand,
						Force:          body.Force,
						Limits:         options.Limits,
					})
					if err != nil {
						writeHTTPError(w, http.StatusBadRequest, err)
						return
					}
					writeHTTPJSON(w, http.StatusOK, index)
					return
				}
				index, err := store.LoadFrameIndex(id)
				if err != nil {
					writeHTTPError(w, http.StatusNotFound, err)
					return
				}
				writeHTTPJSON(w, http.StatusOK, index.Chunks)
				return
			}
			if len(parts) == 4 && r.Method == http.MethodGet {
				chunkIndex, err := strconv.Atoi(parts[3])
				if err != nil {
					writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("invalid chunk index %q", parts[3]))
					return
				}
				if strings.EqualFold(r.URL.Query().Get("format"), "raw") || r.URL.Query().Get("download") == "true" {
					file, err := store.LoadFrameChunkFile(id, chunkIndex)
					if err != nil {
						writeHTTPError(w, http.StatusNotFound, err)
						return
					}
					w.Header().Set("Content-Type", file.ContentType)
					w.Header().Set("X-MDSRV-Chunk-Encoding", file.Encoding)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(file.Bytes)
					return
				}
				chunk, err := store.LoadFrameChunk(id, chunkIndex)
				if err != nil {
					writeHTTPError(w, http.StatusNotFound, err)
					return
				}
				writeHTTPJSON(w, http.StatusOK, chunk)
				return
			}
			http.NotFound(w, r)
			return
		}
		if len(parts) == 3 && parts[2] == "index" {
			if r.Method == http.MethodPost {
				chunkSize, err := optionalQueryInt(r, "chunk_size", 128)
				if err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				if r.Body != nil {
					var body struct {
						ChunkSize int `json:"chunk_size"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.ChunkSize > 0 {
						chunkSize = body.ChunkSize
					}
				}
				index, err := store.BuildFrameIndexWithOptions(r.Context(), id, mdsrv.BuildFrameIndexOptions{
					ChunkSize:      chunkSize,
					GromacsCommand: options.GromacsCommand,
					Limits:         options.Limits,
				})
				if err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				writeHTTPJSON(w, http.StatusOK, index)
				return
			}
			index, err := store.LoadFrameIndex(id)
			if err != nil {
				writeHTTPError(w, http.StatusNotFound, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, index)
			return
		}
		if len(parts) == 3 && parts[2] == "count" {
			requestBackend := firstNonEmpty(r.URL.Query().Get("backend"), options.Backend)
			if requestBackend == "" {
				requestBackend = "auto"
			}
			info, err := trajectoryInfoWithPolicy(r.Context(), store, m, id, requestBackend, options.GromacsCommand)
			if err != nil {
				writeHTTPError(w, http.StatusServiceUnavailable, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, info)
			return
		}
		if len(parts) == 3 && parts[2] == "range" {
			handleFrameRange(w, r, store, m, id, options)
			return
		}
		if len(parts) == 3 {
			frameIndex, err := strconv.Atoi(parts[2])
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("invalid frame index %q", parts[2]))
				return
			}
			// Reject out-of-range indices with a clear 400 instead of surfacing
			// the backend's internal failure as a confusing 503.
			if frameIndex < 0 {
				writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("frame index %d must be >= 0", frameIndex))
				return
			}
			if total := datasetFrameCount(r.Context(), store, m, id, firstNonEmpty(r.URL.Query().Get("backend"), options.Backend), options.GromacsCommand); total > 0 && frameIndex >= total {
				writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("frame index %d is out of range for %d frames (valid indices 0..%d)", frameIndex, total, total-1))
				return
			}
			format := strings.TrimPrefix(strings.ToLower(r.URL.Query().Get("format")), ".")
			if format == "json" || format == "bin" || strings.Contains(r.Header.Get("Accept"), "application/vnd.mdsrv.frame+bin") {
				atomSubset := r.URL.Query().Get("atom_subset")
				frame, err := frameWithPolicy(r.Context(), store, m, id, frameIndex, atomSubset, firstNonEmpty(r.URL.Query().Get("backend"), options.Backend), options.GromacsCommand)
				if err != nil {
					writeHTTPError(w, http.StatusServiceUnavailable, err)
					return
				}
				if err := options.Limits.CheckFrame(fmt.Sprintf("frame %d", frameIndex), frame); err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				if format == "bin" || strings.Contains(r.Header.Get("Accept"), "application/vnd.mdsrv.frame+bin") {
					data, err := mdsrv.EncodeFrameBinary(frame)
					if err != nil {
						writeHTTPError(w, http.StatusInternalServerError, err)
						return
					}
					w.Header().Set("Content-Type", "application/vnd.mdsrv.frame+bin")
					_, _ = w.Write(data)
					return
				}
				writeHTTPJSON(w, http.StatusOK, frame)
				return
			}
			if format == "" {
				format = "gro"
			}
			tmp, err := os.CreateTemp("", fmt.Sprintf("mdsrv-frame-%s-%d-*.%s", id, frameIndex, format))
			if err != nil {
				writeHTTPError(w, http.StatusInternalServerError, err)
				return
			}
			outputPath := tmp.Name()
			_ = tmp.Close()
			defer os.Remove(outputPath)
			if _, err := store.ExtractFrame(r.Context(), id, frameIndex, outputPath, options.GromacsCommand); err != nil {
				writeHTTPError(w, http.StatusBadGateway, err)
				return
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-frame-%d.%s"`, id, frameIndex, format))
			http.ServeFile(w, r, outputPath)
			return
		}
		http.NotFound(w, r)
	case "selections":
		if len(parts) == 2 {
			switch r.Method {
			case http.MethodGet:
				writeHTTPJSON(w, http.StatusOK, m.Selections)
			case http.MethodPost:
				var selection mdsrv.Selection
				if err := json.NewDecoder(r.Body).Decode(&selection); err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				saved, err := store.SaveSelection(id, selection)
				if err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				writeHTTPJSON(w, http.StatusOK, saved)
			default:
				methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
			}
			return
		}
		if len(parts) == 3 {
			selectionID := parts[2]
			switch r.Method {
			case http.MethodGet:
				selection, ok := mdsrv.FindSelection(m, selectionID)
				if !ok {
					writeHTTPError(w, http.StatusNotFound, fmt.Errorf("selection %q not found", selectionID))
					return
				}
				writeHTTPJSON(w, http.StatusOK, selection)
			case http.MethodDelete:
				if err := store.DeleteSelection(id, selectionID); err != nil {
					writeHTTPError(w, http.StatusBadRequest, err)
					return
				}
				writeHTTPJSON(w, http.StatusOK, map[string]string{"deleted": selectionID})
			default:
				methodNotAllowed(w, http.MethodGet+", "+http.MethodDelete)
			}
			return
		}
		http.NotFound(w, r)
	case "analyses":
		if r.Method == http.MethodPost {
			var request mdsrv.AnalysisRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			// Resolve and confine the output path before doing any backend work
			// so a traversal/absolute-path attempt is rejected up front.
			outputPath, err := resolveTraceOutputWithinStore(store, request.Output, id, request.Type)
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			trace, err := analyzeWithPolicy(r.Context(), store, m, id, request, firstNonEmpty(r.URL.Query().Get("backend"), options.Backend), options.GromacsCommand)
			if err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			if err := mdsrv.WriteTrace(outputPath, request.Format, trace); err != nil {
				writeHTTPError(w, http.StatusInternalServerError, err)
				return
			}
			recordedOutput := ""
			if relative, ok := storeRelativePathIfInside(store.Root, outputPath); ok {
				recordedOutput = relative
			}
			_ = store.RecordAnalysis(id, mdsrv.Analysis{ID: firstNonEmpty(request.ID, request.Type), Type: request.Type, Selection: request.Selection, Selections: request.Selections, ReferenceFrame: request.ReferenceFrame, Frames: "all", Output: recordedOutput})
			writeHTTPJSON(w, http.StatusOK, trace)
			return
		}
		writeHTTPJSON(w, http.StatusOK, m.Analyses)
	case "rename":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var body struct {
			ID    string `json:"id"`
			NewID string `json:"new_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		newID := firstNonEmpty(body.NewID, body.ID)
		renamed, err := store.RenameDataset(id, newID)
		if err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, renamed)
	default:
		http.NotFound(w, r)
	}
}

func allowedDatasetWriteMethod(method string, parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	switch parts[1] {
	case "analyses":
		return method == http.MethodPost
	case "rename":
		return method == http.MethodPost
	case "selections":
		return method == http.MethodPost || method == http.MethodDelete
	case "frames":
		return method == http.MethodPost && len(parts) == 3 && (parts[2] == "index" || parts[2] == "chunks")
	default:
		return false
	}
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func handleFrameRange(w http.ResponseWriter, r *http.Request, store mdsrv.Store, m mdsrv.Manifest, id string, options serverOptions) {
	if err := options.Limits.CheckManifest(m); err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	start, err := optionalQueryInt(r, "start", 0)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	stop, err := optionalQueryInt(r, "stop", start)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	stride, err := optionalQueryInt(r, "stride", 1)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	if start < 0 || stop < start || stride < 1 {
		writeHTTPError(w, http.StatusBadRequest, errors.New("range requires start >= 0, stop >= start, and stride >= 1"))
		return
	}
	count := ((stop - start) / stride) + 1
	if count > options.MaxFrameRange {
		writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("frame range returns %d frames; maximum is %d", count, options.MaxFrameRange))
		return
	}
	atomSubset := r.URL.Query().Get("atom_subset")
	requestBackend := firstNonEmpty(r.URL.Query().Get("backend"), options.Backend)
	// Reject out-of-range indices up front with a clear 400 instead of letting a
	// non-existent frame fail deep inside the backend, which surfaces as a
	// confusing 503 that leaks the backend's internal error.
	if total := datasetFrameCount(r.Context(), store, m, id, requestBackend, options.GromacsCommand); total > 0 && stop >= total {
		writeHTTPError(w, http.StatusBadRequest, fmt.Errorf("frame range %d..%d is out of range for %d frames (valid indices 0..%d)", start, stop, total, total-1))
		return
	}
	frames := make([]mdsrv.Frame, 0, count)
	for frameIndex := start; frameIndex <= stop; frameIndex += stride {
		frame, err := frameWithPolicy(r.Context(), store, m, id, frameIndex, atomSubset, requestBackend, options.GromacsCommand)
		if err != nil {
			writeHTTPError(w, http.StatusServiceUnavailable, err)
			return
		}
		if err := options.Limits.CheckFrame(fmt.Sprintf("frame %d", frameIndex), frame); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err)
			return
		}
		frames = append(frames, frame)
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"dataset_id": id, "start": start, "stop": stop, "stride": stride, "frames": frames})
}

// datasetFrameCount returns the total number of frames for bounds validation,
// preferring the manifest's cached count and falling back to a backend probe.
// It returns 0 when the count cannot be determined, in which case callers skip
// the bounds check rather than reject a possibly-valid request.
func datasetFrameCount(ctx context.Context, store mdsrv.Store, m mdsrv.Manifest, id, backend, gromacsCommand string) int {
	if len(m.Inputs.Trajectories) > 0 && m.Inputs.Trajectories[0].FrameCount > 0 {
		return m.Inputs.Trajectories[0].FrameCount
	}
	if info, err := trajectoryInfoWithPolicy(ctx, store, m, id, backend, gromacsCommand); err == nil {
		return info.Frames
	}
	return 0
}

func serveRefFile(w http.ResponseWriter, r *http.Request, store mdsrv.Store, ref mdsrv.FileRef) {
	if ref.Path == "" {
		writeHTTPError(w, http.StatusBadRequest, errors.New("remote URL-backed files are not served by this process"))
		return
	}
	path, err := store.SafeResolvePath(ref.Path)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err)
		return
	}
	http.ServeFile(w, r, path)
}

func serveStoreFile(store mdsrv.Store, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		http.ServeFile(w, r, filepath.Join(store.Root, name))
	}
}

func validateIngestAgainstServerPolicy(opts mdsrv.IngestOptions, options serverOptions) error {
	for _, path := range []string{opts.Topology, opts.Trajectory, opts.Cache} {
		if err := ensureAllowedPath(path, options.AllowPaths); err != nil {
			return err
		}
	}
	for _, rawURL := range []string{opts.TopologyURL, opts.TrajectoryURL} {
		if err := ensureAllowedHost(rawURL, options.AllowHosts); err != nil {
			return err
		}
	}
	return nil
}

func validateSessionAgainstServerPolicy(opts mdsrv.SessionOptions, options serverOptions) error {
	return ensureAllowedPath(opts.File, options.AllowPaths)
}

func ensureAllowedPath(path string, allowedRoots []string) error {
	if path == "" || len(allowedRoots) == 0 {
		return nil
	}
	if strings.Contains(path, "://") {
		return nil
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(absoluteRoot, absolutePath)
		if err != nil {
			continue
		}
		if relative == "." || (!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != "..") {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside allowed roots", path)
}

func ensureAllowedHost(rawURL string, allowedHosts []string) error {
	if rawURL == "" || len(allowedHosts) == 0 {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range allowedHosts {
		if strings.EqualFold(host, normalizedHost(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("url host %q is not allowed", host)
}

func normalizedHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return value
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil {
			return parsed.Hostname()
		}
	}
	if strings.Contains(value, ":") {
		host, _, err := net.SplitHostPort(value)
		if err == nil {
			return strings.ToLower(host)
		}
	}
	return strings.Trim(value, "[]")
}

type responseLogRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseLogRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseLogRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	written, err := r.ResponseWriter.Write(data)
	r.bytes += written
	return written, err
}

func requestLogMiddleware(next http.Handler, out io.Writer) http.Handler {
	if out == nil {
		out = io.Discard
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseLogRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		_ = json.NewEncoder(out).Encode(map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339Nano),
			"method":      r.Method,
			"path":        r.URL.Path,
			"query":       r.URL.RawQuery,
			"request_id":  w.Header().Get("X-Request-ID"),
			"status":      recorder.status,
			"bytes":       recorder.bytes,
			"duration_ms": float64(time.Since(started).Microseconds()) / 1000,
			"remote_addr": r.RemoteAddr,
		})
	})
}

// maxRequestBodyBytes bounds JSON control payloads (ingest options, selection,
// analysis, and job requests). Trajectory and session data enter through paths
// or URLs, never the request body, so 1 MiB is a generous ceiling.
const maxRequestBodyBytes = 1 << 20

func maxBodyBytesMiddleware(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/version" {
			next.ServeHTTP(w, r)
			return
		}
		if tokenMatchesRequest(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		writeHTTPError(w, http.StatusUnauthorized, errors.New("authentication required"))
	})
}

func tokenMatchesRequest(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	if subtleConstantTimeEqual(r.Header.Get("X-MDSRV-Token"), token) {
		return true
	}
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if strings.HasPrefix(value, prefix) {
		return subtleConstantTimeEqual(strings.TrimSpace(strings.TrimPrefix(value, prefix)), token)
	}
	return false
}

func subtleConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// storeValidationError names the datasets that failed so the message points at
// what to fix rather than just saying the store is bad.
func storeValidationError(datasetFailures []string) error {
	if len(datasetFailures) == 0 {
		return codedErrorf(codeValidationFailed, "store validation failed")
	}
	return codedErrorf(codeValidationFailed, "store validation failed: %s", strings.Join(datasetFailures, ", "))
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeHTTPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHTTPError(w http.ResponseWriter, status int, err error) {
	body := map[string]string{
		"error": err.Error(),
		"code":  httpStatusCode(status),
	}
	if requestID := w.Header().Get("X-Request-ID"); requestID != "" {
		body["request_id"] = requestID
	}
	writeHTTPJSON(w, status, body)
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeHTTPError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func httpStatusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	// The server returns 429 when the job queue is full and 502 when an upstream
	// call fails, but neither was mapped: 429 fell through to the generic "error"
	// and 502 to "internal_error". Backpressure is the one server response a
	// client most needs to branch on — it is the retryable one — so it cannot be
	// the only response without a typed code.
	case http.StatusTooManyRequests:
		return "too_many_requests"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "error"
	}
}

func optionalQueryInt(r *http.Request, name string, fallback int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return parsed, nil
}

func reportError(report mdsrv.ValidationReport) error {
	if validationOK(report) {
		return nil
	}
	return fmt.Errorf("validation failed")
}

func validationOK(report mdsrv.ValidationReport) bool {
	for _, file := range report.Files {
		if !file.Exists || file.Error != "" {
			return false
		}
	}
	return true
}

func checkWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".hlmdsrv-doctor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

func checkWritableDirectory(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return checkWritable(dir)
}

type trajectoryInfoReport struct {
	ID         string  `json:"id"`
	AtomCount  int     `json:"atom_count,omitempty"`
	FrameCount int     `json:"frame_count,omitempty"`
	TimeStart  float64 `json:"time_start,omitempty"`
	TimeEnd    float64 `json:"time_end,omitempty"`
	TimeStep   float64 `json:"time_step,omitempty"`
	TimeUnit   string  `json:"time_unit,omitempty"`
}

type demoReport struct {
	ID         string `json:"id"`
	Topology   string `json:"topology"`
	Frames     string `json:"frames"`
	Trajectory string `json:"trajectory"`
	Job        string `json:"job,omitempty"`
	Store      string `json:"store,omitempty"`
	Manifest   string `json:"manifest,omitempty"`
	AtomCount  int    `json:"atom_count,omitempty"`
	FrameCount int    `json:"frame_count,omitempty"`
}

func trajectoryReport(m mdsrv.Manifest) trajectoryInfoReport {
	report := trajectoryInfoReport{ID: m.Metadata.ID}
	if len(m.Inputs.Trajectories) == 0 {
		return report
	}
	trajectory := m.Inputs.Trajectories[0]
	report.AtomCount = trajectory.AtomCount
	report.FrameCount = trajectory.FrameCount
	report.TimeStart = trajectory.TimeStart
	report.TimeEnd = trajectory.TimeEnd
	report.TimeStep = trajectory.TimeStep
	report.TimeUnit = trajectory.TimeUnit
	return report
}

func (a app) runDemoGromacs(ctx context.Context, flags *demoFlags) (demoReport, error) {
	if err := mdsrv.ValidateID(flags.id); err != nil {
		return demoReport{}, fmt.Errorf("id: %w", err)
	}
	demo, err := gromacs.CreateDemoTrajectory(ctx, gromacs.DemoTrajectoryOptions{
		OutDir:  flags.out,
		Frames:  flags.frames,
		Force:   flags.force,
		Command: flags.gmxCommand,
	})
	if err != nil {
		return demoReport{}, err
	}
	report := demoReport{
		ID:         flags.id,
		Topology:   demo.Topology,
		Frames:     demo.Frames,
		Trajectory: demo.Trajectory,
		AtomCount:  demo.AtomCount,
		FrameCount: demo.FrameCount,
	}
	if flags.store == "" {
		return report, nil
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return demoReport{}, err
	}
	manifest, err := store.Ingest(mdsrv.IngestOptions{
		ID:          flags.id,
		Name:        flags.name,
		Description: "Tiny synthetic trajectory generated with gmx trjconv for CLI testing.",
		CreatedBy:   "hlmdsrv demo gromacs",
		Topology:    demo.Topology,
		Trajectory:  demo.Trajectory,
		Force:       flags.force,
	})
	if err != nil {
		return demoReport{}, err
	}
	if probed, err := store.ProbeDataset(ctx, manifest.Metadata.ID, flags.gmxCommand); err == nil {
		manifest = probed
	} else {
		fmt.Fprintln(a.stderr, "warning: gromacs probe failed:", err)
	}
	report.Store = store.Root
	report.Manifest = store.ManifestPath(manifest.Metadata.ID)
	if len(manifest.Inputs.Trajectories) > 0 {
		report.AtomCount = manifest.Inputs.Trajectories[0].AtomCount
		report.FrameCount = manifest.Inputs.Trajectories[0].FrameCount
	}
	return report, nil
}

func (a app) runDemoCreate(ctx context.Context, flags *demoFlags) (demoReport, error) {
	if strings.TrimSpace(flags.job) == "" {
		flags.job = "job.yaml"
	}
	if !filepath.IsAbs(flags.job) {
		flags.job = filepath.Join(flags.out, flags.job)
	}
	if err := ensureOutputPath(flags.job, flags.force); err != nil {
		return demoReport{}, err
	}
	report, err := a.runDemoGromacs(ctx, flags)
	if err != nil {
		return demoReport{}, err
	}
	jobDir := filepath.Dir(flags.job)
	topologyPath := report.Topology
	trajectoryPath := report.Trajectory
	if relative, err := filepath.Rel(jobDir, topologyPath); err == nil {
		topologyPath = relative
	}
	if relative, err := filepath.Rel(jobDir, trajectoryPath); err == nil {
		trajectoryPath = relative
	}
	manifest := mdsrv.Manifest{
		Version: mdsrv.ManifestVersion,
		Metadata: mdsrv.Metadata{
			ID:          flags.id,
			Name:        flags.name,
			Description: "Tiny synthetic trajectory generated by hlmdsrv demo create.",
			CreatedBy:   "hlmdsrv demo create",
		},
		Inputs: mdsrv.Inputs{
			Topology: mdsrv.FileRef{Path: filepath.ToSlash(topologyPath), Format: "gro"},
			Trajectories: []mdsrv.FileRef{{
				Path:      filepath.ToSlash(trajectoryPath),
				Format:    "xtc",
				TimeUnit:  "ps",
				CoordUnit: "nm",
			}},
		},
		Selections: []mdsrv.Selection{{
			ID:         "first-two",
			Expression: "1-2",
			Kind:       "atom-index",
		}},
		Streaming: mdsrv.Streaming{
			ChunkSizeFrames:   2,
			MaterializeChunks: true,
		},
		Analyses: []mdsrv.Analysis{{
			ID:         "d12",
			Type:       "distance",
			Selections: map[string]string{"a": "1", "b": "2"},
			Output:     filepath.ToSlash(filepath.Join("traces", "d12.csv")),
		}},
		Visualization: mdsrv.Visualization{
			MVS: mdsrv.MVSVisualization{Scene: filepath.ToSlash(filepath.Join("visualization", flags.id+".mvsj"))},
			Camera: mdsrv.Camera{
				Focus: "first-two",
			},
		},
		Outputs: []mdsrv.Output{{Type: "mdsrvx", Path: flags.id + ".mdsrvx"}},
	}
	if err := mdsrv.WriteManifestFile(flags.job, manifest); err != nil {
		return demoReport{}, err
	}
	report.Job = flags.job
	return report, nil
}

func manifestRoot(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == mdsrv.DatasetsDir {
		return filepath.Dir(dir)
	}
	return dir
}
