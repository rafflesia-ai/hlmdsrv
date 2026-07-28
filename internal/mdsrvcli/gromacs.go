package mdsrvcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
)

type gromacsFlags struct {
	command    string
	out        string
	topology   string
	trajectory string
	frame      int
	time       string
	force      bool
	jsonReport bool
}

func (a app) gromacsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gromacs",
		Short: "Raw GROMACS bridge helpers",
	}
	cmd.AddCommand(a.gromacsDoctorCommand())
	cmd.AddCommand(a.gromacsProbeCommand())
	cmd.AddCommand(a.gromacsConvertCommand())
	cmd.AddCommand(a.gromacsExtractCommand())
	return cmd
}

func (a app) gromacsDoctorCommand() *cobra.Command {
	flags := &gromacsFlags{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the configured GROMACS command",
		RunE: func(cmd *cobra.Command, args []string) error {
			gmx := gromacs.New(gromacs.Options{Command: flags.command})
			capability := gmx.Check(cmd.Context())
			report := struct {
				OK bool `json:"ok"`
				gromacs.CapabilityReport
			}{OK: capability.Available, CapabilityReport: capability}
			if len(report.Command) == 0 {
				report.Command = gmx.Command
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			if report.OK {
				fmt.Fprintf(a.stdout, "ok\t%s\t%s\t%s\n", strings.Join(report.Command, " "), report.Source, report.Version)
				return nil
			}
			fmt.Fprintf(a.stdout, "fail\t%s\t%s", strings.Join(report.Command, " "), report.Error)
			if report.Hint != "" {
				fmt.Fprintf(a.stdout, "\t%s", report.Hint)
			}
			fmt.Fprintln(a.stdout)
			return nil
		},
	}
	bindGromacsBridgeFlags(cmd, flags)
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) gromacsProbeCommand() *cobra.Command {
	flags := &gromacsFlags{}
	cmd := &cobra.Command{
		Use:   "probe TRAJECTORY",
		Short: "Probe trajectory metadata with gmx check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gmx := gromacs.New(gromacs.Options{Command: flags.command})
			if err := gmx.RequireAvailable(); err != nil {
				return err
			}
			probe, err := gmx.Probe(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			report := struct {
				Trajectory string `json:"trajectory"`
				gromacs.TrajectoryProbe
			}{Trajectory: args[0], TrajectoryProbe: probe}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintf(a.stdout, "%s\tatoms=%d\tframes=%d\ttime=%g:%g\ttimestep=%g\n",
				args[0], probe.AtomCount, probe.FrameCount, probe.TimeStart, probe.TimeEnd, probe.TimeStep)
			return nil
		},
	}
	bindGromacsBridgeFlags(cmd, flags)
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) gromacsConvertCommand() *cobra.Command {
	flags := &gromacsFlags{}
	cmd := &cobra.Command{
		Use:   "convert INPUT",
		Short: "Convert a trajectory-like file with gmx trjconv",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.out) == "" {
				return fmt.Errorf("--out is required")
			}
			if err := ensureOutputPath(flags.out, flags.force); err != nil {
				return err
			}
			gmx := gromacs.New(gromacs.Options{Command: flags.command})
			if err := gmx.RequireAvailable(); err != nil {
				return err
			}
			if err := gmx.Convert(cmd.Context(), gromacs.ConvertOptions{Input: args[0], Output: flags.out}); err != nil {
				return err
			}
			report := struct {
				Input  string `json:"input"`
				Output string `json:"output"`
			}{Input: args[0], Output: flags.out}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, flags.out)
			return nil
		},
	}
	bindGromacsBridgeFlags(cmd, flags)
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output path")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing output path")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func (a app) gromacsExtractCommand() *cobra.Command {
	flags := &gromacsFlags{frame: -1}
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract one frame from raw topology and trajectory files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.topology) == "" {
				return fmt.Errorf("--topology is required")
			}
			if strings.TrimSpace(flags.trajectory) == "" {
				return fmt.Errorf("--trajectory is required")
			}
			if flags.frame < 0 && strings.TrimSpace(flags.time) == "" {
				return fmt.Errorf("one of --frame or --time is required")
			}
			if strings.TrimSpace(flags.out) == "" {
				flags.out = defaultGromacsFramePath(flags.trajectory, flags.frame, flags.time)
			}
			if err := ensureOutputPath(flags.out, flags.force); err != nil {
				return err
			}
			gmx := gromacs.New(gromacs.Options{Command: flags.command})
			if err := gmx.RequireAvailable(); err != nil {
				return err
			}
			var extractedTime float64
			if strings.TrimSpace(flags.time) != "" {
				parsed, err := strconv.ParseFloat(flags.time, 64)
				if err != nil {
					return fmt.Errorf("invalid --time %q", flags.time)
				}
				extractedTime = parsed
				if err := gmx.ExtractFrame(cmd.Context(), gromacs.ExtractFrameOptions{
					Topology:   flags.topology,
					Trajectory: flags.trajectory,
					Output:     flags.out,
					Time:       &extractedTime,
				}); err != nil {
					return err
				}
			} else {
				probe, err := gmx.Probe(cmd.Context(), flags.trajectory)
				if err != nil {
					return err
				}
				extractedTime, err = probe.TimeForFrame(flags.frame)
				if err != nil {
					return err
				}
				if err := gmx.ExtractFrame(cmd.Context(), gromacs.ExtractFrameOptions{
					Topology:   flags.topology,
					Trajectory: flags.trajectory,
					Output:     flags.out,
					FrameIndex: flags.frame,
					Probe:      probe,
				}); err != nil {
					return err
				}
			}
			report := struct {
				Topology   string  `json:"topology"`
				Trajectory string  `json:"trajectory"`
				Frame      *int    `json:"frame,omitempty"`
				Time       float64 `json:"time"`
				Output     string  `json:"output"`
			}{
				Topology:   flags.topology,
				Trajectory: flags.trajectory,
				Time:       extractedTime,
				Output:     flags.out,
			}
			if flags.frame >= 0 {
				report.Frame = &flags.frame
			}
			if flags.jsonReport {
				return writeJSON(a.stdout, report)
			}
			fmt.Fprintln(a.stdout, flags.out)
			return nil
		},
	}
	bindGromacsBridgeFlags(cmd, flags)
	cmd.Flags().StringVar(&flags.topology, "topology", "", "topology/reference structure path passed to -s")
	cmd.Flags().StringVar(&flags.trajectory, "trajectory", "", "trajectory path passed to -f")
	cmd.Flags().IntVar(&flags.frame, "frame", -1, "zero-based frame index; mapped to time with gmx check")
	cmd.Flags().StringVar(&flags.time, "time", "", "trajectory time passed directly to -dump")
	cmd.Flags().StringVarP(&flags.out, "out", "o", "", "output frame path; defaults to TRAJECTORY-frame-N.gro or TRAJECTORY-time-T.gro")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing output path")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write machine-readable output")
	return cmd
}

func bindGromacsBridgeFlags(cmd *cobra.Command, flags *gromacsFlags) {
	cmd.Flags().StringVar(&flags.command, "gmx-command", "", "GROMACS command override; defaults to MDSRV_GMX, gmx, or gmx_mpi")
	cmd.Flags().StringVar(&flags.command, "command", "", "alias for --gmx-command")
}

func ensureOutputPath(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", path)
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func defaultGromacsFramePath(trajectory string, frame int, timeValue string) string {
	base := filepath.Base(trajectory)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if frame >= 0 {
		return fmt.Sprintf("%s-frame-%d.gro", base, frame)
	}
	cleanTime := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(timeValue))
	if cleanTime == "" {
		cleanTime = "0"
	}
	return fmt.Sprintf("%s-time-%s.gro", base, cleanTime)
}
