package mdsrvcli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
)

type publishFlags struct {
	store      string
	out        string
	force      bool
	verify     bool
	jsonReport bool
}

func (a app) publishCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "publish", Short: "Publish MDsrv artifacts for deployment"}
	flags := &publishFlags{}
	static := &cobra.Command{
		Use:   "static",
		Short: "Copy a store into a read-only static directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.out == "" {
				return fmt.Errorf("--out is required")
			}
			store, err := mdsrv.OpenStore(flags.store)
			if err != nil {
				return err
			}
			report, err := store.PublishStatic(flags.out, flags.force)
			if err != nil {
				return err
			}
			if flags.verify {
				verification, err := mdsrv.VerifyStaticPublish(report.Out)
				if err != nil {
					return err
				}
				report.Verification = &verification
				if !verification.OK {
					if flags.jsonReport {
						_ = writeJSON(a.stdout, report)
					}
					return codedErrorf(codeValidationFailed, "static publish verification failed")
				}
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, report.Out)
			return nil
		},
	}
	static.Flags().StringVar(&flags.store, "store", "./mdsrv-data", "MDsrv store root")
	static.Flags().StringVarP(&flags.out, "out", "o", "", "output directory")
	static.Flags().BoolVar(&flags.force, "force", false, "overwrite existing files")
	static.Flags().BoolVar(&flags.verify, "verify", false, "verify copied catalogs and referenced artifacts")
	static.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	cmd.AddCommand(static)
	return cmd
}
