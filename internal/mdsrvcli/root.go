package mdsrvcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type app struct {
	stdout io.Writer
	stderr io.Writer
}

func Execute(ctx context.Context, args []string) error {
	a := app{stdout: os.Stdout, stderr: os.Stderr}
	root, cleanup := a.rootCommandWithCleanup()
	// cleanup cancels any pending --timeout context; run it on every exit path
	// because cobra skips PersistentPostRun when a command returns an error.
	defer cleanup()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		coded := ClassifyError(err)
		fmt.Fprintf(a.stderr, "%s: %s\n", coded.Code, coded.Error())
		return coded
	}
	return nil
}

func (a app) rootCommand() *cobra.Command {
	cmd, _ := a.rootCommandWithCleanup()
	return cmd
}

func (a app) rootCommandWithCleanup() (*cobra.Command, func()) {
	var profileName string
	var timeout time.Duration
	var timeoutCancel context.CancelFunc
	cmd := &cobra.Command{
		Use:           "hlmdsrv",
		Short:         "Headless MDsrv dataset and session manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if commandTopLevelName(cmd) != "config" {
				profile, ok, err := loadNamedProfile(firstNonEmpty(profileName, os.Getenv("MDSRV_PROFILE")), "")
				if err != nil {
					return err
				}
				if ok {
					applyProfileValues(cmd, profile)
					if timeout == 0 && profile.Timeout > 0 {
						timeout = profile.Timeout
					}
				}
			}
			if timeout > 0 {
				if timeoutCancel != nil {
					timeoutCancel()
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
				timeoutCancel = cancel
				cmd.SetContext(ctx)
			}
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if timeoutCancel != nil {
				timeoutCancel()
				timeoutCancel = nil
			}
		},
	}
	cmd.PersistentFlags().StringVar(&profileName, "profile", "", "load defaults from a named config profile or MDSRV_PROFILE")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 0, "overall command timeout, for example 5m; profile timeout is used when unset")
	cmd.AddCommand(a.doctorCommand())
	cmd.AddCommand(a.initCommand())
	cmd.AddCommand(a.storeCommand())
	cmd.AddCommand(a.configCommand())
	cmd.AddCommand(a.completionCommand())
	cmd.AddCommand(a.docsCommand())
	cmd.AddCommand(a.versionCommand())
	cmd.AddCommand(a.capabilitiesCommand())
	cmd.AddCommand(a.explainCommand())
	cmd.AddCommand(a.quickstartCommand())
	cmd.AddCommand(a.selfTestCommand())
	cmd.AddCommand(a.benchCommand())
	cmd.AddCommand(a.datasetCommand())
	cmd.AddCommand(a.inspectCommand())
	cmd.AddCommand(a.gromacsCommand())
	cmd.AddCommand(a.runCommand())
	cmd.AddCommand(a.ingestCommand())
	cmd.AddCommand(a.listCommand())
	cmd.AddCommand(a.probeCommand())
	cmd.AddCommand(a.framesCommand())
	cmd.AddCommand(a.demoCommand())
	cmd.AddCommand(a.fixturesCommand())
	cmd.AddCommand(a.validateCommand())
	cmd.AddCommand(a.frameCommand())
	cmd.AddCommand(a.analyzeCommand())
	cmd.AddCommand(a.exportCommand())
	cmd.AddCommand(a.selectionCommand())
	cmd.AddCommand(a.indexCommand())
	cmd.AddCommand(a.visualizeCommand())
	cmd.AddCommand(a.sessionCommand())
	cmd.AddCommand(a.batchCommand())
	cmd.AddCommand(a.jobsCommand())
	cmd.AddCommand(a.packCommand())
	cmd.AddCommand(a.unpackCommand())
	cmd.AddCommand(a.compatCommand())
	cmd.AddCommand(a.gcCommand())
	cmd.AddCommand(a.schemaCommand())
	cmd.AddCommand(a.installCommand())
	cmd.AddCommand(a.publishCommand())
	cmd.AddCommand(a.debugCommand())
	cmd.AddCommand(a.serveCommand())
	cleanup := func() {
		if timeoutCancel != nil {
			timeoutCancel()
			timeoutCancel = nil
		}
	}
	return cmd, cleanup
}

func commandTopLevelName(cmd *cobra.Command) string {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	if fields := strings.Fields(cmd.Use); len(fields) > 0 {
		return fields[0]
	}
	return cmd.Name()
}
