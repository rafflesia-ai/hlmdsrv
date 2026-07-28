package mdsrvcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/job"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
	"github.com/rafflesia-ai/hlmdsrv/internal/mvs"
)

type datasetFlags struct {
	store       string
	name        string
	description string
	source      string
	license     string
	deleteFiles bool
	backend     string
	gmxCommand  string
	jsonReport  bool
}

type exportFlags struct {
	store      string
	out        string
	format     string
	frames     string
	group      string
	selection  string
	force      bool
	backend    string
	gmxCommand string
	jsonReport bool
}

type selectionFlags struct {
	store       string
	id          string
	expression  string
	kind        string
	target      string
	description string
	out         string
	force       bool
	jsonReport  bool
}

type indexFlags struct {
	store         string
	chunkSize     int
	encoding      string
	force         bool
	gmxCommand    string
	maxAtoms      int
	maxFrames     int
	maxChunkBytes int64
	jsonReport    bool
}

type visualizeFlags struct {
	store             string
	out               string
	component         string
	repr              string
	color             string
	background        string
	focus             string
	frame             int
	selection         []string
	includeSelections bool
	gmxCommand        string
	jsonReport        bool
}

type installFlags struct {
	home          string
	binDir        string
	completionDir string
	name          string
	shell         string
	out           string
	force         bool
	jsonReport    bool
}

type gcFlags struct {
	store      string
	jsonReport bool
}

func (a app) datasetCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "dataset", Short: "Manage dataset lifecycle"}
	flags := &datasetFlags{}
	inspect := &cobra.Command{
		Use:   "inspect DATASET_ID",
		Short: "Inspect dataset files, backend metadata, and frame index status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.inspectDataset(cmd.Context(), args[0], flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", report.Manifest.Metadata.ID, report.Manifest.Metadata.Name)
			if report.Trajectory != nil {
				fmt.Fprintf(a.stdout, "trajectory\tbackend=%s\tatoms=%d\tframes=%d\n", report.Trajectory.Backend, report.Trajectory.Atoms, report.Trajectory.Frames)
			}
			for _, file := range report.Validation.Files {
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
			if report.FrameIndex != nil {
				fmt.Fprintf(a.stdout, "index\tframes=%d\tchunks=%d\tpath=%s\n", report.FrameIndex.FrameCount, len(report.FrameIndex.Chunks), report.Manifest.Streaming.FrameIndex)
			}
			for _, warning := range report.Validation.Warnings {
				fmt.Fprintln(a.stderr, "warning:", warning)
			}
			return reportError(report.Validation)
		},
	}
	inspect.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	bindBackendFlag(inspect, &flags.backend)
	inspect.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	inspect.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	update := &cobra.Command{
		Use:   "update DATASET_ID",
		Short: "Update dataset metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.UpdateDataset(args[0], mdsrv.UpdateOptions{
				Name:        flags.name,
				Description: flags.description,
				Source:      flags.source,
				License:     flags.license,
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, m)
			}
			fmt.Fprintln(a.stdout, m.Metadata.ID)
			return nil
		},
	}
	update.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	update.Flags().StringVar(&flags.name, "name", "", "dataset name")
	update.Flags().StringVar(&flags.description, "description", "", "dataset description")
	update.Flags().StringVar(&flags.source, "source", "", "source URL, DOI, or label")
	update.Flags().StringVar(&flags.license, "license", "", "dataset license")
	update.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	rename := &cobra.Command{
		Use:   "rename OLD_ID NEW_ID",
		Short: "Rename a dataset manifest id",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.RenameDataset(args[0], args[1])
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, m)
			}
			fmt.Fprintln(a.stdout, m.Metadata.ID)
			return nil
		},
	}
	rename.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	rename.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	deleteCmd := &cobra.Command{
		Use:   "delete DATASET_ID",
		Short: "Delete a dataset manifest and optionally its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			if err := store.DeleteDataset(args[0], flags.deleteFiles); err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, map[string]any{"deleted": args[0], "files": flags.deleteFiles})
			}
			fmt.Fprintln(a.stdout, args[0])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	deleteCmd.Flags().BoolVar(&flags.deleteFiles, "files", false, "also delete topology, trajectory, indexes, and traces referenced by the dataset")
	deleteCmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	gc := &cobra.Command{
		Use:   "gc",
		Short: "Remove unreferenced store files",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			report, err := store.GC()
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			for _, path := range report.Removed {
				fmt.Fprintln(a.stdout, path)
			}
			return nil
		},
	}
	gc.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	gc.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(inspect, update, rename, deleteCmd, gc)
	return cmd
}

