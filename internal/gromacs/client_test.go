package gromacs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	paths map[string]string
	runs  []fakeCommandRun
}

type fakeCommandRun struct {
	command []string
	stdin   string
}

func (f *fakeCommandRunner) LookPath(file string) (string, error) {
	if f.paths == nil {
		return "", fmt.Errorf("%s not found", file)
	}
	path, ok := f.paths[file]
	if !ok {
		return "", fmt.Errorf("%s not found", file)
	}
	return path, nil
}

func (f *fakeCommandRunner) Run(ctx context.Context, command []string, stdin []byte) (string, error) {
	f.runs = append(f.runs, fakeCommandRun{command: append([]string{}, command...), stdin: string(stdin)})
	if len(command) > 1 && command[1] == "--version" {
		return "GROMACS version: fake-2026\n", nil
	}
	if len(command) > 1 && command[1] == "check" {
		return `
Reading frame       0 time    0.000
# Atoms  3
Reading frame       1 time    1.500
Last frame          1 time    1.500
`, nil
	}
	return "", nil
}

func TestParseCheckOutput(t *testing.T) {
	output := `
Reading frame       0 time    0.000
# Atoms  3
Reading frame       1 time    1.000
Reading frame       2 time    2.000
Last frame          2 time    2.000

Item        #frames Timestep (ps)
Step             3    1
Time             3    1
Lambda           0
Coords           3    1
Velocities       0
Forces           0
Box              3    1
`
	probe := ParseCheckOutput(output)
	if probe.AtomCount != 3 {
		t.Fatalf("AtomCount = %d", probe.AtomCount)
	}
	if probe.FrameCount != 3 {
		t.Fatalf("FrameCount = %d", probe.FrameCount)
	}
	if probe.TimeStart != 0 || probe.TimeEnd != 2 || probe.TimeStep != 1 {
		t.Fatalf("unexpected times: %#v", probe)
	}
	if len(probe.FrameTimes) != 3 || probe.FrameTimes[1] != 1 {
		t.Fatalf("unexpected frame times: %#v", probe.FrameTimes)
	}
}

func TestParseCheckOutputIrregularFrameTimes(t *testing.T) {
	output := `
Reading frame       0 time    0.000
# Atoms  3
Reading frame       1 time    0.500
Reading frame       2 time    2.250
Reading frame       3 time    7.000
Last frame          3 time    7.000
`
	probe := ParseCheckOutput(output)
	if probe.FrameCount != 4 {
		t.Fatalf("FrameCount = %d", probe.FrameCount)
	}
	want := []float64{0, 0.5, 2.25, 7}
	if len(probe.FrameTimes) != len(want) {
		t.Fatalf("FrameTimes = %#v", probe.FrameTimes)
	}
	for i := range want {
		if probe.FrameTimes[i] != want[i] {
			t.Fatalf("FrameTimes[%d] = %g, want %g", i, probe.FrameTimes[i], want[i])
		}
	}
}

func TestTimeForFrameUsesExactFrameTimes(t *testing.T) {
	probe := TrajectoryProbe{
		FrameCount: 4,
		TimeStep:   2.3333333333333335,
		FrameTimes: []float64{0, 0.5, 2.25, 7},
	}
	got, err := probe.TimeForFrame(2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2.25 {
		t.Fatalf("TimeForFrame = %g, want 2.25", got)
	}
}

func TestTimeForFrameFallsBackToStep(t *testing.T) {
	probe := TrajectoryProbe{FrameCount: 4, TimeStart: 10, TimeStep: 0.5}
	got, err := probe.TimeForFrame(3)
	if err != nil {
		t.Fatal(err)
	}
	if got != 11.5 {
		t.Fatalf("TimeForFrame = %g, want 11.5", got)
	}
}

func TestClientCommandSources(t *testing.T) {
	t.Setenv("MDSRV_GMX", "env-gmx -quiet")
	fromEnv := New(Options{})
	if got := fromEnv.CommandString(); got != "env-gmx -quiet" {
		t.Fatalf("env command = %q", got)
	}
	if fromEnv.Source != "env:MDSRV_GMX" {
		t.Fatalf("env source = %q", fromEnv.Source)
	}

	fromOption := New(Options{Command: "option-gmx -quiet"})
	if got := fromOption.CommandString(); got != "option-gmx -quiet" {
		t.Fatalf("option command = %q", got)
	}
	if fromOption.Source != "option" {
		t.Fatalf("option source = %q", fromOption.Source)
	}
}

func TestClientUsesInjectedRunner(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"gmx": "/fake/bin/gmx", "/fake/bin/gmx": "/fake/bin/gmx"}}
	client := New(Options{Runner: runner})
	if client.CommandString() != "/fake/bin/gmx" || client.Source != "path:gmx" {
		t.Fatalf("unexpected fake command resolution: %#v", client)
	}
	report := client.Check(context.Background())
	if !report.Available || report.Version != "fake-2026" {
		t.Fatalf("unexpected fake capability report: %#v", report)
	}
	probe, err := client.Probe(context.Background(), "trajectory.xtc")
	if err != nil {
		t.Fatal(err)
	}
	if probe.FrameCount != 2 || probe.AtomCount != 3 || probe.FrameTimes[1] != 1.5 {
		t.Fatalf("unexpected fake probe: %#v", probe)
	}
}

