package mdsrvcli

import (
	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type storeFlags struct {
	store      string
	init       bool
	strict     bool
	jsonReport bool
}

func (a app) storeCommand() *cobra.Command {
	flags := &storeFlags{}
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Inspect and maintain an MDsrv store",
	}
	doctor := &cobra.Command{
		Use:   "doctor",
		Short: "Check store layout, version metadata, and migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			if flags.init {
				if err := store.Init(); err != nil {
					return err
				}
			}
			report := store.Doctor()
			if flags.jsonReport {
				if err := writeJSON(a.stdout, report); err != nil {
					return err
				}
				if flags.strict && !report.OK {
					return codedErrorf(codeValidationFailed, "store doctor failed")
				}
				return nil
			}
			writeStoreDoctorText(a.stdout, report)
			if flags.strict && !report.OK {
				return codedErrorf(codeValidationFailed, "store doctor failed")
			}
			return nil
		},
	}
	doctor.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	doctor.Flags().BoolVar(&flags.init, "init", false, "initialize missing store directories and metadata before checking")
	doctor.Flags().BoolVar(&flags.strict, "strict", false, "fail when any store check fails")
	doctor.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(doctor)
	return cmd
}