func (a app) inspectCommand() *cobra.Command {
	flags := &datasetFlags{}
	cmd := &cobra.Command{
		Use:        "inspect DATASET_ID",
		Short:      "Inspect dataset files, backend metadata, and frame index status",
		Deprecated: "use `hlmdsrv dataset inspect DATASET_ID` instead",
		Hidden:     true,
		Args:       cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.inspectDataset(cmd.Context(), args[0], flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", report.Manifest.Metadata.ID, report.Manifest.Metadata.Name)
			if report.Trajectory != nil {
				fmt.Fprintf(a.stdout, "trajectory\tbackend=%s\tatoms=%d\tframes=%d\n", report.Trajectory.Backend, report.Trajectory.Atoms, report.Trajectory.Frames)
			}
			if report.FrameIndex != nil {
				fmt.Fprintf(a.stdout, "index\tframes=%d\tchunks=%d\tpath=%s\n", report.FrameIndex.FrameCount, len(report.FrameIndex.Chunks), report.Manifest.Streaming.FrameIndex)
			}
			for _, warning := range report.Validation.Warnings {
				fmt.Fprintln(a.stderr, "warning:", warning)
			}
			return reportError(report.Validation)
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

type datasetInspectReport struct {
	Store      string                 `json:"store"`
	Manifest   mdsrv.Manifest         `json:"manifest"`
	Validation mdsrv.ValidationReport `json:"validation"`
	Trajectory *mdsrv.TrajectoryInfo  `json:"trajectory,omitempty"`
	FrameIndex *mdsrv.FrameIndex      `json:"frame_index,omitempty"`
}

func (a app) inspectDataset(ctx context.Context, id string, flags *datasetFlags) (datasetInspectReport, error) {
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return datasetInspectReport{}, err
	}
	m, err := store.LoadDataset(id)
	if err != nil {
		return datasetInspectReport{}, err
	}
	report := datasetInspectReport{
		Store:      store.Root,
		Manifest:   m,
		Validation: store.CheckDataset(m),
	}
	info, err := trajectoryInfoWithPolicy(ctx, store, m, id, flags.backend, flags.gmxCommand)
	if err != nil {
		report.Validation.Warnings = append(report.Validation.Warnings, "backend inspection failed: "+err.Error())
	} else {
		report.Trajectory = &info
	}
	if index, err := store.LoadFrameIndex(id); err == nil {
		report.FrameIndex = &index
	}
	return report, nil
}

func (a app) gcCommand() *cobra.Command {
	flags := &gcFlags{}
	cmd := &cobra.Command{
		Use:        "gc",
		Short:      "Remove unreferenced store files",
		Deprecated: "use `hlmdsrv dataset gc` instead",
		Hidden:     true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			report, err := store.GC()
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			for _, path := range report.Removed {
				fmt.Fprintf(a.stdout, "removed\t%s\n", path)
			}
			if len(report.Removed) == 0 {
				fmt.Fprintln(a.stdout, "nothing removed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) exportCommand() *cobra.Command {
	flags := &exportFlags{}
	cmd := &cobra.Command{
		Use:   "export DATASET_ID",
		Short: "Export a trajectory slice with GROMACS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runExport(cmd.Context(), args[0], flags)
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output trajectory path")
	cmd.Flags().StringVar(&flags.format, "format", "", "output format; inferred from --out")
	cmd.Flags().StringVar(&flags.frames, "frames", "", "frame/time range START:STOP:STRIDE using frame indexes")
	cmd.Flags().StringVar(&flags.group, "group", "0", "GROMACS group name/index to pass to trjconv")
	cmd.Flags().StringVar(&flags.selection, "selection", "", "1-based atom-index selection; writes a temporary index file")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing output")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) selectionCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "selection", Short: "Manage named dataset selections"}
	flags := &selectionFlags{}
	save := &cobra.Command{
		Use:   "save DATASET_ID",
		Short: "Save a named selection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.id) == "" {
				return fmt.Errorf("--id is required")
			}
			if strings.TrimSpace(flags.expression) == "" {
				return fmt.Errorf("--expression or --expr is required")
			}
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			selection, err := store.SaveSelection(args[0], mdsrv.Selection{
				ID:          flags.id,
				Expression:  flags.expression,
				Kind:        flags.kind,
				Description: flags.description,
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, selection)
			}
			fmt.Fprintln(a.stdout, selection.ID)
			return nil
		},
	}
	save.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	save.Flags().StringVar(&flags.id, "id", "", "selection id")
	save.Flags().StringVar(&flags.expression, "expr", "", "selection expression")
	save.Flags().StringVar(&flags.expression, "expression", "", "selection expression")
	save.Flags().StringVar(&flags.kind, "kind", "atom-index", "selection kind: atom-index, mdtraj, mdanalysis, mvs")
	save.Flags().StringVar(&flags.description, "description", "", "selection description")
	save.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	list := &cobra.Command{
		Use:   "list DATASET_ID",
		Short: "List selections",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.LoadDataset(args[0])
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, m.Selections)
			}
			for _, selection := range m.Selections {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", selection.ID, selection.Kind, selection.Expression)
			}
			return nil
		},
	}
	list.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	list.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	deleteCmd := &cobra.Command{
		Use:   "delete DATASET_ID SELECTION_ID",
		Short: "Delete a selection",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			if err := store.DeleteSelection(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, args[1])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	resolve := &cobra.Command{
		Use:   "resolve DATASET_ID SELECTION_OR_EXPR",
		Short: "Resolve a saved selection into a target backend dialect",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.LoadDataset(args[0])
			if err != nil {
				return err
			}
			atomCount := 0
			if len(m.Inputs.Trajectories) > 0 {
				atomCount = m.Inputs.Trajectories[0].AtomCount
			}
			resolved, err := mdsrv.ResolveSelectionForTarget(m, args[1], flags.target, atomCount)
			if err != nil {
				return err
			}
			report := map[string]any{
				"dataset":  args[0],
				"input":    args[1],
				"target":   flags.target,
				"resolved": resolved,
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, resolved)
			return nil
		},
	}
	resolve.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	resolve.Flags().StringVar(&flags.target, "target", "gromacs", "target dialect: gromacs, mdtraj, mdanalysis, python, mvs")
	resolve.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	exportIndex := &cobra.Command{
		Use:   "export-index DATASET_ID SELECTION_ID",
		Short: "Export an atom-index selection as a GROMACS .ndx file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.LoadDataset(args[0])
			if err != nil {
				return err
			}
			var found *mdsrv.Selection
			for i := range m.Selections {
				if m.Selections[i].ID == args[1] {
					found = &m.Selections[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("selection %q not found", args[1])
			}
			atomCount := 0
			if len(m.Inputs.Trajectories) > 0 {
				atomCount = m.Inputs.Trajectories[0].AtomCount
			}
			text, err := mdsrv.AtomSelectionToIndexFile(found.ID, found.Expression, atomCount)
			if err != nil {
				return err
			}
			if flags.out == "" {
				flags.out = found.ID + ".ndx"
			}
			if err := ensureOutputPath(flags.out, flags.force); err != nil {
				return err
			}
			if err := os.WriteFile(flags.out, []byte(text), 0o644); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, flags.out)
			return nil
		},
	}
	exportIndex.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	exportIndex.Flags().StringVarP(&flags.out, "out", "o", "", "output .ndx path")
	exportIndex.Flags().BoolVar(&flags.force, "force", false, "overwrite existing output")
	cmd.AddCommand(save, list, deleteCmd, resolve, exportIndex)
	return cmd
}

func (a app) indexCommand() *cobra.Command {
	flags := &indexFlags{}
	cmd := &cobra.Command{Use: "index", Short: "Build static frame indexes"}
	build := &cobra.Command{
		Use:   "build DATASET_ID",
		Short: "Build a JSON frame/chunk index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			index, err := store.BuildFrameIndexWithOptions(cmd.Context(), args[0], mdsrv.BuildFrameIndexOptions{
				ChunkSize:      flags.chunkSize,
				GromacsCommand: flags.gmxCommand,
				Limits:         indexResourceLimits(flags),
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, index)
			}
			fmt.Fprintf(a.stdout, "%s\n", filepath.ToSlash(filepath.Join("indexes", args[0]+"-frame-index.json")))
			return nil
		},
	}
	build.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	build.Flags().IntVar(&flags.chunkSize, "chunk-size", 128, "frames per logical chunk")
	build.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	build.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "fail if the dataset exceeds this atom count; 0 disables")
	build.Flags().IntVar(&flags.maxFrames, "max-frames", 0, "fail if the dataset exceeds this frame count; 0 disables")
	build.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	show := &cobra.Command{
		Use:   "show DATASET_ID",
		Short: "Show a JSON frame/chunk index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			index, err := store.LoadFrameIndex(args[0])
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, index)
			}
			fmt.Fprintf(a.stdout, "%s\tframes=%d\tchunks=%d\n", index.DatasetID, index.FrameCount, len(index.Chunks))
			return nil
		},
	}
	show.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	show.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	chunks := &cobra.Command{
		Use:   "chunks DATASET_ID",
		Short: "Materialize static frame chunks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			index, err := store.BuildFrameChunksWithOptions(cmd.Context(), args[0], mdsrv.BuildFrameChunksOptions{
				ChunkSize:      flags.chunkSize,
				Encoding:       flags.encoding,
				GromacsCommand: flags.gmxCommand,
				Force:          flags.force,
				Limits:         indexResourceLimits(flags),
			})
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, index)
			}
			for _, chunk := range index.Chunks {
				if chunk.Path != "" {
					fmt.Fprintln(a.stdout, chunk.Path)
				}
			}
			return nil
		},
	}
	chunks.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	chunks.Flags().IntVar(&flags.chunkSize, "chunk-size", 128, "frames per static chunk")
	chunks.Flags().StringVar(&flags.encoding, "encoding", "json", "chunk encoding: json, bin, or bin-zstd")
	chunks.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	chunks.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "fail if the dataset or decoded frame exceeds this atom count; 0 disables")
	chunks.Flags().IntVar(&flags.maxFrames, "max-frames", 0, "fail if the dataset exceeds this frame count; 0 disables")
	chunks.Flags().Int64Var(&flags.maxChunkBytes, "max-chunk-bytes", 0, "fail if an encoded chunk exceeds this byte count; 0 disables")
	chunks.Flags().BoolVar(&flags.force, "force", false, "overwrite existing chunk files")
	chunks.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(build, show, chunks)
	return cmd
}

func indexResourceLimits(flags *indexFlags) mdsrv.ResourceLimits {
	return mdsrv.ResourceLimits{
		MaxAtoms:      flags.maxAtoms,
		MaxFrames:     flags.maxFrames,
		MaxChunkBytes: flags.maxChunkBytes,
	}
}

