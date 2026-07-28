package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

type versionFlags struct {
	jsonReport bool
}

type mdsrvVersionReport struct {
	OK              bool                  `json:"ok"`
	CLI             mdsrvCLIVersionReport `json:"cli"`
	Service         string                `json:"service"`
	ManifestVersion string                `json:"manifest_version"`
}

type mdsrvCLIVersionReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit,omitempty"`
	Date       string `json:"date,omitempty"`
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	Executable string `json:"executable,omitempty"`
	Module     string `json:"module,omitempty"`
}

type mdsrvCapabilitiesFlags struct {
	store      string
	gmxCommand string
	jsonReport bool
}

type mdsrvCapabilitiesReport struct {
	OK              bool                      `json:"ok"`
	Service         string                    `json:"service"`
	ManifestVersion string                    `json:"manifest_version"`
	Runtime         mdsrvCapabilitiesRuntime  `json:"runtime"`
	Backends        mdsrvCapabilitiesBackends `json:"backends"`
	Features        mdsrvCapabilitiesFeatures `json:"features"`
}

type mdsrvCapabilitiesRuntime struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	Executable string `json:"executable,omitempty"`
	Store      string `json:"store,omitempty"`
}

type mdsrvCapabilitiesBackends struct {
	Gromacs    backendCapability `json:"gromacs"`
	Python     backendCapability `json:"python"`
	MDTraj     backendCapability `json:"mdtraj"`
	MDAnalysis backendCapability `json:"mdanalysis"`
}

type backendCapability struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message,omitempty"`
	Command   string `json:"command,omitempty"`
	Source    string `json:"source,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

type mdsrvCapabilitiesFeatures struct {
	Datasets          bool     `json:"datasets"`
	DatasetWrites     bool     `json:"dataset_writes"`
	FrameDecoding     bool     `json:"frame_decoding"`
	FrameIndex        bool     `json:"frame_index"`
	FrameRanges       bool     `json:"frame_ranges"`
	GromacsExtraction bool     `json:"gromacs_extraction"`
	Analysis          bool     `json:"analysis"`
	Selections        bool     `json:"selections"`
	Sessions          bool     `json:"sessions"`
	ChunkEncodings    []string `json:"chunk_encodings"`
	StreamingBaseline []string `json:"streaming_baseline"`
}

func (a app) versionCommand() *cobra.Command {
	flags := &versionFlags{}
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Report MDsrv headless CLI build provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("version does not accept positional arguments")
			}
			report := buildMDSrvVersionReport()
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "hlmdsrv %s\n", report.CLI.Version)
			fmt.Fprintf(a.stdout, "manifest %s\n", report.ManifestVersion)
			fmt.Fprintf(a.stdout, "go %s %s/%s\n", report.CLI.GoVersion, report.CLI.GOOS, report.CLI.GOARCH)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) capabilitiesCommand() *cobra.Command {
	flags := &mdsrvCapabilitiesFlags{}
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Report MDsrv headless backend and feature capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("capabilities does not accept positional arguments")
			}
			report := buildMDSrvCapabilitiesReport(cmd.Context(), flags)
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "datasets\t%t\n", report.Features.Datasets)
			fmt.Fprintf(a.stdout, "frame_decoding\t%t\n", report.Features.FrameDecoding)
			fmt.Fprintf(a.stdout, "gromacs\t%t", report.Backends.Gromacs.Available)
			if report.Backends.Gromacs.Version != "" {
				fmt.Fprintf(a.stdout, "\t%s", report.Backends.Gromacs.Version)
			}
			fmt.Fprintln(a.stdout)
			fmt.Fprintf(a.stdout, "python\t%t\n", report.Backends.Python.Available)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root used for backend checks")
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func buildMDSrvVersionReport() mdsrvVersionReport {
	executable, _ := os.Executable()
	cli := mdsrvCLIVersionReport{
		Version:    buildVersion,
		Commit:     buildCommit,
		Date:       buildDate,
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		Executable: executable,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		cli.Module = info.Main.Path
		if cli.Version == "" || cli.Version == "dev" {
			cli.Version = info.Main.Version
			if cli.Version == "(devel)" {
				cli.Version = "dev"
			}
		}
		if cli.Commit == "" || cli.Date == "" {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if cli.Commit == "" {
						cli.Commit = setting.Value
					}
				case "vcs.time":
					if cli.Date == "" {
						cli.Date = setting.Value
					}
				}
			}
		}
	}
	if strings.TrimSpace(cli.Version) == "" {
		cli.Version = "dev"
	}
	return mdsrvVersionReport{
		OK:              true,
		CLI:             cli,
		Service:         "hlmdsrv",
		ManifestVersion: mdsrv.ManifestVersion,
	}
}

func buildMDSrvCapabilitiesReport(ctx context.Context, flags *mdsrvCapabilitiesFlags) mdsrvCapabilitiesReport {
	store, storeErr := mdsrv.OpenStore(flags.store)
	executable, _ := os.Executable()
	gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
	gromacsReport := gmx.Check(ctx)
	gromacsCapability := backendCapability{
		Available: gromacsReport.Available,
		Version:   gromacsReport.Version,
		Message:   gromacsReport.Error,
		Command:   gromacsReport.CommandString,
		Source:    gromacsReport.Source,
		Hint:      gromacsReport.Hint,
	}

	var backendDoctor mdsrv.BackendDoctor
	var pythonMessage string
	if storeErr == nil {
		doctor, err := mdsrv.NewBackend(store).Doctor(ctx)
		if err == nil {
			backendDoctor = doctor
		} else {
			pythonMessage = err.Error()
		}
	} else {
		pythonMessage = storeErr.Error()
	}
	pythonAvailable := backendDoctor.MDTraj || backendDoctor.MDAnalysis
	frameDecoding := pythonAvailable || gromacsCapability.Available
	report := mdsrvCapabilitiesReport{
		OK:              true,
		Service:         "hlmdsrv",
		ManifestVersion: mdsrv.ManifestVersion,
		Runtime: mdsrvCapabilitiesRuntime{
			GOOS:       runtime.GOOS,
			GOARCH:     runtime.GOARCH,
			Executable: executable,
			Store:      store.Root,
		},
		Backends: mdsrvCapabilitiesBackends{
			Gromacs: gromacsCapability,
			Python: backendCapability{
				Available: pythonAvailable,
				Message:   pythonMessage,
			},
			MDTraj: backendCapability{
				Available: backendDoctor.MDTraj,
			},
			MDAnalysis: backendCapability{
				Available: backendDoctor.MDAnalysis,
			},
		},
		Features: mdsrvCapabilitiesFeatures{
			Datasets:          true,
			DatasetWrites:     true,
			FrameDecoding:     frameDecoding,
			FrameIndex:        gromacsCapability.Available,
			FrameRanges:       frameDecoding,
			GromacsExtraction: gromacsCapability.Available,
			Analysis:          frameDecoding,
			Selections:        true,
			Sessions:          true,
			ChunkEncodings:    []string{"json", "bin", "bin-zstd"},
			StreamingBaseline: []string{"xtc"},
		},
	}
	return report
}
