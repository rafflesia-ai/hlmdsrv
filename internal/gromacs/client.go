package gromacs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/rafflesia-ai/hlmdsrv/internal/procgroup"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Options struct {
	Command string
	Runner  CommandRunner
}

type Client struct {
	Command     []string
	Source      string
	LookupError string
	Runner      CommandRunner
}

type CommandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, command []string, stdin []byte) (string, error)
}

type OSCommandRunner struct{}

type TrajectoryProbe struct {
	AtomCount  int       `json:"atom_count,omitempty"`
	FrameCount int       `json:"frame_count,omitempty"`
	TimeStart  float64   `json:"time_start,omitempty"`
	TimeEnd    float64   `json:"time_end,omitempty"`
	TimeStep   float64   `json:"time_step,omitempty"`
	FrameTimes []float64 `json:"frame_times,omitempty"`
}

type ConvertOptions struct {
	Input  string
	Output string
}

type ExtractFrameOptions struct {
	Topology   string
	Trajectory string
	Output     string
	FrameIndex int
	Time       *float64
	Probe      TrajectoryProbe
	GroupInput []byte
}

type ExportOptions struct {
	Topology   string
	Trajectory string
	Output     string
	Start      float64
	Stop       float64
	Stride     int
	GroupInput []byte
	IndexPath  string
}

type CapabilityReport struct {
	Command       []string `json:"command"`
	CommandString string   `json:"command_string"`
	Source        string   `json:"source"`
	Available     bool     `json:"available"`
	Version       string   `json:"version,omitempty"`
	Error         string   `json:"error,omitempty"`
	Hint          string   `json:"hint,omitempty"`
}

type CommandUnavailableError struct {
	Command []string
	Source  string
	Err     error
}

func (e CommandUnavailableError) Error() string {
	command := strings.Join(e.Command, " ")
	if command == "" {
		command = "gmx"
	}
	if e.Err != nil {
		return fmt.Sprintf("GROMACS command %q is unavailable: %v", command, e.Err)
	}
	return fmt.Sprintf("GROMACS command %q is unavailable", command)
}

func (e CommandUnavailableError) Unwrap() error {
	return e.Err
}

func (e CommandUnavailableError) Remediation() string {
	return installHint()
}

var (
	atomCountPattern    = regexp.MustCompile(`# Atoms\s+(\d+)`)
	readingFramePattern = regexp.MustCompile(`Reading frame\s+(\d+)\s+time\s+([-+0-9.eE]+)`)
	lastFramePattern    = regexp.MustCompile(`Last frame\s+(\d+)\s+time\s+([-+0-9.eE]+)`)
	coordsPattern       = regexp.MustCompile(`(?m)^Coords\s+(\d+)\s+([-+0-9.eE]+)`)
)

func New(options Options) Client {
	runner := options.Runner
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if strings.TrimSpace(options.Command) != "" {
		return Client{Command: strings.Fields(options.Command), Source: "option", Runner: runner}
	}
	if value := strings.TrimSpace(os.Getenv("MDSRV_GMX")); value != "" {
		return Client{Command: strings.Fields(value), Source: "env:MDSRV_GMX", Runner: runner}
	}
	if path, err := runner.LookPath("gmx"); err == nil {
		return Client{Command: []string{path}, Source: "path:gmx", Runner: runner}
	}
	if path, err := runner.LookPath("gmx_mpi"); err == nil {
		return Client{Command: []string{path}, Source: "path:gmx_mpi", Runner: runner}
	}
	return Client{Command: []string{"gmx"}, Source: "fallback:gmx", LookupError: "gmx and gmx_mpi were not found on PATH", Runner: runner}
}

func (c Client) Available() bool {
	if len(c.Command) == 0 {
		return false
	}
	_, err := c.commandRunner().LookPath(c.Command[0])
	return err == nil
}

func (c Client) CommandString() string {
	return strings.Join(c.Command, " ")
}

func (c Client) Check(ctx context.Context) CapabilityReport {
	report := CapabilityReport{
		Command:       append([]string{}, c.Command...),
		CommandString: c.CommandString(),
		Source:        c.Source,
		Available:     c.Available(),
	}
	if c.LookupError != "" {
		report.Error = c.LookupError
	}
	if report.Source == "" {
		report.Source = "unknown"
	}
	if !report.Available {
		if report.Error == "" {
			report.Error = CommandUnavailableError{Command: c.Command, Source: c.Source}.Error()
		}
		report.Hint = installHint()
		return report
	}
	version, err := c.Version(ctx)
	if err != nil {
		report.Available = false
		report.Error = err.Error()
		report.Hint = "GROMACS was found but could not run; check the binary, dynamic libraries, and --gmx-command value"
		return report
	}
	report.Version = version
	return report
}

func (c Client) RequireAvailable() error {
	if len(c.Command) == 0 {
		return CommandUnavailableError{Command: c.Command, Source: c.Source, Err: errors.New("empty command")}
	}
	if _, err := c.commandRunner().LookPath(c.Command[0]); err != nil {
		return CommandUnavailableError{Command: c.Command, Source: c.Source, Err: err}
	}
	return nil
}

