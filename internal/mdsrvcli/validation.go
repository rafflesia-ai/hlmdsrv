package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type validationIssue struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type datasetValidationReport struct {
	mdsrv.ValidationReport
	OK         bool                  `json:"ok"`
	Trajectory *mdsrv.TrajectoryInfo `json:"trajectory,omitempty"`
	Issues     []validationIssue     `json:"issues,omitempty"`
}

func buildDatasetValidationReport(ctx context.Context, store mdsrv.Store, m mdsrv.Manifest, root string, flags *validateFlags) (datasetValidationReport, error) {
	report := store.CheckDataset(m)
	var deepInfo *mdsrv.TrajectoryInfo
	if flags.deep {
		info, err := trajectoryInfoWithPolicy(ctx, store, m, m.Metadata.ID, flags.backend, flags.gmxCommand)
		if err != nil {
			report.Warnings = append(report.Warnings, "deep validation failed: "+err.Error())
		} else {
			deepInfo = &info
		}
	}
	issues := validateManifestReferences(ctx, store, m, root, flags, deepInfo)
	return datasetValidationReport{
		ValidationReport: report,
		OK:               validationOK(report) && validationIssuesOK(issues),
		Trajectory:       deepInfo,
		Issues:           issues,
	}, nil
}

func validationIssuesOK(issues []validationIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return false
		}
	}
	return true
}

func validateManifestReferences(ctx context.Context, store mdsrv.Store, m mdsrv.Manifest, root string, flags *validateFlags, trajectory *mdsrv.TrajectoryInfo) []validationIssue {
	var issues []validationIssue
	addIssue := func(kind, severity, path, message string) {
		issues = append(issues, validationIssue{Kind: kind, Severity: severity, Path: filepath.ToSlash(path), Message: message})
	}
	optionalSeverity := "warning"
	if flags.strict {
		optionalSeverity = "error"
	}
	checkStorePath := func(kind, path string, required bool) {
		if strings.TrimSpace(path) == "" {
			return
		}
		resolved, err := store.SafeResolvePath(path)
		if err != nil {
			addIssue(kind, "error", path, err.Error())
			return
		}
		info, err := os.Stat(resolved)
		if err != nil {
			severity := optionalSeverity
			if required {
				severity = "error"
			}
			addIssue(kind, severity, path, err.Error())
			return
		}
		if info.IsDir() {
			addIssue(kind, "error", path, "is a directory")
		}
	}
	for _, analysis := range m.Analyses {
		checkStorePath("analysis_output", analysis.Output, flags.strict)
	}
	if m.Streaming.FrameIndex != "" {
		checkStorePath("frame_index", m.Streaming.FrameIndex, flags.strict)
	}
	if m.Visualization.MVS.Scene != "" {
		checkStorePath("mvs_scene", m.Visualization.MVS.Scene, flags.strict)
	}
	if m.Visualization.Molstar.State != "" {
		checkStorePath("molstar_state", m.Visualization.Molstar.State, flags.strict)
	}
	for _, session := range m.Visualization.Sessions {
		checkStorePath("session", session.Path, flags.strict)
	}
	for _, output := range m.Outputs {
		switch strings.ToLower(strings.TrimSpace(output.Type)) {
		case "manifest", "server-store":
			continue
		}
		if strings.TrimSpace(output.Path) == "" || strings.Contains(output.Path, "://") {
			continue
		}
		resolved := output.Path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(root, filepath.FromSlash(output.Path))
		}
		if relative, err := filepath.Rel(root, resolved); err == nil && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			addIssue("output", "error", output.Path, "path escapes manifest root")
			continue
		}
		if _, err := os.Stat(resolved); err == nil {
			severity := "warning"
			if flags.strict {
				severity = "error"
			}
			addIssue("output", severity, output.Path, "output already exists")
		}
	}
	if m.Inputs.Topology.AtomCount > 0 {
		for i, trajectoryRef := range m.Inputs.Trajectories {
			if trajectoryRef.AtomCount > 0 && trajectoryRef.AtomCount != m.Inputs.Topology.AtomCount {
				addIssue("compatibility", "error", fmt.Sprintf("inputs.trajectories[%d]", i), fmt.Sprintf("topology atom count %d does not match trajectory atom count %d", m.Inputs.Topology.AtomCount, trajectoryRef.AtomCount))
			}
		}
	}
	if trajectory != nil && trajectory.TopologyAtoms > 0 && trajectory.Atoms > 0 && trajectory.TopologyAtoms != trajectory.Atoms {
		addIssue("compatibility", "error", "trajectory", fmt.Sprintf("topology atom count %d does not match trajectory atom count %d", trajectory.TopologyAtoms, trajectory.Atoms))
	}
	if flags.strict {
		issues = append(issues, validateBackendAvailability(ctx, store, flags)...)
	}
	return issues
}

func validateBackendAvailability(ctx context.Context, store mdsrv.Store, flags *validateFlags) []validationIssue {
	var issues []validationIssue
	add := func(kind, message string) {
		issues = append(issues, validationIssue{Kind: kind, Severity: "error", Message: message})
	}
	backend := strings.ToLower(strings.TrimSpace(flags.backend))
	switch backend {
	case "", "auto":
		backendDoctor, backendErr := mdsrv.NewBackend(store).Doctor(ctx)
		pythonOK := backendErr == nil && (backendDoctor.MDTraj || backendDoctor.MDAnalysis)
		gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
		if !pythonOK && !gmx.Available() {
			add("backend", "neither a Python trajectory backend nor GROMACS is available")
		}
	case "gromacs", "gmx":
		gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
		if !gmx.Available() {
			add("backend", fmt.Sprintf("gromacs command %q was not found", gmx.CommandString()))
		}
	case "python", "mdtraj", "mdanalysis":
		backendDoctor, err := mdsrv.NewBackend(store).Doctor(ctx)
		if err != nil {
			add("backend", err.Error())
			return issues
		}
		if backend == "mdtraj" && !backendDoctor.MDTraj {
			add("backend", "mdtraj is not available")
		}
		if backend == "mdanalysis" && !backendDoctor.MDAnalysis {
			add("backend", "MDAnalysis is not available")
		}
		if backend == "python" && !backendDoctor.MDTraj && !backendDoctor.MDAnalysis {
			add("backend", "no Python trajectory backend is available")
		}
	default:
		add("backend", fmt.Sprintf("unsupported backend %q", flags.backend))
	}
	return issues
}
