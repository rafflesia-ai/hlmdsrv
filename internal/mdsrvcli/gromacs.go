package mdsrvcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/hlmdsrv/internal/gromacs"
	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrv"
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
			if err := requireVerifiedGromacs(cmd.Context(), gmx); err != nil {
				return err
			}
			if err := requireInputFile(args[0]); err != nil {
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
			if err := requireInputFile(args[0]); err != nil {
				return err
			}
			if err := ensureOutputPathAgainst(flags.out, flags.force, args[0]); err != nil {
				return err
			}
			gmx := gromacs.New(gromacs.Options{Command: flags.command})
			if err := requireVerifiedGromacs(cmd.Context(), gmx); err != nil {
				return err
			}
			if err := gmx.Convert(cmd.Context(), gromacs.ConvertOptions{Input: args[0], Output: flags.out}); err != nil {
				return err
			}
			if err := requireProducedFile(flags.out, "gromacs convert"); err != nil {
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
			if err := requireVerifiedGromacs(cmd.Context(), gmx); err != nil {
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
			if err := requireProducedFile(flags.out, "gromacs extract"); err != nil {
				return err
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
	return ensureOutputPathAgainst(path, force)
}

// requireInputFile rejects an input path that is missing, a directory, a special
// file, or unreadable. Without it these were handed straight to gmx, which failed
// with its own banner and got classified internal_error (exit 1) — telling a
// caller to report a bug when the real problem was a fixable path. Readability is
// checked by opening, since a mode-000 regular file passes every stat check.
func requireInputFile(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return codedErrorf(codeInvalidInput, "input path is required")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		if os.IsNotExist(err) {
			return codedErrorf(codeInvalidInput, "%s: no such file", trimmed)
		}
		return codedErrorf(codeInvalidInput, "%s: %v", trimmed, err)
	}
	if info.IsDir() {
		return codedErrorf(codeInvalidInput, "%s is a directory, not a file", trimmed)
	}
	if !info.Mode().IsRegular() {
		return codedErrorf(codeInvalidInput, "%s is not a regular file", trimmed)
	}
	file, err := os.Open(trimmed)
	if err != nil {
		return codedErrorf(codeInvalidInput, "%s: %v", trimmed, err)
	}
	_ = file.Close()
	return nil
}

// rejectNonRegularOutput refuses an existing FIFO, device, or socket as an output
// target. Directories are left to the caller: several commands legitimately write
// into one.
func rejectNonRegularOutput(path string) error {
	return rejectNonRegularPath(path, "write to")
}

// requireVerifiedGromacs gates a run path on GROMACS actually being GROMACS, not
// merely on a PATH hit. RequireAvailable only does a lookup, so `--gmx-command
// /usr/bin/true` sailed past it and each command then failed in its own way —
// render_failed here, internal_error there — for what is one condition: the
// configured backend is not usable. doctor and capabilities already report this
// correctly; this puts the run paths on the same footing, at the cost of one
// `gmx --version` per invocation.
func requireVerifiedGromacs(ctx context.Context, gmx gromacs.Client) error {
	if err := gmx.RequireAvailable(); err != nil {
		return err
	}
	capability := gmx.Check(ctx)
	if !capability.Available {
		message := capability.Error
		if message == "" {
			message = "the configured GROMACS command is not usable"
		}
		return codedErrorf(codeMissingBackend, "gromacs command %q is not usable: %s", gmx.CommandString(), message)
	}
	return nil
}

// requireProducedFile verifies that an external tool actually produced the output
// it was asked for. GROMACS is invoked through a caller-supplied command, and
// availability is only a PATH lookup, so a wrong or stub binary (or a gmx that
// failed without a non-zero status) left the command reporting success with an
// `output` path that does not exist. Trust the exit status, then verify.
func requireProducedFile(path, produced string) error {
	info, err := os.Stat(path)
	if err != nil {
		return codedErrorf(codeRenderFailed, "%s reported success but %s was not created", produced, path)
	}
	// A directory must be rejected explicitly: it stats with a non-zero size, so a
	// bare size check passes an --out that is an empty directory and reports a
	// trajectory export that produced nothing.
	if info.IsDir() {
		return codedErrorf(codeInvalidInput, "%s is a directory, not a file; --out must name a file", path)
	}
	if info.Size() == 0 {
		return codedErrorf(codeRenderFailed, "%s reported success but %s is empty", produced, path)
	}
	return nil
}

// rejectDatasetInputOverwrite refuses an output path that resolves to one of the
// dataset's own files. ensureOutputPathAgainst covers commands that already hold
// resolved input paths, but the dataset-oriented commands (frames get, pack,
// debug bundle) only know a dataset id, and each of them truncated a store's own
// topology at exit 0 when pointed at it. Resolution goes through the store so a
// relative --out and a store-relative manifest path compare as the same file, and
// identity is os.SameFile so hardlinks and symlinks are caught too.
func rejectDatasetInputOverwrite(store mdsrv.Store, m mdsrv.Manifest, out string) error {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	outInfo, err := os.Stat(out)
	if err != nil {
		return nil // nothing there yet: nothing to destroy
	}
	relatives := []string{m.Inputs.Topology.Path}
	for _, trajectory := range m.Inputs.Trajectories {
		relatives = append(relatives, trajectory.Path)
	}
	for _, relative := range relatives {
		if strings.TrimSpace(relative) == "" {
			continue
		}
		resolved, resolveErr := store.SafeResolvePath(relative)
		if resolveErr != nil {
			continue
		}
		inputInfo, statErr := os.Stat(resolved)
		if statErr != nil {
			continue
		}
		if os.SameFile(outInfo, inputInfo) {
			return codedErrorf(codeInvalidInput, "output %s is an input of dataset %s; refusing to overwrite it", out, m.Metadata.ID)
		}
	}
	return nil
}

// rejectNonRegularPath refuses a special file on either side of an I/O. Opening a
// FIFO blocks in open(2) in *both* directions — for write until a reader appears,
// for read until a writer does — and that block is invisible to context
// cancellation, so such a path has to be refused rather than merely interrupted.
func rejectNonRegularPath(path, verb string) error {
	if isNullDevice(path) {
		// "run it but throw the output away" is an established idiom, and the null
		// device is the one special file that neither blocks on open nor can be
		// meaningfully destroyed by writing to it — the two hazards this guard
		// exists for.
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode().IsRegular() {
		return nil
	}
	return codedErrorf(codeInvalidInput, "%s is not a regular file; refusing to %s it", path, verb)
}

func isNullDevice(path string) bool {
	switch strings.ToUpper(strings.TrimSpace(path)) {
	case "/DEV/NULL", "NUL":
		return true
	default:
		return false
	}
}

// ensureOutputPathAgainst prepares an output path, refusing two destructive
// cases before any work is done.
//
// A non-regular existing target (FIFO, device, socket) is refused because
// writing "through" it does not mean what a caller expects: opening a named pipe
// blocks until a reader appears, and the writer would otherwise unlink the pipe
// and leave a regular file where another process expected to keep reading.
//
// An output that resolves to one of the run's own inputs is refused because the
// tool truncates the output the instant it opens it — with --force that silently
// destroyed the source (a store's own topology file) and reported success.
// Identity is tested with os.SameFile (device+inode), so a hardlink or a symlink
// aliasing the input is caught too, not just a literal path match.
func ensureOutputPathAgainst(path string, force bool, inputs ...string) error {
	// The null device is deliberately NOT exempted here. Every caller of this
	// helper either stages a temp file and renames (pack tries /dev/null.tmp and
	// gets EPERM) or hands the path to GROMACS (which derives the format from the
	// extension and fails). Allowing it only moved a clean invalid_input into an
	// unclassified failure deeper in. Sinks that can genuinely discard — the
	// pure-Go writers — go through rejectNonRegularPath, which does exempt it.
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		// Every caller of this helper writes a single file. A directory target
		// otherwise slipped through to the tool, which either failed obscurely
		// (internal_error) or "succeeded" against a path it never wrote.
		return codedErrorf(codeInvalidInput, "%s is a directory, not a file; --out must name a file", path)
	case err == nil && !info.Mode().IsRegular():
		return codedErrorf(codeInvalidInput, "%s is not a regular file; refusing to write to it", path)
	case err == nil && !force:
		return fmt.Errorf("%s already exists; pass --force to overwrite", path)
	}
	if err == nil {
		for _, input := range inputs {
			if input == "" {
				continue
			}
			inputInfo, statErr := os.Stat(input)
			if statErr != nil {
				continue
			}
			if os.SameFile(info, inputInfo) {
				return codedErrorf(codeInvalidInput, "output %s is also an input of this run; refusing to overwrite it", path)
			}
		}
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