func (a app) visualizeCommand() *cobra.Command {
	flags := &visualizeFlags{}
	cmd := &cobra.Command{
		Use:   "visualize DATASET_ID",
		Short: "Generate a static MVS scene from a dataset topology",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			m, err := store.LoadDataset(args[0])
			if err != nil {
				return err
			}
			topologyPath, err := store.SafeResolvePath(m.Inputs.Topology.Path)
			if err != nil {
				return err
			}
			inputPath := topologyPath
			inputFormat := m.Inputs.Topology.Format
			var snapshot string
			if flags.frame >= 0 {
				snapshot = filepath.Join(store.Root, "visualization", fmt.Sprintf("%s-frame-%d.gro", args[0], flags.frame))
				if _, err := store.ExtractFrame(cmd.Context(), args[0], flags.frame, snapshot, flags.gmxCommand); err != nil {
					return err
				}
				inputPath = snapshot
				inputFormat = "gro"
			}
			if flags.out == "" {
				flags.out = filepath.Join(store.Root, "visualization", args[0]+".mvsj")
			}
			component := firstNonEmpty(flags.component, "all")
			repr := firstNonEmpty(flags.repr, "cartoon")
			components, err := visualizationComponents(m, component, repr, flags)
			if err != nil {
				return err
			}
			j := job.Job{
				Version: 1,
				Inputs:  map[string]job.Input{"topology": {Path: inputPath, Format: inputFormat}},
				Scene: job.Scene{
					Canvas: job.Canvas{Background: firstNonEmpty(flags.background, "white")},
					Structures: []job.Structure{{
						Source:     "topology",
						Components: components,
					}},
					Camera: job.Camera{Focus: flags.focus},
				},
			}
			compiled, err := mvs.Compile(j)
			if err != nil {
				return err
			}
			if err := mvs.WriteFile(flags.out, compiled.Document); err != nil {
				return err
			}
			if relative, ok := storeRelativePathIfInside(store.Root, flags.out); ok {
				m.Visualization.MVS.Scene = relative
				if snapshot != "" {
					m.Inputs.Topology.OriginalPath = topologyPath
				}
				if flags.focus != "" {
					m.Visualization.Camera.Focus = flags.focus
				}
				_ = mdsrv.WriteManifestFile(store.ManifestPath(args[0]), m)
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, map[string]any{
					"scene":      flags.out,
					"snapshot":   snapshot,
					"components": componentRefs(components),
				})
			}
			fmt.Fprintln(a.stdout, flags.out)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output .mvsj path")
	cmd.Flags().StringVar(&flags.component, "component", "all", "component selector")
	cmd.Flags().StringVar(&flags.repr, "repr", "cartoon", "representation type")
	cmd.Flags().StringVar(&flags.color, "color", "", "explicit color or high-level theme")
	cmd.Flags().StringVar(&flags.background, "background", "white", "canvas background")
	cmd.Flags().StringVar(&flags.focus, "focus", "", "camera focus component or selector")
	cmd.Flags().IntVar(&flags.frame, "frame", -1, "extract this trajectory frame before generating the static scene; -1 uses topology")
	cmd.Flags().StringArrayVar(&flags.selection, "selection", nil, "include a named selection id or raw MVS selector as an extra component; repeatable")
	cmd.Flags().BoolVar(&flags.includeSelections, "include-selections", false, "include all saved named selections as extra components")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override for --frame")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func visualizationComponents(m mdsrv.Manifest, component string, repr string, flags *visualizeFlags) ([]job.Component, error) {
	components := []job.Component{{
		Ref:    component,
		Select: component,
		Representation: job.Representation{
			Type:  repr,
			Color: flags.color,
		},
	}}
	requested := map[string]bool{}
	for _, value := range flags.selection {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				requested[strings.TrimPrefix(part, "@")] = true
			}
		}
	}
	if !flags.includeSelections && len(requested) == 0 {
		return components, nil
	}
	atomCount := 0
	if len(m.Inputs.Trajectories) > 0 {
		atomCount = m.Inputs.Trajectories[0].AtomCount
	}
	colors := []string{"#cc3399", "#0072b2", "#009e73", "#d55e00", "#f0e442", "#56b4e9"}
	for _, selection := range m.Selections {
		if !flags.includeSelections && !requested[selection.ID] {
			continue
		}
		resolved, err := mdsrv.ResolveSelectionForTarget(m, "@"+selection.ID, "mvs", atomCount)
		if err != nil {
			return nil, err
		}
		components = append(components, job.Component{
			Ref:    selection.ID,
			Select: resolved,
			Representation: job.Representation{
				Type:  "ball-and-stick",
				Color: colors[(len(components)-1)%len(colors)],
			},
		})
		delete(requested, selection.ID)
	}
	for raw := range requested {
		components = append(components, job.Component{
			Ref:    "selection-" + strconv.Itoa(len(components)),
			Select: raw,
			Representation: job.Representation{
				Type:  "ball-and-stick",
				Color: colors[(len(components)-1)%len(colors)],
			},
		})
	}
	return components, nil
}

func componentRefs(components []job.Component) []string {
	refs := make([]string, 0, len(components))
	for _, component := range components {
		refs = append(refs, firstNonEmpty(component.Ref, component.Select))
	}
	return refs
}

// resolveTraceOutputWithinStore confines a server-supplied analysis output path
// to the store root. Untrusted callers (HTTP /datasets/{id}/analyses and /jobs)
// must not be able to write outside the store via an absolute path or "../"
// escape, so the requested path is routed through Store.SafeResolvePath.
func resolveTraceOutputWithinStore(store mdsrv.Store, output, id, analysisType string) (string, error) {
	if strings.TrimSpace(output) == "" {
		output = filepath.ToSlash(filepath.Join("traces", id+"-"+analysisType+".csv"))
	}
	resolved, err := store.SafeResolvePath(output)
	if err != nil {
		return "", fmt.Errorf("analysis output %q escapes the store: %w", output, err)
	}
	return resolved, nil
}

func storeRelativePathIfInside(root, path string) (string, bool) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil {
		return "", false
	}
	if relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func (a app) schemaCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "schema", Short: "Print JSON schemas and OpenAPI documents"}
	cmd.AddCommand(schemaLeaf("job", manifestSchema()))
	cmd.AddCommand(schemaLeaf("manifest", manifestSchema()))
	cmd.AddCommand(schemaLeaf("batch", batchSchema()))
	cmd.AddCommand(schemaLeaf("openapi", openAPISchema()))
	return cmd
}

func (a app) installCommand() *cobra.Command {
	flags := &installFlags{shell: "zsh"}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install local CLI extras and print backend setup guidance",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.writeBackendInstallGuide(flags)
		},
	}
	backends := &cobra.Command{
		Use:   "backends",
		Short: "Print setup guidance for optional trajectory backends",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.writeBackendInstallGuide(flags)
		},
	}
	localFlags := &installFlags{name: "hlmdsrv"}
	local := &cobra.Command{
		Use:   "local",
		Short: "Build and install hlmdsrv plus shell completions",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.runInstallLocal(cmd.Context(), localFlags)
			if err != nil {
				return err
			}
			if localFlags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "installed %s\n", report.Binary)
			for shell, path := range report.Completions {
				fmt.Fprintf(a.stdout, "completion %s %s\n", shell, path)
			}
			return nil
		},
	}
	local.Flags().StringVar(&localFlags.home, "home", "", "source checkout root; auto-detected when omitted")
	local.Flags().StringVar(&localFlags.binDir, "bin-dir", "", "directory to install the hlmdsrv binary into")
	local.Flags().StringVar(&localFlags.completionDir, "completion-dir", "", "directory to install bash, zsh, and fish completions into")
	local.Flags().StringVar(&localFlags.name, "name", localFlags.name, "installed executable name")
	local.Flags().BoolVar(&localFlags.force, "force", false, "overwrite an existing executable and completions")
	local.Flags().BoolVar(&localFlags.jsonReport, "json", false, "write machine-readable output")
	completions := &cobra.Command{
		Use:   "completions",
		Short: "Install shell completions",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := a.rootCommand()
			out := flags.out
			if out == "" {
				out = filepath.Join(os.Getenv("HOME"), ".local", "share", "zsh", "site-functions", "_hlmdsrv")
			}
			if err := ensureOutputPath(out, flags.force); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			file, err := os.Create(out)
			if err != nil {
				return err
			}
			defer file.Close()
			switch flags.shell {
			case "zsh":
				err = root.GenZshCompletion(file)
			case "bash":
				err = root.GenBashCompletion(file)
			case "fish":
				err = root.GenFishCompletion(file, true)
			default:
				err = fmt.Errorf("unsupported shell %q", flags.shell)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, out)
			return nil
		},
	}
	completions.Flags().StringVar(&flags.shell, "shell", "zsh", "shell: zsh, bash, fish")
	completions.Flags().StringVarP(&flags.out, "out", "o", "", "completion output path")
	completions.Flags().BoolVar(&flags.force, "force", false, "overwrite existing completion file")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	backends.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(backends, local, completions)
	return cmd
}

