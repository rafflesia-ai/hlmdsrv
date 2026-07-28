package mdsrvcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type frameFlags struct {
	store      string
	out        string
	format     string
	atomSubset string
	backend    string
	gmxCommand string
}

type analyzeFlags struct {
	store          string
	out            string
	format         string
	id             string
	selection      string
	a              string
	b              string
	c              string
	d              string
	cutoff         float64
	referenceFrame int
	record         bool
	backend        string
	gmxCommand     string
	jsonReport     bool
}

type sessionFlags struct {
	store       string
	id          string
	datasetID   string
	name        string
	description string
	source      string
	version     string
	file        string
	sticky      bool
	force       bool
	jsonReport  bool
}

type batchCLIFlags struct {
	store           string
	concurrency     int
	continueOnError bool
	force           bool
	jsonReport      bool
}

type packFlags struct {
	store      string
	out        string
	force      bool
	jsonReport bool
}

type unpackFlags struct {
	store      string
	force      bool
	jsonReport bool
}

type compatFlags struct {
	store      string
	docker     bool
	image      string
	port       int
	timeout    time.Duration
	jsonReport bool
}

type compatReport struct {
	Store      string   `json:"store"`
	OK         bool     `json:"ok"`
	Checks     []string `json:"checks,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Docker     string   `json:"docker,omitempty"`
	DockerOK   bool     `json:"docker_ok,omitempty"`
	DockerLogs string   `json:"docker_logs,omitempty"`
}

func (a app) frameCommand() *cobra.Command {
	flags := &frameFlags{}
	cmd := &cobra.Command{
		Use:        "frame DATASET_ID FRAME_INDEX",
		Aliases:    []string{"get-frame"},
		Short:      "Extract one trajectory frame through the backend bridge",
		Deprecated: "use `hlmdsrv frames get DATASET_ID FRAME_INDEX` instead",
		Hidden:     true,
		Args:       cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			frameIndex, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid frame index %q", args[1])
			}
			return a.runFrame(cmd.Context(), args[0], frameIndex, flags)
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output path; stdout when omitted")
	cmd.Flags().StringVar(&flags.format, "format", "", "output format: json or bin; inferred from --out when omitted")
	cmd.Flags().StringVar(&flags.atomSubset, "atom-subset", "", "override atom subset selection for this frame")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS fallback command override")
	return cmd
}

func (a app) analyzeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "analyze", Aliases: []string{"analysis"}, Short: "Run trajectory analysis through the backend bridge"}
	for _, kind := range []string{"distance", "angle", "dihedral", "rmsd", "rgyr", "rmsf", "contacts", "sasa", "hbonds"} {
		flags := &analyzeFlags{}
		commandKind := kind
		c := &cobra.Command{
			Use:   commandKind + " DATASET_ID",
			Short: "Run " + commandKind + " analysis",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.runAnalyze(cmd.Context(), args[0], commandKind, flags)
			},
		}
		c.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
		c.Flags().StringVarP(&flags.out, "out", "o", "", "trace output path")
		c.Flags().StringVar(&flags.format, "format", "", "output format: csv or json")
		c.Flags().StringVar(&flags.id, "id", "", "analysis id")
		c.Flags().StringVar(&flags.selection, "selection", "", "atom selection for single-group analyses (rmsd, rmsf, rgyr, sasa)")
		c.Flags().StringVar(&flags.a, "a", "", "first selection")
		c.Flags().StringVar(&flags.b, "b", "", "second selection")
		c.Flags().StringVar(&flags.c, "c", "", "third selection for angle/dihedral")
		c.Flags().StringVar(&flags.d, "d", "", "fourth selection for dihedral")
		c.Flags().Float64Var(&flags.cutoff, "cutoff", 0.5, "contact cutoff in nm")
		c.Flags().IntVar(&flags.referenceFrame, "reference-frame", 0, "reference frame for RMSD")
		c.Flags().BoolVar(&flags.record, "record", true, "record analysis metadata into the dataset manifest")
		bindBackendFlag(c, &flags.backend)
		c.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS fallback command override; fallback selections are 1-based atom indices")
		c.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable completion report")
		cmd.AddCommand(c)
	}
	return cmd
}

func (a app) sessionCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage published MDsrv sessions"}
	flags := &sessionFlags{}
	publish := &cobra.Command{
		Use:   "publish",
		Short: "Publish a Mol* session/state file into the MDsrv store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			ref, err := store.PublishSession(mdsrv.SessionOptions{
				ID:          flags.id,
				DatasetID:   flags.datasetID,
				Name:        flags.name,
				Description: flags.description,
				Source:      flags.source,
				Version:     flags.version,
				File:        flags.file,
				IsSticky:    flags.sticky,
				Force:       flags.force,
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, ref)
			}
			fmt.Fprintf(a.stdout, "published %s\n", ref.ID)
			fmt.Fprintf(a.stdout, "session %s\n", ref.Path)
			return nil
		},
	}
	publish.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	publish.Flags().StringVar(&flags.id, "id", "", "session id")
	publish.Flags().StringVar(&flags.datasetID, "dataset", "", "dataset id")
	publish.Flags().StringVar(&flags.name, "name", "", "display name")
	publish.Flags().StringVar(&flags.description, "description", "", "session description")
	publish.Flags().StringVar(&flags.source, "source", "", "source URL, DOI, or provenance label")
	publish.Flags().StringVar(&flags.version, "version", "3.4.0", "MDsrv viewer version")
	publish.Flags().StringVar(&flags.file, "file", "", "session file, usually .molj")
	publish.Flags().BoolVar(&flags.sticky, "sticky", false, "mark as sticky in session_index.json")
	publish.Flags().BoolVar(&flags.force, "force", false, "overwrite existing session file")
	publish.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	_ = publish.MarkFlagRequired("id")
	_ = publish.MarkFlagRequired("dataset")
	_ = publish.MarkFlagRequired("file")
	listFlags := &listFlags{}
	list := &cobra.Command{
		Use:   "list",
		Short: "List sessions in a store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(listFlags.store)
			if err != nil {
				return err
			}
			items, err := store.ListSessions()
			if err != nil {
				return err
			}
			if listFlags.jsonReport {
				return writeJSON(a.stdout, items)
			}
			for _, item := range items {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", item.ID, item.Name, item.Path)
			}
			return nil
		},
	}
	list.Flags().StringVar(&listFlags.store, "store", "./mdsrv-data", "MDsrv store root")
	list.Flags().BoolVar(&listFlags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(publish, list)
	return cmd
}

func (a app) batchCommand() *cobra.Command {
	flags := &batchCLIFlags{}
	cmd := &cobra.Command{
		Use:   "batch JOBS",
		Short: "Ingest a JSONL/YAML/JSON batch of trajectory datasets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runBatch(cmd.Context(), args[0], flags)
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().IntVar(&flags.concurrency, "concurrency", 1, "number of ingest jobs to run concurrently")
	cmd.Flags().BoolVar(&flags.continueOnError, "continue-on-error", false, "continue after failed jobs")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing datasets")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", true, "write JSON report lines to stdout")
	return cmd
}

func (a app) packCommand() *cobra.Command {
	flags := &packFlags{}
	cmd := &cobra.Command{
		Use:     "pack DATASET_ID",
		Aliases: []string{"archive"},
		Short:   "Pack a dataset into a .mdsrvx archive",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			out := flags.out
			if out == "" {
				out = args[0] + ".mdsrvx"
			}
			report, err := store.PackDataset(args[0], out, flags.force)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "packed %s\n", report.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output .mdsrvx path")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing archive")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) unpackCommand() *cobra.Command {
	flags := &unpackFlags{}
	cmd := &cobra.Command{
		Use:     "unpack ARCHIVE.mdsrvx",
		Aliases: []string{"restore"},
		Short:   "Unpack a .mdsrvx archive into a store",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			report, err := store.UnpackArchive(args[0], flags.force)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "unpacked %s\n", report.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing store files")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) compatCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "compat", Short: "Check upstream MDsrv compatibility"}
	flags := &compatFlags{}
	check := &cobra.Command{
		Use:   "check",
		Short: "Check store layout and optionally smoke-test mdsrv-remote Docker",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := a.runCompatCheck(cmd.Context(), flags)
			if flags.jsonReport {
				if err := writeJSON(a.stdout, report); err != nil {
					return err
				}
			} else {
				status := "ok"
				if !report.OK {
					status = "fail"
				}
				fmt.Fprintf(a.stdout, "%s\t%s\n", status, report.Store)
				for _, check := range report.Checks {
					fmt.Fprintf(a.stdout, "ok\t%s\n", check)
				}
				for _, warning := range report.Warnings {
					fmt.Fprintln(a.stderr, "warning:", warning)
				}
				if report.Docker != "" {
					dockerStatus := "ok"
					if !report.DockerOK {
						dockerStatus = "fail"
					}
					fmt.Fprintf(a.stdout, "%s\tdocker %s\n", dockerStatus, report.Docker)
				}
			}
			if !report.OK {
				return fmt.Errorf("compatibility check failed")
			}
			return nil
		},
	}
	check.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	check.Flags().BoolVar(&flags.docker, "docker", false, "run upstream mdsrv-remote container smoke test")
	check.Flags().StringVar(&flags.image, "image", "dwiegreffe/mdsrv-remote", "Docker image for upstream streaming server")
	check.Flags().IntVar(&flags.port, "port", 18087, "temporary host port for Docker smoke test")
	check.Flags().DurationVar(&flags.timeout, "timeout", 30*time.Second, "Docker smoke test timeout")
	check.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(check)
	return cmd
}

func (a app) runFrame(ctx context.Context, datasetID string, frameIndex int, flags *frameFlags) error {
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return err
	}
	m, err := store.LoadDataset(datasetID)
	if err != nil {
		return err
	}
	frame, err := frameWithPolicy(ctx, store, m, datasetID, frameIndex, flags.atomSubset, flags.backend, flags.gmxCommand)
	if err != nil {
		return err
	}
	format := outputFormat(flags.format, flags.out, "json")
	switch format {
	case "json":
		if flags.out != "" {
			return writeJSONFile(flags.out, frame)
		}
		return writeJSON(a.stdout, frame)
	case "bin", "binary":
		data, err := mdsrv.EncodeFrameBinary(frame)
		if err != nil {
			return err
		}
		if flags.out == "" {
			_, err = a.stdout.Write(data)
			return err
		}
		if err := os.MkdirAll(filepath.Dir(flags.out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(flags.out, data, 0o644)
	default:
		return fmt.Errorf("unsupported frame output format %q", format)
	}
}

func frameWithFallback(ctx context.Context, store mdsrv.Store, m mdsrv.Manifest, datasetID string, frameIndex int, atomSubset string, gromacsCommand string) (mdsrv.Frame, error) {
	frame, err := mdsrv.NewBackend(store).Frame(ctx, m, frameIndex, atomSubset)
	if err == nil {
		return frame, nil
	}
	pythonErr := err
	if atomSubset != "" {
		return mdsrv.Frame{}, fmt.Errorf("%w; GROMACS fallback does not support atom-subset JSON extraction", pythonErr)
	}
	tmp, tmpErr := os.CreateTemp("", fmt.Sprintf("mdsrv-frame-%s-%d-*.gro", datasetID, frameIndex))
	if tmpErr != nil {
		return mdsrv.Frame{}, tmpErr
	}
	outputPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(outputPath)
	if _, err := store.ExtractFrame(ctx, datasetID, frameIndex, outputPath, gromacsCommand); err != nil {
		return mdsrv.Frame{}, fmt.Errorf("python backend failed: %v; GROMACS fallback failed: %w", pythonErr, err)
	}
	frame, err = mdsrv.ParseGROFrame(outputPath, frameIndex)
	if err != nil {
		return mdsrv.Frame{}, err
	}
	return frame, nil
}

func (a app) runAnalyze(ctx context.Context, datasetID, kind string, flags *analyzeFlags) error {
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return err
	}
	m, err := store.LoadDataset(datasetID)
	if err != nil {
		return err
	}
	// Single-group analyses read --selection; accept the advertised --a flag as an
	// alias so a user who reaches for --a (which every analyze subcommand exposes)
	// is not met with a confusing "atom index selection is required" error.
	selection := flags.selection
	if selection == "" && isSingleGroupAnalysis(kind) {
		selection = flags.a
	}
	request := mdsrv.AnalysisRequest{
		ID:             firstNonEmpty(flags.id, kind),
		Type:           kind,
		Selection:      selection,
		Selections:     selectionsFor(kind, flags),
		ReferenceFrame: flags.referenceFrame,
		Cutoff:         flags.cutoff,
		Output:         outputAnalysisPath(datasetID, kind, flags.out, flags.format),
		Format:         flags.format,
	}
	trace, err := analyzeWithPolicy(ctx, store, m, datasetID, request, flags.backend, flags.gmxCommand)
	if err != nil {
		return err
	}
	outputPath := request.Output
	if flags.out == "" && !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(store.Root, outputPath)
	}
	if err := mdsrv.WriteTrace(outputPath, flags.format, trace); err != nil {
		return err
	}
	if flags.record {
		recordedOutput := request.Output
		if flags.out != "" {
			if relative, ok := storeRelativePathIfInside(store.Root, outputPath); ok {
				recordedOutput = relative
			} else {
				recordedOutput = ""
			}
		}
		if err := store.RecordAnalysis(datasetID, mdsrv.Analysis{
			ID:             request.ID,
			Type:           kind,
			Selection:      selection,
			Selections:     request.Selections,
			ReferenceFrame: flags.referenceFrame,
			Cutoff:         flags.cutoff,
			Frames:         "all",
			Output:         recordedOutput,
		}); err != nil {
			return err
		}
	}
	if flags.jsonReport {
		return writeJSON(a.stdout, map[string]any{
			"ok":       true,
			"dataset":  datasetID,
			"id":       request.ID,
			"type":     kind,
			"backend":  trace.Backend,
			"path":     outputPath,
			"format":   flags.format,
			"values":   len(trace.Values),
			"recorded": flags.record,
		})
	}
	fmt.Fprintln(a.stdout, outputPath)
	return nil
}

func analyzeWithGromacsFallback(ctx context.Context, store mdsrv.Store, m mdsrv.Manifest, datasetID string, request mdsrv.AnalysisRequest, gromacsCommand string) (mdsrv.Trace, error) {
	report := trajectoryReport(m)
	if report.FrameCount == 0 {
		probed, err := store.ProbeDataset(ctx, datasetID, gromacsCommand)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		m = probed
		report = trajectoryReport(m)
	}
	if report.FrameCount <= 0 {
		return mdsrv.Trace{}, errors.New("GROMACS fallback could not determine frame count")
	}
	frames := make([]mdsrv.Frame, 0, report.FrameCount)
	for i := 0; i < report.FrameCount; i++ {
		tmp, err := os.CreateTemp("", fmt.Sprintf("mdsrv-analysis-%s-%d-*.gro", datasetID, i))
		if err != nil {
			return mdsrv.Trace{}, err
		}
		path := tmp.Name()
		_ = tmp.Close()
		if _, err := store.ExtractFrame(ctx, datasetID, i, path, gromacsCommand); err != nil {
			_ = os.Remove(path)
			return mdsrv.Trace{}, err
		}
		frame, err := mdsrv.ParseGROFrame(path, i)
		_ = os.Remove(path)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		frames = append(frames, frame)
	}
	trace := mdsrv.Trace{
		Backend: "gromacs",
		ID:      firstNonEmpty(request.ID, request.Type),
		Type:    request.Type,
		Unit:    "nm",
	}
	values := make([]mdsrv.TraceValue, 0, len(frames))
	switch request.Type {
	case "distance":
		a, b, err := twoSelectionGroups(request, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: vecDistance(center(frame, a), center(frame, b))})
		}
	case "angle":
		groups, err := selectionGroups(request, []string{"a", "b", "c"}, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		trace.Unit = "degree"
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: angleValue(center(frame, groups[0]), center(frame, groups[1]), center(frame, groups[2]))})
		}
	case "dihedral":
		groups, err := selectionGroups(request, []string{"a", "b", "c", "d"}, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		trace.Unit = "degree"
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: dihedralValue(center(frame, groups[0]), center(frame, groups[1]), center(frame, groups[2]), center(frame, groups[3]))})
		}
	case "rmsd":
		group, err := parseAtomSelection(request.Selection, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		if request.ReferenceFrame < 0 || request.ReferenceFrame >= len(frames) {
			return mdsrv.Trace{}, fmt.Errorf("reference frame %d is out of range", request.ReferenceFrame)
		}
		reference := selectedCoords(frames[request.ReferenceFrame], group)
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: rmsdValue(selectedCoords(frame, group), reference)})
		}
	case "rgyr":
		group, err := parseAtomSelection(request.Selection, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: radiusOfGyration(selectedCoords(frame, group))})
		}
	case "rmsf":
		group, err := parseAtomSelection(request.Selection, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		values = rmsfTrace(frames, group)
		trace.Unit = "nm"
	case "contacts":
		a, b, err := twoSelectionGroups(request, report.AtomCount)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		cutoff := request.Cutoff
		if cutoff <= 0 {
			cutoff = 0.5
		}
		trace.Unit = "count"
		for _, frame := range frames {
			values = append(values, mdsrv.TraceValue{Frame: frame.Frame, Time: frame.Time, Value: float64(contactCount(frame, a, b, cutoff))})
		}
	default:
		return mdsrv.Trace{}, fmt.Errorf("unsupported GROMACS fallback analysis %q", request.Type)
	}
	trace.Values = values
	return trace, nil
}

func twoSelectionGroups(request mdsrv.AnalysisRequest, atomCount int) ([]int, []int, error) {
	groups, err := selectionGroups(request, []string{"a", "b"}, atomCount)
	if err != nil {
		return nil, nil, err
	}
	return groups[0], groups[1], nil
}

func selectionGroups(request mdsrv.AnalysisRequest, keys []string, atomCount int) ([][]int, error) {
	groups := make([][]int, 0, len(keys))
	for _, key := range keys {
		value := request.Selections[key]
		group, err := parseAtomSelection(value, atomCount)
		if err != nil {
			return nil, fmt.Errorf("selection %s: %w", key, err)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func parseAtomSelection(value string, atomCount int) ([]int, error) {
	atoms, err := mdsrv.ParseAtomSelection(value, atomCount)
	if err != nil {
		return nil, fmt.Errorf("%w; GROMACS fallback accepts all, atom:1, atom:1-3, and comma-separated 1-based atom indexes", err)
	}
	return atoms, nil
}

func center(frame mdsrv.Frame, indices []int) [3]float64 {
	var result [3]float64
	for _, index := range indices {
		coord := frame.Coordinates[index]
		result[0] += float64(coord[0])
		result[1] += float64(coord[1])
		result[2] += float64(coord[2])
	}
	scale := float64(len(indices))
	result[0] /= scale
	result[1] /= scale
	result[2] /= scale
	return result
}

func selectedCoords(frame mdsrv.Frame, indices []int) [][3]float64 {
	result := make([][3]float64, len(indices))
	for i, index := range indices {
		coord := frame.Coordinates[index]
		result[i] = [3]float64{float64(coord[0]), float64(coord[1]), float64(coord[2])}
	}
	return result
}

func vecDistance(a, b [3]float64) float64 {
	return math.Sqrt(square(a[0]-b[0]) + square(a[1]-b[1]) + square(a[2]-b[2]))
}

func angleValue(a, b, c [3]float64) float64 {
	ba := [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
	bc := [3]float64{c[0] - b[0], c[1] - b[1], c[2] - b[2]}
	denom := math.Sqrt(dot(ba, ba) * dot(bc, bc))
	if denom == 0 {
		return 0
	}
	value := dot(ba, bc) / denom
	if value > 1 {
		value = 1
	}
	if value < -1 {
		value = -1
	}
	return math.Acos(value) * 180 / math.Pi
}

func dihedralValue(a, b, c, d [3]float64) float64 {
	b0 := [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
	b1 := normalize([3]float64{c[0] - b[0], c[1] - b[1], c[2] - b[2]})
	b2 := [3]float64{d[0] - c[0], d[1] - c[1], d[2] - c[2]}
	v := subtract(b0, scale(b1, dot(b0, b1)))
	w := subtract(b2, scale(b1, dot(b2, b1)))
	return math.Atan2(dot(cross(b1, v), w), dot(v, w)) * 180 / math.Pi
}

func rmsdValue(coords, reference [][3]float64) float64 {
	if len(coords) == 0 || len(coords) != len(reference) {
		return 0
	}
	var total float64
	for i := range coords {
		total += square(coords[i][0]-reference[i][0]) + square(coords[i][1]-reference[i][1]) + square(coords[i][2]-reference[i][2])
	}
	return math.Sqrt(total / float64(len(coords)))
}

func radiusOfGyration(coords [][3]float64) float64 {
	if len(coords) == 0 {
		return 0
	}
	var centroid [3]float64
	for _, coord := range coords {
		centroid[0] += coord[0]
		centroid[1] += coord[1]
		centroid[2] += coord[2]
	}
	centroid[0] /= float64(len(coords))
	centroid[1] /= float64(len(coords))
	centroid[2] /= float64(len(coords))
	var total float64
	for _, coord := range coords {
		total += square(coord[0]-centroid[0]) + square(coord[1]-centroid[1]) + square(coord[2]-centroid[2])
	}
	return math.Sqrt(total / float64(len(coords)))
}

func rmsfTrace(frames []mdsrv.Frame, indices []int) []mdsrv.TraceValue {
	if len(frames) == 0 || len(indices) == 0 {
		return nil
	}
	means := make([][3]float64, len(indices))
	for _, frame := range frames {
		for i, index := range indices {
			coord := frame.Coordinates[index]
			means[i][0] += float64(coord[0])
			means[i][1] += float64(coord[1])
			means[i][2] += float64(coord[2])
		}
	}
	for i := range means {
		means[i][0] /= float64(len(frames))
		means[i][1] /= float64(len(frames))
		means[i][2] /= float64(len(frames))
	}
	values := make([]mdsrv.TraceValue, 0, len(indices))
	for i, index := range indices {
		var total float64
		for _, frame := range frames {
			coord := frame.Coordinates[index]
			total += square(float64(coord[0])-means[i][0]) + square(float64(coord[1])-means[i][1]) + square(float64(coord[2])-means[i][2])
		}
		values = append(values, mdsrv.TraceValue{Frame: index + 1, Time: float64(index + 1), Value: math.Sqrt(total / float64(len(frames)))})
	}
	return values
}

func contactCount(frame mdsrv.Frame, a []int, b []int, cutoff float64) int {
	var count int
	for _, ai := range a {
		ac := frame.Coordinates[ai]
		av := [3]float64{float64(ac[0]), float64(ac[1]), float64(ac[2])}
		for _, bi := range b {
			if ai == bi {
				continue
			}
			bc := frame.Coordinates[bi]
			bv := [3]float64{float64(bc[0]), float64(bc[1]), float64(bc[2])}
			if vecDistance(av, bv) <= cutoff {
				count++
			}
		}
	}
	return count
}

func dot(a, b [3]float64) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func cross(a, b [3]float64) [3]float64 {
	return [3]float64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func subtract(a, b [3]float64) [3]float64 {
	return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func scale(a [3]float64, factor float64) [3]float64 {
	return [3]float64{a[0] * factor, a[1] * factor, a[2] * factor}
}

func normalize(a [3]float64) [3]float64 {
	length := math.Sqrt(dot(a, a))
	if length == 0 {
		return a
	}
	return [3]float64{a[0] / length, a[1] / length, a[2] / length}
}

func square(value float64) float64 {
	return value * value
}

type batchReport struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Manifest string `json:"manifest,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (a app) runBatch(ctx context.Context, path string, flags *batchCLIFlags) error {
	jobs, err := mdsrv.LoadBatchFile(path)
	if err != nil {
		return err
	}
	if flags.concurrency < 1 {
		flags.concurrency = 1
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return err
	}
	type work struct {
		index int
		job   mdsrv.BatchJob
	}
	batchDir := filepath.Dir(path)
	workCh := make(chan work)
	reportCh := make(chan batchReport)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < flags.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				job := resolveBatchJobPaths(batchDir, item.job)
				report := batchReport{Index: item.index, ID: job.ID}
				manifest, err := store.Ingest(job.IngestOptions(flags.force))
				if err != nil {
					report.Error = err.Error()
					if !flags.continueOnError {
						cancel()
					}
				} else {
					report.ID = manifest.Metadata.ID
					report.Manifest = filepath.ToSlash(filepath.Join(mdsrv.DatasetsDir, manifest.Metadata.ID+".yaml"))
				}
				reportCh <- report
			}
		}()
	}
	go func() {
		defer close(workCh)
		for i, job := range jobs {
			select {
			case <-ctx.Done():
				return
			case workCh <- work{index: i, job: job}:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(reportCh)
	}()
	var failed bool
	for report := range reportCh {
		if report.Error != "" {
			failed = true
		}
		if flags.jsonReport {
			data, _ := json.Marshal(report)
			fmt.Fprintln(a.stdout, string(data))
		} else if report.Error != "" {
			fmt.Fprintf(a.stderr, "job %d failed: %s\n", report.Index, report.Error)
		} else {
			fmt.Fprintf(a.stdout, "job %d ok: %s\n", report.Index, report.ID)
		}
	}
	if failed {
		return fmt.Errorf("one or more batch jobs failed")
	}
	return nil
}

func resolveBatchJobPaths(batchDir string, job mdsrv.BatchJob) mdsrv.BatchJob {
	job.Topology = resolveBatchPath(batchDir, job.Topology)
	job.Trajectory = resolveBatchPath(batchDir, job.Trajectory)
	job.Cache = resolveBatchPath(batchDir, job.Cache)
	return job
}

func resolveBatchPath(batchDir string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "://") {
		return path
	}
	return filepath.Join(batchDir, path)
}

