package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type selfTestFlags struct {
	out               string
	frames            int
	backend           string
	gmxCommand        string
	runQuickstart     bool
	requireQuickstart bool
	force             bool
	jsonReport        bool
}

type selfTestCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

type selfTestReport struct {
	OK               bool              `json:"ok"`
	Root             string            `json:"root"`
	Store            string            `json:"store,omitempty"`
	Static           string            `json:"static,omitempty"`
	Gromacs          bool              `json:"gromacs_available"`
	QuickstartStatus string            `json:"quickstart_status"`
	Checks           []selfTestCheck   `json:"checks"`
	Steps            []selfTestCheck   `json:"steps,omitempty"`
	Doctor           []doctorCheck     `json:"doctor"`
	Job              string            `json:"job"`
	RunReport        string            `json:"run_report,omitempty"`
	Explain          explainReport     `json:"explain"`
	Plan             runPlanReport     `json:"plan"`
	Quickstart       *quickstartReport `json:"quickstart,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	NextCommands     []string          `json:"next_commands,omitempty"`
}

func (a app) selfTestCommand() *cobra.Command {
	flags := &selfTestFlags{
		frames:        4,
		backend:       "gromacs",
		runQuickstart: true,
		force:         true,
		jsonReport:    true,
	}
	cmd := &cobra.Command{
		Use:   "self-test",
		Short: "Run local MDsrv headless smoke checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("self-test does not accept positional arguments")
			}
			report, err := a.runSelfTest(cmd.Context(), flags)
			if flags.jsonReport {
				if writeErr := writeJSON(a.stdout, report); writeErr != nil && err == nil {
					err = writeErr
				}
			} else {
				status := "ok"
				if !report.OK {
					status = "fail"
				}
				fmt.Fprintf(a.stdout, "%s\t%s\n", status, report.Root)
				fmt.Fprintf(a.stdout, "quickstart\t%s\n", report.QuickstartStatus)
				for _, check := range report.Checks {
					if check.Skipped {
						fmt.Fprintf(a.stdout, "skip\t%s\t%s\n", check.Name, check.Message)
					} else if check.OK {
						fmt.Fprintf(a.stdout, "ok\t%s\t%s\n", check.Name, check.Path)
					} else {
						fmt.Fprintf(a.stdout, "fail\t%s\t%s\n", check.Name, check.Message)
					}
				}
				for _, command := range report.NextCommands {
					fmt.Fprintf(a.stdout, "next %s\n", command)
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&flags.out, "out", "", "self-test output directory; defaults to a temporary directory")
	cmd.Flags().StringVar(&flags.out, "out-dir", "", "self-test output directory; alias for --out")
	cmd.Flags().IntVar(&flags.frames, "frames", flags.frames, "number of quickstart trajectory frames")
	bindBackendFlag(cmd, &flags.backend)
	cmd.Flags().StringVar(&flags.gmxCommand, "gmx-command", "", "GROMACS command override")
	cmd.Flags().BoolVar(&flags.runQuickstart, "quickstart", flags.runQuickstart, "run the full GROMACS quickstart path when GROMACS is available")
	cmd.Flags().BoolVar(&flags.requireQuickstart, "require-gromacs", false, "fail when the quickstart path cannot run because GROMACS is unavailable")
	cmd.Flags().BoolVar(&flags.force, "force", flags.force, "overwrite self-test quickstart artifacts")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", flags.jsonReport, "write machine-readable output")
	return cmd
}

func (a app) runSelfTest(ctx context.Context, flags *selfTestFlags) (selfTestReport, error) {
	root, err := selfTestRoot(flags.out)
	if err != nil {
		return selfTestReport{}, err
	}
	report := selfTestReport{OK: true, Root: root, QuickstartStatus: "disabled"}
	cacheDir := filepath.Join(root, "cache")
	staticDir := filepath.Join(root, "static")
	storeDir := filepath.Join(root, "store")

	doctorFlags := &doctorFlags{
		store:      storeDir,
		cache:      cacheDir,
		staticOut:  staticDir,
		gmxCommand: flags.gmxCommand,
	}
	report.Doctor = a.runDoctor(ctx, doctorFlags)
	if !requiredDoctorChecksOK(report.Doctor) {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "doctor", OK: false, Message: "required doctor checks failed"})
		report.Steps = append(report.Steps, selfTestCheck{Name: "doctor", OK: false, Message: "required doctor checks failed"})
		return report, codedErrorf(codeValidationFailed, "self-test doctor checks failed")
	}
	report.Checks = append(report.Checks, selfTestCheck{Name: "doctor", OK: true})

	store, err := mdsrv.OpenStore(storeDir)
	if err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "open store", OK: false, Message: err.Error(), Path: storeDir})
		return report, err
	}
	if err := store.Init(); err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "init store", OK: false, Message: err.Error(), Path: storeDir})
		return report, err
	}
	report.Checks = append(report.Checks, selfTestCheck{Name: "init store", OK: true, Path: store.Root})

	jobPath := filepath.Join(root, "self-test.job.yaml")
	job := quickstartJobManifest("self-test-plan")
	if err := mdsrv.WriteManifestFile(jobPath, job); err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "write job", OK: false, Message: err.Error(), Path: jobPath})
		return report, err
	}
	report.Job = jobPath
	report.Checks = append(report.Checks, selfTestCheck{Name: "write job", OK: true, Path: jobPath})

	if err := validateMDSrvJobSchemaFile(jobPath); err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "validate schema", OK: false, Message: err.Error(), Path: jobPath})
		return report, err
	}
	report.Checks = append(report.Checks, selfTestCheck{Name: "validate schema", OK: true, Path: jobPath})

	explanation, err := a.explainJob(jobPath, &explainFlags{store: store.Root, backend: flags.backend, gmxCommand: flags.gmxCommand})
	if err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "explain", OK: false, Message: err.Error(), Path: jobPath})
		return report, err
	}
	report.Explain = explanation
	report.Checks = append(report.Checks, selfTestCheck{Name: "explain", OK: true, Path: jobPath})

	plan, err := a.planJob(jobPath, &runFlags{store: store.Root, backend: flags.backend, gmxCommand: flags.gmxCommand, probe: true, index: true})
	if err != nil {
		report.OK = false
		report.Checks = append(report.Checks, selfTestCheck{Name: "plan", OK: false, Message: err.Error(), Path: jobPath})
		return report, err
	}
	report.Plan = plan
	report.Checks = append(report.Checks, selfTestCheck{Name: "plan", OK: len(plan.Steps) > 0, Path: jobPath})
	if len(plan.Steps) == 0 {
		report.OK = false
		return report, codedErrorf(codeValidationFailed, "self-test generated an empty run plan")
	}

	if flags.runQuickstart {
		gmx := gromacs.New(gromacs.Options{Command: flags.gmxCommand})
		report.Gromacs = gmx.Available()
		if !report.Gromacs {
			message := "GROMACS is unavailable; skipping quickstart"
			report.QuickstartStatus = "skipped"
			report.Warnings = append(report.Warnings, message)
			report.Checks = append(report.Checks, selfTestCheck{Name: "quickstart", OK: true, Message: message, Skipped: true})
			if len(report.Steps) == 0 {
				report.Steps = append(report.Steps, report.Checks...)
			}
			if flags.requireQuickstart {
				report.OK = false
				return report, codedErrorf(codeMissingBackend, "GROMACS is required for self-test quickstart")
			}
		} else {
			report.QuickstartStatus = "running"
			quickstart, err := a.runQuickstart(ctx, &quickstartFlags{
				out:        filepath.Join(root, "quickstart"),
				id:         "self-test",
				frames:     flags.frames,
				backend:    flags.backend,
				gmxCommand: flags.gmxCommand,
				force:      flags.force,
				jsonReport: true,
			})
			if err != nil {
				report.OK = false
				report.QuickstartStatus = "failed"
				report.Checks = append(report.Checks, selfTestCheck{Name: "quickstart", OK: false, Message: err.Error()})
				return report, err
			}
			report.QuickstartStatus = "passed"
			report.Quickstart = &quickstart
			report.Job = quickstart.Job
			report.Store = quickstart.Store
			report.Static = quickstart.Static
			report.RunReport = quickstart.RunReport
			report.Plan.Store = quickstart.Store
			explanation, err := a.explainJob(quickstart.Job, &explainFlags{store: quickstart.Store, backend: flags.backend, gmxCommand: flags.gmxCommand})
			if err != nil {
				report.OK = false
				report.Checks = append(report.Checks, selfTestCheck{Name: "explain quickstart", OK: false, Message: err.Error(), Path: quickstart.Job})
				return report, err
			}
			report.Explain = explanation
			validationOK, validationMessage := a.selfTestValidateStrict(ctx, quickstart.Store, quickstart.ID, flags)
			report.Steps = []selfTestCheck{
				{Name: "doctor", OK: true},
				{Name: "demo_create", OK: quickstart.Demo.Topology != "" && quickstart.Demo.Trajectory != ""},
				{Name: "explain", OK: len(explanation.Plan) > 0, Path: quickstart.Job},
				{Name: "run", OK: quickstart.Run.ID == quickstart.ID, Path: quickstart.RunReport},
				{Name: "validate_strict", OK: validationOK, Message: validationMessage, Path: quickstart.Store},
				{Name: "publish_static", OK: quickstart.Publish.Verification != nil && quickstart.Publish.Verification.OK, Path: quickstart.Static},
				{Name: "serve_smoke", OK: quickstart.ServeSmoke.OK, Path: quickstart.Store},
			}
			if !selfTestStepsOK(report.Steps) {
				report.OK = false
				return report, codedErrorf(codeValidationFailed, "self-test quickstart checks failed")
			}
			report.Checks = append(report.Checks, selfTestCheck{Name: "quickstart", OK: true, Path: quickstart.Root})
			report.NextCommands = append(report.NextCommands, quickstart.NextCommands...)
		}
	}
	if len(report.Steps) == 0 {
		report.Steps = append(report.Steps, report.Checks...)
	}
	return report, nil
}

func selfTestRoot(out string) (string, error) {
	if out == "" {
		return os.MkdirTemp("", "hlmdsrv-self-test-*")
	}
	root, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func requiredDoctorChecksOK(checks []doctorCheck) bool {
	for _, check := range checks {
		if check.Level == "required" && !check.OK {
			return false
		}
	}
	return true
}

func (a app) selfTestValidateStrict(ctx context.Context, storePath, id string, flags *selfTestFlags) (bool, string) {
	store, err := mdsrv.OpenStore(storePath)
	if err != nil {
		return false, err.Error()
	}
	manifest, err := store.LoadDataset(id)
	if err != nil {
		return false, err.Error()
	}
	report := store.CheckDataset(manifest)
	issues := validateManifestReferences(ctx, store, manifest, store.Root, &validateFlags{
		strict:     true,
		backend:    flags.backend,
		gmxCommand: flags.gmxCommand,
	}, nil)
	if validationOK(report) && validationIssuesOK(issues) {
		return true, ""
	}
	if !validationOK(report) {
		return false, "dataset file validation failed"
	}
	for _, issue := range issues {
		if issue.Severity == "error" {
			return false, issue.Message
		}
	}
	return false, "strict validation failed"
}

func selfTestStepsOK(steps []selfTestCheck) bool {
	for _, step := range steps {
		if !step.OK {
			return false
		}
	}
	return true
}
