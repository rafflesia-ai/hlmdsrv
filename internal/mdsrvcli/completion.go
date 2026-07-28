package mdsrvcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type completionFlags struct {
	out   string
	force bool
}

func (a app) completionCommand() *cobra.Command {
	flags := &completionFlags{}
	cmd := &cobra.Command{
		Use:   "completion SHELL",
		Short: "Print shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out io.Writer
			if flags.out == "" {
				out = a.stdout
			} else {
				if err := ensureOutputPath(flags.out, flags.force); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(flags.out), 0o755); err != nil {
					return err
				}
				file, err := os.Create(flags.out)
				if err != nil {
					return err
				}
				defer file.Close()
				out = file
			}
			root := a.rootCommand()
			switch args[0] {
			case "bash":
				if err := root.GenBashCompletion(out); err != nil {
					return err
				}
			case "zsh":
				if err := root.GenZshCompletion(out); err != nil {
					return err
				}
			case "fish":
				if err := root.GenFishCompletion(out, true); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
			if flags.out != "" {
				fmt.Fprintln(a.stdout, flags.out)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "write completion script to this path instead of stdout")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing completion file")
	return cmd
}