func (c Client) Version(ctx context.Context) (string, error) {
	output, err := c.run(ctx, nil, "--version")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "GROMACS version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "GROMACS version:")), nil
		}
	}
	// Identity guard. Falling back to the first line of output meant any binary
	// that merely exits 0 on --version passed as GROMACS: `--gmx-command
	// /usr/bin/true` reported available:true with an empty version, and doctor
	// gave the environment a clean bill of health it had not earned. Real gmx
	// always prints the "GROMACS version:" banner, so its absence means whatever
	// ran is not GROMACS.
	return "", fmt.Errorf("%s ran but did not identify itself as GROMACS (no %q line in --version output)", c.Command[0], "GROMACS version:")
}

func (c Client) Probe(ctx context.Context, trajectoryPath string) (TrajectoryProbe, error) {
	output, err := c.run(ctx, nil, "check", "-f", trajectoryPath)
	if err != nil {
		return TrajectoryProbe{}, err
	}
	probe := ParseCheckOutput(output)
	if probe.FrameCount == 0 {
		return TrajectoryProbe{}, errors.New("gmx check did not report a frame count")
	}
	return probe, nil
}

func (c Client) Convert(ctx context.Context, opts ConvertOptions) error {
	if strings.TrimSpace(opts.Input) == "" {
		return errors.New("convert input is required")
	}
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("convert output is required")
	}
	_, err := c.run(ctx, nil, "trjconv", "-f", opts.Input, "-o", opts.Output)
	return err
}

func (c Client) ExtractFrame(ctx context.Context, opts ExtractFrameOptions) error {
	if strings.TrimSpace(opts.Topology) == "" {
		return errors.New("extract topology is required")
	}
	if strings.TrimSpace(opts.Trajectory) == "" {
		return errors.New("extract trajectory is required")
	}
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("extract output is required")
	}
	timeValue := 0.0
	if opts.Time != nil {
		timeValue = *opts.Time
	} else {
		if opts.FrameIndex < 0 {
			return errors.New("frame index cannot be negative")
		}
		var err error
		timeValue, err = opts.Probe.TimeForFrame(opts.FrameIndex)
		if err != nil {
			return err
		}
	}
	groupInput := opts.GroupInput
	if len(groupInput) == 0 {
		groupInput = []byte("0\n")
	}
	_, err := c.run(ctx, groupInput,
		"trjconv",
		"-s", opts.Topology,
		"-f", opts.Trajectory,
		"-dump", formatFloat(timeValue),
		"-o", opts.Output,
	)
	return err
}

func (c Client) Export(ctx context.Context, opts ExportOptions) error {
	if strings.TrimSpace(opts.Topology) == "" {
		return errors.New("export topology is required")
	}
	if strings.TrimSpace(opts.Trajectory) == "" {
		return errors.New("export trajectory is required")
	}
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("export output is required")
	}
	args := []string{"trjconv", "-s", opts.Topology, "-f", opts.Trajectory, "-o", opts.Output}
	if opts.Start >= 0 {
		args = append(args, "-b", formatFloat(opts.Start))
	}
	if opts.Stop >= 0 {
		args = append(args, "-e", formatFloat(opts.Stop))
	}
	if opts.Stride > 1 {
		args = append(args, "-skip", strconv.Itoa(opts.Stride))
	}
	if opts.IndexPath != "" {
		args = append(args, "-n", opts.IndexPath)
	}
	groupInput := opts.GroupInput
	if len(groupInput) == 0 {
		groupInput = []byte("0\n")
	}
	_, err := c.run(ctx, groupInput, args...)
	return err
}

func (p TrajectoryProbe) TimeForFrame(frameIndex int) (float64, error) {
	if frameIndex < 0 {
		return 0, errors.New("frame index cannot be negative")
	}
	if p.FrameCount > 0 && frameIndex >= p.FrameCount {
		return 0, fmt.Errorf("frame index %d is out of range for %d frames", frameIndex, p.FrameCount)
	}
	if len(p.FrameTimes) > frameIndex {
		return p.FrameTimes[frameIndex], nil
	}
	if p.TimeStep > 0 || p.TimeStart != 0 {
		return p.TimeStart + float64(frameIndex)*p.TimeStep, nil
	}
	return float64(frameIndex), nil
}

