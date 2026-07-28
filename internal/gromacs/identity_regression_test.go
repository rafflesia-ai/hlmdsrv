package gromacs

import (
	"context"
	"strings"
	"testing"
)

// stubRunner returns fixed output for any command, so a Client can be pointed at
// an "installed" binary whose --version output the test controls.
type stubRunner struct {
	output string
	err    error
}

func (s stubRunner) LookPath(file string) (string, error) { return "/usr/bin/" + file, nil }

func (s stubRunner) Run(context.Context, []string, []byte) (string, error) {
	return s.output, s.err
}

// Finding #7: Version fell back to the first line of --version output when the
// GROMACS banner was absent, so any binary that merely exits 0 passed as GROMACS.
// `--gmx-command /usr/bin/true` reported available:true with an empty version and
// doctor gave the environment a clean bill of health it had not earned.
func TestVersionRejectsABinaryThatIsNotGromacs(t *testing.T) {
	client := Client{
		Command: []string{"true"},
		Runner:  stubRunner{},
	}
	if _, err := client.Version(context.Background()); err == nil {
		t.Fatal("a binary that prints no GROMACS banner must not pass as GROMACS")
	}
}

// A binary that prints *something* but not the banner is still not GROMACS.
func TestVersionRejectsAnUnrelatedBanner(t *testing.T) {
	client := Client{
		Command: []string{"node"},
		Runner:  stubRunner{output: "v26.4.0\n"},
	}
	_, err := client.Version(context.Background())
	if err == nil {
		t.Fatal("an unrelated version banner must not pass as GROMACS")
	}
	if !strings.Contains(err.Error(), "GROMACS") {
		t.Errorf("error should explain the identity check, got: %v", err)
	}
}

func TestVersionAcceptsRealGromacsOutput(t *testing.T) {
	client := Client{
		Command: []string{"gmx"},
		Runner: stubRunner{output: strings.Join([]string{
			"                  :-) GROMACS - gmx, 2026.3-Homebrew (-:",
			"",
			"GROMACS version:     2026.3-Homebrew",
			"Precision:           mixed",
		}, "\n")},
	}
	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("real GROMACS output must be accepted: %v", err)
	}
	if version != "2026.3-Homebrew" {
		t.Errorf("version = %q, want %q", version, "2026.3-Homebrew")
	}
}

// Check must report unavailable (not merely versionless) when the identity guard
// fires, so doctor surfaces the problem instead of a silent pass.
func TestCheckReportsUnavailableForAnImpostor(t *testing.T) {
	client := Client{
		Command: []string{"true"},
		Runner:  stubRunner{},
	}
	report := client.Check(context.Background())
	if report.Available {
		t.Error("an impostor binary must not be reported as available")
	}
	if report.Error == "" {
		t.Error("the report should carry an explanation")
	}
}

// Finding #11: every gmx invocation prints a banner, executable path, data
// prefix, working dir and command line before anything useful, and all of it was
// attached to the error — burying the one line that explains the failure and
// dumping ~15 lines of boilerplate into the JSON error message.
func TestGromacsDiagnosticExtractsTheFatalError(t *testing.T) {
	output := strings.Join([]string{
		"                  :-) GROMACS - gmx check, 2026.3-Homebrew (-:",
		"",
		"Executable:   /opt/homebrew/bin/gmx",
		"Data prefix:  /opt/homebrew",
		"Working dir:  /tmp",
		"Command line:",
		"  gmx check -f bad.gro",
		"",
		"Reading frames from gro file",
		"-------------------------------------------------------",
		"Program:     gmx check, version 2026.3-Homebrew",
		"Source file: src/gromacs/fileio/groio.cpp (line 66)",
		"",
		"Fatal error:",
		"gro file does not have the number of atoms on the second line",
		"",
		"For more information and tips for troubleshooting, please check the GROMACS",
		"website at https://manual.gromacs.org/current/user-guide/run-time-errors.html",
		"-------------------------------------------------------",
	}, "\n")

	got := gromacsDiagnostic(output)
	want := "gro file does not have the number of atoms on the second line"
	if got != want {
		t.Errorf("gromacsDiagnostic() = %q, want %q", got, want)
	}
	for _, boilerplate := range []string{"Data prefix", "Working dir", "Command line", ":-) GROMACS"} {
		if strings.Contains(got, boilerplate) {
			t.Errorf("diagnostic still carries banner text %q: %q", boilerplate, got)
		}
	}
}

// Output with no fatal-error section (a crash, a missing dynamic library) must
// still surface something rather than an empty message.
func TestGromacsDiagnosticFallsBackToFullOutput(t *testing.T) {
	output := "dyld: Library not loaded: libgromacs.dylib"
	if got := gromacsDiagnostic(output); got != output {
		t.Errorf("gromacsDiagnostic() = %q, want the full output %q", got, output)
	}
}