func (a app) writeBackendInstallGuide(flags *installFlags) error {
	report := map[string]any{
		"gromacs": map[string]any{
			"required_for": []string{"gmx check metadata probing", "gmx trjconv frame extraction", "trajectory export", "fallback full-frame analysis"},
			"macos":        "brew install gromacs",
			"linux":        "install your distribution package or load an HPC GROMACS module",
			"override":     "set MDSRV_GMX=/path/to/gmx or pass --gmx-command",
		},
		"python": map[string]any{
			"required_for": []string{"atom-subset JSON/binary frames", "mdtraj/MDAnalysis-native analyses", "non-GROMACS trajectory decoding"},
			"mdtraj":       "python3 -m pip install mdtraj",
			"mdanalysis":   "python3 -m pip install MDAnalysis",
			"override":     "set MDSRV_PYTHON=/path/to/python",
		},
	}
	if flags.jsonReport {
		return writeJSON(a.stdout, report)
	}
	fmt.Fprintln(a.stdout, "GROMACS")
	fmt.Fprintln(a.stdout, "  required for: gmx check metadata, gmx trjconv extraction/export, fallback full-frame analysis")
	fmt.Fprintln(a.stdout, "  macOS: brew install gromacs")
	fmt.Fprintln(a.stdout, "  Linux/HPC: install a distro package or load a GROMACS module")
	fmt.Fprintln(a.stdout, "  override: MDSRV_GMX=/path/to/gmx or --gmx-command")
	fmt.Fprintln(a.stdout, "Python trajectory backend")
	fmt.Fprintln(a.stdout, "  required for: atom-subset frame JSON/binary, mdtraj/MDAnalysis analysis, non-GROMACS decoding")
	fmt.Fprintln(a.stdout, "  mdtraj: python3 -m pip install mdtraj")
	fmt.Fprintln(a.stdout, "  MDAnalysis: python3 -m pip install MDAnalysis")
	fmt.Fprintln(a.stdout, "  override: MDSRV_PYTHON=/path/to/python")
	return nil
}

func (a app) runExport(ctx context.Context, datasetID string, flags *exportFlags) error {
	switch strings.ToLower(strings.TrimSpace(flags.backend)) {
	case "", "auto", "gromacs", "gmx":
	case "python", "mdtraj", "mdanalysis":
		return fmt.Errorf("export currently requires the GROMACS backend")
	default:
		return fmt.Errorf("unsupported backend %q", flags.backend)
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return err
	}
	m, err := store.LoadDataset(datasetID)
	if err != nil {
		return err
	}
	if len(m.Inputs.Trajectories) == 0 {
		return fmt.Errorf("dataset has no trajectory")
	}
	if flags.frames != "" && (m.Inputs.Trajectories[0].FrameCount == 0 || (m.Inputs.Trajectories[0].FrameCount > 1 && m.Inputs.Trajectories[0].TimeStep == 0)) {
		probed, probeErr := store.ProbeDataset(ctx, datasetID, flags.gmxCommand)
		if probeErr != nil && m.Inputs.Trajectories[0].FrameCount == 0 {
			return probeErr
		}
		if probeErr == nil {
			m = probed
		}
	}
	if flags.out == "" {
		ext := firstNonEmpty(flags.format, "xtc")
		flags.out = datasetID + "-export." + strings.TrimPrefix(ext, ".")
	}
	// Resolve the inputs before preparing the output so the output path can be
	// checked against them: `export --out <the store's own topology> --force`
	// otherwise truncated the source file and reported success.
	topologyPath, err := store.SafeResolvePath(m.Inputs.Topology.Path)
	if err != nil {
		return err
	}
	trajectoryPath, err := store.SafeResolvePath(m.Inputs.Trajectories[0].Path)
	if err != nil {
		return err
	}
	if err := ensureOutputPathAgainst(flags.out, flags.force, topologyPath, trajectoryPath); err != nil {
		return err
	}
	start, stop, stride, err := parseFrameRange(flags.frames, m.Inputs.Trajectories[0])
	if err != nil {
		return err
	}
	var indexPath string
	groupInput := []byte(strings.TrimSpace(flags.group) + "\n")
	if flags.selection != "" {
		atomCount := m.Inputs.Trajectories[0].AtomCount
		selectionExpression, err := mdsrv.ResolveSelectionForTarget(m, flags.selection, "gromacs", atomCount)
		if err != nil {
			return err
		}
		text, err := mdsrv.AtomSelectionToIndexFile("export", selectionExpression, atomCount)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp("", "mdsrv-export-*.ndx")
		if err != nil {
			return err
		}
		indexPath = tmp.Name()
		if _, err := tmp.WriteString(text); err != nil {
			_ = tmp.Close()
			return err
		}
		_ = tmp.Close()
		defer os.Remove(indexPath)
		groupInput = []byte("export\n")
	}
	gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
	if !gmx.Available() {
		return fmt.Errorf("gromacs command %q was not found", gmx.CommandString())
	}
	if err := gmx.Export(ctx, gromacs.ExportOptions{
		Topology:   topologyPath,
		Trajectory: trajectoryPath,
		Output:     flags.out,
		Start:      start,
		Stop:       stop,
		Stride:     stride,
		GroupInput: groupInput,
		IndexPath:  indexPath,
	}); err != nil {
		return err
	}
	report := map[string]any{"dataset": datasetID, "output": flags.out}
	if flags.jsonReport {
		return writeJSON(a.stdout, report)
	}
	fmt.Fprintln(a.stdout, flags.out)
	return nil
}

func parseFrameRange(value string, ref mdsrv.FileRef) (float64, float64, int, error) {
	if strings.TrimSpace(value) == "" {
		return -1, -1, 0, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return 0, 0, 0, codedErrorf(codeValidationFailed, "invalid frame range %q: expected START:STOP:STRIDE", value)
	}
	parsePart := func(index int, fallback int) (int, error) {
		if index >= len(parts) || strings.TrimSpace(parts[index]) == "" {
			return fallback, nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[index]))
		if err != nil {
			return 0, codedErrorf(codeValidationFailed, "invalid frame range %q: expected START:STOP:STRIDE with integer frame indexes", value)
		}
		return n, nil
	}
	startFrame, err := parsePart(0, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	defaultStop := ref.FrameCount - 1
	if defaultStop < startFrame {
		defaultStop = startFrame
	}
	stopFrame, err := parsePart(1, defaultStop)
	if err != nil {
		return 0, 0, 0, err
	}
	stride, err := parsePart(2, 1)
	if err != nil {
		return 0, 0, 0, err
	}
	if stride < 1 {
		return 0, 0, 0, codedErrorf(codeValidationFailed, "stride must be positive")
	}
	if stopFrame < startFrame {
		return 0, 0, 0, codedErrorf(codeValidationFailed, "stop frame must be greater than or equal to start frame")
	}
	if ref.FrameCount > 0 && stopFrame >= ref.FrameCount {
		return 0, 0, 0, codedErrorf(codeValidationFailed, "stop frame %d is out of range for %d frames", stopFrame, ref.FrameCount)
	}
	if ref.TimeStep == 0 && stopFrame != startFrame {
		return 0, 0, 0, codedErrorf(codeValidationFailed, "trajectory time step is unknown; run probe or index build before exporting a frame range")
	}
	start := ref.TimeStart + float64(startFrame)*ref.TimeStep
	stop := ref.TimeStart + float64(stopFrame)*ref.TimeStep
	return start, stop, stride, nil
}