func ParseCheckOutput(output string) TrajectoryProbe {
	normalized := strings.ReplaceAll(output, "\r", "\n")
	var probe TrajectoryProbe
	frameTimes := map[int]float64{}
	if match := atomCountPattern.FindStringSubmatch(normalized); len(match) == 2 {
		probe.AtomCount = mustAtoi(match[1])
	}
	if match := coordsPattern.FindStringSubmatch(normalized); len(match) == 3 {
		probe.FrameCount = mustAtoi(match[1])
		probe.TimeStep = mustParseFloat(match[2])
	}
	for _, match := range readingFramePattern.FindAllStringSubmatch(normalized, -1) {
		if len(match) == 3 {
			frame := mustAtoi(match[1])
			timeValue := mustParseFloat(match[2])
			frameTimes[frame] = timeValue
			if frame == 0 {
				probe.TimeStart = timeValue
			}
		}
	}
	if match := lastFramePattern.FindStringSubmatch(normalized); len(match) == 3 {
		lastFrame := mustAtoi(match[1])
		if probe.FrameCount == 0 {
			probe.FrameCount = lastFrame + 1
		}
		probe.TimeEnd = mustParseFloat(match[2])
		frameTimes[lastFrame] = probe.TimeEnd
	}
	if probe.FrameCount == 0 && len(frameTimes) > 0 {
		maxFrame := 0
		for frame := range frameTimes {
			if frame > maxFrame {
				maxFrame = frame
			}
		}
		probe.FrameCount = maxFrame + 1
	}
	// A frame index near math.MaxInt (from a crafted trajectory header) can
	// overflow lastFrame+1 / maxFrame+1 to a negative value; never let that
	// escape as a negative count that would panic a downstream make().
	if probe.FrameCount < 0 {
		probe.FrameCount = 0
	}
	// Only build the per-frame time slice when frameTimes actually contains an
	// entry for every frame — otherwise `complete` can never be true anyway.
	// This ties the allocation to real parsed data and prevents a crafted
	// "Last frame 2000000000" line from forcing a multi-gigabyte make().
	if probe.FrameCount > 0 && len(frameTimes) >= probe.FrameCount {
		times := make([]float64, probe.FrameCount)
		complete := true
		for frame := 0; frame < probe.FrameCount; frame++ {
			timeValue, ok := frameTimes[frame]
			if !ok {
				complete = false
				break
			}
			times[frame] = timeValue
		}
		if complete {
			probe.FrameTimes = times
			probe.TimeStart = times[0]
			probe.TimeEnd = times[len(times)-1]
		}
	}
	if probe.TimeStep == 0 && probe.FrameCount > 1 {
		probe.TimeStep = (probe.TimeEnd - probe.TimeStart) / float64(probe.FrameCount-1)
	}
	return probe
}

func (c Client) run(ctx context.Context, stdin []byte, args ...string) (string, error) {
	if err := c.RequireAvailable(); err != nil {
		return "", err
	}
	fullArgs := append(append([]string{}, c.Command[1:]...), args...)
	command := append([]string{c.Command[0]}, fullArgs...)
	text, err := c.commandRunner().Run(ctx, command, stdin)
	if err != nil {
		if out := gromacsDiagnostic(text); out != "" {
			return text, fmt.Errorf("%s %s failed: %w: %s", c.Command[0], strings.Join(fullArgs, " "), err, out)
		}
		return text, fmt.Errorf("%s %s failed: %w", c.Command[0], strings.Join(fullArgs, " "), err)
	}
	return text, nil
}

func (c Client) commandRunner() CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return OSCommandRunner{}
}

func (OSCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (OSCommandRunner) Run(ctx context.Context, command []string, stdin []byte) (string, error) {
	if len(command) == 0 {
		return "", errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	procgroup.Isolate(cmd)
	cmd.Env = os.Environ()
	if os.Getenv("GMX_MAXBACKUP") == "" {
		cmd.Env = append(cmd.Env, "GMX_MAXBACKUP=-1")
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	text := string(output)
	if err != nil {
		return text, err
	}
	return text, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func mustAtoi(value string) int {
	i, _ := strconv.Atoi(value)
	return i
}

func mustParseFloat(value string) float64 {
	f, _ := strconv.ParseFloat(value, 64)
	return f
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func trimOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 4000 {
		return value
	}
	return value[:4000] + "\n..."
}

// gromacsDiagnostic extracts the actionable part of a failed gmx run. Every gmx
// invocation prints a banner, executable path, data prefix, working dir and the
// command line before anything useful, so attaching the raw output buried the one
// line that explains the failure under ~15 lines of boilerplate — and put all of
// it into the JSON error message. GROMACS delimits the real diagnosis with a
// "Fatal error:" header, so prefer that; fall back to the full output when the
// failure has no such section (a crash, a missing dynamic library).
func gromacsDiagnostic(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "Fatal error:") {
			continue
		}
		var detail []string
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			// The section ends at the rule or at the boilerplate "For more
			// information..." pointer that follows every fatal error.
			if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "For more information") {
				break
			}
			if trimmed == "" && len(detail) > 0 {
				break
			}
			if trimmed != "" {
				detail = append(detail, trimmed)
			}
		}
		if len(detail) > 0 {
			return strings.Join(detail, " ")
		}
	}
	return trimOutput(output)
}

func installHint() string {
	return "install GROMACS, add gmx/gmx_mpi to PATH, set MDSRV_GMX, or pass --gmx-command"
}
