package mdsrvcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type app struct {
	stdout io.Writer
	stderr io.Writer
}

// errorEnvelope is the machine-readable failure payload written to stdout when
// --json is requested. It mirrors the success reports' shape (a top-level "ok")
// so a consumer can branch on one field regardless of outcome.
type errorEnvelope struct {
	OK        bool              `json:"ok"`
	Command   string            `json:"command"`
	Error     errorEnvelopeBody `json:"error"`
	Timestamp string            `json:"timestamp"`
}

type errorEnvelopeBody struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code"`
}

func Execute(ctx context.Context, args []string) error {
	return app{stdout: os.Stdout, stderr: os.Stderr}.execute(ctx, args)
}

// execute carries the whole of Execute's behavior with injectable writers so the
// error-envelope and exit-code contract can be tested without spawning a process.
func (a app) execute(ctx context.Context, args []string) error {
	root, cleanup, deadlineExceeded := a.rootCommandWithDeadline()
	// cleanup cancels any pending --timeout context; run it on every exit path
	// because cobra skips PersistentPostRun when a command returns an error.
	defer cleanup()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	// Cancel the context on an operator SIGINT/SIGTERM so an in-flight run tears
	// down gracefully: exec.CommandContext kills the GROMACS/python child, the
	// HTTP server stops accepting, deferred cleanups fire, and the failure is
	// reported as a cancellation (exit 130) with a --json envelope instead of the
	// process dying abruptly. Nothing else wires signals to the context, so this
	// is what makes the documented canceled/130 contract reachable at all.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := root.ExecuteContext(ctx)
	// The cancellation check precedes the success return on purpose. A long-running
	// command that shuts down gracefully on SIGINT (`serve`) returns nil, so keying
	// only off a non-nil error would report exit 0 and leave the documented
	// canceled/130 row unreachable for exactly the command an operator is most
	// likely to Ctrl-C. If the operator ended the run, that is the outcome to
	// report, whether or not the leaf considered its own teardown successful.
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(ctxErr, context.Canceled) {
		coded := &CLIError{Code: codeCanceled, Message: "canceled", Err: err}
		if wantsJSON(args) {
			a.writeErrorEnvelope(commandNameForArgs(root, args), coded)
			return coded
		}
		fmt.Fprintf(a.stderr, "%s: %s\n", coded.Code, coded.Error())
		return coded
	}
	if err == nil {
		return nil
	}
	// Reclassify any failure that coincided with a blown --timeout deadline: the
	// leaf reports whatever it hit first (a killed child says "signal: killed"),
	// which would otherwise be labelled a backend or internal error rather than
	// the timeout the caller actually configured.
	if deadlineExceeded() {
		err = &CLIError{Code: codeBackendTimeout, Message: "command timeout exceeded", Err: err}
	}
	// A child killed by exec.CommandContext reports "signal: killed", not
	// context.Canceled, so classification would otherwise mislabel an operator
	// cancel as a backend failure.
	if killedByInterrupt(err) {
		// Race: the signal reached the child directly (it shares our process group)
		// and the leaf returned its "signal: interrupt" error before NotifyContext's
		// async cancel updated ctx.Err(). The child's death signal is ground truth.
		err = &CLIError{Code: codeCanceled, Message: "canceled", Err: err}
	} else if isUsageError(err) {
		// Arg-count, unknown-flag and unknown-subcommand failures reach here untyped;
		// they are a bad invocation the caller can fix, so classify them as
		// invalid_input (exit 2) rather than internal_error (exit 1), which the
		// error table reserves for "unclassified, report it".
		err = &CLIError{Code: codeInvalidInput, Message: err.Error(), Err: err}
	}
	coded := ClassifyError(err)
	if wantsJSON(args) {
		a.writeErrorEnvelope(commandNameForArgs(root, args), coded)
		return coded
	}
	fmt.Fprintf(a.stderr, "%s: %s\n", coded.Code, coded.Error())
	return coded
}

