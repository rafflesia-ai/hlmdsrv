package mdsrvcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/job"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
	"github.com/rafflesia-ai/hlmdsrv/internal/mvs"
)

type runFlags struct {
	store           string
	backend         string
	gmxCommand      string
	cache           string
	force           bool
	probe           bool
	index           bool
	chunks          bool
	chunkEncoding   string
	plan            bool
	dryRun          bool
	strict          bool
	maxAtoms        int
	maxFrames       int
	maxChunkBytes   int64
	probeTimeout    time.Duration
	analysisTimeout time.Duration
	reportPath      string
	jsonReport      bool
}

type runReport struct {
	ID              string          `json:"id"`
	Store           string          `json:"store"`
	Manifest        string          `json:"manifest"`
	Probed          bool            `json:"probed,omitempty"`
	Index           string          `json:"index,omitempty"`
	Chunks          []string        `json:"chunks,omitempty"`
	Analyses        []string        `json:"analyses,omitempty"`
	Visualization   string          `json:"visualization,omitempty"`
	Sessions        []string        `json:"sessions,omitempty"`
	Archives        []string        `json:"archives,omitempty"`
	Warnings        []string        `json:"warnings,omitempty"`
	Artifacts       []runArtifact   `json:"artifacts,omitempty"`
	Timings         []runStepTiming `json:"timings,omitempty"`
	TotalDurationMS int64           `json:"total_duration_ms"`
}

type runArtifact struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}

