package mdsrvcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type fixtureFlags struct {
	store      string
	id         string
	force      bool
	probe      bool
	gmxCommand string
	jsonReport bool
}

func (a app) fixturesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "fixtures", Short: "Fetch or ingest known trajectory fixtures"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List known fixtures",
		RunE: func(cmd *cobra.Command, args []string) error {
			fixtures := []map[string]string{{
				"id":          "mdanalysis-adk",
				"description": "Real AdK GRO/XTC trajectory from MDAnalysisTests datafiles",
				"requires":    "python package MDAnalysisTests",
			}}
			return writeJSON(a.stdout, fixtures)
		},
	}
	flags := &fixtureFlags{id: "mdanalysis-adk", probe: true, jsonReport: true}
	mdanalysis := &cobra.Command{
		Use:   "mdanalysis-adk",
		Short: "Ingest the real AdK GRO/XTC fixture from MDAnalysisTests",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := a.runMDAnalysisFixture(cmd.Context(), flags)
			if err != nil {
				return err
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, report["id"])
			return nil
		},
	}
	mdanalysis.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	mdanalysis.Flags().StringVar(&flags.id, "id", "mdanalysis-adk", "dataset id")
	mdanalysis.Flags().BoolVar(&flags.force, "force", false, "overwrite existing fixture dataset")
	mdanalysis.Flags().BoolVar(&flags.probe, "probe", true, "probe fixture with GROMACS after ingest")
	mdanalysis.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	mdanalysis.Flags().BoolVar(&flags.jsonReport, "json", true, "write machine-readable output")
	cmd.AddCommand(list, mdanalysis)
	return cmd
}

func (a app) runMDAnalysisFixture(ctx context.Context, flags *fixtureFlags) (map[string]any, error) {
	python := firstNonEmpty(os.Getenv("MDSRV_PYTHON"), "python3")
	script := `import json
from MDAnalysisTests import datafiles
print(json.dumps({"topology": datafiles.GRO, "trajectory": datafiles.XTC}))`
	command := exec.CommandContext(ctx, python, "-c", script)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := stderr.String()
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("load MDAnalysisTests fixture: %s", message)
	}
	var fixture struct {
		Topology   string `json:"topology"`
		Trajectory string `json:"trajectory"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &fixture); err != nil {
		return nil, err
	}
	store, err := mdsrv.OpenStore(flags.store)
	if err != nil {
		return nil, err
	}
	manifest, err := store.Ingest(mdsrv.IngestOptions{
		ID:          flags.id,
		Name:        "MDAnalysis AdK test trajectory",
		Description: "Real GRO/XTC fixture from MDAnalysisTests datafiles.",
		Source:      "MDAnalysisTests.datafiles.GRO/XTC",
		CreatedBy:   "hlmdsrv fixtures",
		Topology:    fixture.Topology,
		Trajectory:  fixture.Trajectory,
		Force:       flags.force,
	})
	if err != nil {
		return nil, err
	}
	if flags.probe {
		if probed, err := store.ProbeDataset(ctx, manifest.Metadata.ID, flags.gmxCommand); err == nil {
			manifest = probed
		}
	}
	result := map[string]any{
		"id":       manifest.Metadata.ID,
		"store":    store.Root,
		"topology": manifest.Inputs.Topology.Path,
	}
	if len(manifest.Inputs.Trajectories) > 0 {
		result["trajectory"] = manifest.Inputs.Trajectories[0].Path
		result["frames"] = manifest.Inputs.Trajectories[0].FrameCount
		result["atoms"] = manifest.Inputs.Trajectories[0].AtomCount
	}
	return result, nil
}
