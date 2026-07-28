package mdsrvcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type explainFlags struct {
	store      string
	backend    string
	cache      string
	gmxCommand string
	strict     bool
	jsonReport bool
}

type explainReport struct {
	ID         string            `json:"id"`
	Version    string            `json:"version,omitempty"`
	Job        string            `json:"job,omitempty"`
	JobRoot    string            `json:"job_root,omitempty"`
	Store      string            `json:"store,omitempty"`
	Backend    string            `json:"backend,omitempty"`
	Cache      string            `json:"cache,omitempty"`
	Inputs     explainInputs     `json:"inputs"`
	Streaming  mdsrv.Streaming   `json:"streaming,omitempty"`
	Runtime    mdsrv.Runtime     `json:"runtime,omitempty"`
	Selections []mdsrv.Selection `json:"selections,omitempty"`
	Analyses   []mdsrv.Analysis  `json:"analyses,omitempty"`
	Outputs    []explainOutput   `json:"outputs,omitempty"`
	Plan       []runPlanStep     `json:"plan"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type explainInputs struct {
	Topology     explainInput   `json:"topology"`
	Trajectories []explainInput `json:"trajectories,omitempty"`
}

type explainInput struct {
	Path           string `json:"path,omitempty"`
	URL            string `json:"url,omitempty"`
	Format         string `json:"format,omitempty"`
	InferredFormat string `json:"inferred_format,omitempty"`
	Resolved       string `json:"resolved,omitempty"`
	Exists         bool   `json:"exists,omitempty"`
	Error          string `json:"error,omitempty"`
}

type explainOutput struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	Resolved string `json:"resolved,omitempty"`
	Exists   bool   `json:"exists,omitempty"`
}

func (a app) explainCommand() *cobra.Command {
	flags := &explainFlags{}
	cmd := &cobra.Command{
		Use:   "explain TOPIC_OR_JOB",
		Short: "Explain a concept or resolve a job manifest plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.jsonReport || looksLikeManifestPath(args[0]) {
				report, err := a.explainJob(args[0], flags)
				if err != nil {
					return err
				}
				if flags.jsonReport {
					return writeJSON(a.stdout, report)
				}
				fmt.Fprintf(a.stdout, "%s\n", report.ID)
				for _, step := range report.Plan {
					fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", step.Action, firstNonEmpty(step.Target, step.Output), step.Note)
				}
				return nil
			}
			topic := strings.ToLower(strings.TrimSpace(args[0]))
			text, ok := explainTopic(topic)
			if !ok {
				return fmt.Errorf("unknown explain topic %q; try job, chunks, selection, backend, visualize, serve, or release", args[0])
			}
			fmt.Fprintln(a.stdout, text)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root used for planned outputs")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.cache, "cache", "", "download cache directory override")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override used by planned backend steps")
	cmd.Flags().BoolVar(&flags.strict, "strict", false, "fail when the explained job has missing inputs")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) explainJob(path string, flags *explainFlags) (explainReport, error) {
	if err := validateMDSrvJobSchemaFile(path); err != nil {
		return explainReport{}, err
	}
	manifest, err := mdsrv.LoadManifestFile(path)
	if err != nil {
		return explainReport{}, err
	}
	plan, err := a.planJob(path, &runFlags{
		store:      flags.store,
		backend:    flags.backend,
		cache:      flags.cache,
		gmxCommand: flags.gmxCommand,
		probe:      true,
		index:      true,
	})
	if err != nil {
		return explainReport{}, err
	}
	jobDir := filepath.Dir(path)
	jobRoot, err := filepath.Abs(jobDir)
	if err != nil {
		return explainReport{}, err
	}
	report := explainReport{
		ID:         manifest.Metadata.ID,
		Version:    manifest.Version,
		Job:        path,
		JobRoot:    jobRoot,
		Store:      plan.Store,
		Backend:    firstNonEmpty(flags.backend, "auto"),
		Cache:      firstNonEmpty(flags.cache, manifest.Streaming.Cache),
		Streaming:  manifest.Streaming,
		Runtime:    manifest.Runtime,
		Selections: manifest.Selections,
		Analyses:   manifest.Analyses,
		Inputs: explainInputs{
			Topology: explainFileRef(jobDir, manifest.Inputs.Topology),
		},
		Plan: plan.Steps,
	}
	for _, trajectory := range manifest.Inputs.Trajectories {
		report.Inputs.Trajectories = append(report.Inputs.Trajectories, explainFileRef(jobDir, trajectory))
	}
	for _, input := range append([]explainInput{report.Inputs.Topology}, report.Inputs.Trajectories...) {
		if input.Resolved != "" && !input.Exists && input.URL == "" {
			report.Warnings = append(report.Warnings, "missing input: "+input.Resolved)
		}
		if input.Error != "" {
			report.Warnings = append(report.Warnings, input.Error+": "+input.Resolved)
		}
	}
	for _, output := range manifest.Outputs {
		item := explainOutput{Type: output.Type, Path: output.Path}
		if output.Path != "" && !strings.Contains(output.Path, "://") {
			item.Resolved = resolveJobFile(jobDir, output.Path)
			if _, err := os.Stat(item.Resolved); err == nil {
				item.Exists = true
				report.Warnings = append(report.Warnings, "output already exists: "+item.Resolved)
			}
		}
		report.Outputs = append(report.Outputs, item)
	}
	if flags.strict && len(report.Warnings) > 0 {
		return report, codedErrorf(codeValidationFailed, "explain strict checks failed: %s", strings.Join(report.Warnings, "; "))
	}
	return report, nil
}

func explainFileRef(jobDir string, ref mdsrv.FileRef) explainInput {
	resolved := ref.Path
	if resolved != "" {
		resolved = resolveJobFile(jobDir, resolved)
	}
	explained := explainInput{
		Path:           ref.Path,
		URL:            ref.URL,
		Format:         ref.Format,
		InferredFormat: mdsrv.NormalizeFormat(mdsrv.InferFormat(firstNonEmpty(ref.Path, ref.URL))),
		Resolved:       resolved,
	}
	if resolved != "" {
		if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
			explained.Exists = true
		} else if err == nil {
			explained.Error = "is a directory"
		} else {
			explained.Error = err.Error()
		}
	}
	return explained
}

func explainTopic(topic string) (string, bool) {
	switch topic {
	case "job", "manifest", "run":
		return "A job is an mdsrv.job/v1 manifest. `run` ingests inputs, saves selections, probes metadata, builds indexes or chunks, runs analyses, writes visualization artifacts, publishes sessions, and packs requested archives.", true
	case "chunks", "chunk", "streaming":
		return "`index chunks` materializes frame chunks referenced by the frame index. Encodings are json, bin, and bin-zstd. Use max_atoms, max_frames, and max_chunk_bytes to keep chunk jobs bounded.", true
	case "selection", "selections":
		return "Selections can be saved in the dataset manifest and reused by frame, analyze, export, and visualize commands. Atom-index selections are 1-based in the CLI and converted for GROMACS, mdtraj, MDAnalysis, or MVS when needed.", true
	case "backend", "backends":
		return "`--backend auto` tries Python then GROMACS fallback. `--backend mdtraj` and `--backend mdanalysis` force a specific Python package. `--backend gromacs` uses GROMACS extraction and fallback analyses.", true
	case "visualize", "mvs":
		return "`visualize` writes a static MVS scene. Use `--frame N` to extract a trajectory snapshot first, and `--include-selections` or `--selection` to layer named selections as separate components.", true
	case "serve", "server":
		return "`serve` exposes datasets, frames, chunks, selections, analyses, and sessions over HTTP. Harden it with --read-only, auth token, allowlists, request timeout, frame range, atom, frame, and chunk byte limits.", true
	case "release", "verify":
		return "`scripts/verify-release.sh` runs tests, vet, schema drift checks, local binary builds, archive install verification, and optional Docker smoke before a tagged GoReleaser release.", true
	default:
		return "", false
	}
}

func looksLikeManifestPath(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json")
}
