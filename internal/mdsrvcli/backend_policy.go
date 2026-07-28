package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

func bindBackendFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "backend", "auto", "trajectory backend: auto, python, mdtraj, mdanalysis, or gromacs")
}

func frameWithPolicy(ctx context.Context, store mdsrv.Store, manifest mdsrv.Manifest, datasetID string, frameIndex int, atomSubset, backend, gromacsCommand string) (mdsrv.Frame, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "auto":
		return frameWithFallback(ctx, store, manifest, datasetID, frameIndex, atomSubset, gromacsCommand)
	case "python", "mdtraj", "mdanalysis":
		resolvedAtomSubset, err := resolveSelectionForBackend(manifest, atomSubset, backend)
		if err != nil {
			return mdsrv.Frame{}, err
		}
		return pythonBackendForPolicy(store, backend).Frame(ctx, manifest, frameIndex, resolvedAtomSubset)
	case "gromacs", "gmx":
		resolvedAtomSubset, err := resolveSelectionForBackend(manifest, atomSubset, backend)
		if err != nil {
			return mdsrv.Frame{}, err
		}
		return frameWithGromacs(ctx, store, datasetID, frameIndex, resolvedAtomSubset, gromacsCommand)
	default:
		return mdsrv.Frame{}, unsupportedBackendError(backend)
	}
}

func analyzeWithPolicy(ctx context.Context, store mdsrv.Store, manifest mdsrv.Manifest, datasetID string, request mdsrv.AnalysisRequest, backend, gromacsCommand string) (mdsrv.Trace, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "auto":
		pythonRequest, resolveErr := resolveAnalysisRequestForBackend(manifest, request, "python")
		if resolveErr != nil {
			return mdsrv.Trace{}, resolveErr
		}
		trace, err := pythonBackendForPolicy(store, "python").Analyze(ctx, manifest, pythonRequest)
		if err == nil {
			return trace, nil
		}
		gromacsRequest, resolveErr := resolveAnalysisRequestForBackend(manifest, request, "gromacs")
		if resolveErr != nil {
			return mdsrv.Trace{}, resolveErr
		}
		fallback, fallbackErr := analyzeWithGromacsFallback(ctx, store, manifest, datasetID, gromacsRequest, gromacsCommand)
		if fallbackErr != nil {
			// The fallback is wrapped with %w on purpose: GROMACS is the engine that
			// can still succeed when Python is absent, so its failure is the
			// actionable one and its classification should win. (Flattening both to
			// text was tried and is worse: with neither engine usable the composite
			// matches no pattern and lands in internal_error, telling the caller to
			// report a bug about a backend they simply have not installed.)
			return mdsrv.Trace{}, fmt.Errorf("python backend failed: %v; GROMACS fallback failed: %w", err, fallbackErr)
		}
		return fallback, nil
	case "python", "mdtraj", "mdanalysis":
		resolvedRequest, err := resolveAnalysisRequestForBackend(manifest, request, backend)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		return pythonBackendForPolicy(store, backend).Analyze(ctx, manifest, resolvedRequest)
	case "gromacs", "gmx":
		resolvedRequest, err := resolveAnalysisRequestForBackend(manifest, request, backend)
		if err != nil {
			return mdsrv.Trace{}, err
		}
		return analyzeWithGromacsFallback(ctx, store, manifest, datasetID, resolvedRequest, gromacsCommand)
	default:
		return mdsrv.Trace{}, unsupportedBackendError(backend)
	}
}

func resolveSelectionForBackend(manifest mdsrv.Manifest, value string, backend string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return value, nil
	}
	return mdsrv.ResolveSelectionForTarget(manifest, value, selectionTargetForBackend(backend), trajectoryAtomCount(manifest))
}

func resolveAnalysisRequestForBackend(manifest mdsrv.Manifest, request mdsrv.AnalysisRequest, backend string) (mdsrv.AnalysisRequest, error) {
	target := selectionTargetForBackend(backend)
	atomCount := trajectoryAtomCount(manifest)
	var err error
	if strings.TrimSpace(request.Selection) != "" {
		request.Selection, err = mdsrv.ResolveSelectionForTarget(manifest, request.Selection, target, atomCount)
		if err != nil {
			return request, err
		}
	}
	request.Selections, err = mdsrv.ResolveSelectionMapForTarget(manifest, request.Selections, target, atomCount)
	return request, err
}

func selectionTargetForBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "gromacs", "gmx":
		return "gromacs"
	case "mdtraj":
		return "mdtraj"
	case "mdanalysis":
		return "mdanalysis"
	case "python", "", "auto":
		return "python"
	default:
		return backend
	}
}

func trajectoryAtomCount(manifest mdsrv.Manifest) int {
	if len(manifest.Inputs.Trajectories) == 0 {
		return 0
	}
	return manifest.Inputs.Trajectories[0].AtomCount
}

func frameWithGromacs(ctx context.Context, store mdsrv.Store, datasetID string, frameIndex int, atomSubset string, gromacsCommand string) (mdsrv.Frame, error) {
	if atomSubset != "" {
		return mdsrv.Frame{}, fmt.Errorf("GROMACS backend cannot return atom-subset JSON frames; use --backend python, --backend mdtraj, or --backend mdanalysis for --atom-subset, or omit --atom-subset for full-frame GROMACS extraction")
	}
	tmp, err := os.CreateTemp("", fmt.Sprintf("mdsrv-frame-%s-%d-*.gro", datasetID, frameIndex))
	if err != nil {
		return mdsrv.Frame{}, err
	}
	outputPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(outputPath)
	if _, err := store.ExtractFrame(ctx, datasetID, frameIndex, outputPath, gromacsCommand); err != nil {
		return mdsrv.Frame{}, err
	}
	return mdsrv.ParseGROFrame(outputPath, frameIndex)
}

func trajectoryInfoWithPolicy(ctx context.Context, store mdsrv.Store, manifest mdsrv.Manifest, datasetID, backend, gromacsCommand string) (mdsrv.TrajectoryInfo, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "auto":
		info, err := pythonBackendForPolicy(store, "python").Info(ctx, manifest)
		if err == nil {
			return info, nil
		}
		fallback, fallbackErr := trajectoryInfoFromGromacs(ctx, store, manifest, datasetID, gromacsCommand)
		if fallbackErr != nil {
			return mdsrv.TrajectoryInfo{}, fmt.Errorf("python backend failed: %v; GROMACS fallback failed: %w", err, fallbackErr)
		}
		return fallback, nil
	case "python", "mdtraj", "mdanalysis":
		return pythonBackendForPolicy(store, backend).Info(ctx, manifest)
	case "gromacs", "gmx":
		return trajectoryInfoFromGromacs(ctx, store, manifest, datasetID, gromacsCommand)
	default:
		return mdsrv.TrajectoryInfo{}, unsupportedBackendError(backend)
	}
}

func unsupportedBackendError(backend string) error {
	return fmt.Errorf("unsupported backend %q; expected auto, python, mdtraj, mdanalysis, or gromacs", backend)
}

func pythonBackendForPolicy(store mdsrv.Store, backend string) mdsrv.Backend {
	python := mdsrv.NewBackend(store)
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "mdtraj":
		python.Preferred = "mdtraj"
	case "mdanalysis":
		python.Preferred = "mdanalysis"
	}
	return python
}

func trajectoryInfoFromGromacs(ctx context.Context, store mdsrv.Store, manifest mdsrv.Manifest, datasetID, gromacsCommand string) (mdsrv.TrajectoryInfo, error) {
	if len(manifest.Inputs.Trajectories) == 0 {
		return mdsrv.TrajectoryInfo{}, fmt.Errorf("dataset has no trajectory")
	}
	probe := mdsrv.ProbeFromFileRef(manifest.Inputs.Trajectories[0])
	if probe.FrameCount == 0 {
		probed, err := store.ProbeManifest(ctx, manifest, gromacsCommand)
		if err != nil {
			return mdsrv.TrajectoryInfo{}, err
		}
		manifest = probed
		probe = mdsrv.ProbeFromFileRef(manifest.Inputs.Trajectories[0])
	}
	return mdsrv.TrajectoryInfo{
		Backend:        "gromacs",
		Frames:         probe.FrameCount,
		Atoms:          probe.AtomCount,
		TopologyAtoms:  probe.AtomCount,
		TimeUnit:       firstNonEmpty(manifest.Inputs.Trajectories[0].TimeUnit, "ps"),
		CoordinateUnit: firstNonEmpty(manifest.Inputs.Trajectories[0].CoordUnit, "nm"),
		FirstTime:      probe.TimeStart,
		LastTime:       probe.TimeEnd,
		HasUnitCell:    true,
	}, nil
}