type runStepTiming struct {
	Step       string `json:"step"`
	Target     string `json:"target,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type runPlanReport struct {
	ID     string        `json:"id"`
	Store  string        `json:"store"`
	DryRun bool          `json:"dry_run,omitempty"`
	Steps  []runPlanStep `json:"steps"`
}

type runPlanStep struct {
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Note   string `json:"note,omitempty"`
}

func (a app) runCommand() *cobra.Command {
	flags := &runFlags{probe: true, index: true, jsonReport: true}
	cmd := &cobra.Command{
		Use:   "run JOB.yaml",
		Short: "Run an end-to-end MDsrv headless job manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.plan || flags.dryRun {
				plan, err := a.planJob(args[0], flags)
				if err != nil {
					return err
				}
				plan.DryRun = flags.dryRun
				if flags.jsonReport {
					return writeJSON(a.stdout, plan)
				}
				for _, step := range plan.Steps {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", step.Action, firstNonEmpty(step.Target, step.Output), step.Note)
				}
				return nil
			}
			report, err := a.runJob(cmd.Context(), args[0], flags)
			if flags.reportPath != "" && report.ID != "" {
				if writeErr := writeRunReportFile(flags.reportPath, report); writeErr != nil {
					if err == nil {
						return writeErr
					}
					fmt.Fprintln(a.stderr, "warning: write report:", writeErr)
				}
			}
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "dataset %s\n", report.ID)
			fmt.Fprintf(a.stdout, "manifest %s\n", report.Manifest)
			for _, warning := range report.Warnings {
				fmt.Fprintln(a.stderr, "warning:", warning)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "download cache directory for URL inputs")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing datasets and generated outputs")
	cmd.Flags().BoolVar(&flags.probe, "probe", true, "probe trajectory metadata after ingest")
	cmd.Flags().BoolVar(&flags.index, "index", true, "build a frame index after ingest")
	cmd.Flags().BoolVar(&flags.chunks, "chunks", false, "materialize static frame chunks after indexing")
	cmd.Flags().StringVar(&flags.chunkEncoding, "chunk-encoding", "", "chunk encoding override: json, bin, or bin-zstd; defaults to job streaming.encoding or json")
	cmd.Flags().BoolVar(&flags.plan, "plan", false, "print the job steps without touching the store")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "alias for --plan with dry_run=true in JSON output")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "fail if optional job artifacts such as sessions are referenced but missing")
	cmd.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "fail if the dataset or decoded frame exceeds this atom count; 0 uses runtime.max_atoms")
	cmd.Flags().IntVar(&flags.maxFrames, "max-frames", 0, "fail if the dataset exceeds this frame count; 0 uses runtime.max_frames")
	cmd.Flags().Int64Var(&flags.maxChunkBytes, "max-chunk-bytes", 0, "fail if an encoded chunk exceeds this byte count; 0 uses runtime.max_chunk_bytes")
	cmd.Flags().DurationVar(&flags.probeTimeout, "probe-timeout", 0, "timeout for probe/index steps; 0 uses the command context")
	cmd.Flags().DurationVar(&flags.analysisTimeout, "analysis-timeout", 0, "timeout for each analysis step; 0 uses the command context")
	cmd.Flags().StringVar(&flags.reportPath, "report", "", "write a durable JSON run report to this path")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", true, "write machine-readable output")
	return cmd
}

func (a app) planJob(path string, flags *runFlags) (runPlanReport, error) {
	if err := validateMDSrvJobSchemaFile(path); err != nil {
		return runPlanReport{}, err
	}
	job, err := mdsrv.LoadManifestFile(path)
	if err != nil {
		return runPlanReport{}, err
	}
	if len(job.Inputs.Trajectories) == 0 {
		return runPlanReport{}, errors.New("job requires at least one trajectory")
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return runPlanReport{}, err
	}
	jobDir := filepath.Dir(path)
	topology := resolveJobFile(jobDir, job.Inputs.Topology.Path)
	trajectory := resolveJobFile(jobDir, job.Inputs.Trajectories[0].Path)
	report := runPlanReport{ID: job.Metadata.ID, Store: store.Root}
	report.Steps = append(report.Steps, runPlanStep{
		Action: "ingest",
		Target: job.Metadata.ID,
		Input:  strings.TrimSpace(topology + " " + trajectory),
		Output: filepath.ToSlash(filepath.Join(mdsrv.DatasetsDir, job.Metadata.ID+".yaml")),
		Note:   overwriteNote(flags.force),
	})
	for _, selection := range job.Selections {
		report.Steps = append(report.Steps, runPlanStep{Action: "selection", Target: selection.ID, Note: selection.Expression})
	}
	if flags.probe {
		report.Steps = append(report.Steps, runPlanStep{Action: "probe", Target: job.Metadata.ID, Note: timeoutNote(flags.probeTimeout)})
	}
	if flags.index || flags.chunks || job.Streaming.MaterializeChunks {
		chunkSize := firstPositive(job.Streaming.ChunkSizeFrames, 128)
		action := "index"
		if flags.chunks || job.Streaming.MaterializeChunks {
			action = "chunks"
		}
		limitNote := resourceLimitNote(runResourceLimits(job, flags))
		report.Steps = append(report.Steps, runPlanStep{
			Action: action,
			Target: job.Metadata.ID,
			Output: filepath.ToSlash(filepath.Join(mdsrv.IndexesDir, job.Metadata.ID+"-frame-index.json")),
			Note:   strings.TrimSpace(fmt.Sprintf("chunk_size=%d encoding=%s %s %s", chunkSize, runChunkEncoding(job, flags), timeoutNote(flags.probeTimeout), limitNote)),
		})
	}
	if job.Visualization.MVS.Scene != "" {
		report.Steps = append(report.Steps, runPlanStep{Action: "visualize", Target: job.Metadata.ID, Output: job.Visualization.MVS.Scene})
	}
	for _, analysis := range job.Analyses {
		report.Steps = append(report.Steps, runPlanStep{
			Action: "analysis",
			Target: firstNonEmpty(analysis.ID, analysis.Type),
			Output: firstNonEmpty(analysis.Output, filepath.ToSlash(filepath.Join("traces", job.Metadata.ID+"-"+analysis.Type+".csv"))),
			Note:   timeoutNote(flags.analysisTimeout),
		})
	}
	for _, session := range job.Visualization.Sessions {
		report.Steps = append(report.Steps, runPlanStep{Action: "session", Target: firstNonEmpty(session.ID, job.Metadata.ID+"-session"), Input: resolveJobFile(jobDir, session.Path), Note: "publish if file exists"})
	}
	if job.Visualization.Molstar.State != "" && len(job.Visualization.Sessions) == 0 {
		report.Steps = append(report.Steps, runPlanStep{Action: "session", Target: job.Metadata.ID + "-session", Input: resolveJobFile(jobDir, job.Visualization.Molstar.State), Note: "publish if file exists"})
	}
	for _, output := range job.Outputs {
		if strings.EqualFold(output.Type, "mdsrvx") || strings.EqualFold(output.Type, "archive") {
			report.Steps = append(report.Steps, runPlanStep{Action: "pack", Target: job.Metadata.ID, Output: resolveJobFile(jobDir, output.Path), Note: overwriteNote(flags.force)})
		}
	}
	return report, nil
}

func (a app) runJob(ctx context.Context, path string, flags *runFlags) (report runReport, err error) {
	runStarted := time.Now()
	defer func() {
		report.TotalDurationMS = durationMS(runStarted)
	}()
	if err := validateMDSrvJobSchemaFile(path); err != nil {
		return runReport{}, err
	}
	job, err := mdsrv.LoadManifestFile(path)
	if err != nil {
		return runReport{}, err
	}
	if len(job.Inputs.Trajectories) == 0 {
		return runReport{}, errors.New("job requires at least one trajectory")
	}
	if job.Runtime.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(job.Runtime.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	limits := runResourceLimits(job, flags)
	if err := limits.Validate(); err != nil {
		return runReport{}, err
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return runReport{}, err
	}
	jobDir := filepath.Dir(path)
	topology := resolveJobFile(jobDir, job.Inputs.Topology.Path)
	trajectory := resolveJobFile(jobDir, job.Inputs.Trajectories[0].Path)
	stepStarted := time.Now()
	manifest, err := store.Ingest(mdsrv.IngestOptions{
		ID:            job.Metadata.ID,
		Name:          job.Metadata.Name,
		Description:   job.Metadata.Description,
		Source:        job.Metadata.Source,
		License:       job.Metadata.License,
		CreatedBy:     job.Metadata.CreatedBy,
		Topology:      topology,
		TopologyURL:   job.Inputs.Topology.URL,
		Trajectory:    trajectory,
		TrajectoryURL: job.Inputs.Trajectories[0].URL,
		Cache:         firstNonEmpty(flags.cache, job.Streaming.Cache),
		Stride:        job.Processing.Stride,
		AtomSubset:    job.Processing.AtomSubset,
		TimeUnit:      firstNonEmpty(job.Inputs.Trajectories[0].TimeUnit, "ps"),
		CoordUnit:     firstNonEmpty(job.Inputs.Trajectories[0].CoordUnit, "nm"),
		Force:         flags.force,
	})
	if err != nil {
		return runReport{}, err
	}
	report = runReport{
		ID:       manifest.Metadata.ID,
		Store:    store.Root,
		Manifest: filepath.ToSlash(filepath.Join(mdsrv.DatasetsDir, manifest.Metadata.ID+".yaml")),
	}
	addRunTiming(&report, "ingest", manifest.Metadata.ID, stepStarted)
	recordRunArtifact(&report, store.Root, "manifest", report.Manifest)
	recordRunArtifact(&report, store.Root, "topology", manifest.Inputs.Topology.Path)
	for _, trajectory := range manifest.Inputs.Trajectories {
		recordRunArtifact(&report, store.Root, "trajectory", trajectory.Path)
	}
	stepStarted = time.Now()
	for _, selection := range job.Selections {
		if _, err := store.SaveSelection(manifest.Metadata.ID, selection); err != nil {
			return report, err
		}
	}
	if len(job.Selections) > 0 {
		addRunTiming(&report, "selection", manifest.Metadata.ID, stepStarted)
		recordRunArtifact(&report, store.Root, "manifest", report.Manifest)
	}
	if flags.probe {
		stepStarted = time.Now()
		probeCtx, cancel := contextWithOptionalTimeout(ctx, flags.probeTimeout)
		if probed, err := store.ProbeDataset(probeCtx, manifest.Metadata.ID, flags.gmxCommand); err == nil {
			cancel()
			if err := limits.CheckManifest(probed); err != nil {
				return report, err
			}
			manifest = probed
			report.Probed = true
			addRunTiming(&report, "probe", manifest.Metadata.ID, stepStarted)
			recordRunArtifact(&report, store.Root, "manifest", report.Manifest)
		} else if flags.strict {
			cancel()
			return report, err
		} else {
			cancel()
			addRunTiming(&report, "probe", manifest.Metadata.ID, stepStarted)
			report.Warnings = append(report.Warnings, "probe failed: "+err.Error())
		}
	}
	if flags.index || flags.chunks || job.Streaming.MaterializeChunks {
		chunkSize := manifest.Streaming.ChunkSizeFrames
		if job.Streaming.ChunkSizeFrames > 0 {
			chunkSize = job.Streaming.ChunkSizeFrames
		}
		if chunkSize <= 0 {
			chunkSize = 128
		}
		materializeChunks := flags.chunks || job.Streaming.MaterializeChunks
		var index mdsrv.FrameIndex
		var err error
		stepStarted = time.Now()
		indexCtx, cancel := contextWithOptionalTimeout(ctx, flags.probeTimeout)
		if materializeChunks {
			index, err = store.BuildFrameChunksWithOptions(indexCtx, manifest.Metadata.ID, mdsrv.BuildFrameChunksOptions{
				ChunkSize:      chunkSize,
				Encoding:       runChunkEncoding(job, flags),
				GromacsCommand: flags.gmxCommand,
				Force:          flags.force,
				Limits:         limits,
			})
		} else {
			index, err = store.BuildFrameIndexWithOptions(indexCtx, manifest.Metadata.ID, mdsrv.BuildFrameIndexOptions{
				ChunkSize:      chunkSize,
				GromacsCommand: flags.gmxCommand,
				Limits:         limits,
			})
		}
		cancel()
		stepName := "index"
		if materializeChunks {
			stepName = "chunks"
		}
		addRunTiming(&report, stepName, manifest.Metadata.ID, stepStarted)
		if err != nil {
			if flags.strict {
				return report, err
			}
			report.Warnings = append(report.Warnings, "frame index failed: "+err.Error())
		} else {
			report.Index = filepath.ToSlash(filepath.Join(mdsrv.IndexesDir, manifest.Metadata.ID+"-frame-index.json"))
			recordRunArtifact(&report, store.Root, "frame_index", report.Index)
			for _, chunk := range index.Chunks {
				if chunk.Path != "" {
					report.Chunks = append(report.Chunks, chunk.Path)
					recordRunArtifact(&report, store.Root, "chunk", chunk.Path)
				}
			}
			if index.FrameCount == 0 {
				report.Warnings = append(report.Warnings, "frame index has zero frames")
			}
		}
	}
	if job.Visualization.MVS.Scene != "" {
		stepStarted = time.Now()
		path, err := a.generateJobVisualization(store, manifest.Metadata.ID, job)
		addRunTiming(&report, "visualize", manifest.Metadata.ID, stepStarted)
		if err != nil {
			if flags.strict {
				return report, err
			}
			report.Warnings = append(report.Warnings, "visualization failed: "+err.Error())
		} else {
			report.Visualization = path
			recordRunArtifact(&report, store.Root, "visualization", path)
		}
	}
	for _, analysis := range job.Analyses {
		stepStarted = time.Now()
		analysisCtx, cancel := contextWithOptionalTimeout(ctx, flags.analysisTimeout)
		output, err := a.runJobAnalysis(analysisCtx, store, manifest.Metadata.ID, analysis, flags)
		cancel()
		addRunTiming(&report, "analysis", firstNonEmpty(analysis.ID, analysis.Type), stepStarted)
		if err != nil {
			return report, err
		}
		report.Analyses = append(report.Analyses, output)
		recordRunArtifact(&report, store.Root, "analysis", output)
	}
	stepStarted = time.Now()
	sessionRefs, err := publishJobSessions(store, jobDir, manifest.Metadata.ID, job, flags)
	if len(sessionRefs) > 0 || err != nil {
		addRunTiming(&report, "session", manifest.Metadata.ID, stepStarted)
	}
	if err != nil {
		return report, err
	}
	report.Sessions = sessionRefs
	for _, session := range sessionRefs {
		recordRunArtifact(&report, store.Root, "session", session)
	}
	stepStarted = time.Now()
	archives, err := packJobOutputs(store, manifest.Metadata.ID, job.Outputs, jobDir, flags.force)
	if len(archives) > 0 || err != nil {
		addRunTiming(&report, "pack", manifest.Metadata.ID, stepStarted)
	}
	if err != nil {
		return report, err
	}
	report.Archives = archives
	for _, archive := range archives {
		recordRunArtifact(&report, store.Root, "archive", archive)
	}
	return report, nil
}

func (a app) generateJobVisualization(store mdsrv.Store, datasetID string, jobManifest mdsrv.Manifest) (string, error) {
	m, err := store.LoadDataset(datasetID)
	if err != nil {
		return "", err
	}
	topologyPath, err := store.SafeResolvePath(m.Inputs.Topology.Path)
	if err != nil {
		return "", err
	}
	outputPath := jobManifest.Visualization.MVS.Scene
	if outputPath == "" {
		outputPath = filepath.Join(store.Root, "visualization", datasetID+".mvsj")
	} else if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(store.Root, filepath.FromSlash(outputPath))
	}
	component := "all"
	components, err := visualizationComponents(m, component, "cartoon", &visualizeFlags{
		includeSelections: len(m.Selections) > 0,
		focus:             jobManifest.Visualization.Camera.Focus,
	})
	if err != nil {
		return "", err
	}
	renderJob := job.Job{
		Version: 1,
		Inputs:  map[string]job.Input{"topology": {Path: topologyPath, Format: m.Inputs.Topology.Format}},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source:     "topology",
				Components: components,
			}},
			Camera: job.Camera{Focus: jobManifest.Visualization.Camera.Focus},
		},
	}
	compiled, err := mvs.Compile(renderJob)
	if err != nil {
		return "", err
	}
	if err := mvs.WriteFile(outputPath, compiled.Document); err != nil {
		return "", err
	}
	if relative, ok := storeRelativePathIfInside(store.Root, outputPath); ok {
		m.Visualization.MVS.Scene = relative
		_ = mdsrv.WriteManifestFile(store.ManifestPath(datasetID), m)
		return relative, nil
	}
	return outputPath, nil
}

func (a app) runJobAnalysis(ctx context.Context, store mdsrv.Store, datasetID string, analysis mdsrv.Analysis, flags *runFlags) (string, error) {
	manifest, err := store.LoadDataset(datasetID)
	if err != nil {
		return "", err
	}
	request := mdsrv.AnalysisRequest{
		ID:             firstNonEmpty(analysis.ID, analysis.Type),
		Type:           analysis.Type,
		Selection:      mdsrv.ResolveSelectionExpression(manifest, analysis.Selection),
		Selections:     mdsrv.ResolveSelectionMap(manifest, analysis.Selections),
		ReferenceFrame: analysis.ReferenceFrame,
		Cutoff:         analysis.Cutoff,
		Output:         firstNonEmpty(analysis.Output, filepath.ToSlash(filepath.Join("traces", datasetID+"-"+analysis.Type+".csv"))),
		Format:         analysis.Format,
	}
	trace, err := analyzeWithPolicy(ctx, store, manifest, datasetID, request, flags.backend, flags.gmxCommand)
	if err != nil {
		return "", err
	}
	outputPath := request.Output
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(store.Root, outputPath)
	}
	if err := mdsrv.WriteTrace(outputPath, analysis.Format, trace); err != nil {
		return "", err
	}
	recordedOutput := request.Output
	if relative, ok := storeRelativePathIfInside(store.Root, outputPath); ok {
		recordedOutput = relative
	}
	err = store.RecordAnalysis(datasetID, mdsrv.Analysis{
		ID:             request.ID,
		Type:           request.Type,
		Selection:      request.Selection,
		Selections:     request.Selections,
		ReferenceFrame: request.ReferenceFrame,
		Cutoff:         request.Cutoff,
		Frames:         firstNonEmpty(analysis.Frames, "all"),
		Format:         analysis.Format,
		Output:         recordedOutput,
		Backend:        trace.Backend,
	})
	return recordedOutput, err
}

func publishJobSessions(store mdsrv.Store, jobDir, datasetID string, job mdsrv.Manifest, flags *runFlags) ([]string, error) {
	var sessions []mdsrv.SessionRef
	sessions = append(sessions, job.Visualization.Sessions...)
	if job.Visualization.Molstar.State != "" && len(sessions) == 0 {
		sessions = append(sessions, mdsrv.SessionRef{ID: datasetID + "-session", Path: job.Visualization.Molstar.State})
	}
	var published []string
	for _, session := range sessions {
		file := resolveJobFile(jobDir, session.Path)
		if _, err := os.Stat(file); err != nil {
			if flags.strict {
				return published, err
			}
			continue
		}
		ref, err := store.PublishSession(mdsrv.SessionOptions{
			ID:          firstNonEmpty(session.ID, datasetID+"-session"),
			DatasetID:   datasetID,
			Description: session.Description,
			Version:     session.Version,
			File:        file,
			IsSticky:    session.IsSticky,
			Force:       flags.force,
		})
		if err != nil {
			return published, err
		}
		published = append(published, ref.Path)
	}
	return published, nil
}

func packJobOutputs(store mdsrv.Store, datasetID string, outputs []mdsrv.Output, jobDir string, force bool) ([]string, error) {
	var archives []string
	for _, output := range outputs {
		switch strings.ToLower(strings.TrimSpace(output.Type)) {
		case "mdsrvx", "archive":
			path := resolveJobFile(jobDir, output.Path)
			report, err := store.PackDataset(datasetID, path, force)
			if err != nil {
				return archives, err
			}
			archives = append(archives, report.Path)
		}
	}
	return archives, nil
}

func resolveJobFile(jobDir, path string) string {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "://") {
		return path
	}
	return filepath.Join(jobDir, filepath.FromSlash(path))
}

func contextWithOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func overwriteNote(force bool) string {
	if force {
		return "overwrite=true"
	}
	return "overwrite=false"
}

func timeoutNote(timeout time.Duration) string {
	if timeout <= 0 {
		return "timeout=command"
	}
	return "timeout=" + timeout.String()
}

func runResourceLimits(job mdsrv.Manifest, flags *runFlags) mdsrv.ResourceLimits {
	limits := job.Runtime.ResourceLimits
	if flags.maxAtoms > 0 {
		limits.MaxAtoms = flags.maxAtoms
	}
	if flags.maxFrames > 0 {
		limits.MaxFrames = flags.maxFrames
	}
	if flags.maxChunkBytes > 0 {
		limits.MaxChunkBytes = flags.maxChunkBytes
	}
	return limits
}

func runChunkEncoding(job mdsrv.Manifest, flags *runFlags) string {
	encoding := firstNonEmpty(flags.chunkEncoding, job.Streaming.Encoding)
	if encoding == "" || encoding == "mdsrv-frame-v1" {
		return "json"
	}
	return encoding
}

func resourceLimitNote(limits mdsrv.ResourceLimits) string {
	var parts []string
	if limits.MaxAtoms > 0 {
		parts = append(parts, fmt.Sprintf("max_atoms=%d", limits.MaxAtoms))
	}
	if limits.MaxFrames > 0 {
		parts = append(parts, fmt.Sprintf("max_frames=%d", limits.MaxFrames))
	}
	if limits.MaxChunkBytes > 0 {
		parts = append(parts, fmt.Sprintf("max_chunk_bytes=%d", limits.MaxChunkBytes))
	}
	return strings.Join(parts, " ")
}

func addRunTiming(report *runReport, step string, target string, started time.Time) {
	report.Timings = append(report.Timings, runStepTiming{Step: step, Target: target, DurationMS: durationMS(started)})
}

func durationMS(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}
	return time.Since(started).Milliseconds()
}

func recordRunArtifact(report *runReport, storeRoot string, kind string, path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	artifact := runArtifact{Type: kind, Path: filepath.ToSlash(path)}
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(storeRoot, filepath.FromSlash(path))
	}
	sha, bytes, err := checksumRunArtifact(resolved)
	if err != nil {
		artifact.Error = err.Error()
	} else {
		artifact.SHA256 = sha
		artifact.Bytes = bytes
	}
	for i, existing := range report.Artifacts {
		if existing.Type == artifact.Type && existing.Path == artifact.Path {
			report.Artifacts[i] = artifact
			return
		}
	}
	report.Artifacts = append(report.Artifacts, artifact)
}

func checksumRunArtifact(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func writeRunReportFile(path string, report runReport) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