func (a app) runCompatCheck(ctx context.Context, flags *compatFlags) compatReport {
	store, err := mdsrv.OpenStore(flags.store)
	report := compatReport{Store: flags.store}
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
		return report
	}
	report.Store = store.Root
	if err := store.Init(); err != nil {
		report.Warnings = append(report.Warnings, err.Error())
		return report
	}
	items, err := store.ListDatasets()
	if err != nil {
		report.Warnings = append(report.Warnings, err.Error())
		return report
	}
	report.OK = true
	report.Checks = append(report.Checks, "store directories exist", "trajectory_index.json exists", "session_index.json exists")
	trajectoryIndex, err := readCompatIndex(filepath.Join(store.Root, "trajectory_index.json"))
	if err != nil {
		addCompatFailure(&report, "trajectory_index.json: "+err.Error())
	} else {
		report.Checks = append(report.Checks, fmt.Sprintf("trajectory_index.json entries=%d", len(trajectoryIndex)))
	}
	sessionIndex, err := readCompatIndex(filepath.Join(store.Root, "session_index.json"))
	if err != nil {
		addCompatFailure(&report, "session_index.json: "+err.Error())
	} else {
		report.Checks = append(report.Checks, fmt.Sprintf("session_index.json entries=%d", len(sessionIndex)))
	}
	trajectoryIDs := map[string]bool{}
	for _, entry := range trajectoryIndex {
		trajectoryIDs[entry.ID] = true
		path := filepath.Join(store.Root, mdsrv.TrajectoryDir, entry.ID+".xtc")
		if _, err := os.Stat(path); err != nil {
			addCompatFailure(&report, fmt.Sprintf("trajectory_index entry %s points to missing upstream trajectory %s", entry.ID, filepath.ToSlash(filepath.Join(mdsrv.TrajectoryDir, entry.ID+".xtc"))))
		}
	}
	for _, entry := range sessionIndex {
		matches, _ := filepath.Glob(filepath.Join(store.Root, mdsrv.SessionDir, entry.ID+".*"))
		if len(matches) == 0 {
			addCompatFailure(&report, fmt.Sprintf("session_index entry %s has no matching file in %s", entry.ID, mdsrv.SessionDir))
		}
	}
	for _, item := range items {
		m, err := store.LoadDataset(item.ID)
		if err != nil {
			addCompatFailure(&report, fmt.Sprintf("dataset %s cannot be loaded: %v", item.ID, err))
			continue
		}
		validation := store.CheckDataset(m)
		for _, file := range validation.Files {
			if !file.Exists || file.Error != "" {
				addCompatFailure(&report, fmt.Sprintf("dataset %s file %s invalid: %s", item.ID, file.Path, firstNonEmpty(file.Error, "missing")))
			}
		}
		for _, warning := range validation.Warnings {
			addCompatFailure(&report, fmt.Sprintf("dataset %s: %s", item.ID, warning))
		}
		if item.Trajectory != "" {
			id := strings.TrimSuffix(filepath.Base(item.Trajectory), filepath.Ext(item.Trajectory))
			if id != item.ID {
				addCompatFailure(&report, fmt.Sprintf("dataset %s trajectory filename base %s does not match index id", item.ID, id))
			}
			if strings.ToLower(filepath.Ext(item.Trajectory)) != ".xtc" {
				addCompatFailure(&report, fmt.Sprintf("dataset %s trajectory %s is not .xtc; upstream streaming-server compatibility expects XTC", item.ID, item.Trajectory))
			}
			if !trajectoryIDs[item.ID] {
				addCompatFailure(&report, fmt.Sprintf("dataset %s is missing from trajectory_index.json", item.ID))
			}
		}
	}
	if flags.docker {
		report.Docker = flags.image
		ok, logs := runDockerCompat(ctx, store.Root, flags)
		report.DockerOK = ok
		report.DockerLogs = logs
		if !ok {
			report.OK = false
			report.Warnings = append(report.Warnings, "upstream Docker smoke test failed")
		}
	}
	return report
}

