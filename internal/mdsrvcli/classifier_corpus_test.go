package mdsrvcli

import (
	"errors"
	"testing"
)

// The classifier decides every exit code, and its rules accreted across thirteen
// dogfood rounds. This corpus pins the classification of one message per rule
// plus the adversarial cases those rounds produced, so a future edit to the rule
// table cannot quietly move a code. It was captured from the behaviour before the
// table was restructured and verified byte-identical afterwards.
func TestClassifierCorpus(t *testing.T) {
	for _, tc := range []struct {
		want    errorCode
		message string
	}{
		{codeInvalidManifest, "schema validation failed: version"},
		{codeInvalidManifest, "decode yaml: bad"},
		{codeInvalidManifest, "decode json batch: x"},
		{codeInvalidManifest, "version is required"},
		{codeMissingInput, "metadata.id: is required"},
		{codeInvalidManifest, "job requires at least one trajectory"},
		{codeInvalidManifest, "unsupported manifest version \"mdsrv.job/v9\""},
		{codeInvalidManifest, "unsupported store version \"v9\"; expected \"v1\""},
		{codeInvalidManifest, "unsupported job version 9"},
		{codeMissingBackend, "gromacs command \"gmx\" was not found"},
		{codeMissingBackend, "gromacs command \"true\" is not usable: nope"},
		{codeMissingBackend, "python backend failed: trajectory backend unavailable: install mdtraj"},
		{codeMissingBackend, "no python interpreter found"},
		{codeMissingBackend, "python backend is unavailable"},
		{codeMissingBackend, "dial tcp 127.0.0.1:1: connection refused"},
		{codeMissingBackend, "no such host"},
		{codeMissingBackend, "network is unreachable"},
		{codeMissingBackend, "python backend failed: exit 1; GROMACS fallback failed: exit 1"},
		{codeInvalidInput, "python backend failed: GROMACS fallback failed: unsupported GROMACS fallback analysis \"sasa\""},
		{codeInvalidInput, "lstat /tmp/x: not a directory"},
		{codeInvalidInput, "/tmp/x is a directory, not a file"},
		{codeInvalidInput, "/tmp/p is not a regular file"},
		{codeInvalidInput, "--frames must be at least 2"},
		{codeInvalidInput, "--workers cannot be negative"},
		{codeInvalidInput, "--x must be greater than 0"},
		{codeInvalidInput, "--y must be positive"},
		{codeInvalidInput, "x.gro is not a valid .mdsrvx archive"},
		{codeInvalidInput, "atom index 4 out of range 1..3"},
		{codeInvalidInput, "invalid atom index selection \"!!!\""},
		{codeInvalidInput, "invalid descending range 5-1"},
		{codeInvalidInput, "unknown selection kind \"bogus\""},
		{codeInvalidInput, "cannot convert atom-index selection to \"bogus\""},
		{codeInvalidInput, "invalid id: must start with an alphanumeric character"},
		{codeInvalidInput, "unsupported analysis type 'sasa'"},
		{codeInvalidInput, "unsupported trace format \"xml\""},
		{codeInvalidInput, "unsupported shell \"csh\""},
		{codeBackendTimeout, "context deadline exceeded"},
		{codeBackendTimeout, "operation timed out"},
		{codeBackendTimeout, "command timeout exceeded"},
		{codeUnsafePath, "x escapes store root /s"},
		{codeUnsafePath, "path escapes"},
		{codeUnsafePath, "outside the source store"},
		{codeUnsafePath, "outside allowed"},
		{codeValidationFailed, "validation failed"},
		{codeValidationFailed, "verification failed"},
		{codeValidationFailed, "compatibility check failed"},
		{codeValidationFailed, "trajectory has 6 frames, exceeding max_frames=1"},
		{codeConflict, "x already exists; pass --force to overwrite"},
		{codeRenderFailed, "render failed"},
		{codeRenderFailed, "visualization failed"},
		{codeMissingInput, "dataset \"a\" not found"},
		{codeMissingInput, "no such file"},
		{codeMissingInput, "store /x does not exist"},
		{codeMissingInput, "--out is required"},
		{codeMissingInput, "contacts analysis requires selections a, b"},
		{codeMissingInput, "required flag(s) \"file\" not set"},
		{codeInternalError, "runtime error: index out of range [5] with length 3"},
		{codeInternalError, "runtime error: invalid memory address or nil pointer dereference"},
		{codeInternalError, "something entirely unrecognised happened"},
	} {
		if got := classifyErrorCode(errors.New(tc.message)); got != tc.want {
			t.Errorf("classify(%q)\n  got  %q\n  want %q\n  (claimed by rule %q)",
				tc.message, got, tc.want, classifyRuleName(tc.message))
		}
	}
}

// Ordering is part of the classifier's contract: the first matching rule wins, so
// a broad rule above a narrow one silently swallows it. Each pair below is a
// relationship that was got wrong at least once while the table was built, so
// these assert WHICH rule claims the message, not merely the resulting code.
func TestClassifierRulePriority(t *testing.T) {
	for _, tc := range []struct{ message, wantRule string }{
		// A panic must outrank every substring rule, or a real defect is reported as
		// the caller's mistake.
		{"runtime error: index out of range [5] with length 3", "runtime-panic"},
		// The version spellings must outrank the generic "unsupported " rule.
		{`unsupported manifest version "v9"`, "manifest-version"},
		{`unsupported analysis type 'sasa'`, "unsupported-choice"},
		// A fallback that REFUSED the analysis outranks the both-engines-failed
		// last resort: the backend ran, so it is not missing.
		{`python backend failed: x; GROMACS fallback failed: unsupported GROMACS fallback analysis "sasa"`, "unsupported-choice"},
		{"python backend failed: exit 1; GROMACS fallback failed: exit 1", "no-analysis-backend"},
		// A specific unavailability signal outranks the last resort too.
		{"python backend failed: trajectory backend unavailable: install mdtraj; GROMACS fallback failed: exit 1", "python-unavailable"},
	} {
		if got := classifyRuleName(tc.message); got != tc.wantRule {
			t.Errorf("%q\n  claimed by %q, want %q", tc.message, got, tc.wantRule)
		}
	}
}

// Every rule must be reachable: an unreachable rule is one an earlier rule has
// swallowed, which is exactly the failure this table exists to prevent.
func TestEveryClassificationRuleIsReachable(t *testing.T) {
	claimed := map[string]bool{}
	for _, tc := range corpusRuleProbes {
		claimed[classifyRuleName(tc)] = true
	}
	for _, rule := range classificationRules {
		if !claimed[rule.name] {
			t.Errorf("rule %q is never reached by any probe; an earlier rule may be swallowing it", rule.name)
		}
	}
}

// One probe per rule, in rule order.
var corpusRuleProbes = []string{
	"runtime error: boom",
	"decode yaml: bad",
	`unsupported manifest version "v9"`,
	`gromacs command "gmx" was not found`,
	"trajectory backend unavailable: install mdtraj",
	"connection refused",
	"/tmp/x is a directory, not a file",
	"--frames must be at least 2",
	`invalid atom index selection "!!!"`,
	"invalid id: bad",
	`unsupported trace format "xml"`,
	"operation timed out",
	"x escapes store root /s",
	"validation failed",
	"x already exists; pass --force",
	"render failed",
	"--out is required",
	"python backend failed: exit 1; GROMACS fallback failed: exit 1",
}