func schemaLeaf(name string, value any) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: "Print " + name + " schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(value)
		},
	}
}

func manifestSchema() map[string]any {
	fileRef := map[string]any{
		"type":                 "object",
		"required":             []string{"format"},
		"additionalProperties": false,
		"properties": map[string]any{
			"path":            map[string]any{"type": "string"},
			"url":             map[string]any{"type": "string", "format": "uri"},
			"format":          map[string]any{"type": "string"},
			"sha256":          map[string]any{"type": "string"},
			"bytes":           map[string]any{"type": "integer", "minimum": 0},
			"atom_count":      map[string]any{"type": "integer", "minimum": 0},
			"frame_count":     map[string]any{"type": "integer", "minimum": 0},
			"time_start":      map[string]any{"type": "number"},
			"time_end":        map[string]any{"type": "number"},
			"time_step":       map[string]any{"type": "number"},
			"original_path":   map[string]any{"type": "string"},
			"time_unit":       map[string]any{"type": "string"},
			"coordinate_unit": map[string]any{"type": "string"},
		},
		"anyOf": []map[string]any{
			{"required": []string{"path"}},
			{"required": []string{"url"}},
		},
	}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"title":                "MDsrv Headless Manifest and Run Job",
		"type":                 "object",
		"required":             []string{"version", "metadata", "inputs"},
		"additionalProperties": false,
		"properties": map[string]any{
			"version": map[string]any{"const": mdsrv.ManifestVersion},
			"runtime": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"max_atoms":       map[string]any{"type": "integer", "minimum": 0},
					"max_frames":      map[string]any{"type": "integer", "minimum": 0},
					"max_chunk_bytes": map[string]any{"type": "integer", "minimum": 0},
					"timeout_seconds": map[string]any{"type": "integer", "minimum": 0},
				},
			},
			"metadata": map[string]any{
				"type":                 "object",
				"required":             []string{"id"},
				"additionalProperties": false,
				"properties": map[string]any{
					"id":          map[string]any{"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*$"},
					"name":        map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"source":      map[string]any{"type": "string"},
					"license":     map[string]any{"type": "string"},
					"created_by":  map[string]any{"type": "string"},
					"created_at":  map[string]any{"type": "string", "format": "date-time"},
				},
			},
			"inputs": map[string]any{
				"type":                 "object",
				"required":             []string{"topology", "trajectories"},
				"additionalProperties": false,
				"properties": map[string]any{
					"topology":     fileRef,
					"trajectories": map[string]any{"type": "array", "minItems": 1, "items": fileRef},
				},
			},
			"processing": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"stride":      map[string]any{"type": "integer", "minimum": 0},
					"atom_subset": map[string]any{"type": "string"},
					"pbc": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"unwrap": map[string]any{"type": "boolean"},
							"center": map[string]any{"type": "string"},
							"superpose": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"selection":       map[string]any{"type": "string"},
									"reference_frame": map[string]any{"type": "integer", "minimum": 0},
								},
							},
						},
					},
				},
			},
			"streaming": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"encoding":                  map[string]any{"type": "string", "examples": []string{"mdsrv-frame-v1", "mdsrv-frames-json-v1", "mdsrv-frames-bin-v1", "mdsrv-frames-bin-zstd-v1"}},
					"cache":                     map[string]any{"type": "string"},
					"chunk_size_frames":         map[string]any{"type": "integer", "minimum": 0},
					"materialize_chunks":        map[string]any{"type": "boolean"},
					"allow_atom_subset_queries": map[string]any{"type": "boolean"},
					"frame_index":               map[string]any{"type": "string"},
				},
			},
			"selections": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"required":             []string{"id", "expression"},
					"additionalProperties": false,
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"expression":  map[string]any{"type": "string"},
						"kind":        map[string]any{"type": "string", "examples": []string{"atom-index", "mdtraj", "mdanalysis", "mvs"}},
						"description": map[string]any{"type": "string"},
						"atom_count":  map[string]any{"type": "integer", "minimum": 0},
					},
				},
			},
			"analyses": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"required":             []string{"id", "type"},
					"additionalProperties": false,
					"properties": map[string]any{
						"id":              map[string]any{"type": "string"},
						"type":            map[string]any{"type": "string"},
						"selection":       map[string]any{"type": "string"},
						"selections":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"reference_frame": map[string]any{"type": "integer", "minimum": 0},
						"cutoff":          map[string]any{"type": "number"},
						"frames":          map[string]any{"type": "string"},
						"format":          map[string]any{"type": "string", "enum": []string{"", "csv", "json"}},
						"output":          map[string]any{"type": "string"},
					},
				},
			},
			"visualization": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"molstar": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"state": map[string]any{"type": "string"},
						},
					},
					"mvs": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"scene": map[string]any{"type": "string"},
						},
					},
					"camera": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"focus": map[string]any{"type": "string"},
						},
					},
					"sessions": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"required":             []string{"path"},
							"additionalProperties": false,
							"properties": map[string]any{
								"id":          map[string]any{"type": "string"},
								"path":        map[string]any{"type": "string"},
								"version":     map[string]any{"type": "string"},
								"is_sticky":   map[string]any{"type": "boolean"},
								"description": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
			"outputs": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"required":             []string{"type", "path"},
					"additionalProperties": false,
					"properties": map[string]any{
						"type": map[string]any{"type": "string", "examples": []string{"mdsrvx", "archive", "manifest", "server-store"}},
						"path": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

func batchSchema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "MDsrv Headless Batch",
		"type":    "array",
		"items": map[string]any{
			"type":     "object",
			"required": []string{"id"},
			"properties": map[string]any{
				"id":              map[string]any{"type": "string"},
				"name":            map[string]any{"type": "string"},
				"description":     map[string]any{"type": "string"},
				"source":          map[string]any{"type": "string"},
				"license":         map[string]any{"type": "string"},
				"created_by":      map[string]any{"type": "string"},
				"topology":        map[string]any{"type": "string"},
				"topology_url":    map[string]any{"type": "string", "format": "uri"},
				"trajectory":      map[string]any{"type": "string"},
				"trajectory_url":  map[string]any{"type": "string", "format": "uri"},
				"stride":          map[string]any{"type": "integer", "minimum": 0},
				"atom_subset":     map[string]any{"type": "string"},
				"time_unit":       map[string]any{"type": "string"},
				"coordinate_unit": map[string]any{"type": "string"},
			},
			"allOf": []map[string]any{
				{"anyOf": []map[string]any{{"required": []string{"topology"}}, {"required": []string{"topology_url"}}}},
				{"anyOf": []map[string]any{{"required": []string{"trajectory"}}, {"required": []string{"trajectory_url"}}}},
			},
		},
	}
}