type compatIndexEntry struct {
	ID string `json:"id"`
}

func readCompatIndex(path string) ([]compatIndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []compatIndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for i, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			return nil, fmt.Errorf("entry %d has empty id", i)
		}
	}
	return entries, nil
}

func addCompatFailure(report *compatReport, message string) {
	report.OK = false
	report.Warnings = append(report.Warnings, message)
}

func runDockerCompat(ctx context.Context, storeRoot string, flags *compatFlags) (bool, string) {
	if _, err := exec.LookPath("docker"); err != nil {
		return false, "docker command not found"
	}
	name := fmt.Sprintf("hlmdsrv-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(ctx, flags.timeout)
	defer cancel()
	run := exec.CommandContext(ctx, "docker", "run", "--rm", "-d", "--name", name, "-p", fmt.Sprintf("%d:1337", flags.port), "-v", storeRoot+":/mdsrv/server", flags.image)
	output, err := run.CombinedOutput()
	if err != nil {
		return false, string(output)
	}
	containerID := strings.TrimSpace(string(output))
	defer func() {
		stop := exec.Command("docker", "stop", name)
		_ = stop.Run()
	}()
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(flags.timeout)
	var last string
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/trajectory_index.json", flags.port))
		if err == nil {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return true, fmt.Sprintf("%s\n%s", containerID, string(data))
			}
			last = fmt.Sprintf("status %d: %s", resp.StatusCode, string(data))
		} else {
			last = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
	return false, strings.TrimSpace(last + "\n" + string(logs))
}

func selectionsFor(kind string, flags *analyzeFlags) map[string]string {
	switch kind {
	case "distance", "contacts":
		return map[string]string{"a": flags.a, "b": flags.b}
	case "angle":
		return map[string]string{"a": flags.a, "b": flags.b, "c": flags.c}
	case "dihedral":
		return map[string]string{"a": flags.a, "b": flags.b, "c": flags.c, "d": flags.d}
	default:
		return nil
	}
}

// isSingleGroupAnalysis reports whether kind takes one atom selection (via
// --selection) rather than the multi-group --a/--b/--c/--d flags.
func isSingleGroupAnalysis(kind string) bool {
	switch kind {
	case "rmsd", "rmsf", "rgyr", "sasa":
		return true
	default:
		return false
	}
}

func outputAnalysisPath(datasetID, kind, out, format string) string {
	if out != "" {
		return out
	}
	// Match the extension to the requested content format so `--format json`
	// does not write JSON into a ".csv"-named file (the server job path does
	// the same). Analysis output is csv or json; csv is the default.
	ext := strings.ToLower(strings.TrimSpace(format))
	if ext == "" {
		ext = "csv"
	}
	return filepath.ToSlash(filepath.Join("traces", datasetID+"-"+kind+"."+ext))
}

func outputFormat(format, out, fallback string) string {
	if format != "" {
		return strings.ToLower(format)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(out)), ".")
	if ext != "" {
		return ext
	}
	return fallback
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
