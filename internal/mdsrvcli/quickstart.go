package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type quickstartFlags struct {
	out        string
	id         string
	frames     int
	backend    string
	gmxCommand string
	force      bool
	jsonReport bool
}

type quickstartReport struct {
	ID           string                    `json:"id"`
	Root         string                    `json:"root"`
	Raw          string                    `json:"raw"`
	Store        string                    `json:"store"`
	Static       string                    `json:"static"`
	Job          string                    `json:"job"`
	RunReport    string                    `json:"run_report"`
	Archive      string                    `json:"archive,omitempty"`
	Demo         demoReport                `json:"demo"`
	Run          runReport                 `json:"run"`
	Publish      mdsrv.StaticPublishReport `json:"publish"`
	ServeSmoke   serveSmokeReport          `json:"serve_smoke"`
	NextCommands []string                  `json:"next_commands"`
}

func (a app) quickstartCommand() *cobra.Command {
	flags := &quickstartFlags{
		out:        filepath.Join("outputs", "mdsrv-quickstart"),
		id:         "quickstart",
		frames:     5,
		backend:    "gromacs",
		force:      true,
		jsonReport: true,
	}
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Create, run, publish, and summarize a tiny MDsrv dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("quickstart does not accept positional arguments")
			}
			report, err := a.runQuickstart(cmd.Context(), flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "dataset %s\n", report.ID)
			fmt.Fprintf(a.stdout, "job %s\n", report.Job)
			fmt.Fprintf(a.stdout, "store %s\n", report.Store)
			fmt.Fprintf(a.stdout, "static %s\n", report.Static)
			for _, command := range report.NextCommands {
				fmt.Fprintf(a.stdout, "next %s\n", command)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.out, "out", flags.out, "quickstart output directory")
	cmd.Flags().StringVar(&flags.id, "id", flags.id, "dataset id")
	cmd.Flags().IntVar(&flags.frames, "frames", flags.frames, "number of demo trajectory frames")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.force, "force", flags.force, "overwrite quickstart artifacts")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", flags.jsonReport, "write machine-readable output")
	return cmd
}

func (a app) runQuickstart(ctx context.Context, flags *quickstartFlags) (quickstartReport, error) {
	if strings.TrimSpace(flags.id) == "" {
		return quickstartReport{}, fmt.Errorf("--id is required")
	}
	if err := mdsrv.ValidateID(flags.id); err != nil {
		return quickstartReport{}, fmt.Errorf("id: %w", err)
	}
	if flags.frames < 2 {
		return quickstartReport{}, fmt.Errorf("--frames must be at least 2")
	}
	root, err := filepath.Abs(flags.out)
	if err != nil {
		return quickstartReport{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return quickstartReport{}, err
	}
	rawDir := filepath.Join(root, "raw")
	storeDir := filepath.Join(root, "store")
	staticDir := filepath.Join(root, "static")
	jobPath := filepath.Join(root, "job.yaml")
	runReportPath := filepath.Join(root, "run-report.json")
	archivePath := filepath.Join(root, flags.id+".mdsrvx")

	demo, err := a.runDemoGromacs(ctx, &demoFlags{
		out:        rawDir,
		id:         flags.id,
		name:       "MDsrv quickstart trajectory",
		frames:     flags.frames,
		gmxCommand: flags.gmxCommand,
		force:      flags.force,
	})
	if err != nil {
		return quickstartReport{}, err
	}
	jobManifest := quickstartJobManifest(flags.id)
	if err := mdsrv.WriteManifestFile(jobPath, jobManifest); err != nil {
		return quickstartReport{}, err
	}
	run, err := a.runJob(ctx, jobPath, &runFlags{
		store:         storeDir,
		backend:       flags.backend,
		gmxCommand:    flags.gmxCommand,
		force:         flags.force,
		probe:         true,
		index:         true,
		chunks:        true,
		chunkEncoding: "json",
		jsonReport:    true,
	})
	if err != nil {
		return quickstartReport{}, err
	}
	if err := writeRunReportFile(runReportPath, run); err != nil {
		return quickstartReport{}, err
	}
	store, err := mdsrv.OpenStore(storeDir)
	if err != nil {
		return quickstartReport{}, err
	}
	published, err := store.PublishStatic(staticDir, flags.force)
	if err != nil {
		return quickstartReport{}, err
	}
	verification, err := mdsrv.VerifyStaticPublish(published.Out)
	if err != nil {
		return quickstartReport{}, err
	}
	published.Verification = &verification
	if !verification.OK {
		return quickstartReport{}, codedErrorf(codeValidationFailed, "quickstart static publish verification failed")
	}
	smoke, err := a.runServeSmoke(ctx, store, &serveFlags{
		store:      storeDir,
		backend:    flags.backend,
		gmxCommand: flags.gmxCommand,
		readOnly:   true,
	})
	if err != nil {
		return quickstartReport{}, err
	}
	report := quickstartReport{
		ID:         flags.id,
		Root:       root,
		Raw:        rawDir,
		Store:      store.Root,
		Static:     published.Out,
		Job:        jobPath,
		RunReport:  runReportPath,
		Archive:    archivePath,
		Demo:       demo,
		Run:        run,
		Publish:    published,
		ServeSmoke: smoke,
	}
	report.NextCommands = []string{
		"hlmdsrv serve --store " + shellArg(report.Store) + " --read-only",
		"hlmdsrv serve smoke --store " + shellArg(report.Store) + " --read-only",
		"hlmdsrv frames get " + shellArg(report.ID) + " 0 --store " + shellArg(report.Store) + " --format json",
		"hlmdsrv publish static --store " + shellArg(report.Store) + " --out " + shellArg(report.Static) + " --force --verify",
	}
	return report, nil
}

func quickstartJobManifest(id string) mdsrv.Manifest {
	return mdsrv.Manifest{
		Version: mdsrv.ManifestVersion,
		Metadata: mdsrv.Metadata{
			ID:          id,
			Name:        "MDsrv quickstart trajectory",
			Description: "Tiny synthetic trajectory generated by hlmdsrv quickstart.",
			CreatedBy:   "hlmdsrv quickstart",
		},
		Inputs: mdsrv.Inputs{
			Topology: mdsrv.FileRef{Path: filepath.ToSlash(filepath.Join("raw", "structure.gro")), Format: "gro"},
			Trajectories: []mdsrv.FileRef{{
				Path:      filepath.ToSlash(filepath.Join("raw", "trajectory.xtc")),
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
			MVS: mdsrv.MVSVisualization{Scene: filepath.ToSlash(filepath.Join("visualization", id+".mvsj"))},
			Camera: mdsrv.Camera{
				Focus: "first-two",
			},
		},
		Outputs: []mdsrv.Output{{
			Type: "mdsrvx",
			Path: id + ".mdsrvx",
		}},
	}
}

func shellArg(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '=' || r == '+' || r == ',' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