func openAPISchema() map[string]any {
	jsonBody := func(schemaRef string) map[string]any {
		return map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"$ref": schemaRef}},
			},
		}
	}
	jsonResponse := func(description, schemaRef string) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"$ref": schemaRef}},
			},
		}
	}
	jsonArrayResponse := func(description, itemRef string) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"type":  "array",
					"items": map[string]any{"$ref": itemRef},
				}},
			},
		}
	}
	jsonObjectResponse := func(description string) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"type": "object"}},
			},
		}
	}
	binaryResponse := func(description, contentType string) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				contentType: map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
			},
		}
	}
	textResponse := func(description, contentType string) map[string]any {
		return map[string]any{
			"description": description,
			"content": map[string]any{
				contentType: map[string]any{"schema": map[string]any{"type": "string"}},
			},
		}
	}
	deleteResponse := jsonObjectResponse("Delete confirmation")
	errorResponse := map[string]any{"$ref": "#/components/responses/Error"}
	fileFormat := map[string]any{"type": "string", "examples": []string{"xtc", "gro", "pdb", "psf", "prmtop"}}
	vector3 := map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "items": map[string]any{"type": "number"}}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "MDsrv Headless API", "version": "1.0.0"},
		"paths": map[string]any{
			"/health":       map[string]any{"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Health status", "#/components/schemas/Health"), "default": errorResponse}}},
			"/version":      map[string]any{"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Service version", "#/components/schemas/Version"), "default": errorResponse}}},
			"/capabilities": map[string]any{"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Runtime capabilities", "#/components/schemas/Capabilities"), "default": errorResponse}}},
			"/metrics":      map[string]any{"get": map[string]any{"responses": map[string]any{"200": textResponse("Prometheus metrics", "text/plain"), "default": errorResponse}}},
			"/schema/manifest": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonObjectResponse("Manifest JSON Schema"), "default": errorResponse}},
			},
			"/schema/batch": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonObjectResponse("Batch JSON Schema"), "default": errorResponse}},
			},
			"/schema/openapi": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonObjectResponse("OpenAPI document"), "default": errorResponse}},
			},
			"/datasets": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Dataset list", "#/components/schemas/DatasetSummary"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/IngestOptions"), "responses": map[string]any{"200": jsonResponse("Dataset created", "#/components/schemas/Manifest"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}": map[string]any{
				"get":    map[string]any{"responses": map[string]any{"200": jsonResponse("Dataset manifest", "#/components/schemas/Manifest"), "default": errorResponse}},
				"patch":  map[string]any{"requestBody": jsonBody("#/components/schemas/UpdateOptions"), "responses": map[string]any{"200": jsonResponse("Dataset updated", "#/components/schemas/Manifest"), "default": errorResponse}},
				"delete": map[string]any{"responses": map[string]any{"200": deleteResponse, "default": errorResponse}},
			},
			"/datasets/{dataset_id}/rename": map[string]any{
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/RenameRequest"), "responses": map[string]any{"200": jsonResponse("Dataset renamed", "#/components/schemas/Manifest"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/metadata": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Dataset metadata", "#/components/schemas/Metadata"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/topology": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": binaryResponse("Topology file", "application/octet-stream"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/trajectory": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": binaryResponse("Trajectory file", "application/octet-stream"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/frames/count": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Trajectory frame count and metadata", "#/components/schemas/TrajectoryInfo"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/frames/{frame}": map[string]any{
				"get": map[string]any{"responses": map[string]any{
					"200": map[string]any{
						"description": "Frame as JSON, binary MDSF, or extracted structure file",
						"content": map[string]any{
							"application/json":                map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Frame"}},
							"application/vnd.mdsrv.frame+bin": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
							"chemical/x-gro":                  map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
							"application/octet-stream":        map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
						},
					},
					"default": errorResponse,
				}},
			},
			"/datasets/{dataset_id}/frames/range": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Frame range", "#/components/schemas/FrameRangeResponse"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/frames/index": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonResponse("Frame index", "#/components/schemas/FrameIndex"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/FrameIndexBuildRequest"), "responses": map[string]any{"200": jsonResponse("Frame index built", "#/components/schemas/FrameIndex"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/frames/chunks": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Frame chunk list", "#/components/schemas/FrameChunk"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/ChunkBuildRequest"), "responses": map[string]any{"200": jsonResponse("Static frame chunks materialized", "#/components/schemas/FrameIndex"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/frames/chunks/{chunk}": map[string]any{
				"get": map[string]any{"responses": map[string]any{
					"200": map[string]any{
						"description": "Static frame chunk; decoded JSON by default, raw bytes with format=raw",
						"content": map[string]any{
							"application/json":                  map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/FrameChunkData"}},
							"application/vnd.mdsrv.frames+bin":  map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
							"application/vnd.mdsrv.frames+zstd": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
						},
					},
					"default": errorResponse,
				}},
			},
			"/datasets/{dataset_id}/selections": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Selection list", "#/components/schemas/Selection"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/Selection"), "responses": map[string]any{"200": jsonResponse("Selection saved", "#/components/schemas/Selection"), "default": errorResponse}},
			},
			"/datasets/{dataset_id}/selections/{selection_id}": map[string]any{
				"get":    map[string]any{"responses": map[string]any{"200": jsonResponse("Selection", "#/components/schemas/Selection"), "default": errorResponse}},
				"delete": map[string]any{"responses": map[string]any{"200": deleteResponse, "default": errorResponse}},
			},
			"/datasets/{dataset_id}/analyses": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Analysis list", "#/components/schemas/Analysis"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/AnalysisRequest"), "responses": map[string]any{"200": jsonResponse("Analysis trace", "#/components/schemas/Trace"), "default": errorResponse}},
			},
			"/jobs": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Async job list", "#/components/schemas/JobStatus"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/JobRequest"), "responses": map[string]any{"202": jsonResponse("Async job accepted", "#/components/schemas/JobStatus"), "default": errorResponse}},
			},
			"/jobs/{job_id}": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Async job status", "#/components/schemas/JobStatus"), "default": errorResponse}},
			},
			"/jobs/stats": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Async job queue statistics", "#/components/schemas/JobStats"), "default": errorResponse}},
			},
			"/jobs/metrics": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": textResponse("Async job Prometheus metrics", "text/plain"), "default": errorResponse}},
			},
			"/jobs/{job_id}/logs": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Async job log", "#/components/schemas/JobLog"), "default": errorResponse}},
			},
			"/jobs/{job_id}/events": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": jsonResponse("Async job events", "#/components/schemas/JobEvents"), "default": errorResponse}},
			},
			"/jobs/{job_id}/cancel": map[string]any{
				"post": map[string]any{"responses": map[string]any{"200": jsonResponse("Async job canceled", "#/components/schemas/JobStatus"), "default": errorResponse}},
			},
			"/jobs/{job_id}/retry": map[string]any{
				"post": map[string]any{"responses": map[string]any{"202": jsonResponse("Async job retry accepted", "#/components/schemas/JobStatus"), "default": errorResponse}},
			},
			"/sessions": map[string]any{
				"get":  map[string]any{"responses": map[string]any{"200": jsonArrayResponse("Session list", "#/components/schemas/SessionSummary"), "default": errorResponse}},
				"post": map[string]any{"requestBody": jsonBody("#/components/schemas/SessionOptions"), "responses": map[string]any{"200": jsonResponse("Session published", "#/components/schemas/SessionSummary"), "default": errorResponse}},
			},
			"/trajectory_index.json": map[string]any{"get": map[string]any{"responses": map[string]any{"200": jsonObjectResponse("MDsrv trajectory catalog"), "default": errorResponse}}},
			"/session_index.json":    map[string]any{"get": map[string]any{"responses": map[string]any{"200": jsonObjectResponse("MDsrv session catalog"), "default": errorResponse}}},
		},
		"components": map[string]any{
			"responses": map[string]any{
				"Error": map[string]any{
					"description": "Structured error response",
					"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}},
					},
				},
			},
			"schemas": map[string]any{
				"Error": map[string]any{
					"type":       "object",
					"required":   []string{"error"},
					"properties": map[string]any{"error": map[string]any{"type": "string"}, "code": map[string]any{"type": "string"}, "request_id": map[string]any{"type": "string"}},
				},
				"Health": map[string]any{
					"type":       "object",
					"required":   []string{"status"},
					"properties": map[string]any{"status": map[string]any{"type": "string", "const": "ok"}},
				},
				"Version": map[string]any{
					"type":       "object",
					"required":   []string{"service", "manifest_version"},
					"properties": map[string]any{"service": map[string]any{"type": "string"}, "manifest_version": map[string]any{"type": "string"}},
				},
				"Capabilities": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"datasets":            map[string]any{"type": "boolean"},
						"dataset_writes":      map[string]any{"type": "boolean"},
						"read_only":           map[string]any{"type": "boolean"},
						"auth_required":       map[string]any{"type": "boolean"},
						"metadata":            map[string]any{"type": "boolean"},
						"file_serving":        map[string]any{"type": "boolean"},
						"frame_decoding":      map[string]any{"type": "boolean"},
						"frame_index":         map[string]any{"type": "boolean"},
						"chunk_encodings":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"frame_ranges":        map[string]any{"type": "boolean"},
						"max_frame_range":     map[string]any{"type": "integer"},
						"max_atoms":           map[string]any{"type": "integer"},
						"max_frames":          map[string]any{"type": "integer"},
						"max_chunk_bytes":     map[string]any{"type": "integer"},
						"job_queue":           map[string]any{"type": "boolean"},
						"workers":             map[string]any{"type": "integer"},
						"max_queue":           map[string]any{"type": "integer"},
						"job_timeout_seconds": map[string]any{"type": "integer"},
						"job_ttl_seconds":     map[string]any{"type": "integer"},
						"job_prune_on_start":  map[string]any{"type": "boolean"},
						"gromacs_extraction":  map[string]any{"type": "boolean"},
						"analysis":            map[string]any{"type": "boolean"},
						"selections":          map[string]any{"type": "boolean"},
						"sessions":            map[string]any{"type": "boolean"},
						"streaming_baseline":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
				"IngestOptions": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":              map[string]any{"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*$"},
						"name":            map[string]any{"type": "string"},
						"description":     map[string]any{"type": "string"},
						"source":          map[string]any{"type": "string"},
						"license":         map[string]any{"type": "string"},
						"created_by":      map[string]any{"type": "string"},
						"topology":        map[string]any{"type": "string"},
						"topology_url":    map[string]any{"type": "string", "format": "uri"},
						"trajectory":      map[string]any{"type": "string"},
						"trajectory_url":  map[string]any{"type": "string", "format": "uri"},
						"cache":           map[string]any{"type": "string"},
						"stride":          map[string]any{"type": "integer", "minimum": 0},
						"atom_subset":     map[string]any{"type": "string"},
						"time_unit":       map[string]any{"type": "string"},
						"coordinate_unit": map[string]any{"type": "string"},
						"force":           map[string]any{"type": "boolean"},
					},
					"allOf": []map[string]any{
						{"anyOf": []map[string]any{{"required": []string{"topology"}}, {"required": []string{"topology_url"}}}},
						{"anyOf": []map[string]any{{"required": []string{"trajectory"}}, {"required": []string{"trajectory_url"}}}},
					},
				},
				"UpdateOptions": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"source":      map[string]any{"type": "string"},
						"license":     map[string]any{"type": "string"},
					},
				},
				"RenameRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":     map[string]any{"type": "string"},
						"new_id": map[string]any{"type": "string"},
					},
				},
				"SessionOptions": map[string]any{
					"type":     "object",
					"required": []string{"id", "dataset_id", "file"},
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"dataset_id":  map[string]any{"type": "string"},
						"file":        map[string]any{"type": "string"},
						"version":     map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"is_sticky":   map[string]any{"type": "boolean"},
						"force":       map[string]any{"type": "boolean"},
					},
				},
				"SessionSummary": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"source":      map[string]any{"type": "string"},
						"version":     map[string]any{"type": "string"},
						"is_sticky":   map[string]any{"type": "boolean"},
						"path":        map[string]any{"type": "string"},
					},
				},
				"Selection": map[string]any{
					"type":     "object",
					"required": []string{"id", "expression"},
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"expression":  map[string]any{"type": "string"},
						"kind":        map[string]any{"type": "string", "examples": []string{"atom-index", "mdanalysis", "mdtraj", "mvs"}},
						"description": map[string]any{"type": "string"},
						"atom_count":  map[string]any{"type": "integer", "minimum": 0},
					},
				},
				"AnalysisRequest": map[string]any{
					"type":     "object",
					"required": []string{"type"},
					"properties": map[string]any{
						"id":              map[string]any{"type": "string"},
						"type":            map[string]any{"type": "string", "examples": []string{"distance", "angle", "dihedral", "rmsd", "rgyr", "rmsf", "contacts", "sasa", "hbonds"}},
						"selection":       map[string]any{"type": "string"},
						"selections":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"reference_frame": map[string]any{"type": "integer", "minimum": 0},
						"cutoff":          map[string]any{"type": "number"},
						"format":          map[string]any{"type": "string", "enum": []string{"csv", "json", ""}},
						"output":          map[string]any{"type": "string"},
					},
				},
				"Analysis": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":              map[string]any{"type": "string"},
						"type":            map[string]any{"type": "string"},
						"selection":       map[string]any{"type": "string"},
						"selections":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"reference_frame": map[string]any{"type": "integer", "minimum": 0},
						"cutoff":          map[string]any{"type": "number"},
						"frames":          map[string]any{"type": "string"},
						"output":          map[string]any{"type": "string"},
					},
				},
				"TraceValue": map[string]any{
					"type":     "object",
					"required": []string{"frame", "time", "value"},
					"properties": map[string]any{
						"frame": map[string]any{"type": "integer", "minimum": 0},
						"time":  map[string]any{"type": "number"},
						"value": map[string]any{"type": "number"},
					},
				},
				"Trace": map[string]any{
					"type":     "object",
					"required": []string{"backend", "id", "type", "unit", "values"},
					"properties": map[string]any{
						"backend": map[string]any{"type": "string"},
						"id":      map[string]any{"type": "string"},
						"type":    map[string]any{"type": "string"},
						"unit":    map[string]any{"type": "string"},
						"values":  map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/TraceValue"}},
					},
				},
				"Frame": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"backend":         map[string]any{"type": "string"},
						"frame":           map[string]any{"type": "integer", "minimum": 0},
						"time":            map[string]any{"type": "number"},
						"time_unit":       map[string]any{"type": "string"},
						"coordinate_unit": map[string]any{"type": "string"},
						"unit_cell":       map[string]any{"type": "array", "items": vector3},
						"coordinates":     map[string]any{"type": "array", "items": vector3},
					},
				},
				"FrameRangeResponse": map[string]any{
					"type":     "object",
					"required": []string{"dataset_id", "start", "stop", "stride", "frames"},
					"properties": map[string]any{
						"dataset_id": map[string]any{"type": "string"},
						"start":      map[string]any{"type": "integer", "minimum": 0},
						"stop":       map[string]any{"type": "integer", "minimum": 0},
						"stride":     map[string]any{"type": "integer", "minimum": 1},
						"frames":     map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Frame"}},
					},
				},
				"TrajectoryInfo": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"backend":         map[string]any{"type": "string"},
						"frames":          map[string]any{"type": "integer", "minimum": 0},
						"atoms":           map[string]any{"type": "integer", "minimum": 0},
						"topology_atoms":  map[string]any{"type": "integer", "minimum": 0},
						"time_unit":       map[string]any{"type": "string"},
						"coordinate_unit": map[string]any{"type": "string"},
						"first_time":      map[string]any{"type": "number"},
						"last_time":       map[string]any{"type": "number"},
						"has_unit_cell":   map[string]any{"type": "boolean"},
					},
				},
				"FramePoint": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"index": map[string]any{"type": "integer", "minimum": 0},
						"time":  map[string]any{"type": "number"},
					},
				},
				"FrameChunk": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"index":    map[string]any{"type": "integer", "minimum": 0},
						"start":    map[string]any{"type": "integer", "minimum": 0},
						"stop":     map[string]any{"type": "integer", "minimum": 0},
						"path":     map[string]any{"type": "string"},
						"encoding": map[string]any{"type": "string", "enum": []string{"mdsrv-frames-json-v1", "mdsrv-frames-bin-v1", "mdsrv-frames-bin-zstd-v1"}},
					},
				},
				"FrameIndex": map[string]any{
					"type":     "object",
					"required": []string{"dataset_id", "frame_count", "chunk_size_frames", "chunks"},
					"properties": map[string]any{
						"dataset_id":        map[string]any{"type": "string"},
						"frame_count":       map[string]any{"type": "integer", "minimum": 0},
						"atom_count":        map[string]any{"type": "integer", "minimum": 0},
						"time_start":        map[string]any{"type": "number"},
						"time_end":          map[string]any{"type": "number"},
						"time_step":         map[string]any{"type": "number"},
						"chunk_size_frames": map[string]any{"type": "integer", "minimum": 1},
						"frames":            map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/FramePoint"}},
						"chunks":            map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/FrameChunk"}},
					},
				},
				"FrameChunkData": map[string]any{
					"type":     "object",
					"required": []string{"dataset_id", "chunk", "start", "stop", "encoding", "frames"},
					"properties": map[string]any{
						"dataset_id": map[string]any{"type": "string"},
						"chunk":      map[string]any{"type": "integer", "minimum": 0},
						"start":      map[string]any{"type": "integer", "minimum": 0},
						"stop":       map[string]any{"type": "integer", "minimum": 0},
						"encoding":   map[string]any{"type": "string"},
						"frames":     map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Frame"}},
					},
				},
				"FrameIndexBuildRequest": map[string]any{
					"type":       "object",
					"properties": map[string]any{"chunk_size": map[string]any{"type": "integer", "minimum": 1}},
				},
				"ChunkBuildRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"chunk_size": map[string]any{"type": "integer", "minimum": 1},
						"encoding":   map[string]any{"type": "string", "enum": []string{"json", "bin", "bin-zstd"}},
						"force":      map[string]any{"type": "boolean"},
					},
				},
				"JobRequest": map[string]any{
					"type":     "object",
					"required": []string{"type", "dataset_id"},
					"properties": map[string]any{
						"type":            map[string]any{"type": "string", "enum": []string{"chunks", "analysis"}},
						"dataset_id":      map[string]any{"type": "string"},
						"backend":         map[string]any{"type": "string", "enum": []string{"auto", "python", "mdtraj", "mdanalysis", "gromacs"}},
						"chunk_size":      map[string]any{"type": "integer", "minimum": 1},
						"encoding":        map[string]any{"type": "string", "enum": []string{"json", "bin", "bin-zstd"}},
						"force":           map[string]any{"type": "boolean"},
						"analysis":        map[string]any{"$ref": "#/components/schemas/AnalysisRequest"},
						"timeout_seconds": map[string]any{"type": "integer", "minimum": 0},
					},
				},
				"JobStatus": map[string]any{
					"type":     "object",
					"required": []string{"id", "type", "dataset_id", "status", "created_at", "request"},
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string"},
						"dataset_id":  map[string]any{"type": "string"},
						"status":      map[string]any{"type": "string", "enum": []string{"queued", "running", "succeeded", "failed", "canceled"}},
						"created_at":  map[string]any{"type": "string", "format": "date-time"},
						"started_at":  map[string]any{"type": "string", "format": "date-time"},
						"finished_at": map[string]any{"type": "string", "format": "date-time"},
						"error":       map[string]any{"type": "string"},
						"request":     map[string]any{"$ref": "#/components/schemas/JobRequest"},
						"result":      map[string]any{"type": "object", "additionalProperties": true},
					},
				},
				"JobLog": map[string]any{
					"type":     "object",
					"required": []string{"id", "log"},
					"properties": map[string]any{
						"id":  map[string]any{"type": "string"},
						"log": map[string]any{"type": "string"},
					},
				},
				"JobEvent": map[string]any{
					"type":     "object",
					"required": []string{"version", "at", "id", "type", "message"},
					"properties": map[string]any{
						"version": map[string]any{"type": "string", "const": jobEventVersion},
						"at":      map[string]any{"type": "string", "format": "date-time"},
						"id":      map[string]any{"type": "string"},
						"status":  map[string]any{"type": "string", "enum": []string{"queued", "running", "succeeded", "failed", "canceled"}},
						"type": map[string]any{"type": "string", "enum": []string{
							string(jobEventSubmitted),
							string(jobEventStarted),
							string(jobEventChunksStarted),
							string(jobEventChunksCompleted),
							string(jobEventAnalysisStarted),
							string(jobEventAnalysisCompleted),
							string(jobEventSucceeded),
							string(jobEventFailed),
							string(jobEventCanceled),
							string(jobEventRetried),
						}},
						"message": map[string]any{"type": "string"},
						"fields":  map[string]any{"type": "object", "additionalProperties": true},
					},
				},
				"JobEvents": map[string]any{
					"type":     "object",
					"required": []string{"id", "events"},
					"properties": map[string]any{
						"id":     map[string]any{"type": "string"},
						"events": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/JobEvent"}},
					},
				},
				"JobStats": map[string]any{
					"type":     "object",
					"required": []string{"workers", "max_queue", "queued_channel_depth", "total", "counts"},
					"properties": map[string]any{
						"workers":                   map[string]any{"type": "integer", "minimum": 0},
						"max_queue":                 map[string]any{"type": "integer", "minimum": 0},
						"queued_channel_depth":      map[string]any{"type": "integer", "minimum": 0},
						"total":                     map[string]any{"type": "integer", "minimum": 0},
						"counts":                    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer", "minimum": 0}},
						"oldest_queued_at":          map[string]any{"type": "string", "format": "date-time"},
						"oldest_queued_age_seconds": map[string]any{"type": "number", "minimum": 0},
					},
				},
				"FileRef": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":            map[string]any{"type": "string"},
						"url":             map[string]any{"type": "string", "format": "uri"},
						"format":          fileFormat,
						"sha256":          map[string]any{"type": "string"},
						"bytes":           map[string]any{"type": "integer", "minimum": 0},
						"atom_count":      map[string]any{"type": "integer", "minimum": 0},
						"frame_count":     map[string]any{"type": "integer", "minimum": 0},
						"time_start":      map[string]any{"type": "number"},
						"time_end":        map[string]any{"type": "number"},
						"time_step":       map[string]any{"type": "number"},
						"time_unit":       map[string]any{"type": "string"},
						"coordinate_unit": map[string]any{"type": "string"},
					},
				},
				"Metadata": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"source":      map[string]any{"type": "string"},
						"license":     map[string]any{"type": "string"},
						"created_by":  map[string]any{"type": "string"},
						"created_at":  map[string]any{"type": "string", "format": "date-time"},
					},
				},
				"DatasetSummary": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"source":      map[string]any{"type": "string"},
						"manifest":    map[string]any{"type": "string"},
						"topology":    map[string]any{"type": "string"},
						"trajectory":  map[string]any{"type": "string"},
						"atom_count":  map[string]any{"type": "integer", "minimum": 0},
						"frame_count": map[string]any{"type": "integer", "minimum": 0},
					},
				},
				"Manifest": map[string]any{
					"allOf": []map[string]any{manifestSchema()},
				},
			},
		},
	}
}