func (a app) writeErrorEnvelope(command string, coded *CLIError) {
	exitCode := ExitCode(coded)
	if exitCode == 0 {
		exitCode = 1
	}
	encoder := json.NewEncoder(a.stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(errorEnvelope{
		OK:      false,
		Command: command,
		Error: errorEnvelopeBody{
			Code:     string(coded.Code),
			Message:  coded.Error(),
			ExitCode: exitCode,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func commandNameForArgs(root *cobra.Command, args []string) string {
	command, _, err := root.Find(args)
	if err != nil || command == nil {
		return root.Name()
	}
	path := command.CommandPath()
	rootPath := root.CommandPath()
	if path == rootPath {
		return rootPath
	}
	return strings.TrimPrefix(path, rootPath+" ")
}

// isUsageError reports whether err is one of cobra's stable command/argument
// validation failures (as opposed to an error returned from a command's RunE).
func isUsageError(err error) bool {
	msg := err.Error()
	for _, p := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"invalid argument",
		"flag needs an argument",
		"accepts ", // e.g. "accepts 1 arg(s), received 0"
		"requires at least",
		"requires at most",
		"accepts between",
		"required flag",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// killedByInterrupt reports whether err's chain carries an *exec.ExitError for a
// child killed by SIGINT or SIGTERM — the operator-cancel signals.
func killedByInterrupt(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return false
	}
	switch ws.Signal() {
	case syscall.SIGINT, syscall.SIGTERM:
		return true
	default:
		return false
	}
}

// wantsJSON reports whether --json is effectively enabled, honoring pflag's
// last-wins semantics so the error envelope is emitted exactly when the
// command's own --json flag would have parsed true. Args are scanned rather than
// the parsed flag read, because a flag-parse failure aborts before any flag is
// available and that is precisely a case needing a machine-readable envelope.
func wantsJSON(args []string) bool {
	enabled := false
	for _, arg := range args {
		if arg == "--json" {
			enabled = true
		} else if strings.HasPrefix(arg, "--json=") {
			value := strings.TrimPrefix(arg, "--json=")
			enabled = value != "false" && value != "0"
		}
	}
	return enabled
}

func (a app) rootCommand() *cobra.Command {
	cmd, _ := a.rootCommandWithCleanup()
	return cmd
}

func (a app) rootCommandWithCleanup() (*cobra.Command, func()) {
	cmd, cleanup, _ := a.rootCommandWithDeadline()
	return cmd, cleanup
}

// rootCommandWithDeadline additionally returns a predicate reporting whether the
// --timeout context expired, so Execute can surface a blown deadline that the
// command itself never noticed.
func (a app) rootCommandWithDeadline() (*cobra.Command, func(), func() bool) {
	var profileName string
	var timeout time.Duration
	var timeoutCancel context.CancelFunc
	var timeoutCtx context.Context
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
				timeoutCtx = ctx
				cmd.SetContext(ctx)
				// Fail before the command runs if the budget is already spent. The
				// pure-Go paths (index build, validate, inspect, frames count, store
				// doctor) never consult the context, so without this an exhausted
				// deadline produced a full success report at exit 0. Checking here
				// rather than after execution also keeps stdout to a single document:
				// reporting a timeout afterwards would append an error envelope to a
				// report the command had already written.
				if ctxErr := ctx.Err(); ctxErr != nil {
					return &CLIError{Code: codeBackendTimeout, Message: "command timeout exceeded before the command started", Err: ctxErr}
				}
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
	// Sampled before cleanup() cancels the context, so a deliberate cancel is not
	// mistaken for an expired deadline.
	deadlineExceeded := func() bool {
		return timeoutCtx != nil && errors.Is(timeoutCtx.Err(), context.DeadlineExceeded)
	}
	cleanup := func() {
		if timeoutCancel != nil {
			timeoutCancel()
			timeoutCancel = nil
		}
	}
	return cmd, cleanup, deadlineExceeded
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