func TestClientComposesGromacsCommands(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"fake-gmx": "/fake/bin/fake-gmx"}}
	client := New(Options{Command: "fake-gmx -quiet", Runner: runner})
	if err := client.Convert(context.Background(), ConvertOptions{Input: "frames.gro", Output: "trajectory.xtc"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ExtractFrame(context.Background(), ExtractFrameOptions{
		Topology:   "structure.gro",
		Trajectory: "trajectory.xtc",
		Output:     "frame.gro",
		FrameIndex: 1,
		Probe:      TrajectoryProbe{FrameCount: 2, FrameTimes: []float64{0, 1.5}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.Export(context.Background(), ExportOptions{
		Topology:   "structure.gro",
		Trajectory: "trajectory.xtc",
		Output:     "slice.xtc",
		Start:      1,
		Stop:       4,
		Stride:     2,
		IndexPath:  "subset.ndx",
	}); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(runner.runs))
	for _, run := range runner.runs {
		got = append(got, strings.Join(run.command, " ")+" stdin="+strings.TrimSpace(run.stdin))
	}
	want := []string{
		"fake-gmx -quiet trjconv -f frames.gro -o trajectory.xtc stdin=",
		"fake-gmx -quiet trjconv -s structure.gro -f trajectory.xtc -dump 1.5 -o frame.gro stdin=0",
		"fake-gmx -quiet trjconv -s structure.gro -f trajectory.xtc -o slice.xtc -b 1 -e 4 -skip 2 -n subset.ndx stdin=0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected commands:\n%s", strings.Join(got, "\n"))
	}
}

func TestCapabilityReportForMissingCommand(t *testing.T) {
	command := "definitely-missing-gmx-for-capability-test"
	client := New(Options{Command: command})
	report := client.Check(context.Background())
	if report.Available {
		t.Fatalf("missing command reported available: %#v", report)
	}
	if report.CommandString != command || report.Source != "option" {
		t.Fatalf("unexpected command report: %#v", report)
	}
	if !strings.Contains(report.Hint, "install GROMACS") {
		t.Fatalf("missing actionable hint: %#v", report)
	}
}

func TestRunMissingCommandReturnsStructuredError(t *testing.T) {
	client := New(Options{Command: "definitely-missing-gmx-for-run-test"})
	_, err := client.Probe(context.Background(), "trajectory.xtc")
	if err == nil {
		t.Fatal("expected missing GROMACS command error")
	}
	var unavailable CommandUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error is not CommandUnavailableError: %T %v", err, err)
	}
	if unavailable.Source != "option" || len(unavailable.Command) == 0 {
		t.Fatalf("unexpected unavailable error: %#v", unavailable)
	}
}

func TestFallbackSourceWhenPathDoesNotContainGromacs(t *testing.T) {
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	t.Setenv("MDSRV_GMX", "")
	t.Setenv("PATH", t.TempDir())

	client := New(Options{})
	if client.Source != "fallback:gmx" {
		t.Fatalf("fallback source = %q", client.Source)
	}
	report := client.Check(context.Background())
	if report.Available || report.Error == "" || report.Hint == "" {
		t.Fatalf("unexpected fallback report: %#v", report)
	}
}

func TestDemoGRO(t *testing.T) {
	data := DemoGRO(2)
	if strings.Count(data, "Demo t=") != 2 {
		t.Fatalf("expected two frames in demo GRO:\n%s", data)
	}
	if !strings.Contains(data, "MOL") || !strings.Contains(data, "1.00000") {
		t.Fatalf("demo GRO missing expected atoms/box:\n%s", data)
	}
}

func TestCreateDemoTrajectoryRequiresGromacs(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "demo")
	_, err := CreateDemoTrajectory(context.Background(), DemoTrajectoryOptions{
		OutDir:  outDir,
		Frames:  2,
		Force:   true,
		Command: "definitely-missing-gmx-for-fixture-test",
	})
	if err == nil {
		t.Fatal("expected missing GROMACS command error")
	}
	var unavailable CommandUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error is not CommandUnavailableError: %T %v", err, err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "structure.gro")); !os.IsNotExist(err) {
		t.Fatalf("missing gmx left partial demo file, stat err=%v", err)
	}
}

// TestParseCheckOutputRejectsHugeFrameCount ensures a crafted "Last frame" line
// claiming a giant frame count does not force a multi-gigabyte allocation: with
// only a couple of real frame-time entries, FrameTimes stays empty and no huge
// make() runs.
func TestParseCheckOutputHugeFrameCountDoesNotAllocate(t *testing.T) {
	output := `
Reading frame       0 time    0.000
# Atoms  3
Last frame  2000000000 time    1.000
`
	probe := ParseCheckOutput(output)
	if probe.FrameCount != 2000000001 {
		t.Fatalf("FrameCount = %d", probe.FrameCount)
	}
	if len(probe.FrameTimes) != 0 {
		t.Fatalf("expected no materialized frame times, got %d", len(probe.FrameTimes))
	}
}

// TestParseCheckOutputClampsNegativeFrameCount ensures an overflowed (negative)
// frame count is clamped so it cannot panic a downstream make().
func TestParseCheckOutputClampsNegativeFrameCount(t *testing.T) {
	output := `
# Atoms  3
Last frame  9223372036854775807 time    1.000
`
	probe := ParseCheckOutput(output)
	if probe.FrameCount < 0 {
		t.Fatalf("FrameCount must not be negative, got %d", probe.FrameCount)
	}
}
